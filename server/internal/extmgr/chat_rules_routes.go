package extmgr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/db"
)

// The routes of the user's per-chat rules (#5278, chat_rules.go, docs/protocol/protocol.md "Connector API"), under
// /api/v1/connectors/sent/ with the ledger they are made from:
//
//	GET    /api/v1/connectors/sent/chat-rules        the rules, newest first
//	POST   /api/v1/connectors/sent/chat-rules        {row}: the rule for the chat a ledger row went to
//	DELETE /api/v1/connectors/sent/chat-rules/{id}   drop one, by the id the list (or a ledger row) gives
//
// A rule is made on the manager from the row's own words (connector.ChatRuleFor): the request names a row and nothing
// else, so no client can say what a rule allows.
//
// Who may: the list and the drop are for who may read the ledger (mayReadLedger, sent_recall_route.go): the user's own full
// devices and the owner on this machine. Dropping a rule only makes the gate ask more, so the owner may. Making one lets
// writes through without a card from then on, so it is for what the user does by hand on their own device
// (byUsersOwnHand): not the owner's hub token, an answer-only device, a pairing token or an extension's token, whatever
// its scopes. That is not a wall against anything that can pair: a pairing token is redeemed for a full device's token
// (POST /api/v1/auth/tokens), and a full device can add a device itself, so what stands between a script and a rule is who
// can make a pairing code or a token: the devices already trusted, and the owner on this machine (docs/protocol/protocol.md
// "Authentication"). Not in extwire.RouteScopes, so an extension's token is turned away before these handlers see it, and
// the handlers check again. Each rule made or dropped is recorded in the audit (connector_chat_rule_added / _removed),
// never the text of the row it came from.

// sentRowStore reads one row of the ledger.
type sentRowStore interface {
	SentAsUserByID(ctx context.Context, id int64) (db.SentAsUser, bool, error)
}

// chatRuleView is a rule as the app shows it. id is what DELETE takes: opaque and safe in a path.
type chatRuleView struct {
	ID        string    `json:"id"`
	Connector string    `json:"connector"`
	Chat      string    `json:"chat"` // a Lark conversation's id, or owner/name#number
	At        time.Time `json:"at,omitzero"`
	Row       int64     `json:"row,omitempty"`     // the ledger row it was made from (the ledger is pruned: it may be gone)
	Summary   string    `json:"summary,omitempty"` // that row's one line
}

func viewOfChatRule(u connector.UserRule) chatRuleView {
	return chatRuleView{ID: chatRuleKey(u), Connector: u.Rule.Connector, Chat: u.Chat(), At: u.At, Row: u.Row, Summary: u.Summary}
}

// chatRuleState is what a ledger row says of the rule it could give: can: one can be made from it; exists: one is in
// force for its chat (id is that rule's, for DELETE); reason: why none can be made, one of connector.NoChat… A client
// that does not know the field loses only the button.
type chatRuleState struct {
	Can    bool   `json:"can"`
	Exists bool   `json:"exists"`
	ID     string `json:"id,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// chatRuleStates is the chat_rule of each row of a page (sentItems puts it on the row): what the row could give, and whether
// its chat has a rule already. A rule store that cannot be read says no row has one; the gate does not know them either
// then.
func (h *Manager) chatRuleStates(ctx context.Context, rows []db.SentAsUser) []chatRuleState {
	have := map[string]connector.UserRule{}
	if rules, err := h.chatRules(ctx); err == nil {
		for _, u := range rules {
			have[u.ID()] = u
		}
	}
	commands := sync.OnceValue(func() []string { return h.extensions.gateCommands(connector.Lark) }) // read for the first row that needs it
	out := make([]chatRuleState, len(rows))
	for i, row := range rows {
		rule, err := chatRuleOfRowFor(row, commands)
		var no *connector.NoChatRuleError
		switch {
		case err == nil:
			out[i].Can = true
			if u, ok := have[rule.ID()]; ok {
				out[i].Exists, out[i].ID = true, chatRuleKey(u)
			}
		case errors.As(err, &no):
			out[i].Reason = no.Code
		default:
			out[i].Reason = connector.NoChatUnknown
		}
	}
	return out
}

func (h *Manager) chatRuleRoutes(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/connectors/sent/chat-rules", func(w http.ResponseWriter, r *http.Request) {
		if !chatRuleAuth(w, r) {
			return
		}
		rules, err := h.chatRules(r.Context())
		if !chatRuleStoreOK(w, err) {
			return
		}
		out := make([]chatRuleView, 0, len(rules))
		for _, u := range slices.Backward(rules) {
			out = append(out, viewOfChatRule(u))
		}
		writeJSON(w, map[string]any{"rules": out, "most": chatRulesMost})
	})

	api.HandleFunc("POST /api/v1/connectors/sent/chat-rules", func(w http.ResponseWriter, r *http.Request) {
		if !byUsersOwnHand(r.Context()) {
			writeErr(w, http.StatusForbidden, errors.New("only one of the user's own devices with full rights makes a rule"))
			return
		}
		var body struct {
			Row int64 `json:"row"`
		}
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4<<10))
		if err != nil {
			writeBodyErr(w, err, err)
			return
		}
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields() // a client cannot hand the manager a rule, so none that tries is taken as one
		var more json.RawMessage
		if err := dec.Decode(&body); err != nil || body.Row < 1 || dec.Decode(&more) != io.EOF { // one object, nothing after it
			writeErr(w, http.StatusBadRequest, errors.New(`the body is {"row": <a ledger row's id>} and nothing else`))
			return
		}
		st, _ := h.store.(sentRowStore)
		if st == nil {
			writeErrCode(w, http.StatusServiceUnavailable, "no_database", errors.New("no database: there is no ledger"))
			return
		}
		row, found, err := st.SentAsUserByID(r.Context(), body.Row)
		switch {
		case err != nil:
			writeErr(w, http.StatusInternalServerError, err)
			return
		case !found:
			writeErrCode(w, http.StatusNotFound, "not_found", fmt.Errorf("no row %d in the ledger (it is pruned after a while)", body.Row))
			return
		}
		rule, created, err := h.addChatRule(r.Context(), row)
		var no *connector.NoChatRuleError
		switch {
		case errors.As(err, &no):
			writeCode(w, http.StatusConflict, "no_chat_rule", no.Reason, map[string]any{"reason": no.Code})
		case errors.Is(err, errChatRulesFull):
			writeErrCode(w, http.StatusConflict, "too_many_rules", err)
		case !chatRuleStoreOK(w, err):
		default:
			if created { // asking for a chat that has a rule changes nothing, so records nothing
				h.auditConnector(r.Context(), rule.Rule.Connector, auditConnectorChatRuleAdded, map[string]any{"row": body.Row, "chat": rule.Chat()})
			}
			writeJSON(w, map[string]any{"rule": viewOfChatRule(rule), "created": created})
		}
	})

	api.HandleFunc("DELETE /api/v1/connectors/sent/chat-rules/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !chatRuleAuth(w, r) {
			return
		}
		rule, had, err := h.removeChatRuleKey(r.Context(), r.PathValue("id"))
		if had { // the rule is off in the gate or gone from the store: either is recorded, a refusal or an id that is none is not
			detail := map[string]any{"chat": rule.Chat()}
			if rule.Row > 0 {
				detail["row"] = rule.Row
			}
			h.auditConnector(r.Context(), rule.Rule.Connector, auditConnectorChatRuleRemoved, detail)
		}
		switch {
		case errors.Is(err, errRuleStillStored) && !errors.Is(err, errNoDatabase):
			// The rule is off in the gate but the store did not drop it, this time or the time before: said as it is, and
			// the removal can be asked again.
			writeErrCode(w, http.StatusInternalServerError, "store_failed", err)
		case !chatRuleStoreOK(w, err):
		case !had:
			writeErrCode(w, http.StatusNotFound, "not_found", errors.New("no such rule"))
		default:
			writeJSON(w, map[string]any{"ok": true})
		}
	})
}

// chatRuleAuth lets only who may read the ledger through (mayReadLedger) and answers 403 to the rest: the list and the drop.
func chatRuleAuth(w http.ResponseWriter, r *http.Request) bool {
	if !mayReadLedger(r.Context()) {
		writeErr(w, http.StatusForbidden, errors.New("only a device with full rights changes what is asked about"))
		return false
	}
	return true
}

// chatRuleStoreOK answers for a store error (503 no_database, 500 otherwise) and says whether there was none.
func chatRuleStoreOK(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return true
	case errors.Is(err, errNoDatabase):
		writeErrCode(w, http.StatusServiceUnavailable, "no_database", errors.New("no database: the rules cannot be kept"))
	default:
		writeErr(w, http.StatusInternalServerError, err)
	}
	return false
}

package extmgr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/mail"
)

// The ledger of what Persona wrote as the user (sent_as_user, docs/connectors.md "Sent as the user", #5278). The
// connectors' write gate runs here, so this is where a write that went out is known with the conversation that asked
// and the card the user answered. Registry.SetSent tells recordSent of every gated write whose action succeeded;
// refusals, declines, failures and the user's own sends (Registry.Run) never get here.

// sentStore keeps and reads the ledger (db.RecordSentAsUser…).
type sentStore interface {
	RecordSentAsUser(ctx context.Context, s db.SentAsUser) (int64, error)
	SentAsUsers(ctx context.Context, before int64, limit int, connector string) ([]db.SentAsUser, int64, error)
	PruneSentAsUser(ctx context.Context, before time.Time) (int64, error)
}

var _ sentStore = (*db.DB)(nil)

const (
	sentLedgerDays  = 90 // how long a row is kept unless hub.json sent_ledger_days says otherwise
	sentPruneEvery  = 6 * time.Hour
	sentSummaryMost = 300 // characters of a row's one-line summary
	sentValueMost   = 200 // characters of one value in a row's target
)

type sentTraceKey struct{}

// sentTrace follows one gated call: who asked, and the card the user answered. callConnector puts it in the call's
// ctx and fills the card when it asks (approveCard); recordSent reads it back when the write has gone out.
type sentTrace struct {
	by         asker
	card       string // the id of the card the user was asked on ("" when no card was shown)
	answeredBy string // proto.Resolution.AnsweredBy of its answer: empty when the user answered in the app
}

// newConnectors is the connectors' registry for the gate's rules, with the ledger listening to what goes out.
func (h *Manager) newConnectors(rules []connector.Rule) *connector.Registry {
	r := connector.NewRegistry(rules)
	r.SetSent(h.recordSent)
	return r
}

// recordSent writes a row for a write that went out. A row that cannot be written does not undo the write, which
// already happened: it is logged, without the text.
func (h *Manager) recordSent(ctx context.Context, s connector.Sent) {
	st, _ := h.store.(sentStore)
	if st == nil {
		return
	}
	trace, _ := ctx.Value(sentTraceKey{}).(*sentTrace)
	row := sentRow(s, trace)
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if _, err := st.RecordSentAsUser(ctx, row); err != nil {
		h.logf("sent ledger: %s: %v", s.Action, err)
	}
}

// sentRow is the ledger's row for s: what the connector's arguments say of where it went and what it said, who asked
// and how the gate let it through.
func sentRow(s connector.Sent, trace *sentTrace) db.SentAsUser {
	target, brief := sentTarget(s.Action, s.Args)
	row := db.SentAsUser{At: s.At, Connector: s.Connector, Action: s.Action, Target: mustJSON(target), Text: cleanText(s.Text)}
	row.Summary = sentSummary(s, brief)
	origin := map[string]any{}
	if trace != nil {
		if trace.by.machine != "" {
			origin["machine_id"] = trace.by.machine
		}
		if trace.by.session != "" {
			origin["session_id"] = trace.by.session
		}
	}
	row.Origin = mustJSON(origin)
	through := map[string]any{"kind": s.Through.Kind}
	switch s.Through.Kind {
	case connector.ThroughRule:
		through["rule"] = s.Through.Rule
	case connector.ThroughCard:
		if trace != nil && trace.card != "" {
			through["interaction_id"] = trace.card
			if trace.answeredBy != "" {
				through["answered_by"] = trace.answeredBy
			}
		}
	}
	row.LetThrough = mustJSON(through)
	return row
}

// sentSummary is a row's one line: the first line of the card's words when the user was asked, else the action and
// where it went.
func sentSummary(s connector.Sent, brief string) string {
	line := s.Through.Summary
	if line = strings.TrimSpace(line); line != "" {
		line, _, _ = strings.Cut(line, "\n")
		// i18n-ignore: the card's own punctuation (GitHub's card ends its first line with a full-width colon)
		line = strings.TrimRight(strings.TrimSpace(line), "：: ")
	}
	if line == "" {
		line = strings.TrimSpace(s.Action + " " + brief)
	}
	return cutRunes(cleanText(line), sentSummaryMost)
}

// cleanText is s as Postgres takes text: no NUL, only valid UTF-8.
func cleanText(s string) string {
	return strings.ToValidUTF8(strings.ReplaceAll(s, "\x00", ""), "")
}

func cutRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// sentTarget is where a write went, from the arguments it was given, and the same in a few words. GitHub's comments
// and reviews: the PR or issue (repo#number); mail: the recipients and the subject; any other connector's, an
// extension's included: the fields connectors usually name their destination by (chat, reply_to…), and when none of
// them is there the names of the arguments it was given, so a row still says something.
func sentTarget(action string, args json.RawMessage) (map[string]any, string) {
	switch action {
	case "github_comment", "github_review":
		if t, brief, ok := githubTarget(action, args); ok {
			return t, brief
		}
	case mail.ToolSend:
		if t, brief, ok := mailTarget(args); ok {
			return t, brief
		}
	}
	return genericTarget(args)
}

func githubTarget(action string, args json.RawMessage) (map[string]any, string, bool) {
	var a struct {
		Repo   string `json:"repo"`
		Number int    `json:"number"`
		Chat   string `json:"chat"` // the thread page's reply: "owner/repo#12"
		Event  string `json:"event"`
	}
	if json.Unmarshal(args, &a) != nil {
		return nil, "", false
	}
	ref := a.Chat
	if a.Repo != "" && a.Number > 0 {
		ref = fmt.Sprintf("%s#%d", a.Repo, a.Number)
	}
	if ref == "" {
		return nil, "", false
	}
	target := map[string]any{"ref": cutRunes(cleanText(ref), sentValueMost)}
	brief := target["ref"].(string)
	if action == "github_review" && a.Event != "" {
		target["event"] = cutRunes(cleanText(a.Event), sentValueMost)
		brief += " " + target["event"].(string)
	}
	return target, brief, true
}

func mailTarget(args json.RawMessage) (map[string]any, string, bool) {
	var req mail.SendRequest
	if json.Unmarshal(args, &req) != nil || len(req.To) == 0 {
		return nil, "", false
	}
	emails := func(list []mail.Address) []string {
		var out []string
		for _, a := range list {
			out = append(out, cutRunes(cleanText(a.Email), sentValueMost))
		}
		return out
	}
	target := map[string]any{"to": emails(req.To), "subject": cutRunes(cleanText(req.Subject), sentValueMost)}
	if len(req.Cc) > 0 {
		target["cc"] = emails(req.Cc)
	}
	if len(req.Bcc) > 0 {
		target["bcc"] = emails(req.Bcc)
	}
	if req.AccountID != "" {
		target["account_id"] = cutRunes(cleanText(req.AccountID), sentValueMost)
	}
	brief := "to " + strings.Join(emails(req.To), ", ")
	if req.Subject != "" {
		brief += ": " + target["subject"].(string)
	}
	return target, brief, true
}

// targetFields are the arguments connectors usually name their destination by, in the order a row says them.
var targetFields = []string{"chat", "thread", "channel", "conversation", "recipient", "to", "reply_to", "subject", "repo", "number"}

func genericTarget(args json.RawMessage) (map[string]any, string) {
	var fields map[string]any
	_ = json.Unmarshal(args, &fields)
	target := map[string]any{}
	var brief []string
	for _, k := range targetFields {
		if v, ok := scalarOrStrings(fields[k]); ok {
			target[k] = v
			brief = append(brief, k+" "+fmt.Sprint(v))
		}
	}
	if len(target) == 0 {
		keys := make([]string, 0, len(fields))
		for k := range fields {
			keys = append(keys, cutRunes(cleanText(k), sentValueMost))
		}
		slices.Sort(keys)
		if len(keys) > 0 {
			target["arg_keys"] = keys
		}
		return target, ""
	}
	return target, strings.Join(brief, ", ")
}

// scalarOrStrings is v when it is a short string, a number, a boolean or a short list of strings, which is all a
// target keeps of an argument (never a nested object, which could be anything).
func scalarOrStrings(v any) (any, bool) {
	switch v := v.(type) {
	case string:
		return cutRunes(cleanText(v), sentValueMost), v != ""
	case float64, bool:
		return v, true
	case []any:
		var out []string
		for _, e := range v {
			s, ok := e.(string)
			if !ok || len(out) == 10 {
				return nil, false
			}
			out = append(out, cutRunes(cleanText(s), sentValueMost))
		}
		return out, len(out) > 0
	}
	return nil, false
}

// pruneSentLedger deletes the rows older than the days the ledger is kept (sent_ledger_days).
func (h *Manager) pruneSentLedger(ctx context.Context) {
	st, _ := h.store.(sentStore)
	days := h.cfg.SentLedgerDays
	if st == nil || days < 0 {
		return
	}
	if days == 0 {
		days = sentLedgerDays
	}
	n, err := st.PruneSentAsUser(ctx, time.Now().AddDate(0, 0, -days))
	switch {
	case err != nil && ctx.Err() == nil:
		h.logf("sent ledger: pruning: %v", err)
	case n > 0:
		h.logf("sent ledger: %d rows older than %d days deleted", n, days)
	}
}

// sentLedgerLoop prunes the ledger at the start and every few hours after.
func (h *Manager) sentLedgerLoop(ctx context.Context) {
	if _, ok := h.store.(sentStore); !ok || h.cfg.SentLedgerDays < 0 {
		return
	}
	h.pruneSentLedger(ctx)
	t := time.NewTicker(sentPruneEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			h.pruneSentLedger(ctx)
		}
	}
}

// sentRoutes: GET /api/v1/connectors/sent?before=&limit=&connector=, the ledger newest first, a page at a time
// (docs/protocol.md "Connector API"). Read by one of the user's own full devices or the owner on this machine, never
// by an answer-only device, a pairing token (a script or an AI the user paired) or an extension's token: it holds the
// text of what was written as the user.
func (h *Manager) sentRoutes(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/connectors/sent", func(w http.ResponseWriter, r *http.Request) {
		if p := principalOf(r.Context()); !(byOwnDevice(r.Context()) || p.Owner) || p.Extension != "" {
			writeErr(w, http.StatusForbidden, errors.New("only a device with full rights reads what was sent as the user"))
			return
		}
		st, _ := h.store.(sentStore)
		if st == nil {
			writeErrCode(w, http.StatusServiceUnavailable, "no_database", errors.New("no database: there is no ledger"))
			return
		}
		q := r.URL.Query()
		limit := db.SentAsUserDefault
		if v := q.Get("limit"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 {
				writeErr(w, http.StatusBadRequest, fmt.Errorf("limit %q: a number from 1", v))
				return
			}
			limit = min(n, db.SentAsUserMost)
		}
		var before int64
		if v := q.Get("before"); v != "" {
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil || n < 1 {
				writeErr(w, http.StatusBadRequest, fmt.Errorf("before %q: a row id, as next gave it", v))
				return
			}
			before = n
		}
		rows, next, err := st.SentAsUsers(r.Context(), before, limit, q.Get("connector"))
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		if rows == nil {
			rows = []db.SentAsUser{}
		}
		var nextOut any // null on the last page
		if next > 0 {
			nextOut = next
		}
		writeJSON(w, map[string]any{"items": rows, "next": nextOut})
	})
}

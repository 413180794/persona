package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nwverse/persona/sdk/go/wire"
	"github.com/nwverse/persona/server/internal/apierr"
	"github.com/nwverse/persona/server/internal/db"
)

type viewActionStore interface {
	viewStore
	AssistantCard(context.Context, string, string) (*db.AssistantViewRow, error)
	BeginAssistantViewAction(context.Context, db.AssistantViewActionRow, int64) (*db.AssistantViewActionRow, bool, error)
	AssistantViewAction(context.Context, string, string) (*db.AssistantViewActionRow, error)
	AssistantViewActions(context.Context, string) ([]db.AssistantViewActionRow, error)
	ClaimAssistantViewDelivery(context.Context, string, string, time.Time) (*db.AssistantViewActionRow, error)
	SettleAssistantViewDelivery(context.Context, string, string, time.Time, string, string) error
	PendingAssistantViewActions(context.Context, int) ([]db.AssistantViewActionRow, error)
}

type viewActionInput struct {
	Action      string            `json:"action"`
	Page        string            `json:"page,omitempty"`
	Values      map[string]string `json:"values,omitempty"`
	ClientMsgID string            `json:"client_msg_id"`
	Confirmed   bool              `json:"confirmed,omitempty"`
}

func wireViewAction(a db.AssistantViewActionRow) wire.ViewAction {
	values := map[string]string{}
	_ = json.Unmarshal(a.Values, &values)
	return wire.ViewAction{Key: a.Key, Action: a.Action, Page: a.Page, Values: values, ClientMsgID: a.ClientMsgID, Status: a.Status, At: a.At, Error: clip(a.Error, 2000), UpdatedAt: a.UpdatedAt, Revision: a.Revision}
}

func (s *Service) viewActionRoutes() {
	s.handle("POST /api/v1/assistant/conversations/{id}/views/{key}/actions", func(w http.ResponseWriter, r *http.Request) {
		if !fullRole(r) {
			apierr.Write(w, http.StatusForbidden, errors.New("read-only role cannot reply from a card"))
			return
		}
		if !s.hello(r.Context()).PersonaView {
			apierr.Write(w, http.StatusForbidden, errors.New("assistant views are disabled"))
			return
		}
		var in viewActionInput
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&in); err != nil {
			apierr.Write(w, http.StatusBadRequest, err)
			return
		}
		if in.Action == "" || in.ClientMsgID == "" || len(in.ClientMsgID) > 128 {
			apierr.Write(w, http.StatusBadRequest, errors.New("action and client_msg_id are required"))
			return
		}
		st, ok := s.store().(viewActionStore)
		if !ok {
			apierr.Write(w, http.StatusServiceUnavailable, errors.New("durable card actions are unavailable"))
			return
		}
		conversation, key := r.PathValue("id"), r.PathValue("key")
		old, err := st.AssistantViewAction(r.Context(), conversation, key)
		if err != nil {
			apierr.Write(w, http.StatusServiceUnavailable, err)
			return
		}
		if old != nil {
			writeViewAction(w, *old, in)
			return
		}
		row, err := st.AssistantCard(r.Context(), conversation, key)
		if errors.Is(err, db.ErrAssistantViewNotFound) {
			apierr.Write(w, http.StatusNotFound, err)
			return
		}
		if err != nil {
			apierr.Write(w, http.StatusServiceUnavailable, err)
			return
		}
		v, err := wire.ParseAssistantView(row.View)
		if err != nil {
			apierr.Write(w, http.StatusConflict, err)
			return
		}
		text, values, err := viewReplyMessage(v, key, in)
		if err != nil {
			apierr.Write(w, http.StatusBadRequest, err)
			return
		}
		at := time.Now().UTC()
		a := db.AssistantViewActionRow{Conversation: conversation, Key: key, Action: in.Action, Page: in.Page, Values: values, ClientMsgID: in.ClientMsgID, MessageID: "view-reply-" + randomHex(16), MachineID: row.MachineID, Text: text, At: at}
		stored, first, err := st.BeginAssistantViewAction(r.Context(), a, row.Version)
		if errors.Is(err, db.ErrAssistantViewVersion) {
			apierr.Write(w, http.StatusConflict, errors.New("card changed; reload before submitting"))
			return
		}
		if err != nil {
			apierr.Write(w, http.StatusServiceUnavailable, err)
			return
		}
		if first {
			s.viewActionChanged(r.Context(), *stored)
			go s.deliverViewAction(context.Background(), conversation, key)
		}
		writeViewAction(w, *stored, in)
	})
}

func writeViewAction(w http.ResponseWriter, a db.AssistantViewActionRow, in viewActionInput) {
	values, _ := json.Marshal(in.Values)
	if in.Values == nil {
		values = []byte(`{}`)
	}
	var old, current any
	_ = json.Unmarshal(a.Values, &old)
	_ = json.Unmarshal(values, &current)
	same := a.ClientMsgID == in.ClientMsgID && a.Action == in.Action && a.Page == in.Page
	oldJSON, _ := json.Marshal(old)
	currentJSON, _ := json.Marshal(current)
	if !same || string(oldJSON) != string(currentJSON) {
		body := apierr.Body(http.StatusConflict, "already_answered", errors.New("card already answered"))
		body["action"] = wireViewAction(a)
		apierr.WriteBody(w, http.StatusConflict, body)
		return
	}
	apierr.WriteJSON(w, map[string]any{"action": wireViewAction(a)})
}

func viewReplyMessage(root *wire.View, key string, in viewActionInput) (string, json.RawMessage, error) {
	page := root
	if in.Page != "" {
		detail, ok := root.Pages[in.Page]
		if !ok {
			return "", nil, errors.New("no such card page")
		}
		page = &detail
	}
	var selected *wire.Action
	for _, a := range wire.Actions(page) {
		if a.ID == in.Action {
			selected = a
			break
		}
	}
	if selected == nil || selected.Reply == "" {
		return "", nil, errors.New("only a declared reply action can be submitted")
	}
	if selected.Confirm != "" && !in.Confirmed {
		return "", nil, errors.New("confirm this action before submitting")
	}
	fields := map[string]wire.Component{}
	for _, section := range page.Sections {
		for _, e := range section.Items {
			if selected.Submit && e.Type == "field" || e.Type == "segmented" && e.Action != nil && e.Action.ID == selected.ID {
				fields[e.Name] = e
			}
		}
	}
	if page.Search != nil && (selected.Submit || selected.ID == page.Search.Action.ID || page.Filter != nil && selected.ID == page.Filter.Action.ID) {
		fields["q"] = wire.Component{Type: "field", Kind: "text", Label: "搜索"}
	}
	if page.Filter != nil && (selected.Submit || selected.ID == page.Filter.Action.ID) {
		f := page.Filter
		fields[f.Name] = wire.Component{Type: "field", Kind: "choice", Label: f.Title, Choices: f.Choices}
	}
	for name, value := range in.Values {
		field, ok := fields[name]
		if !ok {
			return "", nil, fmt.Errorf("values.%s: field is not allowed by this action", name)
		}
		if utf8.RuneCountInString(value) > 2000 {
			return "", nil, fmt.Errorf("values.%s: at most 2000 characters", name)
		}
		switch field.Kind {
		case "secret":
			return "", nil, errors.New("card actions must not collect secrets")
		case "toggle":
			if value != "true" && value != "false" {
				return "", nil, fmt.Errorf("values.%s: expected true or false", name)
			}
		case "number":
			n, err := strconv.ParseFloat(value, 64)
			if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
				return "", nil, fmt.Errorf("values.%s: expected a finite number", name)
			}
		case "choice":
			if !slices.ContainsFunc(field.Choices, func(c wire.Choice) bool { return c.Value == value }) {
				return "", nil, fmt.Errorf("values.%s: not one of the declared choices", name)
			}
		}
		if field.Type == "segmented" && !slices.ContainsFunc(field.Choices, func(c wire.Choice) bool { return c.Value == value }) {
			return "", nil, fmt.Errorf("values.%s: not one of the declared choices", name)
		}
	}
	if selected.Submit {
		for name := range fields {
			if _, ok := in.Values[name]; !ok {
				return "", nil, fmt.Errorf("values.%s: submit requires every field on this page", name)
			}
		}
	}
	values := in.Values
	if values == nil {
		values = map[string]string{}
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return "", nil, err
	}
	payload, err := json.Marshal(map[string]any{"values": values})
	if err != nil {
		return "", nil, err
	}
	var text strings.Builder
	text.WriteString(selected.Reply)
	for _, name := range sortedValueNames(values) {
		label := fields[name].Label
		if label == "" {
			label = name
		}
		text.WriteString("\n" + label + "：" + values[name])
	}
	text.WriteString("\n<persona-action view=\"" + html.EscapeString(key) + "\" action=\"" + html.EscapeString(in.Action) + "\">" + string(payload) + "</persona-action>")
	return text.String(), raw, nil
}

func sortedValueNames(values map[string]string) []string {
	out := make([]string, 0, len(values))
	for key := range values {
		out = append(out, key)
	}
	slices.Sort(out)
	return out
}

func (s *Service) viewActionChanged(ctx context.Context, a db.AssistantViewActionRow) {
	event := map[string]any{"type": "assistant_view_action", "conversation": a.Conversation, "action": wireViewAction(a)}
	s.event(ctx, event)
}

func (s *Service) deliverViewAction(parent context.Context, conversation, key string) {
	ctx, cancel := context.WithTimeout(parent, 3*time.Minute)
	defer cancel()
	st, ok := s.store().(viewActionStore)
	if !ok {
		return
	}
	a, err := st.ClaimAssistantViewDelivery(ctx, conversation, key, time.Now().Add(10*time.Minute))
	if err != nil || a == nil {
		return
	}
	status, reason := "sent", ""
	if session, known := s.session(ctx, a.MachineID, a.Conversation); !known || !session.Assistant {
		status, reason = "pending", "等待助手所在机器重新上线"
	} else {
		params := map[string]any{"session_id": a.Conversation, "client_msg_id": a.MessageID, "text": a.Text, "resume_only": true}
		err = s.sendModel(ctx, a.MachineID, a.Conversation, params)
		if err == nil {
			s.UserSpoke(ctx, SessionRef{MachineID: a.MachineID, SessionID: a.Conversation})
			err = s.sendOnce(ctx, a.MachineID, params)
		}
		if err != nil {
			status, reason = "failed", err.Error()
			if errors.Is(err, apierr.ErrOffline) || errors.Is(err, ErrNoDevice) {
				status = "pending"
			}
		}
	}
	finish, cancelFinish := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancelFinish()
	if err := st.SettleAssistantViewDelivery(finish, conversation, key, a.LeaseUntil, status, reason); err != nil {
		s.logf("view action settle failed: %v", err)
		return
	}
	updated, err := st.AssistantViewAction(finish, conversation, key)
	if err == nil && updated != nil {
		s.viewActionChanged(finish, *updated)
	}
}

func (s *Service) viewActionLoop(ctx context.Context) {
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		if s.hello(ctx).PersonaView && !s.hello(ctx).Assistant.Standby {
			if st, ok := s.store().(viewActionStore); ok {
				pending, err := st.PendingAssistantViewActions(ctx, 20)
				if err == nil {
					for _, a := range pending {
						s.deliverViewAction(ctx, a.Conversation, a.Key)
					}
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) conversationViewActions(ctx context.Context, conversation string) (map[string]wire.ViewAction, error) {
	out := map[string]wire.ViewAction{}
	st, ok := s.store().(viewActionStore)
	if !ok {
		return out, nil
	}
	rows, err := st.AssistantViewActions(ctx, conversation)
	if err != nil {
		return nil, err
	}
	for _, a := range rows {
		out[a.Key] = wireViewAction(a)
	}
	return out, nil
}

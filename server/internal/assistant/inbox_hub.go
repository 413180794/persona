package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/nwverse/persona/server/internal/ai"
	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/rpc"
)

// What the inbox asks of the hub: its settings, the sources' (a mail's text, a reply, a connector's group tools…),
// the knowledge base's, and the models. The models are reached in this file only (inboxModelJSON,
// inboxModelComplete): they are the hub's model configuration today (app.call models.*) and the manager's own later.

// The model configuration's purposes the inbox uses.
const (
	purposeTriage = "inbox.triage" // model 1: triage and summary, relating batches to events
	purposeJudge  = "inbox.judge"  // model 2: which 需要你 items the AI 分身 takes
	purposeDraft  = "inbox.draft"  // AI 草拟回复
	purposeAgent  = "inbox.agent"  // model 3: the AI 分身's own conversation (a launcher)
	// purposeTranslate translates mail and short texts (translation, inbox_translate.go).
	purposeTranslate = "translate"
)

// inboxModelJSON asks purpose's model for a JSON object into out; maxTokens is the room the answer needs (0: the
// model's default). ctx must have a deadline: the hub gives the call no longer (app.call's timeout_ms), so a call
// given up on here is not left running there.
func (s *Service) inboxModelJSON(ctx context.Context, purpose, system, input string, maxTokens int, out any) error {
	var raw json.RawMessage
	params := map[string]any{"purpose": purpose, "system": system, "input": input}
	if maxTokens > 0 {
		params["max_tokens"] = maxTokens
	}
	if err := s.appCall(ctx, AppModels, ModelsJSON, params, &raw); err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

// inboxModelComplete asks purpose's model for text (ctx with a deadline, like inboxModelJSON).
func (s *Service) inboxModelComplete(ctx context.Context, purpose, system, input string, maxTokens int) (string, error) {
	var res struct {
		Text string `json:"text"`
	}
	err := s.appCall(ctx, AppModels, ModelsComplete, map[string]any{"purpose": purpose, "system": system, "input": input,
		"max_tokens": maxTokens}, &res)
	return res.Text, err
}

// modelUnconfigured: nothing is set up for the purpose (the hub's ai.IsNotConfigured).
func modelUnconfigured(err error) bool {
	if ai.IsNotConfigured(err) {
		return true
	}
	var re *rpc.Error
	return errors.As(err, &re) && re.Code == assistantwire.CodeModelUnconfigured
}

// modelPaused: the model's connection is held; quota: its quota is used up (rather than busy).
func modelPaused(err error) (paused, quota bool) {
	var p *ai.PausedError
	if !errors.As(err, &p) {
		return false, false
	}
	return true, p.Reason == ai.ReasonQuota
}

// The hub's inbox methods (the sources'), by their wire names.
const (
	inboxKick          = assistantwire.InboxKick
	inboxReplyMail     = assistantwire.InboxReplyMail
	inboxReplyMethod   = assistantwire.InboxReply
	inboxSettle        = assistantwire.InboxSettle
	inboxGroupTool     = assistantwire.InboxGroupTool
	inboxGroupFallback = assistantwire.InboxGroupFallback
	inboxGroupAppend   = assistantwire.InboxGroupAppend
)

// inboxConfigFor is how long the hub's inbox settings are taken as they were read.
const inboxConfigFor = 5 * time.Second

// inboxConfig is the hub's inbox settings (InboxConfigMethod), read again after a few seconds; while the hub can't
// say, the last ones (none: the zero settings).
func (s *Service) inboxConfig(ctx context.Context) assistantwire.InboxConfig {
	b := &s.inbox
	b.cfgMu.Lock()
	defer b.cfgMu.Unlock()
	if !b.cfgAt.IsZero() && time.Since(b.cfgAt) < inboxConfigFor {
		return b.cfg
	}
	var cfg assistantwire.InboxConfig
	if err := s.appCall(ctx, AppInbox, assistantwire.InboxConfigMethod, map[string]any{}, &cfg); err != nil {
		s.logf("inbox: the hub's settings: %v", err)
		return b.cfg
	}
	b.cfg, b.cfgAt = cfg, time.Now()
	return cfg
}

// inboxMailBody is one mail read in full by the hub.
func (s *Service) inboxMailBody(ctx context.Context, ref assistantwire.InboxMailRef) (assistantwire.InboxMailMessage, error) {
	var m assistantwire.InboxMailMessage
	err := s.appCall(ctx, AppInbox, assistantwire.InboxMailBody, ref, &m)
	return m, err
}

// connectorReply sends a reply (or a document's status moves) through connector source at the hub, once per sendID
// ("": no such guard; inboxSent).
func (s *Service) connectorReply(ctx context.Context, source string, r connector.InboxReply, sendID string) error {
	var sent assistantwire.InboxSent
	err := s.appCall(ctx, AppInbox, inboxReplyMethod, map[string]any{"source": source, "reply": r, "send_id": sendID}, &sent)
	s.inboxSent(sent, sendID)
	return err
}

// inboxSent notes a reply the hub had sent before under the same send id (a retry after the answer was lost): it is
// taken as sent.
func (s *Service) inboxSent(sent assistantwire.InboxSent, sendID string) {
	if sent.Already {
		s.logf("inbox: reply %s had gone out before: not sent again", sendID)
	}
}

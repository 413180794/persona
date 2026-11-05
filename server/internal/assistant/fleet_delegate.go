package assistant

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/apierr"
	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
)

// An agent's question goes to the assistant before the user (hub.json agents.delegate, off by default;
// docs/assistant.md「提问先由助手代答」): the card shows on the apps at once, marked delegating, but nothing is pushed.
// The assistant answers what is the agent's own call (a technical option, a default, whether to run again, which
// branch) the way interaction_answer does, with why (delegate.go decides); it leaves red lines (irreversible, or seen
// by other people) and the goal itself to the user, with the context it filled in. When it does not decide in time,
// the user is asked as before: a question never waits on it. Permissions, plans, terminal dialogs and forms are never
// delegated.

// delegable: ix is a question the assistant decides on first.
func (s *Service) delegable(ix proto.Interaction) bool {
	if ix.Kind != proto.IxQuestion || len(ix.Questions) == 0 || len(ix.Fields) > 0 || ix.ToolName == "ask_user" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return s.hello(ctx).Agents.Delegate.On()
}

// delegate shows ix as delegating and has the assistant decide on it, in the background.
func (s *Service) delegate(ix proto.Interaction) {
	s.fleet.mu.Lock()
	if p := s.fleet.interactions[ix.ID]; p != nil {
		p.ix.Delegating = true
	}
	s.fleet.mu.Unlock()
	shown := ix
	shown.Delegating = true
	s.show(shown)
	go s.delegateNow(ix)
}

// delegateNow waits for the assistant's call on ix (agents.delegate's timeout at most), then answers it or tells the
// user. A call that comes too late is dropped.
func (s *Service) delegateNow(ix proto.Interaction) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	timeout := s.hello(ctx).Agents.Delegate.Timeout()
	cancel()
	ctx, cancel = context.WithTimeout(context.Background(), timeout)
	defer cancel()
	type decided struct {
		res DelegateResult
		err error
	}
	got := make(chan decided, 1)
	go func() {
		res, err := s.delegateCall(ctx, DelegateParams{Interaction: s.withTask(ix)})
		got <- decided{res, err}
	}()
	var d decided
	select {
	case d = <-got:
	case <-ctx.Done():
		d.err = ctx.Err()
	}
	switch {
	case d.err != nil:
		s.logf("delegate %s: %v; asking the user", ix.ID, d.err)
	case d.res.Answer:
		ok, err := s.answerQuestion(ix.MachineID, ix.SessionID, ix.ID, d.res.Answers, d.res.Reason)
		if ok || err == nil {
			return // answered, or no longer pending (the user answered it meanwhile)
		}
		s.logf("delegate %s: answer: %v; asking the user", ix.ID, err)
	default:
		ix.Delegate = d.res.Ask
	}
	s.escalate(ix)
}

// delegateCall is Delegate, or what a test has in its place.
func (s *Service) delegateCall(ctx context.Context, p DelegateParams) (DelegateResult, error) {
	if f := s.delegateHook; f != nil {
		return f(ctx, p)
	}
	return s.Delegate(ctx, p)
}

// escalate tells the user of ix, still pending, with the assistant's context when it gave some.
func (s *Service) escalate(ix proto.Interaction) {
	s.fleet.mu.Lock()
	p := s.fleet.interactions[ix.ID]
	still := p != nil && !p.claimed
	if still {
		p.ix.Delegating, p.ix.Delegate = false, ix.Delegate
	}
	s.fleet.mu.Unlock()
	if still {
		s.tell(ix)
	}
}

// ---- 改答: the user changes an answer the assistant gave ----

// interactionRecorder is the store's one interaction with its outcome (db.DB).
type interactionRecorder interface {
	InteractionRecord(ctx context.Context, id string) (*db.InteractionRecord, error)
}

var (
	errNotDelegated = errors.New("only a question the assistant answered can be answered again")
	errNoAnswers    = errors.New("answers is required: an object of question → answer")
)

// serveReanswer is POST /api/v1/interactions/{id}/reanswer {answers}: a question the assistant answered for the user
// gets the user's answer instead. The agent went on with the first one already, so the new one goes to it as a
// message; the user's answer is kept as the task's (its record, its decisions).
func (s *Service) serveReanswer(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Answers map[string]string `json:"answers"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body); err != nil {
		apierr.Write(w, http.StatusBadRequest, err)
		return
	}
	status, err := s.reanswer(r.Context(), r.PathValue("id"), body.Answers)
	if err != nil {
		apierr.Write(w, status, err)
		return
	}
	apierr.WriteJSON(w, map[string]any{"ok": true})
}

// reanswer sends the user's answers to a question the assistant answered (id) to its agent, once; the HTTP status
// goes with an error.
func (s *Service) reanswer(ctx context.Context, id string, answers map[string]string) (int, error) {
	st, ok := s.store().(interactionRecorder)
	if !ok {
		return http.StatusServiceUnavailable, errors.New("no interaction history")
	}
	rec, err := st.InteractionRecord(ctx, id)
	if err != nil {
		return http.StatusInternalServerError, err
	}
	if rec == nil || rec.Status != "resolved" || rec.Resolution == nil || rec.Resolution.AnsweredBy != proto.AnsweredByAssistant {
		return http.StatusConflict, errNotDelegated
	}
	ix := rec.Interaction
	var items []prompts.AgentReanswerItem
	kept := map[string]string{}
	for _, q := range ix.Questions {
		a := strings.TrimSpace(answers[q.Question])
		if a == "" {
			continue
		}
		kept[q.Question] = a
		items = append(items, prompts.AgentReanswerItem{Question: q.Question, Before: rec.Resolution.Answers[q.Question], Answer: a})
	}
	if len(items) == 0 {
		return http.StatusBadRequest, errNoAnswers
	}
	text := prompts.RenderAgentReanswer(prompts.AgentReanswer{Items: items})
	sum := sha256.Sum256([]byte(text))
	key := id + "_" + hex.EncodeToString(sum[:6])
	s.fleet.mu.Lock()
	sent := s.fleet.reanswered[key]
	s.fleet.mu.Unlock()
	if sent {
		return 0, nil // the same answer again: it went already
	}
	if err := s.sendToSession(ctx, ix.MachineID, ix.SessionID, text); err != nil {
		return http.StatusBadGateway, err
	}
	s.fleet.mu.Lock()
	if s.fleet.reanswered == nil {
		s.fleet.reanswered = map[string]bool{}
	}
	s.fleet.reanswered[key] = true
	s.fleet.mu.Unlock()
	res := proto.Resolution{Answers: kept}
	s.event(ctx, map[string]any{"type": "interaction_reanswered", "id": id, "resolution": res})
	go s.InteractionResolved(context.WithoutCancel(ctx), InteractionResolved{Interaction: ix, Resolution: res, Status: "resolved"})
	return 0, nil
}

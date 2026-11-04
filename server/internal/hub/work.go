package hub

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/proto"
)

// Where an agent stands in its work (docs/agent-work.md, #1686). When a turn ends the hub triages it — rules first,
// then a cheap model reading the last reply — into done or blocked on a named precondition, and acts on what it can:
// 继续 after a transient model error (with backoff and a cap), a wake when what it waits for should be over, a card
// for what the user is to decide (the answer goes back to the agent). Everything else (pushing code, deploying,
// messaging people, touching someone else's MR) is only ever a card. Each action checks, the moment before, that the
// session is still stopped on the same turn with nothing pending or queued: whatever the user did meanwhile wins.

// TriageConfig is hub.json agents.triage. Unset switches are on.
type TriageConfig struct {
	Enabled      *bool `json:"enabled,omitempty"`
	AutoContinue *bool `json:"auto_continue,omitempty"` // 继续 after a transient model error
	Wake         *bool `json:"wake,omitempty"`          // wake agents whose wait should be over
	Decide       *bool `json:"decide,omitempty"`        // cards for what the user is to decide
	// MaxRetries: 继续 sent for one run of transient errors before asking the user. Default 3.
	MaxRetries int `json:"max_retries,omitempty"`
	// MaxWakes: wakes for one wait before asking the user. Default 6.
	MaxWakes int `json:"max_wakes,omitempty"`
	// SettleSeconds: how long a stopped session stays stopped before its turn is triaged. Default 15.
	SettleSeconds int `json:"settle_seconds,omitempty"`
}

// AgentsConfig is hub.json "agents".
type AgentsConfig struct {
	Triage TriageConfig `json:"triage,omitzero"`
}

func on(b *bool) bool { return b == nil || *b }

func (c TriageConfig) maxRetries() int { return cmp0(c.MaxRetries, 3) }
func (c TriageConfig) maxWakes() int   { return cmp0(c.MaxWakes, 6) }
func (c TriageConfig) settle() time.Duration {
	return secondsOr(c.SettleSeconds, 15)
}

func cmp0(n, def int) int {
	if n > 0 {
		return n
	}
	return def
}

// retryBackoff is how long after a transient error each 继续 goes (the last repeats).
var retryBackoff = []time.Duration{30 * time.Second, 2 * time.Minute, 8 * time.Minute}

// workEvery is how often the hub looks for due wakes, cards to restore and turns to triage.
var workEvery = 15 * time.Second

const (
	workCardTool  = "persona_triage" // the card's tool_name: who asks is Persona, about the agent's turn
	workCardTTL   = 24 * time.Hour
	workKeep      = 30 * 24 * time.Hour
	workBackfill  = 24 * time.Hour // turns that ended this long ago still get triaged when the hub comes up
	workSampleGap = 5 * time.Minute
)

// continueText is what goes on a turn the model service cut off: what the chat's 重试 on an API error card sends.
const continueText = "继续"

// Card answers the hub acts on itself instead of handing them to the agent.
const (
	answerSkip    = "先不管"
	answerRetry   = "再试一次"
	answerHandled = "已处理，继续"
	answerAuthed  = "已授权，继续"
	answerWait    = "再等等"
	answerStop    = "不等了"
	answerGo      = "等好了就去做"
	answerHold    = "先别做"
)

type workStore interface {
	PutSessionWork(ctx context.Context, w db.SessionWork) error
	SessionWorks(ctx context.Context, since time.Time) ([]db.SessionWork, error)
	PruneSessionWork(ctx context.Context, before time.Time) error
	AddWorkEvent(ctx context.Context, e db.WorkEvent) error
	WorkEvents(ctx context.Context, since time.Time) ([]db.WorkEvent, error)
}

// workBook is the hub's copy of session_work, plus what only matters while it runs.
type workBook struct {
	mu       sync.Mutex
	entries  map[[2]string]*db.SessionWork
	loaded   bool
	triaging map[[2]string]bool
	tried    map[[2]string]triedTurn // the turn a triage was last attempted on, and when
	sent     map[[2]string]time.Time // when the hub itself last sent the session something
	seen     map[[2]string]time.Time // the turn whose first look was recorded
	approved map[[2]string]string    // a risky next step the user said yes to (its wait.then)
	sampleAt time.Time
	pruneAt  time.Time
	// testEvents collects the events when there is no store (tests).
	testEvents []db.WorkEvent
}

func (b *workBook) init() {
	if b.entries == nil {
		b.entries = map[[2]string]*db.SessionWork{}
		b.triaging = map[[2]string]bool{}
		b.tried = map[[2]string]triedTurn{}
		b.sent = map[[2]string]time.Time{}
		b.seen = map[[2]string]time.Time{}
		b.approved = map[[2]string]string{}
	}
}

// triedTurn is a turn a triage was attempted on: a failed one is tried again after workRetryTriage, not every tick.
type triedTurn struct{ turn, at time.Time }

var workRetryTriage = 10 * time.Minute

func (h *Hub) triageCfg() TriageConfig {
	if h.cfg == nil {
		return TriageConfig{}
	}
	return h.cfg.Agents.Triage
}

func (h *Hub) workOn() bool { return on(h.triageCfg().Enabled) }

func (h *Hub) workDB() workStore {
	s, _ := h.store.(workStore)
	return s
}

// sameTurn: s is still on the turn that ended at turnAt (a stopped transcript does not change).
func sameTurn(s proto.Session, turnAt time.Time) bool {
	d := s.UpdatedAt.Sub(turnAt)
	return d > -2*time.Second && d < 2*time.Second
}

// loadWork reads session_work back (once): the hub's memory goes with every deploy.
func (h *Hub) loadWork(ctx context.Context) {
	h.work.mu.Lock()
	h.work.init()
	if h.work.loaded {
		h.work.mu.Unlock()
		return
	}
	h.work.mu.Unlock()
	var rows []db.SessionWork
	if st := h.workDB(); st != nil {
		var err error
		if rows, err = st.SessionWorks(ctx, time.Now().Add(-workBackfill*2)); err != nil {
			h.logf("work: load: %v", err)
			return // tried again next tick
		}
	}
	h.work.mu.Lock()
	defer h.work.mu.Unlock()
	for i := range rows {
		r := rows[i]
		k := [2]string{r.MachineID, r.SessionID}
		if _, ok := h.work.entries[k]; !ok {
			h.work.entries[k] = &r
		}
	}
	h.work.loaded = true
}

func (h *Hub) saveWork(e db.SessionWork) {
	st := h.workDB()
	if st == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := st.PutSessionWork(ctx, e); err != nil {
		h.logf("work: save %s/%s: %v", e.MachineID, e.SessionID, err)
	}
}

func (h *Hub) workEvent(machine, session, kind string, detail map[string]any) {
	e := db.WorkEvent{At: time.Now().UTC(), MachineID: machine, SessionID: session, Kind: kind, Detail: detail}
	st := h.workDB()
	if st == nil {
		h.work.mu.Lock()
		h.work.testEvents = append(h.work.testEvents, e)
		h.work.mu.Unlock()
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := st.AddWorkEvent(ctx, e); err != nil {
			h.logf("work: event: %v", err)
		}
	}()
}

// entry is a copy of a session's stored work state.
func (h *Hub) workEntry(k [2]string) (db.SessionWork, bool) {
	h.work.mu.Lock()
	defer h.work.mu.Unlock()
	h.work.init()
	e, ok := h.work.entries[k]
	if !ok {
		return db.SessionWork{}, false
	}
	return *e, true
}

// putWork stores e (memory and database).
func (h *Hub) putWork(e db.SessionWork) {
	e.UpdatedAt = time.Now().UTC()
	h.work.mu.Lock()
	h.work.init()
	c := e
	h.work.entries[[2]string{e.MachineID, e.SessionID}] = &c
	h.work.mu.Unlock()
	h.saveWork(e)
}

// ---- what the lists show ----

// workOf is s's work state for the lists (s decorated but for Work; cards are the pending interactions of the
// machine, by session).
func (h *Hub) workOf(machine string, s proto.Session, asking map[string]proto.Interaction) *proto.WorkState {
	if s.Assistant || s.Status == proto.StatusOffline {
		return nil
	}
	if s.Status == proto.StatusWorking {
		return &proto.WorkState{Phase: proto.WorkWorking}
	}
	e, ok := h.workEntry([2]string{machine, s.SessionID})
	ix, asks := asking[s.SessionID]
	if asks && ix.ToolName != workCardTool { // the agent asks itself: that is what it waits for
		return &proto.WorkState{Phase: proto.WorkBlocked, On: proto.WaitUser, Line: interactionLine(ix)}
	}
	if s.Status == proto.StatusBlocked && !asks {
		return &proto.WorkState{Phase: proto.WorkBlocked, On: proto.WaitUser, Line: "终端等你处理"}
	}
	if !ok || e.Work.Phase == "" || !sameTurn(s, e.TurnAt) {
		return nil
	}
	w := e.Work
	if w.InteractionID != "" && (!asks || ix.ID != w.InteractionID) {
		w.InteractionID = "" // answered, withdrawn, or not restored yet
	}
	return &w
}

// workState is the unified state once the work state is known: a turn that is done, or blocked on something the hub
// waits out for it, no longer counts as 完成未看 (docs/agent-work.md「模型」). A wait nobody will end (the hub's wakes
// off, or dropped because the user went near it) stays 完成未看.
func workState(state string, w *proto.WorkState) string {
	if state != proto.StateUnread || w == nil {
		return state
	}
	if w.Phase == proto.WorkDone || hubWaits(w) {
		return proto.StateIdle
	}
	return state
}

// hubWaits: w is a wait that ends without the user — the hub wakes it (wake_at), or it wakes itself (a background
// task, a /loop: on external with nothing named to wait for).
func hubWaits(w *proto.WorkState) bool {
	if w.Phase != proto.WorkBlocked || w.On == proto.WaitUser {
		return false
	}
	return w.WakeAt != nil || w.On == proto.WaitExternal && w.Wait == "" && w.Agent == nil
}

// interactionLine is what a pending interaction waits for, in a line.
func interactionLine(ix proto.Interaction) string {
	switch ix.Kind {
	case proto.IxQuestion:
		if len(ix.Questions) > 0 {
			return clip(oneLine(ix.Questions[0].Question), 60)
		}
		return "在问你"
	case proto.IxPlan:
		return "计划等你审批"
	case proto.IxPermission:
		if ix.Summary != "" {
			return clip(oneLine("等你授权："+ix.Summary), 60)
		}
		return "等你授权"
	case proto.IxElicitation:
		return "MCP 请求等你处理"
	}
	return "终端等你处理"
}

// ---- turns ----

// workStarted: s began working (again). Whatever the hub was going to do for its last turn is off; a card for it is
// withdrawn. How long the turn waited is recorded.
func (h *Hub) workStarted(machine, sid string, now time.Time) {
	k := [2]string{machine, sid}
	h.work.mu.Lock()
	h.work.init()
	e, ok := h.work.entries[k]
	if !ok || e.Work.Phase == proto.WorkWorking {
		h.work.mu.Unlock()
		return
	}
	by := "user"
	if now.Sub(h.work.sent[k]) < 2*time.Minute {
		by = "hub"
	}
	card := e.InteractionID
	prev := *e
	e.WakeAt, e.Work.WakeAt, e.InteractionID, e.Work.InteractionID = nil, nil, "", ""
	e.UpdatedAt = now.UTC()
	saved := *e
	h.work.mu.Unlock()
	if card != "" {
		h.withdrawCard(card)
		h.workEvent(machine, sid, "withdrawn", map[string]any{"by": by})
	}
	if prev.Work.Phase != "" {
		h.workEvent(machine, sid, "resumed", map[string]any{"after": now.Sub(prev.TurnAt).Seconds(), "phase": prev.Work.Phase,
			"on": prev.Work.On, "by": by})
	}
	h.saveWork(saved)
}

// workSeen: the user looked at a session. The first look after a turn ended is how long it waited for them.
func (h *Hub) workSeen(machine, sid string, at time.Time) {
	k := [2]string{machine, sid}
	h.work.mu.Lock()
	h.work.init()
	e, ok := h.work.entries[k]
	if !ok || e.Work.Phase == "" || !h.work.seen[k].Before(e.TurnAt) {
		h.work.mu.Unlock()
		return
	}
	h.work.seen[k] = at
	phase, on, turn := e.Work.Phase, e.Work.On, e.TurnAt
	h.work.mu.Unlock()
	h.workEvent(machine, sid, "seen", map[string]any{"after": at.Sub(turn).Seconds(), "phase": phase, "on": on})
}

// workTurnEnded: a session on m stopped. Once it stays stopped a moment, its turn is triaged; agents waiting for this
// one are woken.
func (h *Hub) workTurnEnded(m *Machine, sid string) {
	if !h.workOn() {
		return
	}
	h.loadWork(context.Background())
	h.wakeWaitersOn(m.cfg.ID, sid)
	time.Sleep(h.triageCfg().settle())
	s, ok := h.decoratedSession(m, sid)
	if !ok || s.Status == proto.StatusWorking {
		return
	}
	h.triageTurn(m, s)
}

func (h *Hub) decoratedSession(m *Machine, sid string) (proto.Session, bool) {
	for _, s := range h.decorated(m) {
		if s.SessionID == sid {
			return s, true
		}
	}
	return proto.Session{}, false
}

// triageTurn works out where s stands after its turn and acts on it. Once per turn.
func (h *Hub) triageTurn(m *Machine, s proto.Session) {
	if s.Assistant || s.Status == proto.StatusOffline || s.Status == proto.StatusWorking || s.Starting {
		return
	}
	k := [2]string{m.cfg.ID, s.SessionID}
	h.work.mu.Lock()
	h.work.init()
	prev, had := h.work.entries[k]
	h.work.tried[k] = triedTurn{s.UpdatedAt, time.Now()}
	if had && sameTurn(s, prev.TurnAt) && (prev.Work.Phase != "" || prev.Work.By != "") || h.work.triaging[k] {
		h.work.mu.Unlock()
		return
	}
	h.work.triaging[k] = true
	var last db.SessionWork
	if had {
		last = *prev
	}
	h.work.mu.Unlock()
	defer func() {
		h.work.mu.Lock()
		delete(h.work.triaging, k)
		h.work.mu.Unlock()
	}()

	start := time.Now()
	w, by, skip := triageRules(s, h.userLocation())
	if skip {
		return // the agent asks itself (or its terminal does): the list shows that
	}
	if w == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		var err error
		w, err = h.triageTurnModel(ctx, m, s)
		if err != nil {
			h.logf("triage %s/%s: %v", m.cfg.ID, short(s.SessionID), err)
			h.workEvent(m.cfg.ID, s.SessionID, "failed", map[string]any{"error": clip(err.Error(), 200)})
			return
		}
		by = "model"
	}
	w.TurnAt, w.At, w.By = s.UpdatedAt, time.Now().UTC(), by
	e := db.SessionWork{MachineID: m.cfg.ID, SessionID: s.SessionID, TurnAt: s.UpdatedAt, Work: *w}
	// The same wait goes on (the hub woke it, it still waits): its counts carry over.
	if had && last.Work.Phase == proto.WorkBlocked && w.Phase == proto.WorkBlocked && last.Work.On == w.On {
		e.Retries, e.Wakes = last.Retries, last.Wakes
	}
	if s, ok := m.session(s.SessionID); ok && !sameTurn(s, e.TurnAt) {
		return // it went on while the model read it: the next end decides
	}
	h.actOn(m, s, &e)
	h.putWork(e)
	h.logf("triage %s/%s: %s%s (%s %.1fs): %s", m.cfg.ID, short(s.SessionID), w.Phase, orEmpty("/", e.Work.On), by,
		time.Since(start).Seconds(), e.Work.Line)
	h.workEvent(m.cfg.ID, s.SessionID, "triaged", map[string]any{"phase": w.Phase, "on": e.Work.On, "by": by,
		"line": e.Work.Line, "ms": time.Since(start).Milliseconds()})
	if lm := h.localMachine(m.cfg.ID); lm != nil {
		h.broadcastSessions(lm, h.decorated(lm))
	}
}

func orEmpty(sep, s string) string {
	if s == "" {
		return ""
	}
	return sep + s
}

// triageRules settles what needs no model. skip: the agent waits for the user on its own card or dialog.
func triageRules(s proto.Session, loc *time.Location) (w *proto.WorkState, by string, skip bool) {
	if s.PendingInteractions > 0 || s.Status == proto.StatusBlocked {
		return nil, "", true
	}
	if e := s.TurnError; e != "" {
		if transientError(e) {
			return &proto.WorkState{Phase: proto.WorkBlocked, On: proto.WaitTransient, Line: "模型服务暂时出错，自动重试",
				Wait: clip(oneLine(e), 200)}, "rule", false
		}
		return &proto.WorkState{Phase: proto.WorkBlocked, On: proto.WaitUser, Line: clip(oneLine("模型服务出错："+e), 60),
			Questions: []proto.Question{{Question: "模型服务出错，需要你处理：" + clip(oneLine(e), 300), Header: "模型服务",
				Options: []proto.QuestionOption{{Label: answerHandled}, {Label: answerSkip}}}}}, "rule", false
	}
	if n := runningInBackground(s); n > 0 {
		return &proto.WorkState{Phase: proto.WorkBlocked, On: proto.WaitExternal, Line: fmt.Sprintf("后台 %d 个任务在跑，跑完会自己接着做", n)},
			"rule", false
	}
	if s.Loop != nil {
		line := "定时循环在跑"
		if !s.Loop.NextAt.IsZero() {
			line += "，下次 " + s.Loop.NextAt.In(loc).Format("15:04")
		}
		return &proto.WorkState{Phase: proto.WorkBlocked, On: proto.WaitExternal, Line: line}, "rule", false
	}
	return nil, "", false
}

func runningInBackground(s proto.Session) int {
	n := len(s.Background)
	for _, a := range s.Subagents {
		if a.Running {
			n++
		}
	}
	return n
}

// transientError: a model API error worth a 继续 (the service failed for a moment), not one the user must fix
// (sign-in, key, the model picked).
func transientError(text string) bool {
	t := strings.ToLower(text)
	for _, w := range []string{"failed to authenticate", "oauth token has expired", "invalid api key", "issue with the selected model",
		" 401", " 403", "permission_error", "authentication_error", "credit balance", "billing"} {
		if strings.Contains(t, w) {
			return false
		}
	}
	for _, w := range []string{"overloaded", " 500", " 502", " 503", " 504", " 529", " 429", "rate limit", "rate_limit",
		"connection", "timed out", "timeout", "socket hang up", "econnreset", "fetch failed", "no eligible upstream",
		"internal server error", "service unavailable", "bad gateway", "network", "api_error", "upstream"} {
		if strings.Contains(t, w) {
			return true
		}
	}
	return false
}

// riskyWords mark a next step the hub must not set off itself (docs/agent-work.md「安全边界」): it is only ever a card.
var riskyWords = []string{"push", "deploy", "部署", "发布", "上线", "merge", "合并", "合入", "发消息", "回复", "评论", "testflight",
	"release", "推送", "推到", "comment", "发版", "提审", "send"}

func risky(texts ...string) bool {
	for _, t := range texts {
		t = strings.ToLower(t)
		for _, w := range riskyWords {
			if strings.Contains(t, w) {
				return true
			}
		}
	}
	return false
}

// ---- acting ----

// actOn decides what the hub does about e's work state and records it on e (wake_at, a card). It sends nothing:
// the tick does, after checking again.
func (h *Hub) actOn(m *Machine, s proto.Session, e *db.SessionWork) {
	cfg := h.triageCfg()
	w := &e.Work
	now := time.Now()
	k := [2]string{e.MachineID, e.SessionID}
	if w.Phase != proto.WorkBlocked || s.ReadOnlyReason != "" {
		return // nothing to do, or nothing Persona may send it
	}
	switch w.On {
	case proto.WaitTransient:
		if !on(cfg.AutoContinue) {
			w.Line = clip(oneLine("模型服务出错："+w.Wait), 60)
			return
		}
		if e.Retries >= cfg.maxRetries() {
			*w = proto.WorkState{Phase: proto.WorkBlocked, On: proto.WaitUser, TurnAt: w.TurnAt, At: w.At, By: w.By,
				Line: fmt.Sprintf("模型服务一直出错，自动重试了 %d 次", e.Retries),
				Questions: []proto.Question{{Question: fmt.Sprintf("模型服务一直出错，已经自动重试了 %d 次：%s", e.Retries, clip(w.Wait, 300)),
					Header: "模型服务", Options: []proto.QuestionOption{{Label: answerRetry}, {Label: answerSkip}}}}}
			break
		}
		at := now.Add(retryBackoff[min(e.Retries, len(retryBackoff)-1)]).UTC()
		e.WakeAt, w.WakeAt, w.Attempts, w.Max = &at, &at, e.Retries, cfg.maxRetries()
		return
	case proto.WaitExternal, proto.WaitAgent:
		if w.Wait == "" && w.Agent == nil {
			return // a background task or a /loop: it wakes itself
		}
		h.work.mu.Lock()
		approved := h.work.approved[k]
		h.work.mu.Unlock()
		if risky(w.Then) && (approved == "" || approved != w.Then) {
			*w = proto.WorkState{Phase: proto.WorkBlocked, On: proto.WaitUser, TurnAt: w.TurnAt, At: w.At, By: w.By, Wait: w.Wait, Then: w.Then,
				Line: clip(oneLine("等「"+w.Wait+"」以后要「"+w.Then+"」，等你同意"), 60),
				Questions: []proto.Question{{Question: "它在等「" + w.Wait + "」，等到以后要「" + w.Then + "」。要它去做吗？", Header: "要不要做",
					Options: []proto.QuestionOption{{Label: answerGo, Description: "它会自己查，好了再做"}, {Label: answerHold}}}}}
			break
		}
		if !on(cfg.Wake) {
			return
		}
		if e.Wakes >= cfg.maxWakes() {
			*w = proto.WorkState{Phase: proto.WorkBlocked, On: proto.WaitUser, TurnAt: w.TurnAt, At: w.At, By: w.By, Wait: w.Wait, Then: w.Then,
				Line: clip(oneLine(fmt.Sprintf("等「%s」叫了 %d 次还没好", w.Wait, e.Wakes)), 60),
				Questions: []proto.Question{{Question: fmt.Sprintf("等「%s」已经叫醒它 %d 次了还没好。", w.Wait, e.Wakes), Header: "还等吗",
					Options: []proto.QuestionOption{{Label: answerWait}, {Label: answerStop}}}}}
			break
		}
		at := now.Add(wakeAfter(w.Every, e.Wakes)).UTC()
		e.WakeAt, w.WakeAt, w.Attempts, w.Max = &at, &at, e.Wakes, cfg.maxWakes()
		return
	}
	// On the user: a card.
	if w.On != proto.WaitUser || !on(cfg.Decide) || h.muted(e.MachineID, e.SessionID) {
		return
	}
	if len(w.Questions) == 0 {
		w.Questions = []proto.Question{{Question: w.Line, Options: []proto.QuestionOption{{Label: "好，按你说的做"}, {Label: answerSkip}}}}
	}
	if w.Link != nil {
		w.Questions = append([]proto.Question{{Question: linkQuestion(w, h.userLocation()), Header: "授权",
			Options: []proto.QuestionOption{{Label: answerAuthed}, {Label: "先不"}}}}, w.Questions...)
		if len(w.Questions) > 4 {
			w.Questions = w.Questions[:4]
		}
	}
	e.InteractionID = h.openCard(m, s, *w)
	w.InteractionID = e.InteractionID
}

// wakeAfter: the first wake after checkMinutes (2–240, else 10), then each one twice as late, at most an hour (or
// checkMinutes, when that is longer).
func wakeAfter(checkMinutes, wakes int) time.Duration {
	m := checkMinutes
	if m < 2 || m > 240 {
		m = 10
	}
	d := time.Duration(m) * time.Minute * time.Duration(math.Pow(2, float64(wakes)))
	if wakes > 0 && d > time.Hour {
		d = max(time.Hour, time.Duration(m)*time.Minute)
	}
	return d
}

func linkQuestion(w *proto.WorkState, loc *time.Location) string {
	q := "请打开授权链接：" + w.Link.URL
	if w.Link.ExpiresAt != nil {
		q += fmt.Sprintf("（%s 前有效）", w.Link.ExpiresAt.In(loc).Format("15:04"))
	}
	return q
}

// openCard puts w's questions on a card for s (shown and pushed like any question); its answer goes to the agent.
// Returns the card's id.
func (h *Hub) openCard(m *Machine, s proto.Session, w proto.WorkState) string {
	ix := proto.Interaction{
		ID: "work_" + randomHex(6), MachineID: m.cfg.ID, SessionID: s.SessionID, SessionTitle: agentLabel(s), Cwd: s.Cwd,
		Kind: proto.IxQuestion, ToolName: workCardTool, Summary: w.Line, Questions: w.Questions,
		CreatedAt: time.Now().UTC(), ExpiresAt: w.TurnAt.Add(workCardTTL).UTC(),
	}
	if s.Agent != "" && s.Agent != "claude" {
		ix.Agent, ix.AgentLabel = s.Agent, s.AgentLabel
	}
	key, sid, turn := [2]string{m.cfg.ID, s.SessionID}, s.SessionID, w.TurnAt
	h.mu.Lock()
	h.interactions[ix.ID] = &pending{ix: ix, local: func(r proto.Resolution) error {
		return h.answerCard(m, key, sid, turn, ix, r)
	}}
	h.mu.Unlock()
	h.record(ix)
	go h.announce(ix)
	h.workEvent(m.cfg.ID, s.SessionID, "asked", map[string]any{"line": w.Line, "questions": len(w.Questions)})
	return ix.ID
}

// answerCard: the user answered the hub's card. What the agent is to hear goes to it; the answers the hub acts on
// itself (先不管, 再等等…) do not.
func (h *Hub) answerCard(m *Machine, key [2]string, sid string, turn time.Time, ix proto.Interaction, r proto.Resolution) error {
	e, ok := h.workEntry(key)
	after := time.Since(turn).Seconds()
	clear := func(line string) {
		if ok && e.InteractionID == ix.ID {
			e.InteractionID, e.Work.InteractionID = "", ""
			if line != "" {
				e.Work.Line = line
			}
			h.putWork(e)
		}
	}
	if r.Dismiss {
		clear("")
		h.workEvent(m.cfg.ID, sid, "answered", map[string]any{"after": after, "dismissed": true})
		return nil
	}
	var items []agentAnswerItem
	only := map[string]bool{}
	for _, q := range ix.Questions {
		a := strings.TrimSpace(r.Answers[q.Question])
		if a == "" {
			continue
		}
		items = append(items, agentAnswerItem{q.Question, a})
		only[a] = true
	}
	h.workEvent(m.cfg.ID, sid, "answered", map[string]any{"after": after, "answers": len(items)})
	text := ""
	switch {
	case len(items) == 0:
		clear("")
		return nil
	case len(only) == 1 && (only[answerSkip] || only["先不"] || only[answerStop] || only[answerHold]):
		clear("你说先不管")
		return nil
	case len(only) == 1 && only[answerWait]:
		if ok {
			at := time.Now().Add(wakeAfter(e.Work.Every, 0)).UTC()
			e.Wakes, e.WakeAt = 0, &at
			e.Work.On, e.Work.WakeAt, e.Work.Attempts, e.Work.Questions = proto.WaitExternal, &at, 0, nil
			e.Work.Line = clip(oneLine("等「"+e.Work.Wait+"」"), 60)
			clear("")
		}
		return nil
	case len(only) == 1 && (only[answerRetry] || only[answerHandled] || only[answerAuthed]):
		text = continueText
		if ok {
			e.Retries = 0
		}
	case only[answerGo]:
		h.work.mu.Lock()
		h.work.init()
		h.work.approved[key] = e.Work.Then
		h.work.mu.Unlock()
		text = renderAnswer(items)
	default:
		text = renderAnswer(items)
	}
	clear("")
	if err := h.sendToSession(m, sid, text); err != nil {
		return err
	}
	return nil
}

type agentAnswerItem struct{ question, answer string }

// sendToSession sends text to sid as the hub (the next working is the hub's doing, not the user's).
func (h *Hub) sendToSession(m *Machine, sid, text string) error {
	h.work.mu.Lock()
	h.work.init()
	h.work.sent[[2]string{m.cfg.ID, sid}] = time.Now()
	h.work.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return m.call(ctx, "session.send", map[string]any{"session_id": sid, "text": text}, nil)
}

// withdrawCard takes the hub's card away: the user went on without it.
func (h *Hub) withdrawCard(id string) {
	h.mu.Lock()
	_, ok := h.interactions[id]
	h.mu.Unlock()
	if !ok {
		return
	}
	h.recordEnd(id, "cancelled", nil)
	h.finishInteraction(id)
}

// ---- the tick ----

// workLoop runs the tick until ctx ends.
func (h *Hub) workLoop(ctx context.Context) {
	t := time.NewTicker(workEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			if h.workOn() {
				h.workTick(ctx, now)
			}
		}
	}
}

// workTick: due wakes go out, cards lost with a restart come back, turns that ended while the hub was away (or
// whose triage failed a while ago) are triaged, and the number of unread sessions is sampled.
func (h *Hub) workTick(ctx context.Context, now time.Time) {
	h.loadWork(ctx)
	h.work.mu.Lock()
	h.work.init()
	var due, cards [][2]string
	for k, e := range h.work.entries {
		switch {
		case e.WakeAt != nil && !e.WakeAt.After(now):
			due = append(due, k)
		case e.WakeAt != nil && e.Work.On == proto.WaitAgent && e.Work.Agent != nil && h.agentStopped(*e.Work.Agent):
			due = append(due, k)
		case e.InteractionID != "":
			cards = append(cards, k)
		}
	}
	sample := now.Sub(h.work.sampleAt) >= workSampleGap
	if sample {
		h.work.sampleAt = now
	}
	prune := now.Sub(h.work.pruneAt) >= 24*time.Hour
	if prune {
		h.work.pruneAt = now
	}
	h.work.mu.Unlock()
	for _, k := range due {
		h.workWake(k, now)
	}
	for _, k := range cards {
		h.restoreCard(k, now)
	}
	h.backfillWork(now)
	if sample {
		h.sampleWork()
	}
	if st := h.workDB(); prune && st != nil {
		if err := st.PruneSessionWork(ctx, now.Add(-workKeep)); err != nil {
			h.logf("work: prune: %v", err)
		}
	}
}

// agentStopped: the agent a session waits for is not working any more (or is gone).
func (h *Hub) agentStopped(a proto.WorkAgent) bool {
	m := h.localMachine(a.MachineID)
	if m == nil {
		return false // its machine is away: the fallback wake decides
	}
	s, ok := m.session(a.SessionID)
	return !ok || s.Status != proto.StatusWorking
}

// wakeWaitersOn: sid on machine stopped; agents waiting for it are due now.
func (h *Hub) wakeWaitersOn(machine, sid string) {
	h.work.mu.Lock()
	defer h.work.mu.Unlock()
	h.work.init()
	now := time.Now().UTC()
	for _, e := range h.work.entries {
		if a := e.Work.Agent; e.WakeAt != nil && e.Work.On == proto.WaitAgent && a != nil && a.MachineID == machine && a.SessionID == sid {
			e.WakeAt, e.Work.WakeAt = &now, &now
		}
	}
}

// workGuard: the hub may act on e now — the session is still stopped on the same turn, nothing pending or queued,
// not looked at in herdr, its machine up. ownCard: the hub's card for it may be pending.
func (h *Hub) workGuard(e db.SessionWork, ownCard bool) (*Machine, proto.Session, string) {
	m := h.localMachine(e.MachineID)
	if m == nil || !m.reachable() {
		return nil, proto.Session{}, "offline"
	}
	s, ok := h.decoratedSession(m, e.SessionID)
	switch {
	case !ok:
		return m, s, "gone"
	case s.ReadOnlyReason != "":
		return m, s, "read-only"
	case s.Status == proto.StatusWorking || !sameTurn(s, e.TurnAt):
		return m, s, "moved on"
	case s.Status == proto.StatusIdle:
		return m, s, "looked at in herdr"
	case s.Status != proto.StatusDone:
		return m, s, "not stopped"
	case len(s.Queued) > 0:
		return m, s, "input queued"
	case s.PendingInteractions > 0 && !(ownCard && s.PendingInteractions == 1 && e.InteractionID != ""):
		return m, s, "asking"
	}
	return m, s, ""
}

// workWake sends what is due for k: 继续 after a transient error, or a wake for a wait.
func (h *Hub) workWake(k [2]string, now time.Time) {
	e, ok := h.workEntry(k)
	if !ok || e.WakeAt == nil {
		return
	}
	m, s, why := h.workGuard(e, false)
	switch why {
	case "":
	case "offline":
		at := now.Add(time.Minute).UTC() // try again when it is back
		e.WakeAt, e.Work.WakeAt = &at, &at
		h.putWork(e)
		return
	default:
		h.logf("work %s/%s: not woken: %s", k[0], short(k[1]), why)
		e.WakeAt, e.Work.WakeAt = nil, nil
		h.putWork(e)
		return
	}
	var text, kind string
	switch e.Work.On {
	case proto.WaitTransient:
		e.Retries++
		text, kind = continueText, "continued"
	case proto.WaitExternal, proto.WaitAgent:
		e.Wakes++
		wk := agentWake{Attempt: e.Wakes, Wait: e.Work.Wait, Then: e.Work.Then}
		if a := e.Work.Agent; a != nil && h.agentStopped(*a) {
			wk.Agent = a.Label
			if am := h.localMachine(a.MachineID); am != nil {
				if as, ok := am.session(a.SessionID); ok {
					wk.AgentLine = clip(oneLine(as.LastText), 200)
				}
			}
		}
		text, kind = renderWake(wk), "woke"
	default:
		return
	}
	e.WakeAt, e.Work.WakeAt = nil, nil
	h.putWork(e)
	if err := h.sendToSession(m, s.SessionID, text); err != nil {
		h.logf("work %s/%s: %s: %v", k[0], short(k[1]), kind, err)
		at := now.Add(2 * time.Minute).UTC()
		e.WakeAt, e.Work.WakeAt = &at, &at
		if kind == "continued" {
			e.Retries--
		} else {
			e.Wakes--
		}
		h.putWork(e)
		return
	}
	h.logf("work %s/%s: %s (%d)", k[0], short(k[1]), kind, max(e.Retries, e.Wakes))
	h.workEvent(k[0], k[1], kind, map[string]any{"attempt": max(e.Retries, e.Wakes), "after": now.Sub(e.TurnAt).Seconds()})
}

// restoreCard: k's card is not pending (the hub restarted): put it back while the turn still waits, or forget it.
func (h *Hub) restoreCard(k [2]string, now time.Time) {
	e, ok := h.workEntry(k)
	if !ok || e.InteractionID == "" {
		return
	}
	h.mu.Lock()
	p, pending := h.interactions[e.InteractionID]
	h.mu.Unlock()
	if pending {
		if now.After(p.ix.ExpiresAt) {
			h.recordEnd(e.InteractionID, "expired", nil)
			h.finishInteraction(e.InteractionID)
			e.InteractionID, e.Work.InteractionID = "", ""
			h.putWork(e)
		}
		return
	}
	m, s, why := h.workGuard(e, true)
	if why != "" || now.After(e.TurnAt.Add(workCardTTL)) || len(e.Work.Questions) == 0 {
		e.InteractionID, e.Work.InteractionID = "", ""
		h.putWork(e)
		return
	}
	e.InteractionID = h.openCard(m, s, e.Work)
	e.Work.InteractionID = e.InteractionID
	h.putWork(e)
}

// backfillWork triages turns that ended while the hub was away (unread, within a day), a few per tick.
func (h *Hub) backfillWork(now time.Time) {
	n := 0
	for _, m := range h.machinesNow() {
		if !m.reachable() {
			continue
		}
		for _, s := range h.decorated(m) {
			if n >= 3 {
				return
			}
			if s.Assistant || s.State != proto.StateUnread || s.Work != nil || now.Sub(s.UpdatedAt) > workBackfill ||
				now.Sub(s.UpdatedAt) < h.triageCfg().settle() {
				continue
			}
			k := [2]string{m.cfg.ID, s.SessionID}
			h.work.mu.Lock()
			tried, ok := h.work.tried[k]
			h.work.mu.Unlock()
			if ok && sameTurn(s, tried.turn) && now.Sub(tried.at) < workRetryTriage {
				continue // tried on this turn a moment ago (a failed model call is not retried every tick)
			}
			n++
			go h.triageTurn(m, s) // a model call each: the tick's wakes do not wait for them
		}
	}
}

// sampleWork records how many sessions are in each state now (for the stats: 完成未看 over time).
func (h *Hub) sampleWork() {
	counts := map[string]any{}
	for _, m := range h.machinesNow() {
		for _, s := range h.decorated(m) {
			if s.Assistant || s.State == proto.StateOffline {
				continue
			}
			n, _ := counts[s.State].(int)
			counts[s.State] = n + 1
		}
	}
	h.workEvent("", "", "sample", counts)
}

// httpsLink: u is an http(s) link worth putting on a card.
func httpsLink(u string) bool {
	p, err := url.Parse(strings.TrimSpace(u))
	return err == nil && (p.Scheme == "https" || p.Scheme == "http") && p.Host != ""
}

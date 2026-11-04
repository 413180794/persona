package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// Tasks dispatched from an assistant conversation (docs/unified-entry.md, #930). The assistant hands the hub a plan
// (task_plan); read-only plans and kinds the user trusts start at once, the rest wait for the user's 开始 on the
// plan card. A task is a todo whose origin is the conversation; its agents are ordinary agents (the Agents page shows
// them as always). What the hub adds to the conversation — the plan card, a started task, an answered question, a
// receipt, a message sent straight to a task — is the conversation's feed, kept apart from Claude's transcript.

// PlanTask is one task of a plan, as the assistant proposed it and the user may have edited it.
type PlanTask struct {
	Title      string `json:"title"`
	Prompt     string `json:"prompt"`
	Machine    string `json:"machine"`
	Cwd        string `json:"cwd"`
	Launcher   string `json:"launcher,omitempty"`
	Model      string `json:"model,omitempty"`
	Permission string `json:"permission,omitempty"`
	Readonly   bool   `json:"readonly,omitempty"`
	Why        string `json:"why,omitempty"`
	// Set once started: the todo the task is, and why its agent did not start.
	TodoID string `json:"todo_id,omitempty"`
	Error  string `json:"error,omitempty"`
}

// TaskPlan is a plan the assistant handed the user.
type TaskPlan struct {
	ID           string     `json:"id"`
	Conversation string     `json:"conversation"`
	Tasks        []PlanTask `json:"tasks"`
	State        string     `json:"state"` // planOpen, planStarted, planDismissed
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

const (
	planOpen      = "open"
	planStarted   = "started"
	planDismissed = "dismissed"
)

// FeedItem is one item the hub inserted into an assistant conversation; clients merge them with the transcript by At.
type FeedItem struct {
	ID           string          `json:"id"`
	Conversation string          `json:"conversation"`
	At           time.Time       `json:"at"`
	Kind         string          `json:"kind"` // feedPlan, feedTask, feedAnswered, feedReceipt, feedRouted
	Ref          string          `json:"ref,omitempty"`
	Body         json.RawMessage `json:"body,omitempty"`
}

const (
	feedPlan     = "plan"     // ref: the plan id
	feedTask     = "task"     // ref: the todo id
	feedAnswered = "answered" // ref: the todo id; body: answeredBody (tasks_answered.go)
	feedReceipt  = "receipt"  // ref: the todo id; body: title, state, summary, pr, machine
	feedRouted   = "routed"   // ref: the todo id; body: text
	feedMessage  = "message"  // ref: the inbox item id; body: messageBody (a message about a task, with its draft reply)
)

const trustSetting = "tasks.trust"

// taskSettleDelay is how long a task's agents stay still before it counts as done (tests shorten it).
var taskSettleDelay = func(c NotifyConfig) time.Duration { return c.doneDelay() }

type tasksState struct {
	mu sync.Mutex
	// Without a store (unit tests) plans and the feed live here.
	plans map[string]*TaskPlan
	feed  map[string][]FeedItem
	// working: when one of a task's agents last started a turn; receipted: when its last receipt went out. A
	// receipt is due when the task settles after working since the last one.
	working   map[string]time.Time
	receipted map[string]time.Time
}

func newTasksState() tasksState {
	return tasksState{plans: map[string]*TaskPlan{}, feed: map[string][]FeedItem{},
		working: map[string]time.Time{}, receipted: map[string]time.Time{}}
}

// ---- plans and the feed: storage ----

func (h *Hub) savePlan(ctx context.Context, p *TaskPlan) error {
	if h.store == nil {
		h.tasks.mu.Lock()
		cp := *p
		cp.Tasks = append([]PlanTask(nil), p.Tasks...)
		h.tasks.plans[p.ID] = &cp
		h.tasks.mu.Unlock()
		return nil
	}
	raw, _ := json.Marshal(p.Tasks)
	return h.store.CreateTaskPlan(ctx, db.TaskPlanRow{ID: p.ID, Conversation: p.Conversation, Tasks: raw, State: p.State,
		CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt})
}

var errPlanNotFound = errors.New("plan not found")

func (h *Hub) loadPlan(ctx context.Context, id string) (*TaskPlan, error) {
	if h.store == nil {
		h.tasks.mu.Lock()
		defer h.tasks.mu.Unlock()
		p := h.tasks.plans[id]
		if p == nil {
			return nil, errPlanNotFound
		}
		cp := *p
		cp.Tasks = append([]PlanTask(nil), p.Tasks...)
		return &cp, nil
	}
	row, err := h.store.TaskPlan(ctx, id)
	if errors.Is(err, db.ErrTaskPlanNotFound) {
		return nil, errPlanNotFound
	}
	if err != nil {
		return nil, err
	}
	return planFromRow(*row), nil
}

func planFromRow(row db.TaskPlanRow) *TaskPlan {
	p := &TaskPlan{ID: row.ID, Conversation: row.Conversation, State: row.State, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
	_ = json.Unmarshal(row.Tasks, &p.Tasks)
	return p
}

// movePlan moves p from state from to its State with its Tasks; false when someone else moved it first.
func (h *Hub) movePlan(ctx context.Context, p *TaskPlan, from string) (bool, error) {
	p.UpdatedAt = time.Now().UTC()
	if h.store == nil {
		h.tasks.mu.Lock()
		defer h.tasks.mu.Unlock()
		cur := h.tasks.plans[p.ID]
		if cur == nil || cur.State != from {
			return false, nil
		}
		cp := *p
		cp.Tasks = append([]PlanTask(nil), p.Tasks...)
		h.tasks.plans[p.ID] = &cp
		return true, nil
	}
	raw, _ := json.Marshal(p.Tasks)
	return h.store.UpdateTaskPlan(ctx, p.ID, from, p.State, raw, p.UpdatedAt)
}

// appendFeed adds an item to a conversation's feed and tells the clients.
func (h *Hub) appendFeed(ctx context.Context, conversation, kind, ref string, body any) (FeedItem, error) {
	return h.appendFeedAt(ctx, conversation, kind, ref, body, time.Now())
}

// appendFeedAt is appendFeed for an item that belongs at another time (the 今天 card: the start of the day).
func (h *Hub) appendFeedAt(ctx context.Context, conversation, kind, ref string, body any, at time.Time) (FeedItem, error) {
	it := FeedItem{ID: "feed_" + randomHex(8), Conversation: conversation, At: at.UTC(), Kind: kind, Ref: ref}
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return FeedItem{}, err
		}
		it.Body = raw
	}
	if h.store == nil {
		h.tasks.mu.Lock()
		h.tasks.feed[conversation] = append(h.tasks.feed[conversation], it)
		h.tasks.mu.Unlock()
	} else if err := h.store.AppendFeed(ctx, db.FeedRow{ID: it.ID, Conversation: conversation, At: it.At, Kind: kind, Ref: ref, Body: it.Body}); err != nil {
		return FeedItem{}, err
	}
	h.broadcast(map[string]any{"type": "assistant_feed", "conversation": conversation, "item": it})
	return it, nil
}

const feedPage = 500

// feed is a conversation's items after since, oldest first, and the plans they name.
func (h *Hub) feed(ctx context.Context, conversation string, since time.Time) ([]FeedItem, map[string]*TaskPlan, error) {
	var items []FeedItem
	if h.store == nil {
		h.tasks.mu.Lock()
		for _, it := range h.tasks.feed[conversation] {
			if it.At.After(since) {
				items = append(items, it)
			}
		}
		h.tasks.mu.Unlock()
	} else {
		rows, err := h.store.Feed(ctx, conversation, since, feedPage)
		if err != nil {
			return nil, nil, err
		}
		for _, r := range rows {
			items = append(items, FeedItem{ID: r.ID, Conversation: r.Conversation, At: r.At, Kind: r.Kind, Ref: r.Ref, Body: r.Body})
		}
	}
	plans := map[string]*TaskPlan{}
	for _, it := range items {
		if it.Kind == feedPlan && plans[it.Ref] == nil {
			if p, err := h.loadPlan(ctx, it.Ref); err == nil {
				plans[it.Ref] = p
			}
		}
	}
	return items, plans, nil
}

// ---- the assistant's task_plan tool ----

func (h *Hub) toolTaskPlan(ctx context.Context, sessionID string, args json.RawMessage) (string, error) {
	var a struct {
		Tasks []PlanTask `json:"tasks"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("tasks: %v", err)
	}
	if len(a.Tasks) == 0 {
		return "", fmt.Errorf("tasks is empty")
	}
	if len(a.Tasks) > 8 {
		return "", fmt.Errorf("at most 8 tasks in one plan")
	}
	for i := range a.Tasks {
		t := &a.Tasks[i]
		t.TodoID, t.Error = "", ""
		t.Title, t.Prompt, t.Cwd = strings.TrimSpace(t.Title), strings.TrimSpace(t.Prompt), strings.TrimSpace(t.Cwd)
		if t.Title == "" || t.Prompt == "" || t.Cwd == "" {
			return "", fmt.Errorf("task %d: title, prompt and cwd are required", i+1)
		}
		if !h.placeTask(t) {
			return "", fmt.Errorf("task %d: unknown machine %q (machines: %s)", i+1, t.Machine, h.machineIDs())
		}
	}
	if err := h.ensureTodosLoaded(ctx); err != nil {
		return "", fmt.Errorf("todos are unavailable: %v", err)
	}
	now := time.Now().UTC()
	p := &TaskPlan{ID: "plan_" + randomHex(6), Conversation: sessionID, Tasks: a.Tasks, State: planOpen, CreatedAt: now, UpdatedAt: now}
	if err := h.savePlan(ctx, p); err != nil {
		return "", err
	}
	if _, err := h.appendFeed(ctx, sessionID, feedPlan, p.ID, nil); err != nil {
		h.logf("task plan %s: feed: %v", p.ID, err)
	}
	if !h.planStartsAtOnce(ctx, p.Tasks) {
		h.broadcast(map[string]any{"type": "task_plan", "plan": p})
		return fmt.Sprintf("Plan %s is on the user's screen as a card; nothing has started. Don't wait for it: carry on. "+
			"When they start it, the tasks show up in the context snapshot.", p.ID), nil
	}
	started, err := h.startPlan(ctx, p.ID, nil, false)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Started %d task(s) without asking (read-only or a kind the user trusts):\n", len(started.Tasks))
	for _, t := range started.Tasks {
		fmt.Fprintf(&b, "- %s → todo %s on %s\n", t.Title, t.TodoID, t.Machine)
	}
	b.WriteString("Their progress shows in the context snapshot; the user sees them as cards.")
	return b.String(), nil
}

// placeTask names t's machine by its id and makes ~/ in its directory that machine's home; false: no such machine.
func (h *Hub) placeTask(t *PlanTask) bool {
	m := h.machineByName(t.Machine)
	if m == nil {
		return false
	}
	t.Machine = m.cfg.ID
	if strings.HasPrefix(t.Cwd, "~/") {
		if home := m.snapshot().Home; home != "" {
			t.Cwd = path.Join(home, t.Cwd[2:])
		}
	}
	return true
}

// planStartsAtOnce: every task only reads, or is a kind the user said to start without asking.
func (h *Hub) planStartsAtOnce(ctx context.Context, tasks []PlanTask) bool {
	trusted := h.trustedKinds(ctx)
	for _, t := range tasks {
		if !t.Readonly && !trusted[trustKey(t)] {
			return false
		}
	}
	return true
}

// trustKey names a kind of task the user can let start without asking: a machine's directory, reading or editing.
func trustKey(t PlanTask) string {
	kind := "edit"
	if t.Readonly {
		kind = "read"
	}
	return t.Machine + "|" + t.Cwd + "|" + kind
}

func (h *Hub) trustedKinds(ctx context.Context) map[string]bool {
	out := map[string]bool{}
	if h.store == nil {
		return out
	}
	var keys []string
	if ok, err := h.store.Setting(ctx, trustSetting, &keys); err == nil && ok {
		for _, k := range keys {
			out[k] = true
		}
	}
	return out
}

func (h *Hub) trustKinds(ctx context.Context, tasks []PlanTask) {
	if h.store == nil {
		return
	}
	have := h.trustedKinds(ctx)
	for _, t := range tasks {
		have[trustKey(t)] = true
	}
	keys := make([]string, 0, len(have))
	for k := range have {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if err := h.store.PutSetting(ctx, trustSetting, keys); err != nil {
		h.logf("tasks: trust: %v", err)
	}
}

// ---- starting and dismissing a plan ----

var errPlanNotOpen = errors.New("this plan was already started or dismissed")

// startPlan opens a todo for each task of the open plan id (edits, when given, replace the tasks' fields the user can
// change) and starts their agents in the background; remember lets this kind start without asking next time.
func (h *Hub) startPlan(ctx context.Context, id string, edits []PlanTask, remember bool) (*TaskPlan, error) {
	if err := h.ensureTodosLoaded(ctx); err != nil {
		return nil, err
	}
	p, err := h.loadPlan(ctx, id)
	if err != nil {
		return nil, err
	}
	if p.State != planOpen {
		return nil, errPlanNotOpen
	}
	for i := range p.Tasks {
		if i < len(edits) {
			applyPlanEdit(&p.Tasks[i], edits[i])
		}
		if !h.placeTask(&p.Tasks[i]) {
			return nil, fmt.Errorf("「%s」：没有这台电脑 %q", p.Tasks[i].Title, p.Tasks[i].Machine)
		}
	}
	p.State = planStarted
	ok, err := h.movePlan(ctx, p, planOpen)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errPlanNotOpen
	}
	for i := range p.Tasks {
		t := &p.Tasks[i]
		todo, err := h.createTodo(ctx, t.Title, t.Prompt, p.Conversation, p.ID)
		if err != nil {
			t.Error = err.Error()
			continue
		}
		t.TodoID = todo.ID
		if _, err := h.appendFeed(ctx, p.Conversation, feedTask, todo.ID, nil); err != nil {
			h.logf("task %s: feed: %v", todo.ID, err)
		}
	}
	if _, err := h.movePlan(ctx, p, planStarted); err != nil { // keep the todo ids
		h.logf("plan %s: %v", p.ID, err)
	}
	if remember {
		h.trustKinds(ctx, p.Tasks)
	}
	h.broadcast(map[string]any{"type": "task_plan", "plan": p})
	for i := range p.Tasks {
		if p.Tasks[i].TodoID != "" {
			go h.startPlanTask(p.ID, p.Conversation, i, p.Tasks[i])
		}
	}
	return p, nil
}

// applyPlanEdit takes what the user changed on the card; an empty field keeps the assistant's.
func applyPlanEdit(t *PlanTask, e PlanTask) {
	if s := strings.TrimSpace(e.Title); s != "" {
		t.Title = s
	}
	if s := strings.TrimSpace(e.Prompt); s != "" {
		t.Prompt = s
	}
	if s := strings.TrimSpace(e.Machine); s != "" {
		t.Machine = s
	}
	if s := strings.TrimSpace(e.Cwd); s != "" {
		t.Cwd = s
	}
	if s := strings.TrimSpace(e.Launcher); s != "" {
		t.Launcher = s
	}
	if s := strings.TrimSpace(e.Model); s != "" {
		t.Model = s
	}
	if s := strings.TrimSpace(e.Permission); s != "" {
		t.Permission = s
	}
}

// startPlanTask starts one task's agent and links it to the task's todo. A failure ends the task: the todo is done
// with why (so nothing shows it as going on, #1077), the plan keeps the error, and the conversation gets a receipt.
func (h *Hub) startPlanTask(planID, conversation string, i int, t PlanTask) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	sid, err := h.startTaskAgent(ctx, t)
	if err != nil && transientStartErr(err) && ctx.Err() == nil {
		// The machine's link dropped (a node restarting, a tunnel blinking): try once more after a moment.
		h.logf("task %s: start: %v; retrying", t.TodoID, err)
		select {
		case <-time.After(taskStartRetryDelay):
			sid, err = h.startTaskAgent(ctx, t)
		case <-ctx.Done():
		}
	}
	if err == nil {
		m := h.machineByName(t.Machine)
		err = h.linkTodoAgent(ctx, t.TodoID, m.cfg.ID, sid, t.Title, t.Cwd, "", "")
	}
	if err == nil {
		return
	}
	h.logf("task %s: start: %v", t.TodoID, err)
	reason := clip(oneLine(err.Error()), 200)
	if ferr := h.failTodo(ctx, t.TodoID, reason); ferr != nil {
		h.logf("task %s: fail: %v", t.TodoID, ferr)
	}
	if p, lerr := h.loadPlan(ctx, planID); lerr == nil && i < len(p.Tasks) {
		p.Tasks[i].Error = err.Error()
		_, _ = h.movePlan(ctx, p, planStarted)
		h.broadcast(map[string]any{"type": "task_plan", "plan": p})
	}
	h.tasks.mu.Lock()
	h.tasks.receipted[t.TodoID] = time.Now()
	h.tasks.mu.Unlock()
	_, _ = h.appendFeed(ctx, conversation, feedReceipt, t.TodoID, receiptBody{Title: t.Title, State: "failed",
		Summary: "没开起来：" + reason})
}

// taskStartRetryDelay is how long a task waits before its one retry after a dropped link (tests shorten it).
var taskStartRetryDelay = 3 * time.Second

// transientStartErr: the agent did not start because a link on the way was down or dropped (this hub's to the
// machine, or one further on that answered with the same words: a member hub, a connector), not because the node
// refused; worth one retry.
func transientStartErr(err error) bool {
	var re *rpc.Error
	return errors.Is(err, rpc.ErrClosed) || errors.Is(err, errOffline) ||
		errors.As(err, &re) && re.Message == rpc.ErrClosed.Error()
}

// failTodo ends a todo whose agent did not start: done, with why, and its agents no longer count for it.
func (h *Hub) failTodo(ctx context.Context, id, reason string) error {
	if err := h.ensureTodosLoaded(ctx); err != nil {
		return err
	}
	now := time.Now().UTC()
	doneAt := now
	if h.store != nil {
		row, err := h.store.FailTodo(ctx, id, reason, now)
		if err != nil {
			return err
		}
		if row.DoneAt != nil {
			doneAt = *row.DoneAt
		}
	}
	affected := map[string]bool{}
	h.todos.mu.Lock()
	t := h.todos.todos[id]
	if t != nil {
		if t.DoneAt == nil {
			t.DoneAt = &doneAt
		}
		t.StartError = reason
		t.UpdatedAt = now
		for _, a := range t.Agents {
			key := [2]string{a.MachineID, a.SessionID}
			if h.todos.activeBySession[key] == id {
				delete(h.todos.activeBySession, key)
				affected[a.MachineID] = true
			}
		}
	}
	h.todos.mu.Unlock()
	if t == nil {
		return db.ErrTodoNotFound
	}
	for mid := range affected {
		if m := h.localMachine(mid); m != nil {
			h.sessionsChanged(m)
		}
	}
	h.broadcast(map[string]any{"type": "todos"})
	return nil
}

// startTaskAgent starts the agent for t on its machine and answers its session id.
func (h *Hub) startTaskAgent(ctx context.Context, t PlanTask) (string, error) {
	m := h.machineByName(t.Machine)
	if m == nil {
		return "", fmt.Errorf("unknown machine %q", t.Machine)
	}
	params := map[string]any{"cwd": t.Cwd, "launcher": t.Launcher, "prompt": t.Prompt, "tab_name": h.autoTabName(ctx, t.Title)}
	if t.Model != "" {
		params["model"] = t.Model
	}
	if t.Permission != "" {
		params["permission_mode"] = t.Permission
	}
	if err := h.applyModel(ctx, "agent.new", params); err != nil {
		return "", err
	}
	var res struct {
		Session proto.Session `json:"session"`
	}
	if err := m.call(ctx, "session.start", params, &res); err != nil {
		return "", err
	}
	return res.Session.SessionID, nil
}

func (h *Hub) dismissPlan(ctx context.Context, id string) (*TaskPlan, error) {
	p, err := h.loadPlan(ctx, id)
	if err != nil {
		return nil, err
	}
	if p.State != planOpen {
		return nil, errPlanNotOpen
	}
	p.State = planDismissed
	ok, err := h.movePlan(ctx, p, planOpen)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errPlanNotOpen
	}
	h.broadcast(map[string]any{"type": "task_plan", "plan": p})
	return p, nil
}

// ---- following a task's agents ----

// taskOf is the task (a todo opened from an assistant conversation) the agent mid/sid works for.
func (h *Hub) taskOf(mid, sid string) (Todo, bool) {
	h.todos.mu.RLock()
	defer h.todos.mu.RUnlock()
	id, ok := h.todos.activeBySession[[2]string{mid, sid}]
	if !ok {
		return Todo{}, false
	}
	t := h.todos.todos[id]
	if t == nil || t.Origin == "" || t.DoneAt != nil {
		return Todo{}, false
	}
	return copyTodoLocked(t), true
}

// withTask names, on an interaction sent to clients, the task its agent works for (the watch and the cards show it).
func (h *Hub) withTask(ix proto.Interaction) proto.Interaction {
	if t, ok := h.taskOf(ix.MachineID, ix.SessionID); ok {
		ix.TaskTitle = t.Title
	}
	return ix
}

// taskAgents are the sessions of t's active agents, as the clients see them (with state).
func (h *Hub) taskAgents(t Todo) []proto.Session {
	var out []proto.Session
	for _, a := range t.Agents {
		if a.ReleasedAt != nil || a.DismissedAt != nil {
			continue
		}
		m := h.localMachine(a.MachineID)
		if m == nil {
			continue
		}
		for _, s := range h.decorated(m) {
			if s.SessionID == a.SessionID {
				out = append(out, s)
				break
			}
		}
	}
	return out
}

// taskWorking: one of the task's agents started a turn.
func (h *Hub) taskWorking(mid, sid string) {
	t, ok := h.taskOf(mid, sid)
	if !ok {
		return
	}
	h.tasks.mu.Lock()
	h.tasks.working[t.ID] = time.Now()
	h.tasks.mu.Unlock()
}

type receiptBody struct {
	Title   string `json:"title"`
	State   string `json:"state"` // done, failed
	Summary string `json:"summary,omitempty"`
	PR      string `json:"pr,omitempty"`
	Machine string `json:"machine,omitempty"`
}

var prLink = regexp.MustCompile(`https://github\.com/[\w.-]+/[\w.-]+/pull/\d+`)

// taskTurnEnded: one of the task's agents ended a turn. Once none of them works or waits for the user any more, the
// conversation gets a receipt (once per stretch of work), and the todo is closed if the judge finds the work finished.
func (m *Machine) taskTurnEnded(sid string) {
	h := m.hub
	time.Sleep(taskSettleDelay(h.cfg.Notify)) // a turn that ends to ask, or goes on at once, is not the end
	t, ok := h.taskOf(m.cfg.ID, sid)
	if !ok {
		return
	}
	agents := h.taskAgents(t)
	var last proto.Session
	for _, s := range agents {
		if s.State == proto.StateWorking || s.State == proto.StateNeedsYou || s.PendingInteractions > 0 {
			return
		}
		if s.UpdatedAt.After(last.UpdatedAt) {
			last = s
		}
	}
	h.tasks.mu.Lock()
	since, worked := h.tasks.working[t.ID]
	if !worked {
		since = t.CreatedAt // a hub restarted since it began: count from the start
	}
	if r, ok := h.tasks.receipted[t.ID]; ok && !r.Before(since) {
		h.tasks.mu.Unlock()
		return
	}
	h.tasks.receipted[t.ID] = time.Now()
	h.tasks.mu.Unlock()
	body := receiptBody{Title: t.Title, State: "done", Summary: clip(strings.TrimSpace(last.LastText), 300), Machine: m.cfg.Name}
	if body.Summary == "" {
		body.Summary = "完成了"
	}
	if pr := prLink.FindAllString(last.LastText, -1); len(pr) > 0 {
		body.PR = pr[len(pr)-1]
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := h.appendFeed(ctx, t.Origin, feedReceipt, t.ID, body); err != nil {
		h.logf("task %s: receipt: %v", t.ID, err)
	}
	h.autoFinishTask(t, since, worked) // the work may be finished: then the todo is done (tasks_done.go)
}

// routeToTask sends text straight to the agent of task todoID that most needs it, and keeps it in the conversation.
func (h *Hub) routeToTask(ctx context.Context, conversation, todoID, text string) (FeedItem, error) {
	if err := h.ensureTodosLoaded(ctx); err != nil {
		return FeedItem{}, err
	}
	h.todos.mu.RLock()
	tp := h.todos.todos[todoID]
	var t Todo
	if tp != nil {
		t = copyTodoLocked(tp)
	}
	h.todos.mu.RUnlock()
	if tp == nil || t.Origin != conversation {
		return FeedItem{}, errors.New("这段对话里没有这个任务")
	}
	agents := h.taskAgents(t)
	if len(agents) == 0 {
		return FeedItem{}, errors.New("这个任务没有在跑的 Agent")
	}
	rank := map[string]int{proto.StateNeedsYou: 0, proto.StateWorking: 1, proto.StateUnread: 2}
	sort.SliceStable(agents, func(i, j int) bool {
		ri, ok := rank[agents[i].State]
		if !ok {
			ri = 3
		}
		rj, ok := rank[agents[j].State]
		if !ok {
			rj = 3
		}
		if ri != rj {
			return ri < rj
		}
		return agents[i].UpdatedAt.After(agents[j].UpdatedAt)
	})
	s := agents[0]
	m := h.localMachine(s.MachineID)
	if m == nil {
		return FeedItem{}, errors.New("那台电脑不在线")
	}
	if err := m.call(ctx, "session.send", map[string]any{"session_id": s.SessionID, "text": text}, nil); err != nil {
		return FeedItem{}, err
	}
	return h.appendFeed(ctx, conversation, feedRouted, todoID, map[string]string{"text": text})
}

// ---- messages about a task ----

// messageBody is a message the inbox's AI 分身 drafted a reply to, about one of the user's tasks: what the card shows
// until the item is sent or done (the app sends the draft through the inbox, by the item's id).
type messageBody struct {
	TodoID  string `json:"todo_id"`
	Source  string `json:"source"`            // lark | mail | …
	Account string `json:"account,omitempty"` // 飞书 / the mail address
	Chat    string `json:"chat"`              // the chat's name / the mail's subject
	Sender  string `json:"sender,omitempty"`
	Text    string `json:"text"` // the message, clipped
	Draft   string `json:"draft"`
}

// messageCardOf is the card for an item whose draft is about a task; nil when it is about none. The inbox is locked.
func messageCardOf(it *InboxItem) *messageBody {
	if it.Agent == nil || it.Agent.TodoID == "" || it.Agent.Draft == "" {
		return nil
	}
	return &messageBody{TodoID: it.Agent.TodoID, Source: it.Source, Account: it.AccountName, Chat: it.Title, Sender: it.Sender,
		Text: clip(strings.TrimSpace(it.Preview), 400), Draft: it.Agent.Draft}
}

// messageCard puts the card into the conversation that opened the task (none when the task is gone or done).
func (h *Hub) messageCard(ctx context.Context, itemID string, body messageBody) {
	h.todos.mu.RLock()
	t := h.todos.todos[body.TodoID]
	origin := ""
	if t != nil && t.DoneAt == nil {
		origin = t.Origin
	}
	h.todos.mu.RUnlock()
	if origin == "" {
		return
	}
	if _, err := h.appendFeed(ctx, origin, feedMessage, itemID, body); err != nil {
		h.logf("task %s: message card: %v", body.TodoID, err)
	}
}

// inboxTasks are the tasks going on, as the inbox's judge and AI 分身 see them.
func (h *Hub) inboxTasks() []prompts.InboxTask {
	var out []prompts.InboxTask
	for _, t := range h.snapshotTasks() {
		if t.State == taskStateLabel("done") || t.State == taskStateLabel("auto_done") || t.State == taskStateLabel("failed") {
			continue
		}
		out = append(out, prompts.InboxTask{ID: t.ID, Title: t.Title, State: t.State, Last: t.Last})
	}
	return out
}

// ---- what the assistant sees ----

// snapshotTasks are the tasks opened from assistant conversations that are going on or ended in the last day.
func (h *Hub) snapshotTasks() []prompts.SnapshotTask {
	h.todos.mu.RLock()
	var list []Todo
	for _, t := range h.todos.todos {
		if t.Origin != "" && (t.DoneAt == nil || time.Since(*t.DoneAt) < 24*time.Hour) && time.Since(t.UpdatedAt) < 7*24*time.Hour {
			list = append(list, copyTodoLocked(t))
		}
	}
	h.todos.mu.RUnlock()
	sort.Slice(list, func(i, j int) bool { return list[i].CreatedAt.After(list[j].CreatedAt) })
	var out []prompts.SnapshotTask
	for _, t := range list {
		agents := h.taskAgents(t)
		st := taskState(t, agents)
		if st == "followup" && time.Since(t.UpdatedAt) > 24*time.Hour {
			continue
		}
		if st == "done" && t.DoneBy == db.DoneByAuto {
			st = "auto_done"
		}
		task := prompts.SnapshotTask{ID: t.ID, Title: t.Title, State: taskStateLabel(st)}
		var last proto.Session
		for _, s := range agents {
			if s.UpdatedAt.After(last.UpdatedAt) {
				last = s
			}
		}
		if last.SessionID != "" {
			task.Machine, task.Session = last.MachineID, short(last.SessionID)
			task.Last = clip(oneLine(last.LastText), 80)
		}
		out = append(out, task)
		if len(out) == 12 {
			break
		}
	}
	return out
}

// taskState follows docs/todos.md「状态计算规则」(clients compute the same).
func taskState(t Todo, agents []proto.Session) string {
	if t.DoneAt != nil && t.StartError != "" {
		return "failed"
	}
	if t.DoneAt != nil {
		return "done"
	}
	has := map[string]bool{}
	for _, s := range agents {
		has[s.State] = true
	}
	switch {
	case has[proto.StateNeedsYou]:
		return "needs_you"
	case has[proto.StateUnread]:
		return "unread"
	case has[proto.StateWorking]:
		return "working"
	case len(agents) > 0:
		return "followup"
	}
	return "not_started"
}

func taskStateLabel(st string) string {
	return map[string]string{"done": "已完成", "auto_done": "已完成（hub 判断做完了，自动关的）", "failed": "没开起来", "needs_you": "等用户", "unread": "做完了（用户还没看）", "working": "在做",
		"followup": "停着", "not_started": "还没开始"}[st]
}

// ---- HTTP ----

func (h *Hub) taskRoutes(api *http.ServeMux) {
	h.doneCheckRoute(api)
	api.HandleFunc("GET /api/v1/task-plans/{id}", func(w http.ResponseWriter, r *http.Request) {
		p, err := h.loadPlan(r.Context(), r.PathValue("id"))
		if err != nil {
			planErr(w, err)
			return
		}
		writeJSON(w, map[string]any{"plan": p})
	})
	api.HandleFunc("POST /api/v1/task-plans/{id}/start", func(w http.ResponseWriter, r *http.Request) {
		if principalOf(r.Context()).Role != roleFull {
			writeErr(w, http.StatusForbidden, errors.New("read-only role cannot start tasks"))
			return
		}
		var in struct {
			Tasks    []PlanTask `json:"tasks"`
			Remember bool       `json:"remember"`
		}
		if r.ContentLength != 0 {
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&in); err != nil {
				writeErr(w, http.StatusBadRequest, err)
				return
			}
		}
		p, err := h.startPlan(r.Context(), r.PathValue("id"), in.Tasks, in.Remember)
		if err != nil {
			planErr(w, err)
			return
		}
		writeJSON(w, map[string]any{"plan": p})
	})
	api.HandleFunc("POST /api/v1/task-plans/{id}/dismiss", func(w http.ResponseWriter, r *http.Request) {
		if principalOf(r.Context()).Role != roleFull {
			writeErr(w, http.StatusForbidden, errors.New("read-only role cannot change tasks"))
			return
		}
		p, err := h.dismissPlan(r.Context(), r.PathValue("id"))
		if err != nil {
			planErr(w, err)
			return
		}
		writeJSON(w, map[string]any{"plan": p})
	})
	api.HandleFunc("GET /api/v1/assistant/conversations/{id}/feed", func(w http.ResponseWriter, r *http.Request) {
		var since time.Time
		if s := r.URL.Query().Get("since"); s != "" {
			t, err := time.Parse(time.RFC3339Nano, s)
			if err != nil {
				writeErr(w, http.StatusBadRequest, fmt.Errorf("since: %v", err))
				return
			}
			since = t
		}
		items, plans, err := h.feed(r.Context(), r.PathValue("id"), since)
		if err != nil {
			writeErr(w, http.StatusServiceUnavailable, err)
			return
		}
		if items == nil {
			items = []FeedItem{}
		}
		writeJSON(w, map[string]any{"items": items, "plans": plans})
	})
	api.HandleFunc("POST /api/v1/assistant/conversations/{id}/route", func(w http.ResponseWriter, r *http.Request) {
		if principalOf(r.Context()).Role != roleFull {
			writeErr(w, http.StatusForbidden, errors.New("read-only role cannot send"))
			return
		}
		var in struct {
			TodoID string `json:"todo_id"`
			Text   string `json:"text"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&in); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		if strings.TrimSpace(in.Text) == "" || in.TodoID == "" {
			writeErr(w, http.StatusBadRequest, errors.New("todo_id and text are required"))
			return
		}
		it, err := h.routeToTask(r.Context(), r.PathValue("id"), in.TodoID, in.Text)
		if err != nil {
			writeErr(w, http.StatusConflict, err)
			return
		}
		writeJSON(w, map[string]any{"item": it})
	})
}

func planErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errPlanNotFound):
		writeErr(w, http.StatusNotFound, err)
	case errors.Is(err, errPlanNotOpen):
		writeErr(w, http.StatusConflict, err)
	default:
		writeErr(w, http.StatusServiceUnavailable, err)
	}
}

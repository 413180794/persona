package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path"
	"regexp"
	"slices"
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
	// retrying: the tasks whose agent is starting again after a failed start (one start at a time, #1396).
	retrying map[string]bool
	// cardSends: card id → the lock its 发送 holds (sendFromCard).
	cardSends sync.Map
}

func newTasksState() tasksState {
	return tasksState{plans: map[string]*TaskPlan{}, feed: map[string][]FeedItem{},
		working: map[string]time.Time{}, receipted: map[string]time.Time{}, retrying: map[string]bool{}}
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

// feed is a conversation's items after since, oldest first (with the message cards sent after since: the client
// has them, not that they went out), and the plans they name.
func (h *Hub) feed(ctx context.Context, conversation string, since time.Time) ([]FeedItem, map[string]*TaskPlan, error) {
	var items []FeedItem
	if h.store == nil {
		h.tasks.mu.Lock()
		for _, it := range h.tasks.feed[conversation] {
			if at := cardSentAt(it); it.At.After(since) || it.Kind == feedMessage && at != nil && at.After(since) {
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
	h.settleCards(ctx, items)
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
	// The plan goes in the item's body too: a client that missed the task_plan event (or raced it) still draws the
	// card from the conversation itself.
	if _, err := h.appendFeed(ctx, sessionID, feedPlan, p.ID, map[string]any{"plan": p}); err != nil {
		h.logf("task plan %s: feed: %v", p.ID, err)
	}
	if !h.planStartsAtOnce(ctx, p.Tasks) {
		h.broadcast(map[string]any{"type": "task_plan", "plan": p})
		// A card below the answer is easy to miss: push it as well.
		titles := make([]string, 0, len(p.Tasks))
		for _, t := range p.Tasks {
			titles = append(titles, t.Title)
		}
		title := fmt.Sprintf("有 %d 件事等你开始", len(p.Tasks))
		if len(titles) == 1 {
			title = "有一件事等你开始"
		}
		machineName := "Persona"
		if m := h.localMachine(h.assistantConfig(ctx).MachineID); m != nil {
			machineName = m.cfg.Name
		}
		h.notifyDone("assistant", h.assistantConfig(ctx).MachineID, machineName, sessionID, title, strings.Join(titles, "、"), pushWants{})
		return fmt.Sprintf("Plan %s is on the user's screen as a card and pushed to their phone; nothing has started. "+
			"Don't wait for it: carry on. When they start it, the tasks show up in the context snapshot.", p.ID), nil
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
			go h.startPlanTask(p.ID, p.Conversation, i, p.Tasks[i], false)
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

// taskStartTimeout bounds one start of a task's agent: a busy machine's shell can take minutes to run claude
// (node's claudeStartWait, #1396).
const taskStartTimeout = 5 * time.Minute

// startPlanTask starts one task's agent and links it to the task's todo. A failure ends the task: the todo is done
// with why (so nothing shows it as going on, #1077), the plan keeps the error, and the conversation gets a receipt —
// one per task: a retry that fails again (retry) updates the error the receipt shows (clients read it off the todo).
func (h *Hub) startPlanTask(planID, conversation string, i int, t PlanTask, retry bool) {
	ctx, cancel := context.WithTimeout(context.Background(), taskStartTimeout)
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
	if retry {
		return
	}
	_, _ = h.appendFeed(ctx, conversation, feedReceipt, t.TodoID, receiptBody{Title: t.Title, State: "failed",
		Summary: "没开起来：" + reason})
}

var (
	errTaskNotRetryable = errors.New("只有助手派出、没开起来的任务能重试")
	errTaskNotInPlan    = errors.New("找不到这个任务的计划，没法照原样再开一次")
	errMachineOffline   = errors.New("现在离线，等它连上再重试")
)

// retryTask starts again the agent of a task that did not start (#1396): the same task as its plan has it (machine,
// directory, prompt, launcher, model, permission, title), in the same todo — reopened, its error and the plan's
// cleared — so the task shows as starting where it was, not as a second one. A task already starting again, or
// started since, is answered as it is: a double tap starts one agent. A machine that is offline is refused before
// anything changes.
func (h *Hub) retryTask(ctx context.Context, todoID string) (Todo, error) {
	if err := h.ensureTodosLoaded(ctx); err != nil {
		return Todo{}, err
	}
	cur, ok := h.todoNow(todoID)
	if !ok {
		return Todo{}, db.ErrTodoNotFound
	}
	if cur.PlanID == "" || cur.Origin == "" {
		return Todo{}, errTaskNotRetryable
	}
	h.tasks.mu.Lock()
	busy := h.tasks.retrying[todoID]
	if !busy && cur.DoneAt != nil && cur.StartError != "" {
		h.tasks.retrying[todoID] = true
	}
	h.tasks.mu.Unlock()
	if busy || cur.DoneAt == nil || cur.StartError == "" {
		if !busy && cur.DoneAt != nil {
			return Todo{}, errTaskNotRetryable // done by hand or by the hub, not a failed start
		}
		return formatTodo(cur), nil
	}
	started := false
	defer func() {
		if !started {
			h.tasks.mu.Lock()
			delete(h.tasks.retrying, todoID)
			h.tasks.mu.Unlock()
		}
	}()
	p, err := h.loadPlan(ctx, cur.PlanID)
	if err != nil {
		return Todo{}, err
	}
	i := slices.IndexFunc(p.Tasks, func(t PlanTask) bool { return t.TodoID == todoID })
	if i < 0 {
		return Todo{}, errTaskNotInPlan
	}
	t := p.Tasks[i]
	m := h.machineByName(t.Machine)
	if m == nil {
		return Todo{}, fmt.Errorf("没有这台电脑 %q", t.Machine)
	}
	if !m.reachable() {
		return Todo{}, fmt.Errorf("「%s」%w", m.cfg.Name, errMachineOffline)
	}

	now := time.Now().UTC()
	var out Todo
	if h.store != nil {
		row, ok, err := h.store.RetryTodo(ctx, todoID, now)
		if err != nil {
			return Todo{}, err
		}
		if !ok { // another hub sharing the store reopened it first
			cur, _ := h.todoNow(todoID)
			return formatTodo(cur), nil
		}
		out, _ = h.applyTodoRow(todoID, *row, nil)
	} else {
		row := db.TodoRow{ID: cur.ID, Title: cur.Title, Notes: cur.Notes, CreatedAt: cur.CreatedAt, UpdatedAt: now,
			Origin: cur.Origin, PlanID: cur.PlanID, ReopenedAt: &now}
		out, _ = h.applyTodoRow(todoID, row, nil)
	}
	p.Tasks[i].Error = ""
	if _, err := h.movePlan(ctx, p, planStarted); err != nil {
		h.logf("plan %s: %v", p.ID, err)
	}
	h.broadcast(map[string]any{"type": "task_plan", "plan": p})
	h.logf("task %s: retrying its start on %s", todoID, m.cfg.ID)
	started = true
	go func() {
		defer func() {
			h.tasks.mu.Lock()
			delete(h.tasks.retrying, todoID)
			h.tasks.mu.Unlock()
		}()
		h.startPlanTask(p.ID, p.Conversation, i, t, true)
	}()
	return out, nil
}

// todoNow is a copy of todo id as the hub has it.
func (h *Hub) todoNow(id string) (Todo, bool) {
	h.todos.mu.RLock()
	defer h.todos.mu.RUnlock()
	t, ok := h.todos.todos[id]
	if !ok {
		return Todo{}, false
	}
	return copyTodoLocked(t), true
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
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// The whole last reply, as Markdown: the session list's last_text is one line cut at 200 characters.
	text := strings.TrimSpace(last.LastText)
	if lm := h.localMachine(last.MachineID); lm != nil {
		if _, answer := lastTurn(ctx, lm, last.SessionID); answer != "" {
			text = answer
		}
	}
	body := receiptBody{Title: t.Title, State: "done", Summary: clipMarkdown(text, 600), Machine: m.cfg.Name}
	if body.Summary == "" {
		body.Summary = "完成了"
	}
	if pr := prLink.FindAllString(text, -1); len(pr) > 0 {
		body.PR = pr[len(pr)-1]
	}
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
	// SentAt: the reply to the item went out (from this card or from the inbox, on any device) and Sent is what it
	// said; set by markCardsSent, never cleared, so the card stays sent when the item opens again. A card sent before
	// the hub recorded it has no Sent (the draft is what went out, as far as anyone knows).
	SentAt *time.Time `json:"sent_at,omitempty"`
	Sent   string     `json:"sent,omitempty"`
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

// markCardsSent records on the cards about an inbox item put in by at that its reply went out (text: what it said;
// empty: not known) and tells the clients (tell), which draw them sent; the cards it changed.
func (h *Hub) markCardsSent(ctx context.Context, itemID string, at time.Time, text string, tell bool) []FeedItem {
	at = at.UTC()
	patch, _ := json.Marshal(struct {
		SentAt time.Time `json:"sent_at"`
		Sent   string    `json:"sent,omitempty"`
	}{at, text})
	var out []FeedItem
	if h.store == nil {
		h.tasks.mu.Lock()
		for c, list := range h.tasks.feed {
			for i, it := range list {
				if it.Kind != feedMessage || it.Ref != itemID || it.At.After(at) || cardSentAt(it) != nil {
					continue
				}
				var body map[string]json.RawMessage
				_ = json.Unmarshal(it.Body, &body)
				if body == nil {
					body = map[string]json.RawMessage{}
				}
				var p map[string]json.RawMessage
				_ = json.Unmarshal(patch, &p)
				for k, v := range p {
					body[k] = v
				}
				it.Body, _ = json.Marshal(body)
				h.tasks.feed[c][i] = it
				out = append(out, it)
			}
		}
		h.tasks.mu.Unlock()
	} else {
		rows, err := h.store.MarkFeedSent(ctx, feedMessage, itemID, at, patch)
		if err != nil {
			h.logf("inbox %s: mark the cards sent: %v", itemID, err)
		}
		for _, r := range rows {
			out = append(out, FeedItem{ID: r.ID, Conversation: r.Conversation, At: r.At, Kind: r.Kind, Ref: r.Ref, Body: r.Body})
		}
	}
	if tell {
		for _, it := range out {
			h.broadcast(map[string]any{"type": "assistant_feed", "conversation": it.Conversation, "item": it})
		}
	}
	return out
}

// cardSentAt is when a message card's reply went out; nil while it has not.
func cardSentAt(it FeedItem) *time.Time {
	var b struct {
		SentAt *time.Time `json:"sent_at"`
	}
	_ = json.Unmarshal(it.Body, &b)
	return b.SentAt
}

// settleCards fills in the cards among items whose item was replied to after they were put in but which do not say
// so: cards from before the hub recorded it on them (the reply went out, the card still offered 发送).
func (h *Hub) settleCards(ctx context.Context, items []FeedItem) {
	for _, it := range items {
		if it.Kind != feedMessage || cardSentAt(it) != nil {
			continue
		}
		h.inbox.mu.Lock()
		var at *time.Time
		if ib := h.inbox.items[it.Ref]; ib != nil && ib.DoneBy == "replied" && ib.DoneAt != nil && !ib.DoneAt.Before(it.At) {
			t := *ib.DoneAt
			at = &t
		}
		h.inbox.mu.Unlock()
		if at == nil {
			continue
		}
		for _, m := range h.markCardsSent(ctx, it.Ref, *at, "", false) {
			for j := range items {
				if items[j].ID == m.ID {
					items[j] = m
				}
			}
		}
	}
}

// sendFromCard is 发送 on a message card (POST /api/v1/inbox/{id}/reply with card): the reply goes out once per
// card however many taps, devices or retries ask — a card already sent answers with the item as it is (already).
func (h *Hub) sendFromCard(ctx context.Context, cardID, itemID string, send func() (InboxItem, error)) (it InboxItem, already bool, err error) {
	l, _ := h.tasks.cardSends.LoadOrStore(cardID, &sync.Mutex{})
	mu := l.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()
	card, err := h.feedItem(ctx, cardID)
	if err != nil {
		return InboxItem{}, false, err
	}
	if card != nil && card.Kind == feedMessage && card.Ref == itemID && cardSentAt(*card) != nil {
		h.inbox.mu.Lock()
		if ib := h.inbox.items[itemID]; ib != nil {
			it = ib.listed()
		}
		h.inbox.mu.Unlock()
		return it, true, nil
	}
	it, err = send()
	return it, false, err
}

// feedItem is one feed item by its id; nil when there is none.
func (h *Hub) feedItem(ctx context.Context, id string) (*FeedItem, error) {
	if h.store == nil {
		h.tasks.mu.Lock()
		defer h.tasks.mu.Unlock()
		for _, list := range h.tasks.feed {
			for _, it := range list {
				if it.ID == id {
					return &it, nil
				}
			}
		}
		return nil, nil
	}
	r, err := h.store.FeedItem(ctx, id)
	if err != nil || r == nil {
		return nil, err
	}
	return &FeedItem{ID: r.ID, Conversation: r.Conversation, At: r.At, Kind: r.Kind, Ref: r.Ref, Body: r.Body}, nil
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
	api.HandleFunc("POST /api/v1/todos/{id}/retry", func(w http.ResponseWriter, r *http.Request) {
		if principalOf(r.Context()).Role != roleFull {
			writeErr(w, http.StatusForbidden, errors.New("read-only role cannot start tasks"))
			return
		}
		t, err := h.retryTask(r.Context(), r.PathValue("id"))
		switch {
		case err == nil:
			writeJSON(w, map[string]any{"todo": t})
		case errors.Is(err, db.ErrTodoNotFound):
			writeErr(w, http.StatusNotFound, err)
		case errors.Is(err, errTaskNotRetryable), errors.Is(err, errTaskNotInPlan), errors.Is(err, errMachineOffline):
			writeErr(w, http.StatusConflict, err)
		default:
			planErr(w, err)
		}
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

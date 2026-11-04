package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/apierr"
	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/proto"
)

// fakeHub is the hub as the assistant sees it in unit tests: a profile, the machines with their sessions, a node on
// each that starts and continues conversations (s1, s2…; resume_only refuses one it doesn't know, like a transcript
// that was cleaned up), a model, and what the assistant pushed and reported.
type fakeHub struct {
	mu            sync.Mutex
	hello         Hello
	sessions      map[string][]proto.Session // by machine: the machines there are
	items         []proto.Item               // session.items answers this
	calls         []DeviceCall
	known         map[string]bool  // conversations the nodes have
	made          int              // conversations started, for the next id
	sends         []map[string]any // assistant.send / assistant.start params, and {"rename": params}
	kept          []string         // the last node.configure keep_sessions
	launcher      string           // what the model configuration runs a purpose on ("" = claude)
	kbReady       bool
	slowSnapshots time.Duration // the connectors' live states come this late (nothing: there are none)
	replies       []string      // the model's answers, in turn (the last one again when they run out)
	noModel       bool
	asked         []modelCall
	pushes        []Notification
	events        []string // the type of each event the apps were sent
	snaps         []Snapshot

	// Agents a task starts (session.start a1, a2…) and what was sent to them; a machine offline, a node refusing.
	starts    []map[string]any
	agentSent []map[string]any
	agents    int
	offline   map[string]bool
	failStart error
	judged    int // models.json calls (the done judge)
	judge     string

	// The hub's todos app. quietOff: 主对话派的任务只在主对话里通知 is off (tasks from the main conversation are quiet
	// otherwise, #1731).
	quietOff  bool
	todos     map[string]*Todo
	active    map[[2]string]string // (machine, session) → the open todo it works for
	todosMade int
}

type modelCall struct{ System, User string }

func newFakeHub() *fakeHub {
	return &fakeHub{sessions: map[string][]proto.Session{}, known: map[string]bool{}, offline: map[string]bool{},
		todos: map[string]*Todo{}, active: map[[2]string]string{}}
}

// withMachine adds the assistant's machine "server" (no sessions yet).
func (f *fakeHub) withMachine() *fakeHub {
	f.sessions["server"] = nil
	f.hello.Assistant.MachineID = "server"
	return f
}

func (f *fakeHub) Hello(context.Context) (Hello, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hello, nil
}

func (f *fakeHub) launcherLocked() string {
	if f.launcher == "" {
		return "claude"
	}
	return f.launcher
}

func (f *fakeHub) DeviceCall(_ context.Context, p DeviceCall) (json.RawMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, p)
	if p.Device == DeviceAll && p.Method == DeviceDevicesList {
		devices := []Device{}
		for id := range f.sessions {
			devices = append(devices, Device{ID: id, Name: id, Online: true, Reachable: true})
		}
		return json.Marshal(map[string]any{"devices": devices})
	}
	list, ok := f.sessions[p.Device]
	if !ok {
		return nil, fmt.Errorf("%w %q", ErrNoDevice, p.Device)
	}
	var a map[string]any
	_ = json.Unmarshal(p.Params, &a)
	switch p.Method {
	case DeviceSessionsList:
		return json.Marshal(map[string]any{"sessions": list})
	case DeviceSessionGet:
		for _, s := range list {
			if s.SessionID == a["session_id"] {
				return json.Marshal(map[string]any{"session": s, "ok": true})
			}
		}
		return json.Marshal(map[string]any{"ok": false})
	case DeviceSessionItems:
		return json.Marshal(map[string]any{"items": f.items})
	case DeviceSessionRename:
		f.sends = append(f.sends, map[string]any{"rename": a})
		return json.RawMessage(`{"ok":true}`), nil
	case DeviceNodeConfigure:
		f.kept = nil
		for _, k := range a["keep_sessions"].([]any) {
			f.kept = append(f.kept, k.(string))
		}
		return json.RawMessage(`{}`), nil
	case DeviceAssistantStart: // the hub applies the model configuration: the launcher it runs on
		a["launcher"] = f.launcherLocked()
		f.sends = append(f.sends, a)
		f.made++
		id := fmt.Sprintf("s%d", f.made)
		f.known[id] = true
		return json.Marshal(map[string]any{"session": proto.Session{SessionID: id, MachineID: p.Device, Launcher: f.launcherLocked(),
			Assistant: true, Status: proto.StatusWorking}})
	case DeviceSessionStart:
		if f.offline[p.Device] {
			return nil, fmt.Errorf("%s: %w", p.Device, apierr.ErrOffline)
		}
		f.starts = append(f.starts, a)
		if f.failStart != nil {
			return nil, f.failStart
		}
		f.agents++
		return json.Marshal(map[string]any{"session": proto.Session{SessionID: fmt.Sprintf("s%d", f.agents), Cwd: a["cwd"].(string), Agent: "claude"}})
	case DeviceSessionSend:
		f.agentSent = append(f.agentSent, a)
		return json.RawMessage(`{}`), nil
	case DeviceAssistantSend:
		f.sends = append(f.sends, a)
		id, _ := a["session_id"].(string)
		if a["resume_only"] == true && !f.known[id] {
			return nil, fmt.Errorf("conversation %s is gone", id)
		}
		return json.Marshal(map[string]any{"session_id": id})
	}
	return json.RawMessage(`{}`), nil
}

func (f *fakeHub) AppCall(ctx context.Context, p AppCall) (json.RawMessage, error) {
	if p.AppID == AppConnectors && p.Method == ConnectorsSnapshots {
		f.mu.Lock()
		slow := f.slowSnapshots
		f.mu.Unlock()
		if slow == 0 {
			return json.Marshal(map[string]any{"snapshots": []any{}})
		}
		select {
		case <-time.After(slow):
			return nil, errors.New("too late")
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	switch p.AppID + " " + p.Method {
	case AppModels + " " + ModelsComplete:
		if f.noModel {
			return nil, errors.New("no model")
		}
		var a struct {
			System string `json:"system"`
			Input  string `json:"input"`
		}
		_ = json.Unmarshal(p.Params, &a)
		f.asked = append(f.asked, modelCall{a.System, a.Input})
		reply := f.replies[min(len(f.asked), len(f.replies))-1]
		return json.Marshal(map[string]any{"text": reply})
	case AppModels + " " + ModelsResolve:
		return json.Marshal(ModelBinding{Provider: "launcher." + f.launcherLocked()})
	case AppModels + " " + ModelsApply, AppModels + " " + ModelsAssistantSend:
		var a struct {
			Params map[string]any `json:"params"`
		}
		_ = json.Unmarshal(p.Params, &a)
		a.Params["launcher"] = f.launcherLocked()
		return json.Marshal(map[string]any{"params": a.Params})
	case AppKB + " " + KBStatus:
		return json.Marshal(map[string]any{"ready": f.kbReady})
	case AppInbox + " " + InboxReplied:
		return json.Marshal(map[string]any{"replied": map[string]any{}})
	case AppModels + " " + ModelsTabName:
		return json.Marshal(map[string]any{"name": ""})
	case AppModels + " " + ModelsJSON:
		f.judged++
		if f.judge == "" {
			return nil, errors.New("no model")
		}
		return json.RawMessage(f.judge), nil
	case AppTodos + " " + p.Method:
		return f.todosCall(p.Method, p.Params)
	}
	return nil, fmt.Errorf("unexpected %s %s", p.AppID, p.Method)
}

func (f *fakeHub) NotifyRequest(_ context.Context, p NotifyRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p.Notification.Kind != NotifyPush {
		return fmt.Errorf("kind %q", p.Notification.Kind)
	}
	f.pushes = append(f.pushes, p.Notification)
	return nil
}

func (f *fakeHub) Broadcast(_ context.Context, b Broadcast) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var ev struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(b.Message, &ev)
	f.events = append(f.events, ev.Type)
}

func (f *fakeHub) Snapshot(_ context.Context, s Snapshot) (SnapshotResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.snaps = append(f.snaps, s)
	return SnapshotResult{OK: true, Seq: s.Seq}, nil
}

func (f *fakeHub) modelCalls() []modelCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]modelCall(nil), f.asked...)
}

func (f *fakeHub) lastSend(t *testing.T) map[string]any {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sends) == 0 {
		t.Fatal("nothing was sent")
	}
	return f.sends[len(f.sends)-1]
}

func (f *fakeHub) sent() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sends)
}

func (f *fakeHub) forget(id string) {
	f.mu.Lock()
	delete(f.known, id)
	f.mu.Unlock()
}

// setSessions replaces the sessions machine has.
func (f *fakeHub) setSessions(machine string, list ...proto.Session) {
	f.mu.Lock()
	f.sessions[machine] = list
	f.mu.Unlock()
}

func (f *fakeHub) setItems(items ...proto.Item) {
	f.mu.Lock()
	f.items = items
	f.mu.Unlock()
}

func (f *fakeHub) lastSnapshot() Snapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.snaps) == 0 {
		return Snapshot{}
	}
	return f.snaps[len(f.snaps)-1]
}

func (f *fakeHub) pushed() []Notification {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Notification(nil), f.pushes...)
}

func (f *fakeHub) eventTypes() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.events, " ")
}

// memStore keeps settings as JSON, like Postgres does; it has no plans or feed (a hub without a database keeps those
// in memory, as Service does with no store).
type memStore struct {
	noPlans
	mu   sync.Mutex
	vals map[string][]byte
	puts int
}

// noPlans is a store without the task_plans and assistant_feed tables.
type noPlans struct{}

func (noPlans) CreateTaskPlan(context.Context, db.TaskPlanRow) error {
	return errors.New("no task_plans here")
}
func (noPlans) TaskPlan(context.Context, string) (*db.TaskPlanRow, error) {
	return nil, errors.New("no task_plans here")
}
func (noPlans) UpdateTaskPlan(context.Context, string, string, string, json.RawMessage, time.Time) (bool, error) {
	return false, errors.New("no task_plans here")
}
func (noPlans) AppendFeed(context.Context, db.FeedRow) error {
	return errors.New("no assistant_feed here")
}
func (noPlans) Feed(context.Context, string, time.Time, int) ([]db.FeedRow, error) {
	return nil, errors.New("no assistant_feed here")
}
func (noPlans) FeedItem(context.Context, string) (*db.FeedRow, error) {
	return nil, errors.New("no assistant_feed here")
}
func (noPlans) MarkFeedSent(context.Context, string, string, time.Time, json.RawMessage) ([]db.FeedRow, error) {
	return nil, errors.New("no assistant_feed here")
}

func (m *memStore) Setting(_ context.Context, key string, out any) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.vals[key]
	if !ok {
		return false, nil
	}
	return true, json.Unmarshal(b, out)
}

func (m *memStore) PutSetting(_ context.Context, key string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.vals == nil {
		m.vals = map[string][]byte{}
	}
	m.vals[key] = b
	m.puts++
	return nil
}

// newTestService is an assistant on f with an in-memory store.
func newTestService(t *testing.T, f *fakeHub) *Service {
	t.Helper()
	st := &memStore{}
	return New(Options{Hub: f, Store: func() Store { return st }, Logf: t.Logf})
}

// newStorelessService is an assistant on f with no database: its state lives in memory only.
func newStorelessService(t *testing.T, f *fakeHub) *Service {
	t.Helper()
	return New(Options{Hub: f, Logf: t.Logf})
}

// newServiceOn is an assistant on f with st as its database.
func newServiceOn(t *testing.T, f *fakeHub, st Store) *Service {
	t.Helper()
	return New(Options{Hub: f, Store: func() Store { return st }, Logf: t.Logf})
}

func (f *fakeHub) keptNow() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.kept...)
}

// todosCall is the hub's todos app, kept in memory: a todo, the agents linked to it, done (by the judge, or as a
// task that did not start).
func (f *fakeHub) todosCall(method string, params json.RawMessage) (json.RawMessage, error) {
	var a struct {
		TodoID    string    `json:"todo_id"`
		Title     string    `json:"title"`
		Notes     string    `json:"notes"`
		Origin    string    `json:"origin"`
		PlanID    string    `json:"plan_id"`
		MachineID string    `json:"machine_id"`
		SessionID string    `json:"session_id"`
		Reason    string    `json:"reason"`
		Work      time.Time `json:"work"`
		IfQuiet   bool      `json:"if_quiet"`
	}
	_ = json.Unmarshal(params, &a)
	now := time.Now().UTC()
	quiet := a.Origin != "" && a.Origin == f.hello.Assistant.Main && !f.quietOff
	switch method {
	case TodosLoad:
		return json.RawMessage(`{}`), nil
	case TodosCreate:
		if a.IfQuiet && !quiet {
			return json.RawMessage(`{"todo":null}`), nil
		}
		f.todosMade++
		t := &Todo{ID: fmt.Sprintf("todo_%d", f.todosMade), Title: a.Title, Notes: a.Notes, Origin: a.Origin, PlanID: a.PlanID,
			Quiet: quiet, CreatedAt: now, UpdatedAt: now, Agents: []TodoAgent{}}
		f.todos[t.ID] = t
		return json.Marshal(map[string]any{"todo": t})
	case TodosLinkAgent:
		t := f.todos[a.TodoID]
		if t == nil {
			return nil, errors.New("todo not found")
		}
		t.Agents = append(t.Agents, TodoAgent{MachineID: a.MachineID, SessionID: a.SessionID})
		f.active[[2]string{a.MachineID, a.SessionID}] = t.ID
		return json.RawMessage(`{}`), nil
	case TodosFail, TodosAutoDone:
		t := f.todos[a.TodoID]
		if t == nil {
			return json.Marshal(map[string]any{"ok": false})
		}
		if method == TodosAutoDone && (t.DoneAt != nil || t.ReopenedAt != nil && !t.ReopenedAt.Before(a.Work)) {
			return json.Marshal(map[string]any{"ok": false})
		}
		t.DoneAt, t.UpdatedAt = &now, now
		if method == TodosFail {
			t.StartError = a.Reason
		} else {
			t.DoneBy, t.DoneReason = "auto", a.Reason
		}
		for k, id := range f.active {
			if id == t.ID {
				delete(f.active, k)
			}
		}
		return json.Marshal(map[string]any{"ok": true})
	case TodosGet:
		t := f.todos[a.TodoID]
		if t == nil {
			return json.Marshal(map[string]any{"ok": false})
		}
		return json.Marshal(map[string]any{"todo": t, "ok": true})
	case TodosList:
		list := []*Todo{}
		for _, t := range f.todos {
			if a.Origin == "*" && t.Origin != "" || t.Origin == a.Origin {
				list = append(list, t)
			}
		}
		return json.Marshal(map[string]any{"todos": list})
	case TodosRetry:
		t := f.todos[a.TodoID]
		if t == nil {
			return json.Marshal(map[string]any{"ok": false})
		}
		t.DoneAt, t.StartError, t.ReopenedAt, t.UpdatedAt = nil, "", &now, now
		return json.Marshal(map[string]any{"todo": t, "ok": true})
	case TodosView:
		t := f.todos[a.TodoID]
		return json.Marshal(map[string]any{"todo": t, "ok": t != nil})
	case TodosOfSession:
		t := f.todos[f.active[[2]string{a.MachineID, a.SessionID}]]
		if t == nil || t.Origin == "" || t.DoneAt != nil {
			return json.Marshal(map[string]any{"ok": false})
		}
		return json.Marshal(map[string]any{"todo": t, "ok": true})
	}
	return nil, fmt.Errorf("todos has no method %s", method)
}

// todoNow is todo id as the todos app has it now.
func (f *fakeHub) todoNow(id string) Todo {
	f.mu.Lock()
	defer f.mu.Unlock()
	if t := f.todos[id]; t != nil {
		return *t
	}
	return Todo{}
}

// onlyTodo is the one todo there is ({} when there is none, or more).
func (f *fakeHub) onlyTodo() Todo {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.todos) != 1 {
		return Todo{}
	}
	for _, t := range f.todos {
		return *t
	}
	return Todo{}
}

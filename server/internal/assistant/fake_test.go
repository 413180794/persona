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
	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/kb"
	"github.com/nwverse/persona/server/internal/proto"
)

// fakeHub is the hub as the assistant sees it in unit tests: its hello, the machines with their sessions, a node on
// each that starts and continues conversations (s1, s2…; resume_only refuses one it doesn't know, like a transcript
// that was cleaned up), a model, and what the assistant pushed and reported. settings are the assistant's own a test
// service starts with (newTestService…).
type fakeHub struct {
	mu            sync.Mutex
	hello         Hello
	settings      *Settings
	record        modelRecord                // 模型配置 › 主对话 (models.record / models.save)
	launcherList  []proto.Launcher           // models.launchers
	devices       []Device                   // devices.list answers these when set (else one per machine with sessions)
	gone          map[string]bool            // launchers models.launcher_check says are gone
	sessions      map[string][]proto.Session // by machine: the machines there are
	items         []proto.Item               // session.items answers this
	calls         []DeviceCall
	known         map[string]bool  // conversations the nodes have
	made          int              // conversations started, for the next id
	sends         []map[string]any // assistant.send params, and {"rename": params}
	prepared      []map[string]any // assistant.prepare params
	launcher      string           // what the model configuration runs a purpose on ("" = claude)
	larkCLI       bool
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
	retired   []string // session.retire's session ids
	retireErr error
	tokens    map[string][]map[string]map[string]string // todo.sync_session_tokens, by machine, in order

	// The hub's other apps as a test answers them (answerApp), and the calls they got.
	apps     map[string]appAnswer
	appCalls []AppCall
}

type modelCall struct{ System, User string }

func newFakeHub() *fakeHub {
	// 主对话派的任务只在主对话里通知 is on by default (tasks from the main conversation are quiet, #1731).
	return &fakeHub{sessions: map[string][]proto.Session{}, known: map[string]bool{}, offline: map[string]bool{},
		tokens: map[string][]map[string]map[string]string{}, gone: map[string]bool{}}
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
		devices := f.devices
		if devices == nil {
			devices = []Device{}
			for id := range f.sessions {
				devices = append(devices, Device{ID: id, Name: id, Online: true, Reachable: true})
			}
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
	case DeviceLaunchersProbe: // a launcher named after the agent it starts
		name, _ := a["name"].(string)
		agent := "claude"
		for _, other := range []string{"codex", "gemini"} {
			if strings.HasPrefix(name, other) {
				agent = other
			}
		}
		return json.Marshal(map[string]any{"detail": proto.LauncherDetail{Agent: agent}})
	case DeviceAssistantPrepare:
		f.prepared = append(f.prepared, a)
		return json.Marshal(map[string]any{"cwd": "/home/me/.jarvis/assistant"})
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
	case deviceSessionRetire:
		if f.retireErr != nil {
			return nil, f.retireErr
		}
		f.retired = append(f.retired, a["session_id"].(string))
		return json.RawMessage(`{"closed":true}`), nil
	case deviceTodoTokens:
		var t struct {
			Tokens map[string]map[string]string `json:"tokens"`
		}
		_ = json.Unmarshal(p.Params, &t)
		f.tokens[p.Device] = append(f.tokens[p.Device], t.Tokens)
		return json.RawMessage(`{}`), nil
	case DeviceAssistantSend:
		f.sends = append(f.sends, a)
		id, _ := a["session_id"].(string)
		if id == "" { // a new conversation
			f.made++
			id = fmt.Sprintf("s%d", f.made)
			f.known[id] = true
		}
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
	case AppModels + " " + ModelsApply:
		var a struct {
			Params map[string]any `json:"params"`
		}
		_ = json.Unmarshal(p.Params, &a)
		if l, _ := a.Params["launcher"].(string); l == "" {
			a.Params["launcher"] = f.launcherLocked()
		}
		return json.Marshal(map[string]any{"params": a.Params})
	case AppModels + " " + ModelsApplyPicked: // checked and fingerprinted
		var a struct {
			Params map[string]any `json:"params"`
		}
		_ = json.Unmarshal(p.Params, &a)
		a.Params["model_fingerprint"] = "picked"
		return json.Marshal(map[string]any{"params": a.Params})
	case AppModels + " " + ModelsRecord:
		return json.Marshal(f.record)
	case AppModels + " " + ModelsSave:
		var a struct {
			Revision int64        `json:"revision"`
			Binding  ModelBinding `json:"binding"`
		}
		_ = json.Unmarshal(p.Params, &a)
		if a.Revision != f.record.Revision {
			return nil, errors.New("配置已被修改，请重新加载后保存")
		}
		f.record = modelRecord{Revision: a.Revision + 1, Binding: a.Binding}
		return json.RawMessage(`{}`), nil
	case AppModels + " " + ModelsLaunchers:
		return json.Marshal(map[string]any{"launchers": f.launcherList})
	case AppModels + " " + ModelsLauncherCheck:
		var a struct {
			Launcher string `json:"launcher"`
			Model    string `json:"model"`
		}
		_ = json.Unmarshal(p.Params, &a)
		return json.Marshal(map[string]any{"available": !f.gone[a.Launcher], "model_ok": !f.gone[a.Launcher+"/"+a.Model]})
	case AppConnectors + " " + ConnectorsSignedIn:
		return json.Marshal(map[string]any{"connectors": []string{}, "clis": map[string]string{}, "lark_cli": f.larkCLI})
	case AppPlugins + " " + PluginsAssistant:
		return json.Marshal(map[string]any{"instructions": "", "skills": nil})
	case AppSettings + " " + SettingsList:
		return json.Marshal(map[string]any{"settings": []Setting{{Key: "inbox.rules", Title: "收件箱分拣规则", About: "the rules",
			Value: "a string", Actions: []string{"get", "set"}}}})
	case AppSettings + " " + AppTool:
		return json.Marshal(proto.TextResult("the hub's: "+string(p.Params), false))
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
	}
	if answer := f.apps[p.AppID+" "+p.Method]; answer != nil {
		f.appCalls = append(f.appCalls, p)
		out, err := answer(p.Params)
		if err != nil {
			return nil, err
		}
		return json.Marshal(out)
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
	return withSettings(t, f, New(Options{Hub: f, Store: func() Store { return st }, Logf: t.Logf}))
}

// newKBService is newTestService with a knowledge base in a directory of its own (kb.go), on as the hub says (its
// hello's kb section, the defaults unless the test set one).
func newKBService(t *testing.T, f *fakeHub) *Service {
	t.Helper()
	st := &memStore{}
	f.mu.Lock()
	if f.hello.KB == nil {
		f.hello.KB = &KBSettings{}
	}
	f.mu.Unlock()
	s := withSettings(t, f, New(Options{Hub: f, Store: func() Store { return st }, Logf: t.Logf, KB: &KBOptions{Dir: t.TempDir()}}))
	t.Cleanup(s.kbClose)
	return s
}

// kbEng is s's knowledge base, open now.
func kbEng(t *testing.T, s *Service) *kb.Engine {
	t.Helper()
	e, err := s.kbGet(context.Background())
	if err != nil {
		t.Fatalf("the knowledge base did not open: %v", err)
	}
	return e
}

// newStorelessService is an assistant on f with no database: its state lives in memory only.
func newStorelessService(t *testing.T, f *fakeHub) *Service {
	t.Helper()
	return withSettings(t, f, New(Options{Hub: f, Logf: t.Logf}))
}

// newServiceOn is an assistant on f with st as its database.
func newServiceOn(t *testing.T, f *fakeHub, st Store) *Service {
	t.Helper()
	return withSettings(t, f, New(Options{Hub: f, Store: func() Store { return st }, Logf: t.Logf}))
}

// withSettings saves f.settings as s's own (none: s keeps what it has).
func withSettings(t *testing.T, f *fakeHub, s *Service) *Service {
	t.Helper()
	if f.settings != nil {
		setSettings(t, s, func(v *Settings) { *v = *f.settings })
	}
	return s
}

// setSettings changes s's own settings, and the hub is told (the snapshot's profile).
func setSettings(t *testing.T, s *Service, fn func(*Settings)) {
	t.Helper()
	if _, err := s.updateSettings(context.Background(), func(v *Settings) error { fn(v); return nil }); err != nil {
		t.Fatal(err)
	}
	s.settingsChanged(context.Background())
}

// todoNow is todo id as the assistant has it now ({} when there is none).
func todoNow(s *Service, id string) Todo {
	t, _ := s.todoNow(id)
	return t
}

// onlyTodo is the one todo there is ({} when there is none, or more).
func onlyTodo(s *Service) Todo {
	s.todos.mu.RLock()
	defer s.todos.mu.RUnlock()
	if len(s.todos.todos) != 1 {
		return Todo{}
	}
	for _, t := range s.todos.todos {
		return copyTodoLocked(t)
	}
	return Todo{}
}

// ---- the hub's knowledge base, interaction queue and inbox, as the tools on them call them (tools_apps.go) ----

// appAnswer is a test's answer to an app method's params. It runs under the fake's lock.
type appAnswer func(params json.RawMessage) (any, error)

// answerApp has the hub answer app's method with answer.
func (f *fakeHub) answerApp(app, method string, answer appAnswer) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.apps == nil {
		f.apps = map[string]appAnswer{}
	}
	f.apps[app+" "+method] = answer
}

// appParams are the params of each call to app's method, in order.
func (f *fakeHub) appParams(app, method string) []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []map[string]any
	for _, c := range f.appCalls {
		if c.AppID == app && c.Method == method {
			var m map[string]any
			_ = json.Unmarshal(c.Params, &m)
			out = append(out, m)
		}
	}
	return out
}

// fakeQueue is the hub's interaction queue (interactions.get / interactions.answer): the pending interactions, how
// the others ended, and the answers taken. raced are pending when read but answered by someone else (with that
// status) before the assistant's answer comes.
type fakeQueue struct {
	pending  map[string]proto.Interaction
	ended    map[string]string
	raced    map[string]string
	answered map[string]map[string]string
}

// withQueue gives the hub an interaction queue with ixs pending.
func (f *fakeHub) withQueue(ixs ...proto.Interaction) *fakeQueue {
	q := &fakeQueue{pending: map[string]proto.Interaction{}, ended: map[string]string{}, raced: map[string]string{},
		answered: map[string]map[string]string{}}
	for _, ix := range ixs {
		q.pending[ix.ID] = ix
	}
	type call struct {
		InteractionID string            `json:"interaction_id"`
		Answers       map[string]string `json:"answers"`
	}
	f.answerApp(AppInteractions, assistantwire.InteractionsGet, func(params json.RawMessage) (any, error) {
		var a call
		_ = json.Unmarshal(params, &a)
		if ix, ok := q.pending[a.InteractionID]; ok {
			return map[string]any{"interaction": ix, "ok": true}, nil
		}
		return map[string]any{"ok": false, "status": q.ended[a.InteractionID]}, nil
	})
	f.answerApp(AppInteractions, assistantwire.InteractionsAnswer, func(params json.RawMessage) (any, error) {
		var a call
		_ = json.Unmarshal(params, &a)
		if status, ok := q.raced[a.InteractionID]; ok {
			return map[string]any{"ok": false, "status": status}, nil
		}
		if _, ok := q.pending[a.InteractionID]; !ok {
			return map[string]any{"ok": false, "status": q.ended[a.InteractionID]}, nil
		}
		delete(q.pending, a.InteractionID)
		q.answered[a.InteractionID] = a.Answers
		return map[string]any{"ok": true}, nil
	})
	return q
}

// putTodo puts t among the assistant's todos as if read from its store (its open agents count for it).
func putTodo(s *Service, t Todo) {
	_ = s.ensureTodosLoaded(context.Background())
	s.todos.mu.Lock()
	defer s.todos.mu.Unlock()
	if t.Agents == nil {
		t.Agents = []TodoAgent{}
	}
	if t.Spaces == nil {
		t.Spaces = []TodoSpace{}
	}
	s.todos.todos[t.ID] = &t
	for _, a := range t.Agents {
		if a.ReleasedAt == nil && a.DismissedAt == nil && t.DoneAt == nil {
			s.todos.activeBySession[[2]string{a.MachineID, a.SessionID}] = t.ID
		}
	}
	for _, sp := range t.Spaces {
		s.todos.spacesByKey[spaceKey(sp.MachineID, sp.Space)] = t.ID
	}
}

// todoCount is how many todos there are.
func todoCount(s *Service) int {
	s.todos.mu.RLock()
	defer s.todos.mu.RUnlock()
	return len(s.todos.todos)
}

package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// getReadiness is GET /api/v1/assistant/readiness (query: its query string) as the hub relays it, in lang.
func getReadiness(t *testing.T, s *Service, query, lang string) Readiness {
	t.Helper()
	header := map[string][]string{}
	if lang != "" {
		header["Accept-Language"] = []string{lang}
	}
	res, err := serveHTTP(context.Background(), s, assistantwire.HTTPRequest{Method: "GET", Path: "/api/v1/assistant/readiness",
		Query: query, Header: header}, s.lang)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != 200 {
		t.Fatalf("readiness: %d %s", res.Status, res.Body)
	}
	var out Readiness
	if err := json.Unmarshal(res.Body, &out); err != nil {
		t.Fatalf("readiness: %s", res.Body)
	}
	return out
}

func readinessStep(t *testing.T, r Readiness, id string) ReadinessStep {
	t.Helper()
	for _, st := range r.Steps {
		if st.ID == id {
			return st
		}
	}
	t.Fatalf("no step %s in %+v", id, r.Steps)
	return ReadinessStep{}
}

func nextOf(r Readiness) string {
	if r.Next == nil {
		return ""
	}
	return *r.Next
}

// readyMachine is a hub with the assistant's machine "server" on Claude Code 2.1.308, its main conversation m1.
func readyMachine(t *testing.T, main ...proto.Session) (*fakeHub, *Service, *memStore) {
	t.Helper()
	f := newFakeHub().withMachine()
	f.doctor = map[string]string{"server": "2.1.308，支持 --plugin-dir"}
	f.settings = &Settings{Main: "m1"}
	if len(main) > 0 {
		f.setSessions("server", main...)
	}
	st := &memStore{}
	return f, newServiceOn(t, f, st), st
}

// With no computer the first missing step is the computer, said in plain words with what to do; what comes after it
// cannot be told, and the suggestions are the local ones only (docs/onboarding.md 5.1).
func TestReadinessWithoutComputer(t *testing.T) {
	s := newStorelessService(t, newFakeHub())
	r := getReadiness(t, s, "", "")
	c := readinessStep(t, r, StepComputer)
	if r.Version != 1 || r.Mode != ReadinessSetup || r.Ready || nextOf(r) != StepComputer || r.Machine != nil || r.Launcher != "claude" ||
		c.State != StepTodo || c.Code != proto.CodeNoMachine || c.Action == nil || c.Action.ID != "add_computer" ||
		c.Title != "添加你的电脑" || c.Detail != "Persona 要在你的一台电脑上干活，现在还没接上电脑。" {
		t.Fatalf("readiness = %+v", r)
	}
	if len(r.Steps) != 4 || r.Steps[1].ID != StepClaude || r.Steps[2].ID != StepModel || r.Steps[3].ID != StepFirstMessage {
		t.Fatalf("steps = %+v", r.Steps)
	}
	for _, st := range r.Steps[1:] {
		if st.State != StepUnknown {
			t.Fatalf("after the computer: %+v", st)
		}
	}
	for _, sg := range r.Suggestions {
		if !sg.Local {
			t.Fatalf("setup suggests %+v", sg)
		}
	}
	if len(r.Suggestions) != 2 || r.Probe != nil || r.Dismissed || r.CheckedAt.IsZero() {
		t.Fatalf("readiness = %+v", r)
	}
	// In English when the request says so.
	if c := readinessStep(t, getReadiness(t, s, "", "en"), StepComputer); c.Title != "Add your computer" {
		t.Fatalf("en: %+v", c)
	}
	// warm carries the same.
	status, body := serveJSON(t, s, "POST", "/api/v1/assistant/warm", "", `{}`)
	rd, _ := body["readiness"].(map[string]any)
	if status != 200 || body["ok"] != true || rd == nil || rd["mode"] != ReadinessSetup || rd["next"] != StepComputer {
		t.Fatalf("warm: %d %v", status, body)
	}
}

// A computer that is not online is named, and said to be asleep rather than the hub unreachable.
func TestReadinessComputerOffline(t *testing.T) {
	f := newFakeHub().withMachine()
	f.devices = []Device{{ID: "server", Name: "小林的 MacBook"}}
	r := getReadiness(t, newStorelessService(t, f), "", "")
	c := readinessStep(t, r, StepComputer)
	if r.Mode != ReadinessSetup || c.State != StepProblem || c.Code != "node_unreachable" || c.Action.ID != "wake_computer" ||
		c.Title != "小林的 MacBook 不在线" || r.Machine == nil || r.Machine.Name != "小林的 MacBook" || c.Machine != "server" {
		t.Fatalf("readiness = %+v", r)
	}
}

// A computer with Claude Code and a model never tried: the main conversation can go (full), the model is unknown
// rather than a problem, and nothing spent tokens to find out.
func TestReadinessFullButUnverified(t *testing.T) {
	f, s, _ := readyMachine(t)
	r := getReadiness(t, s, "", "")
	cl, m, first := readinessStep(t, r, StepClaude), readinessStep(t, r, StepModel), readinessStep(t, r, StepFirstMessage)
	if r.Mode != ReadinessFull || !r.Ready || nextOf(r) != StepModel || r.Machine == nil || r.Machine.ID != "server" ||
		cl.State != StepOK || cl.Version != "2.1.308" ||
		m.State != StepUnknown || m.Code != "model_unverified" || m.Action.ID != "test_assistant" ||
		first.State != StepTodo || first.Code != "never_tried" {
		t.Fatalf("readiness = %+v", r)
	}
	if len(f.checked) != 0 {
		t.Fatalf("checked without a probe: %v", f.checked)
	}
	if len(r.Suggestions) < 3 || r.Suggestions[2].ID != "today" || len(r.Suggestions[2].Needs) != 1 || r.Suggestions[2].Prompt == "" {
		t.Fatalf("suggestions = %+v", r.Suggestions)
	}
}

// The node's self-check found no claude for the plain launcher: Claude Code is missing.
func TestReadinessClaudeMissing(t *testing.T) {
	f, s, _ := readyMachine(t)
	f.doctor["server"] = "读不到 claude 的版本"
	r := getReadiness(t, s, "", "")
	cl := readinessStep(t, r, StepClaude)
	if r.Mode != ReadinessSetup || nextOf(r) != StepClaude || cl.State != StepProblem || cl.Code != proto.CodeClaudeMissing ||
		cl.Action.ID != "install_claude" || len(cl.AltActions) != 1 || cl.AltActions[0].ID != "connect_model" ||
		cl.Detail != "server 上还没装 Claude Code。装好它，或者改用 API Key。" {
		t.Fatalf("claude = %+v (%s)", cl, r.Mode)
	}
}

// A node with no self-check (an old one) leaves Claude Code unknown, which does not stop the main conversation.
func TestReadinessOldNodeUnknown(t *testing.T) {
	f, s, _ := readyMachine(t)
	f.doctor = nil
	r := getReadiness(t, s, "", "")
	if cl := readinessStep(t, r, StepClaude); cl.State != StepUnknown || r.Mode != ReadinessFull {
		t.Fatalf("readiness = %+v", r)
	}
}

// A start refused because Claude Code is not there is the claude step's, until a start goes or a self-check made
// after it reads a version (installed since: in setup mode no start comes to clear it).
func TestReadinessStartRefusal(t *testing.T) {
	f, s, st := readyMachine(t)
	f.doctor["server"] = "读不到 claude 的版本"
	refuse := func() {
		t.Helper()
		data, _ := json.Marshal(map[string]string{"delivered": "none", "launcher": "claude"})
		f.mu.Lock()
		f.failAssistant = &rpc.Error{Code: proto.CodeClaudeMissing, Message: "zsh:1: command not found: claude", Data: data}
		f.mu.Unlock()
		if status, _ := serveJSON(t, s, "POST", "/api/v1/assistant/conversations", "", `{"prompt":"你好","main":true}`); status != 409 {
			t.Fatalf("start: %d", status)
		}
		f.mu.Lock()
		f.failAssistant = nil
		f.mu.Unlock()
	}
	refuse()
	if cl := readinessStep(t, getReadiness(t, s, "", ""), StepClaude); cl.Code != proto.CodeClaudeMissing || cl.Action.ID != "install_claude" {
		t.Fatalf("claude = %+v", cl)
	}
	// Claude Code installed: the next self-check reads a version and the refusal is forgotten.
	old := readinessClaudeRetry
	readinessClaudeRetry = 0
	t.Cleanup(func() { readinessClaudeRetry = old })
	f.mu.Lock()
	f.doctor["server"] = "2.1.308，支持 --plugin-dir"
	f.mu.Unlock()
	if cl := readinessStep(t, getReadiness(t, s, "", ""), StepClaude); cl.State != StepOK || cl.Version != "2.1.308" {
		t.Fatalf("after installing: %+v", cl)
	}
	var rec readinessRecord
	if _, err := st.Setting(context.Background(), readinessKey, &rec); err != nil || rec.Start != nil {
		t.Fatalf("record = %+v (%v)", rec, err)
	}
	// Refused again, and the self-check after it reads no version (the version read before does not count): the
	// refusal holds until a start goes.
	readinessClaudeRetry = time.Hour
	f.mu.Lock()
	f.doctor["server"] = "读不到 claude 的版本"
	f.mu.Unlock()
	refuse()
	if cl := readinessStep(t, getReadiness(t, s, "", ""), StepClaude); cl.Code != proto.CodeClaudeMissing {
		t.Fatalf("refused again: %+v", cl)
	}
	if status, body := serveJSON(t, s, "POST", "/api/v1/assistant/conversations", "", `{"prompt":"你好","main":true}`); status != 200 {
		t.Fatalf("start: %d %v", status, body)
	}
	if _, err := st.Setting(context.Background(), readinessKey, &rec); err != nil || rec.Start != nil {
		t.Fatalf("after a start: %+v (%v)", rec, err)
	}
}

// The main conversation's last turn was Claude Code's「Not logged in」: the model step says sign in (or use a key),
// and so does the first message.
func TestReadinessNotLoggedIn(t *testing.T) {
	_, s, _ := readyMachine(t, proto.Session{SessionID: "m1", State: proto.StateIdle, LastText: "Not logged in · Please run /login",
		UpdatedAt: time.Now()})
	r := getReadiness(t, s, "", "")
	m, first := readinessStep(t, r, StepModel), readinessStep(t, r, StepFirstMessage)
	if r.Mode != ReadinessSetup || nextOf(r) != StepModel || m.State != StepProblem || m.Code != proto.TurnErrorNotLoggedIn ||
		m.Action.ID != "connect_model" || len(m.AltActions) != 1 || m.AltActions[0].ID != "login_claude" ||
		m.Title != "Claude Code 还没登录" || m.Detail != "在 server 上登录一次，或者改用 API Key。" || m.Launcher != "claude" ||
		first.State != StepProblem || first.Code != proto.TurnErrorNotLoggedIn {
		t.Fatalf("readiness = %+v", r)
	}
}

// An answer in the main conversation: all ready, and the first message stays done after a later failure.
func TestReadinessAnswered(t *testing.T) {
	f, s, _ := readyMachine(t, proto.Session{SessionID: "m1", State: proto.StateIdle, LastText: "你好，我是 Persona。",
		UpdatedAt: time.Now()})
	r := getReadiness(t, s, "", "")
	if r.Mode != ReadinessFull || r.Next != nil || readinessStep(t, r, StepModel).State != StepOK || readinessStep(t, r, StepFirstMessage).State != StepOK {
		t.Fatalf("readiness = %+v", r)
	}
	f.setSessions("server", proto.Session{SessionID: "m1", State: proto.StateIdle, LastText: "API Error: 429 rate limit",
		UpdatedAt: time.Now().Add(time.Second)})
	r = getReadiness(t, s, "", "")
	m := readinessStep(t, r, StepModel)
	if m.State != StepProblem || m.Code != proto.TurnErrorRateLimited || r.Mode != ReadinessFull || // a rate limit passes by itself
		readinessStep(t, r, StepFirstMessage).State != StepOK {
		t.Fatalf("after a rate limit: %+v", r)
	}
}

// ?probe=1 runs the one-word check once; asked again within 30 seconds it answers the same result. A key that does
// not work is the model step's problem, with editing the provider to do.
func TestReadinessProbe(t *testing.T) {
	f, s, _ := readyMachine(t)
	f.checks = map[string]proto.LauncherCheck{"server/claude": {Error: "Invalid API key · Please run /login", Millis: 900}}
	r := getReadiness(t, s, "probe=1", "")
	m := readinessStep(t, r, StepModel)
	if r.Probe == nil || r.Probe.OK || r.Probe.Error == "" || m.Code != proto.TurnErrorAuthFailed || m.Action.ID != "edit_provider" ||
		r.Mode != ReadinessSetup {
		t.Fatalf("readiness = %+v probe %+v", r, r.Probe)
	}
	if r2 := getReadiness(t, s, "probe=1", ""); r2.Probe == nil || r2.Probe.Millis != 900 || len(f.checked) != 1 {
		t.Fatalf("again: %+v, checked %v", r2.Probe, f.checked)
	}
	if r3 := getReadiness(t, s, "", ""); r3.Probe != nil || readinessStep(t, r3, StepModel).Code != proto.TurnErrorAuthFailed {
		t.Fatalf("without probe: %+v", r3)
	}
	// Past the reuse window it runs again; this time it answers.
	old := readinessProbeReuse
	readinessProbeReuse = 0
	t.Cleanup(func() { readinessProbeReuse = old })
	f.mu.Lock()
	f.checks = nil
	f.mu.Unlock()
	if r4 := getReadiness(t, s, "probe=1", ""); r4.Probe == nil || !r4.Probe.OK || readinessStep(t, r4, StepModel).State != StepOK ||
		len(f.checked) != 2 || r4.Mode != ReadinessFull {
		t.Fatalf("again later: %+v, checked %v", r4, f.checked)
	}
}

// 以后再说 is kept in the settings (assistant.readiness), with the last result.
func TestReadinessDismiss(t *testing.T) {
	f, s, st := readyMachine(t)
	if status, body := serveJSON(t, s, "POST", "/api/v1/assistant/readiness/dismiss", "", `{"dismissed":true}`); status != 200 || body["ok"] != true {
		t.Fatalf("dismiss: %d %v", status, body)
	}
	if r := getReadiness(t, s, "", ""); !r.Dismissed {
		t.Fatalf("readiness = %+v", r)
	}
	var rec readinessRecord
	if ok, err := st.Setting(context.Background(), readinessKey, &rec); !ok || err != nil || !rec.Dismissed || rec.Last == nil ||
		rec.Last.Mode != ReadinessFull {
		t.Fatalf("record = %+v (%v %v)", rec, ok, err)
	}
	// A new assistant on the same database reads it back.
	if r := getReadiness(t, newServiceOn(t, f, st), "", ""); !r.Dismissed {
		t.Fatalf("after a restart: %+v", r)
	}
	// The same result again is not written again.
	puts := st.puts
	getReadiness(t, s, "", "")
	if st.puts != puts {
		t.Fatalf("written again: %d → %d", puts, st.puts)
	}
	if status, _ := serveJSON(t, s, "POST", "/api/v1/assistant/readiness/dismiss", "", `{"dismissed":false}`); status != 200 {
		t.Fatal(status)
	}
	if r := getReadiness(t, s, "", ""); r.Dismissed {
		t.Fatalf("undone: %+v", r)
	}
}

// A launcher of the user's own that the machine no longer has: launcher_missing; one that runs Codex: skipped.
func TestReadinessLauncher(t *testing.T) {
	f, s, _ := readyMachine(t)
	f.hello.Assistant.Launcher = "cc_gone"
	f.zshrc = map[string][]proto.Launcher{"server": {{Name: "cx", Agent: "codex"}}}
	r := getReadiness(t, s, "", "")
	if cl := readinessStep(t, r, StepClaude); r.Launcher != "cc_gone" || cl.Code != proto.CodeLauncherMissing || cl.Action.ID != "connect_model" ||
		cl.Detail != "在 server 上找不到「cc_gone」这个模型入口，可能被删了。" || r.Mode != ReadinessSetup {
		t.Fatalf("readiness = %+v", r)
	}
	f.mu.Lock()
	f.hello.Assistant.Launcher = "cx"
	f.mu.Unlock()
	if cl := readinessStep(t, getReadiness(t, s, "", ""), StepClaude); cl.State != StepSkipped {
		t.Fatalf("codex: %+v", cl)
	}
}

// A direct API provider added in Persona that speaks Messages starts Claude Code on its gateway (#2455): its launcher
// is there, not launcher_missing, right after the key is added.
func TestReadinessManagedAPILauncher(t *testing.T) {
	f, s, _ := readyMachine(t)
	key, err := s.sealKey("sk-test")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.putSetting(context.Background(), launchersManagedKey, []storedLauncher{{ManagedLauncher: proto.ManagedLauncher{Name: "direct",
		Kind: "api", API: "messages", BaseURL: "https://api.example.com", Model: "m"}, KeySealed: key}}); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.hello.Assistant.Launcher = "direct"
	f.mu.Unlock()
	r := getReadiness(t, s, "", "")
	if cl := readinessStep(t, r, StepClaude); r.Launcher != "direct" || cl.State == StepProblem || r.Mode != ReadinessFull {
		t.Fatalf("readiness = %+v", r)
	}
}

// A record that cannot be read is not written over: what was kept (以后再说) survives a database hiccup.
func TestReadinessUnreadRecordKept(t *testing.T) {
	_, s, st := readyMachine(t)
	if status, _ := serveJSON(t, s, "POST", "/api/v1/assistant/readiness/dismiss", "", `{}`); status != 200 {
		t.Fatal(status)
	}
	s.ready.mu.Lock()
	s.ready.loaded, s.ready.rec = false, readinessRecord{}
	s.ready.mu.Unlock()
	st.mu.Lock()
	st.failSetting = errors.New("database hiccup")
	st.mu.Unlock()
	getReadiness(t, s, "", "")
	if status, _ := serveJSON(t, s, "POST", "/api/v1/assistant/readiness/dismiss", "", `{"dismissed":false}`); status != 500 {
		t.Fatalf("dismiss over an unread record: %d", status)
	}
	st.mu.Lock()
	st.failSetting = nil
	st.mu.Unlock()
	var rec readinessRecord
	if _, err := st.Setting(context.Background(), readinessKey, &rec); err != nil || !rec.Dismissed {
		t.Fatalf("record = %+v (%v)", rec, err)
	}
}

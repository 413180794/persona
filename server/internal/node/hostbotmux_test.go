package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/host"
	"github.com/nwverse/persona/server/internal/proto"
)

// botmux next to the engine (hostbotmux.go), driven through the node's requests against a fake botmux CLI and a fake
// dashboard.

const (
	bmxCwd = "/tmp/bmxproj"
	bmxApp = "cli_test_app"
)

// fakeBotmux is the botmux CLI (observe, dashboard current) and its dashboard's /api/trigger.
type fakeBotmux struct {
	mu       sync.Mutex
	rows     []map[string]any
	offline  bool  // no daemon answers
	fail     error // observe fails
	observes int
	token    string // the dashboard's current token
	told     string // the token `dashboard current` prints
	status   int    // the trigger's reply status (0: 200)
	triggers []map[string]any
	srv      *httptest.Server
}

func newFakeBotmux(t *testing.T) *fakeBotmux {
	f := &fakeBotmux{token: "tok1", told: "tok1"}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.URL.Path != "/api/trigger" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		if c, err := r.Cookie("botmux_dashboard_token"); err != nil || c.Value != f.token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		b, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(b, &body)
		f.triggers = append(f.triggers, body)
		if f.status != 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(f.status)
			fmt.Fprint(w, `{"ok":false,"error":"session_not_found"}`)
			return
		}
		fmt.Fprint(w, `{"ok":true,"action":"delivered"}`)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeBotmux) run(_ context.Context, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch strings.Join(args, " ") {
	case "observe --include-raw":
		f.observes++
		if f.fail != nil {
			return nil, f.fail
		}
		status := "ok"
		if f.offline {
			status = "daemon_offline"
		}
		return json.Marshal(map[string]any{"schemaVersion": 1, "daemons": []any{
			map[string]any{"larkAppId": bmxApp, "probe": map[string]any{"status": status}, "sessions": f.rows},
		}})
	case "dashboard current":
		port := f.srv.URL[strings.LastIndex(f.srv.URL, ":")+1:]
		return []byte("http://192.0.2.1:" + port + "/?t=" + f.told + "\n工作台: http://192.0.2.1:" + port +
			"/workbench?t=" + f.told + "\n⚠️ 安全提示\n"), nil
	}
	return nil, fmt.Errorf("unexpected botmux %v", args)
}

func (f *fakeBotmux) sent() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.triggers
}

// bmxRow is one session as botmux observe reports it.
func bmxRow(id, cli, liveness, turn string, prompt bool, title string) map[string]any {
	return map[string]any{
		"identity":         map[string]any{"sessionId": id, "larkAppId": bmxApp},
		"cli":              map[string]any{"id": cli},
		"backend":          map[string]any{"type": "tmux", "sessionName": "bmx-" + id[:8]},
		"liveness":         liveness,
		"turn":             turn,
		"closed":           liveness == "closed",
		"tuiPromptActive":  prompt,
		"lastActivityAt":   time.Now().Add(-time.Hour).UnixMilli(),
		"workingDirectory": bmxCwd,
		"raw":              map[string]any{"title": title},
	}
}

// bmxNode is a Node whose engine is herdr (not registered, so a call to it shows in the fake herdr's trace) with
// botmux next to it.
func bmxNode(t *testing.T) (*Node, *fakeBotmux, *fakeHerdr, string) {
	t.Helper()
	n, fh, home := herdrNode(t)
	f := newFakeBotmux(t)
	n.hosts = host.NewRegistry(&botmuxHost{run: f.run, client: f.srv.Client()})
	withSubmitGrace(t, time.Millisecond)
	return n, f, fh, home
}

const (
	sidD = "0d000000-0000-4000-8000-00000000000d"
	sidE = "0d000000-0000-4000-8000-00000000000e"
	sidF = "0d000000-0000-4000-8000-00000000000f"
	sidG = "0d000000-0000-4000-8000-000000000010"
)

// Claude Code sessions are listed with botmux's state, in a space per working directory; closed sessions, other CLIs,
// sessions botmux runs in herdr and long-parked ones are not.
func TestBotmuxListsClaudeSessions(t *testing.T) {
	n, f, _, home := bmxNode(t)
	attention := bmxRow(sidC, "claude-code", "alive", "idle", false, "needs a decision")
	attention["raw"] = map[string]any{"title": "needs a decision", "agentAttention": map[string]any{"kind": "blocked"}}
	inHerdr := bmxRow(sidF, "claude-code", "alive", "working", false, "in herdr")
	inHerdr["backend"] = map[string]any{"type": "herdr"}
	oldParked := bmxRow(sidG, "claude-code", "not_running", "unknown", false, "old")
	oldParked["lastActivityAt"] = time.Now().Add(-2 * offlineWindow).UnixMilli()
	f.rows = []map[string]any{
		bmxRow(sidA, "claude-code", "alive", "working", false, "1103 | 功能 | botmux\n接入"),
		bmxRow(sidB, "claude-code", "alive", "idle", true, "asks"),
		attention,
		bmxRow(sidD, "claude-code", "alive", "idle", false, "done"),
		bmxRow(sidE, "claude-code", "not_running", "unknown", false, "parked"),
		bmxRow("0d000000-0000-4000-8000-000000000011", "claude-code", "closed", "unknown", false, "closed"),
		bmxRow("0d000000-0000-4000-8000-000000000012", "traex", "alive", "working", false, "traex"),
		inHerdr, oldParked,
	}
	for _, id := range []string{sidA, sidB, sidC, sidD, sidE} {
		writeTranscript(t, home, bmxCwd, id, time.Now().Add(-time.Minute), `{"type":"user","message":{"role":"user","content":"hi"}}`)
	}
	m := byID(listSessions(t, n))
	want := map[string]string{sidA: proto.StatusWorking, sidB: proto.StatusBlocked, sidC: proto.StatusBlocked,
		sidD: proto.StatusDone, sidE: proto.StatusOffline}
	for id, status := range want {
		s, ok := m[id]
		if !ok {
			t.Fatalf("%s not listed: %v", id, ids(listSessions(t, n)))
		}
		if s.Status != status || s.PaneID != "botmux:"+id || s.SpaceKind != host.Botmux || s.SpaceID != dirSpace+bmxCwd ||
			s.SpaceName != "bmxproj" || s.SlotID != "" {
			t.Errorf("%s: %+v, want status %s", id, s, status)
		}
	}
	if a := m[sidA]; a.AgentName != "1103 | 功能 | botmux 接入" {
		t.Errorf("name %q", a.AgentName)
	}
	for _, id := range []string{sidF, sidG} {
		if s, ok := m[id]; ok && s.PaneID != "" {
			t.Errorf("%s listed with pane %q", id, s.PaneID)
		}
	}
	if len(m) != len(want) {
		t.Errorf("listed %v", ids(listSessions(t, n)))
	}
}

// One observe answers the refreshes of a moment; botmux not running is no sessions, not a failure.
func TestBotmuxObserveIsReusedAndOfflineIsEmpty(t *testing.T) {
	_, f, _, _ := bmxNode(t)
	h := &botmuxHost{run: f.run}
	for range 3 {
		if _, err := h.Agents(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if f.observes != 1 {
		t.Fatalf("observed %d times", f.observes)
	}
	h.at = time.Now().Add(-botmuxReuse)
	if _, err := h.Agents(context.Background()); err != nil || f.observes != 2 {
		t.Fatalf("after %s: observed %d times, %v", botmuxReuse, f.observes, err)
	}
	f.offline = true
	_, err := (&botmuxHost{run: f.run}).Agents(context.Background())
	if !errors.Is(err, host.ErrNotRunning) {
		t.Fatalf("offline: %v", err)
	}
}

// A message goes to botmux as a turn of the session — a running one and a parked one alike (botmux resumes it; no
// claude --resume in herdr) — with the dashboard's token, asked of botmux again once it was rotated.
func TestBotmuxReplyIsDeliveredByBotmux(t *testing.T) {
	n, f, fh, home := bmxNode(t)
	f.rows = []map[string]any{
		bmxRow(sidA, "claude-code", "alive", "working", false, "a"),
		bmxRow(sidE, "claude-code", "not_running", "unknown", false, "parked"),
	}
	for _, id := range []string{sidA, sidE} {
		writeTranscript(t, home, bmxCwd, id, time.Now().Add(-time.Minute), `{"type":"user","message":{"role":"user","content":"hi"}}`)
	}
	listSessions(t, n)
	if err := n.send(context.Background(), sidA, "继续", nil, sendOpts{}); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.token, f.told = "tok2", "tok2" // rotated: the cached token is refused once
	f.mu.Unlock()
	if err := n.send(context.Background(), sidE, "接着做", nil, sendOpts{}); err != nil {
		t.Fatal(err)
	}
	got := f.sent()
	if len(got) != 2 {
		t.Fatalf("triggers %v", got)
	}
	for i, want := range []struct{ id, text string }{{sidA, "继续"}, {sidE, "接着做"}} {
		target, _ := got[i]["target"].(map[string]any)
		env, _ := got[i]["envelope"].(map[string]any)
		if got[i]["instruction"] != want.text || target["kind"] != "turn" || target["sessionId"] != want.id ||
			target["botId"] != bmxApp || env["trusted"] != false || got[i]["options"] != nil {
			t.Errorf("trigger %d: %v", i, got[i])
		}
	}
	if trace := fh.calls(); len(trace) != 0 {
		t.Errorf("herdr was asked: %v", trace)
	}
}

// botmux refusing a message delivered nothing: the phone may send it again. Keys and screens are not botmux's to give.
func TestBotmuxRefusalsAndUnsupported(t *testing.T) {
	n, f, _, home := bmxNode(t)
	f.rows = []map[string]any{bmxRow(sidA, "claude-code", "alive", "idle", false, "a")}
	writeTranscript(t, home, bmxCwd, sidA, time.Now().Add(-time.Minute), `{"type":"user","message":{"role":"user","content":"hi"}}`)
	listSessions(t, n)
	f.status = http.StatusNotFound
	if err := n.send(context.Background(), sidA, "x", nil, sendOpts{}); refusalCode(err) != notDelivered {
		t.Fatalf("refused send = %v", err)
	}
	if err := n.keys(context.Background(), sidA, []string{"esc"}, ""); !errors.Is(err, host.ErrUnsupported) {
		t.Fatalf("keys = %v", err)
	}
	if err := n.keys(context.Background(), sidA, []string{"down", "enter"}, "x"); refusalCode(err) != cannotType {
		t.Fatalf("typing = %v", err)
	}
	if _, _, err := n.screen(context.Background(), sidA, 40, "visible", false); !errors.Is(err, host.ErrUnsupported) {
		t.Fatalf("screen = %v", err)
	}
}

// botmux is registered next to the engine's hosts only when it is installed.
func TestNodeHostsAddBotmuxWhenInstalled(t *testing.T) {
	old := botmuxBin
	t.Cleanup(func() { botmuxBin = old })
	botmuxBin = func() string { return "" }
	if hs := nodeHosts(proto.EngineHerdr, nil); len(hs) != 1 {
		t.Fatalf("without botmux: %d hosts", len(hs))
	}
	botmuxBin = func() string { return "/opt/botmux/bin/botmux" }
	hs := nodeHosts(proto.EngineHerdrBoth, nil)
	if len(hs) != 3 || hs[0].Kind() != host.Herdr || hs[2].Kind() != host.Botmux {
		t.Fatalf("with botmux: %v", hs)
	}
	if defaultHostKind(proto.EngineHerdrBoth) != host.Herdr {
		t.Fatal("new sessions must not go to botmux")
	}
	if ref := host.ParsePane("botmux:" + sidA); ref.Kind != host.Botmux || ref.ID != sidA {
		t.Fatalf("pane ref %+v", ref)
	}
}

// An observe that fails (a timeout, a CLI error) is not botmux stopping: the sessions stay as listed, and a reply in
// the gap still goes to botmux.
func TestBotmuxFailedObserveKeepsSessions(t *testing.T) {
	n, f, _, home := bmxNode(t)
	f.rows = []map[string]any{bmxRow(sidA, "claude-code", "alive", "working", false, "a")}
	writeTranscript(t, home, bmxCwd, sidA, time.Now().Add(-time.Minute), `{"type":"user","message":{"role":"user","content":"hi"}}`)
	listSessions(t, n)
	h := n.hosts.Get(host.Botmux).(*botmuxHost)
	f.fail = errors.New("signal: killed")
	h.at = time.Time{}
	if s := byID(listSessions(t, n))[sidA]; s.PaneID != "botmux:"+sidA || s.Status != proto.StatusWorking {
		t.Fatalf("after a failed observe: %+v", s)
	}
	_, err := h.Agents(context.Background())
	if err == nil || errors.Is(err, host.ErrNotRunning) {
		t.Fatalf("a failed observe = %v, want an error that is not ErrNotRunning", err)
	}
}

// A session parked for longer than the transcripts listed offline is not listed, but a reply to it still goes to
// botmux (which resumes it), not to a claude --resume in herdr.
func TestBotmuxReplyToLongParkedSession(t *testing.T) {
	n, f, fh, home := bmxNode(t)
	old := bmxRow(sidE, "claude-code", "not_running", "unknown", false, "old")
	old["lastActivityAt"] = time.Now().Add(-2 * offlineWindow).UnixMilli()
	f.rows = []map[string]any{old}
	writeTranscript(t, home, bmxCwd, sidE, time.Now().Add(-time.Minute), `{"type":"user","message":{"role":"user","content":"hi"}}`)
	if s := byID(listSessions(t, n))[sidE]; s.PaneID != "" {
		t.Fatalf("listed with a pane: %+v", s)
	}
	if err := n.send(context.Background(), sidE, "接着做", nil, sendOpts{}); err != nil {
		t.Fatal(err)
	}
	if got := f.sent(); len(got) != 1 {
		t.Fatalf("triggers %v", got)
	}
	if trace := fh.calls(); len(trace) != 0 {
		t.Errorf("herdr was asked: %v", trace)
	}
}

// A failure after botmux took the message may have delivered it (no "nothing typed" refusal, the phone must not
// resend blindly); a dashboard that moved to another port is asked for again.
func TestBotmuxUncertainDeliveryAndMovedDashboard(t *testing.T) {
	n, f, _, home := bmxNode(t)
	f.rows = []map[string]any{bmxRow(sidA, "claude-code", "alive", "idle", false, "a")}
	writeTranscript(t, home, bmxCwd, sidA, time.Now().Add(-time.Minute), `{"type":"user","message":{"role":"user","content":"hi"}}`)
	listSessions(t, n)
	h := n.hosts.Get(host.Botmux).(*botmuxHost)
	for _, status := range []int{http.StatusInternalServerError, http.StatusServiceUnavailable} {
		f.mu.Lock()
		f.status = status
		f.mu.Unlock()
		err := h.Prompt(context.Background(), host.PaneRef{Kind: host.Botmux, ID: sidA}, "x")
		if err == nil || errors.Is(err, host.ErrNotDelivered) != (status == http.StatusServiceUnavailable) {
			t.Errorf("%d: %v", status, err)
		}
	}
	f.mu.Lock()
	f.status = 0
	f.mu.Unlock()
	h.mu.Lock()
	h.base = "http://127.0.0.1:1" // where the dashboard was: nothing listens there now
	h.mu.Unlock()
	if err := h.Prompt(context.Background(), host.PaneRef{Kind: host.Botmux, ID: sidA}, "y"); err != nil {
		t.Fatalf("after the dashboard moved: %v", err)
	}
}

// The same session in a herdr pane and as botmux's parked row: the pane that runs it wins, whichever comes first.
func TestRunningPaneWinsOverParkedBotmuxRow(t *testing.T) {
	parked := host.Agent{Agent: "claude", AgentSession: &host.AgentSession{Kind: "id", Value: sidA},
		AgentStatus: proto.StatusOffline, PaneID: "botmux:" + sidA}
	running := host.Agent{Agent: "claude", AgentSession: &host.AgentSession{Kind: "id", Value: sidA},
		AgentStatus: proto.StatusWorking, PaneID: "w1:p1"}
	for _, agents := range [][]host.Agent{{parked, running}, {running, parked}} {
		n, _, _ := herdrNode(t)
		n.setAgents(agents)
		if a, _ := n.agent(sidA); a.PaneID != "w1:p1" {
			t.Fatalf("%v: pane %q", agents, a.PaneID)
		}
	}
}

// The resident node (launchd) has only the system's PATH: botmux is found at the launcher its installer writes.
func TestBotmuxFoundOutsidePATH(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir())
	if bin := findBotmux(); bin != "" {
		t.Fatalf("found %q without botmux", bin)
	}
	dir := filepath.Join(home, ".botmux", "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	launcher := filepath.Join(dir, "botmux")
	if err := os.WriteFile(launcher, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if bin := findBotmux(); bin != launcher {
		t.Fatalf("findBotmux = %q, want %q", bin, launcher)
	}
}

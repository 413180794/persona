package hub

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/db"

	"github.com/nwverse/persona/server/internal/proto"
)

func TestDefaultConfig(t *testing.T) {
	cfg := defaultConfig()
	if cfg.Listen != ":18660" {
		t.Fatalf("listen = %q", cfg.Listen)
	}
	if cfg.HookTimeoutMinutes != 30 {
		t.Fatalf("hook timeout = %d", cfg.HookTimeoutMinutes)
	}
	if len(cfg.Token) != 48 {
		t.Fatalf("token should be 24 random bytes of hex, got %q", cfg.Token)
	}
	if other := defaultConfig(); other.Token == cfg.Token {
		t.Fatal("each default config needs its own token")
	}
	if !strings.Contains(cfg.PublicURL, ":18660") {
		t.Fatalf("public url = %q", cfg.PublicURL)
	}
	ids := map[string]bool{}
	for _, m := range cfg.Machines {
		if m.ID == "" || m.Name == "" || (m.Kind != "local" && m.Kind != "ssh") {
			t.Fatalf("bad machine %+v", m)
		}
		if m.Kind == "ssh" && m.SSHHost == "" {
			t.Fatalf("ssh machine %s has no host", m.ID)
		}
		ids[m.ID] = true
	}
	// A fresh hub knows only itself; other machines are added to hub.json (docs/config.md).
	if len(cfg.Machines) != 1 {
		t.Fatalf("default machines should be just this one, got %+v", cfg.Machines)
	}
	if local := cfg.Machines[0]; local.Kind != "local" {
		t.Fatalf("the first machine should be the local hub, got %+v", local)
	}
}

// The Docker image's hub (JARVIS_LOCAL_NODE=0) starts with no machine: its own has no herdr or claude.
func TestDefaultConfigWithoutLocalNode(t *testing.T) {
	t.Setenv("JARVIS_LOCAL_NODE", "0")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir on Windows
	if _, err := LoadConfig(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(home, ".jarvis", "hub.json"))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	if string(raw["machines"]) != "[]" {
		t.Fatalf("machines written as %s, want [] (not omitted: a missing list would mean the default)", raw["machines"])
	}
}

// A compose file gives the database's address and the hub's own: they win over hub.json and are not written into it.
func TestEnvOverridesConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir on Windows
	dir := filepath.Join(home, ".jarvis")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	file := `{"token":"t","database_url":"postgres://file","public_url":"http://file:18660","listen":":1"}`
	if err := os.WriteFile(filepath.Join(dir, "hub.json"), []byte(file), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("JARVIS_DATABASE_URL", "postgres://env")
	t.Setenv("JARVIS_PUBLIC_URL", "https://hub.example.com")
	t.Setenv("JARVIS_LISTEN", ":18660")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DatabaseURL != "postgres://env" || cfg.PublicURL != "https://hub.example.com" || cfg.Listen != ":18660" {
		t.Fatalf("env not applied: %+v", cfg)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "hub.json")); string(b) != file {
		t.Fatalf("hub.json was rewritten: %s", b)
	}
	t.Setenv("JARVIS_DATABASE_URL", "")
	if cfg, _ := LoadConfig(); cfg.DatabaseURL != "postgres://file" {
		t.Fatalf("an empty variable must not override: %q", cfg.DatabaseURL)
	}
}

func TestNodeBinaryDirFromEnv(t *testing.T) {
	t.Setenv("JARVIS_NODE_BINARIES", "/opt/jarvis/nodes")
	if d := nodeBinaryDir(); d != "/opt/jarvis/nodes" {
		t.Fatalf("node binaries in %s", d)
	}
}

func TestLoadConfigCreatesAndReloads(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir on Windows
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".jarvis", "hub.json")
	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("config file was not created: %v", err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("config file mode = %v", st.Mode().Perm())
	}
	again, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if again.Token != cfg.Token {
		t.Fatal("the token must survive a reload")
	}
}

// scripts/deploy/db.sh creates hub.json with only database_url before the hub's first start: the token generated then must
// be written down, or each start and `jarvisd hub pair` would invent another one and paired phones get 401.
func TestLoadConfigPersistsTokenMissingFromFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir on Windows
	dir := filepath.Join(home, ".jarvis")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "hub.json")
	if err := os.WriteFile(path, []byte(`{"database_url":"postgres://x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	again, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Token == "" || again.Token != cfg.Token {
		t.Fatalf("token changed between loads: %q then %q", cfg.Token, again.Token)
	}
	var raw map[string]any
	b, _ := os.ReadFile(path)
	if err := json.Unmarshal(b, &raw); err != nil || raw["database_url"] != "postgres://x" || raw["token"] != cfg.Token {
		t.Fatalf("hub.json = %s", b)
	}
}

func TestLoadConfigFillsMissingTimeout(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir on Windows
	dir := filepath.Join(home, ".jarvis")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := `{"listen":":1","token":"abc","public_url":"http://x","hook_timeout_minutes":0,"machines":[]}`
	if err := os.WriteFile(filepath.Join(dir, "hub.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HookTimeoutMinutes != 30 {
		t.Fatalf("hook timeout = %d", cfg.HookTimeoutMinutes)
	}
	if cfg.Listen != ":1" || cfg.Token != "abc" {
		t.Fatalf("explicit values were not kept: %+v", cfg)
	}
}

func TestLoadConfigRejectsEmptyToken(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir on Windows
	dir := filepath.Join(home, ".jarvis")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hub.json"), []byte(`{"listen":":1","token":""}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(); err == nil || !strings.Contains(err.Error(), "token is empty") {
		t.Fatalf("want a token error, got %v", err)
	}
}

func TestLoadConfigRejectsBadJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir on Windows
	dir := filepath.Join(home, ".jarvis")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hub.json"), []byte(`{`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(); err == nil || !strings.Contains(err.Error(), "parse") {
		t.Fatalf("want a parse error, got %v", err)
	}
}

func TestPairURLRoundTrips(t *testing.T) {
	cfg := &Config{PublicURL: "http://hub.example.com:18660", Token: "a b&c=d/é"}
	link := cfg.PairURL()
	if !strings.HasPrefix(link, "jarvis://pair?") {
		t.Fatalf("pair url = %q", link)
	}
	u, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme != "jarvis" || u.Host != "pair" {
		t.Fatalf("unexpected scheme/host %q %q", u.Scheme, u.Host)
	}
	q := u.Query()
	if q.Get("url") != cfg.PublicURL {
		t.Fatalf("url = %q", q.Get("url"))
	}
	if q.Get("token") != cfg.Token {
		t.Fatalf("token = %q", q.Get("token"))
	}
}

// The Mac has no camera to scan with: `jarvisd hub pair` prints the link too, to paste into 连接 hub.
func TestPairPrintsTheLinkForTheMac(t *testing.T) {
	cfg := &Config{PublicURL: "http://hub.example.com:18660", Token: "tok"}
	var b strings.Builder
	writePair(&b, cfg)
	out := b.String()
	if !strings.Contains(out, cfg.PairURL()) {
		t.Fatalf("pair output has no link:\n%s", out)
	}
	if !strings.Contains(out, "Server: "+cfg.PublicURL) {
		t.Fatalf("pair output lost the server line:\n%s", out)
	}
}

// newTestHub builds a Hub with no machines, no clients and push disabled, so interaction logic can be exercised offline.
func newTestHub(t *testing.T) *Hub {
	t.Helper()
	cfg := defaultConfig()
	cfg.Machines = nil
	cfg.dir = t.TempDir()
	h := &Hub{
		cfg: cfg, version: "test", logger: log.New(io.Discard, "", 0),
		byID: map[string]*Machine{}, clients: map[*client]struct{}{},
		interactions: map[string]*pending{}, calls: map[string]chan proto.ToolResult{}, notified: map[string]time.Time{}, turns: newTurnTracker(),
		push: newPusher(cfg.dir, nil, func(string, ...any) {}),
	}
	h.live = newLiveActivities(h)
	h.push.hubID = h.hubID
	h.upd = newUpdater(h)
	h.upd.dir = filepath.Join(cfg.dir, "update")
	h.upd.exec = func(string) error { return errors.New("exec is not run in tests") }
	h.upd.startWatch = func(string, string) error { return nil }
	return h
}

func TestWaitInteractionResolved(t *testing.T) {
	h := newTestHub(t)
	ix := proto.Interaction{ID: "ix_1", MachineID: "server", SessionID: "s1", Kind: proto.IxPermission, Summary: "send mail"}
	done := make(chan proto.Resolution, 1)
	go func() {
		r, err := h.waitInteraction(context.Background(), ix)
		if err != nil {
			t.Errorf("waitInteraction: %v", err)
		}
		done <- r
	}()

	// The interaction must show up as pending for the app.
	deadline := time.After(5 * time.Second)
	for {
		if len(h.pendingInteractions()) == 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("interaction never became pending")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	allow := true
	if err := h.resolve("ix_1", proto.Resolution{Allow: &allow}); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	select {
	case r := <-done:
		if r.Allow == nil || !*r.Allow {
			t.Fatalf("resolution = %+v", r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("waitInteraction did not return")
	}
	if got := h.pendingInteractions(); len(got) != 0 {
		t.Fatalf("interaction still pending: %+v", got)
	}
	if err := h.resolve("ix_1", proto.Resolution{}); err == nil || !strings.Contains(err.Error(), "no longer pending") {
		t.Fatalf("a second resolve should fail, got %v", err)
	}
}

func TestWaitInteractionExpiresAsDismiss(t *testing.T) {
	h := newTestHub(t)
	ix := proto.Interaction{ID: "ix_exp", MachineID: "server", Kind: proto.IxQuestion,
		ExpiresAt: time.Now().Add(50 * time.Millisecond)}
	r, err := h.waitInteraction(context.Background(), ix)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Dismiss {
		t.Fatalf("an expired interaction must dismiss, got %+v", r)
	}
	if got := h.pendingInteractions(); len(got) != 0 {
		t.Fatalf("expired interaction left behind: %+v", got)
	}
}

func TestWaitInteractionDefaultExpiry(t *testing.T) {
	h := newTestHub(t)
	ix := proto.Interaction{ID: "ix_def", MachineID: "server", Kind: proto.IxPlan}
	got := make(chan proto.Interaction, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_, _ = h.waitInteraction(ctx, ix)
	}()
	for {
		pending := h.pendingInteractions()
		if len(pending) == 1 {
			got <- pending[0]
			break
		}
		select {
		case <-time.After(5 * time.Second):
			t.Fatal("interaction never became pending")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
	cancel()
	announced := <-got
	want := time.Now().Add(time.Duration(h.cfg.HookTimeoutMinutes) * time.Minute)
	if diff := announced.ExpiresAt.Sub(want); diff > time.Minute || diff < -time.Minute {
		t.Fatalf("default expiry = %v, want about %v", announced.ExpiresAt, want)
	}
}

func TestWaitInteractionCancelledDismisses(t *testing.T) {
	h := newTestHub(t)
	ctx, cancel := context.WithCancel(context.Background())
	ix := proto.Interaction{ID: "ix_ctx", MachineID: "server", Kind: proto.IxPermission,
		ExpiresAt: time.Now().Add(time.Hour)}
	done := make(chan proto.Resolution, 1)
	go func() {
		r, _ := h.waitInteraction(ctx, ix)
		done <- r
	}()
	for len(h.pendingInteractions()) == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case r := <-done:
		if !r.Dismiss {
			t.Fatalf("cancelled interaction should dismiss, got %+v", r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancel did not unblock waitInteraction")
	}
}

func TestWaitInteractionReplacesScreenGuess(t *testing.T) {
	h := newTestHub(t)
	st := newMemStore()
	h.store = st
	c := &client{out: make(chan []byte, 32)}
	h.clients[c] = struct{}{}
	// A screen interaction (a guess from a blocked agent) is already pending for the session.
	scr := proto.Interaction{ID: "scr_server_s1", MachineID: "server", SessionID: "s1", Kind: proto.IxScreen,
		ExpiresAt: time.Now().Add(time.Hour)}
	h.record(scr)
	h.mu.Lock()
	h.interactions[scr.ID] = &pending{ix: scr, done: make(chan proto.Resolution, 1)}
	h.mu.Unlock()
	// The Feishu bot posted a card for the guess.
	h.bots.mu.Lock()
	h.bots.cards = map[string]botCard{scr.ID: {}}
	h.bots.mu.Unlock()

	ix := proto.Interaction{ID: "ix_hook", MachineID: "server", SessionID: "s1", Kind: proto.IxPermission,
		ExpiresAt: time.Now().Add(time.Hour)}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = h.waitInteraction(context.Background(), ix)
	}()
	for {
		h.mu.Lock()
		_, stillThere := h.interactions[scr.ID]
		_, hookThere := h.interactions[ix.ID]
		h.mu.Unlock()
		if hookThere {
			if stillThere {
				t.Fatal("the screen guess must be dropped once the hook speaks")
			}
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	// While the hook still waits, the app hears the guess is gone and the history no longer has it pending.
	resolved := false
	for deadline := time.Now().Add(time.Second); !resolved && time.Now().Before(deadline); {
		select {
		case b := <-c.out:
			resolved = strings.Contains(string(b), `"type":"interaction_resolved"`) && strings.Contains(string(b), scr.ID)
		case <-time.After(5 * time.Millisecond):
		}
	}
	if !resolved {
		t.Fatal("the app never heard the screen guess was withdrawn")
	}
	if s, _, _ := st.InteractionOutcome(context.Background(), scr.ID); s != "cancelled" {
		t.Fatalf("screen guess status %q, want cancelled", s)
	}
	h.bots.mu.Lock()
	_, card := h.bots.cards[scr.ID]
	h.bots.mu.Unlock()
	if card {
		t.Fatal("the bot's card for the screen guess was never closed")
	}
	_ = h.resolve("ix_hook", proto.Resolution{})
	<-done
}

func TestScreenInteractionResolveRunsLocally(t *testing.T) {
	h := newTestHub(t)
	ran := make(chan proto.Resolution, 1)
	h.mu.Lock()
	h.interactions["scr_x"] = &pending{
		ix:    proto.Interaction{ID: "scr_x", MachineID: "server", SessionID: "s1", Kind: proto.IxScreen},
		local: func(r proto.Resolution) error { ran <- r; return nil },
	}
	h.mu.Unlock()

	if err := h.resolve("scr_x", proto.Resolution{Keys: []string{"down", "enter"}}); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-ran:
		if len(r.Keys) != 2 {
			t.Fatalf("keys = %+v", r.Keys)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("local handler did not run")
	}
	if got := h.pendingInteractions(); len(got) != 0 {
		t.Fatalf("screen interaction still pending: %+v", got)
	}
}

func TestPendingInteractionsSortedByCreatedAt(t *testing.T) {
	h := newTestHub(t)
	base := time.Now().UTC()
	h.mu.Lock()
	for i, id := range []string{"b", "a", "c"} {
		h.interactions[id] = &pending{ix: proto.Interaction{ID: id, CreatedAt: base.Add(time.Duration(i) * time.Second)}}
	}
	h.mu.Unlock()
	var ids []string
	for _, ix := range h.pendingInteractions() {
		ids = append(ids, ix.ID)
	}
	if strings.Join(ids, ",") != "b,a,c" {
		t.Fatalf("ids = %v", ids)
	}
}

func TestCancelMachineInteractions(t *testing.T) {
	h := newTestHub(t)
	h.mu.Lock()
	h.interactions["a"] = &pending{ix: proto.Interaction{ID: "a", MachineID: "server"}, done: make(chan proto.Resolution, 1)}
	h.interactions["b"] = &pending{ix: proto.Interaction{ID: "b", MachineID: "laptop"}, done: make(chan proto.Resolution, 1)}
	h.mu.Unlock()
	h.cancelMachineInteractions("server")
	left := h.pendingInteractions()
	if len(left) != 1 || left[0].ID != "b" {
		t.Fatalf("left = %+v", left)
	}
}

func TestApproveAsksThePhone(t *testing.T) {
	h := newTestHub(t)
	done := make(chan bool, 1)
	go func() { done <- h.approve(context.Background(), "server", "mail_send", "send it", nil) }()

	deadline := time.After(5 * time.Second)
	for {
		pending := h.pendingInteractions()
		if len(pending) == 1 {
			ix := pending[0]
			if ix.Kind != proto.IxPermission || ix.ToolName != "mail_send" || ix.Summary != "send it" {
				t.Fatalf("unexpected interaction %+v", ix)
			}
			allow := true
			if err := h.resolve(ix.ID, proto.Resolution{Allow: &allow}); err != nil {
				t.Fatal(err)
			}
			break
		}
		select {
		case <-deadline:
			t.Fatal("no interaction reached the phone")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
	select {
	case ok := <-done:
		if !ok {
			t.Fatal("approve returned false")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("approve did not return")
	}
}

func TestApproveDeclined(t *testing.T) {
	h := newTestHub(t)
	done := make(chan bool, 1)
	go func() { done <- h.approve(context.Background(), "server", "mail_send", "send it", nil) }()
	for len(h.pendingInteractions()) == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	deny := false
	if err := h.resolve(h.pendingInteractions()[0].ID, proto.Resolution{Allow: &deny}); err != nil {
		t.Fatal(err)
	}
	select {
	case ok := <-done:
		if ok {
			t.Fatal("approve returned true for a denial")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("approve did not return")
	}
}

func TestCallPhoneWithoutPhone(t *testing.T) {
	h := newTestHub(t)
	r := h.callPhone(context.Background(), "phone_device_info", json.RawMessage(`{}`))
	if !r.IsError || !strings.Contains(textOf(r), "not connected") {
		t.Fatalf("unexpected result %+v", r)
	}
}

func TestToolResultDeliversToWaiter(t *testing.T) {
	h := newTestHub(t)
	ch := make(chan proto.ToolResult, 1)
	h.mu.Lock()
	h.calls["c1"] = ch
	h.mu.Unlock()
	h.toolResult("c1", proto.TextResult("hello", false))
	select {
	case r := <-ch:
		if textOf(r) != "hello" {
			t.Fatalf("result = %+v", r)
		}
	case <-time.After(time.Second):
		t.Fatal("tool result was not delivered")
	}
	// An unknown call id is ignored rather than blocking.
	h.toolResult("nope", proto.TextResult("x", false))
}

func TestCallToolDispatch(t *testing.T) {
	h := newTestHub(t)
	if r := h.callTool(context.Background(), "server", "phone_device_info", json.RawMessage(`{}`)); !r.IsError {
		t.Fatalf("phone tool without a phone should fail: %+v", r)
	}
	if r := h.callTool(context.Background(), "server", "mail_send", json.RawMessage(`{}`)); !r.IsError {
		t.Fatalf("mail tool without mail should fail: %+v", r)
	}
	r := h.callTool(context.Background(), "server", "something_else", nil)
	if !r.IsError || !strings.Contains(textOf(r), "unknown tool") {
		t.Fatalf("unknown tool result = %+v", r)
	}
}

func TestPhoneToolsPersist(t *testing.T) {
	url := os.Getenv("JARVIS_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("JARVIS_TEST_DATABASE_URL not set (scripts/test/test-server.sh runs this on the hub machine)")
	}
	database, err := db.Open(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	h := newTestHub(t)
	h.store = database
	tools := []proto.Tool{{Name: "phone_device_info", Description: "info", InputSchema: json.RawMessage(`{"type":"object"}`)}}
	h.setPhoneTools(db.PhoneToolRegistration{DeviceID: "legacy", Tools: tools})
	if got := h.mcpTools(false); len(got) != 1 || got[0].Name != "phone_device_info" {
		t.Fatalf("tools = %+v", got)
	}
	reloaded := newTestHub(t)
	reloaded.store = database
	reloaded.loadPhoneTools()
	if got := reloaded.mcpTools(false); len(got) != 1 || got[0].Name != "phone_device_info" {
		t.Fatalf("reloaded tools = %+v", got)
	}
}

func TestTrimScreen(t *testing.T) {
	got := trimScreen("\n  \n⏺ hi\n> \n\n")
	if got != "⏺ hi\n>" {
		t.Fatalf("trimScreen = %q", got)
	}
	if trimScreen("") != "" {
		t.Fatal("empty screen should stay empty")
	}
}

func textOf(r proto.ToolResult) string {
	var b strings.Builder
	for _, c := range r.Content {
		b.WriteString(c.Text)
	}
	return b.String()
}

func TestAgentState(t *testing.T) {
	now := time.Now()
	cases := []struct {
		s    proto.Session
		seen time.Time
		want string
	}{
		{proto.Session{Status: proto.StatusBlocked}, time.Time{}, proto.StateNeedsYou},
		{proto.Session{Status: proto.StatusWorking, PendingInteractions: 1}, time.Time{}, proto.StateNeedsYou},
		{proto.Session{Status: proto.StatusWorking}, time.Time{}, proto.StateWorking},
		{proto.Session{Status: proto.StatusDone, UpdatedAt: now}, time.Time{}, proto.StateUnread},
		{proto.Session{Status: proto.StatusDone, UpdatedAt: now}, now.Add(-time.Minute), proto.StateUnread},
		{proto.Session{Status: proto.StatusDone, UpdatedAt: now}, now.Add(time.Second), proto.StateIdle},
		{proto.Session{Status: proto.StatusIdle, UpdatedAt: now}, time.Time{}, proto.StateIdle},
		{proto.Session{Status: proto.StatusOffline}, time.Time{}, proto.StateOffline},
		{proto.Session{Status: proto.StatusUnknown}, time.Time{}, proto.StateUnknown},
	}
	for i, c := range cases {
		if got := agentState(c.s, c.seen); got != c.want {
			t.Errorf("case %d: %s, want %s", i, got, c.want)
		}
	}
}

func TestMarkStates(t *testing.T) {
	h := &Hub{}
	t0 := time.Now()
	list := []proto.Session{{SessionID: "a", State: proto.StateWorking}}
	h.markStates("m", list, t0)
	if !list[0].StateSince.Equal(t0) {
		t.Fatalf("first sight: %v", list[0].StateSince)
	}
	// Still working a step later: the time it started working stays.
	list = []proto.Session{{SessionID: "a", State: proto.StateWorking}}
	h.markStates("m", list, t0.Add(time.Minute))
	if !list[0].StateSince.Equal(t0) {
		t.Fatalf("same state moved: %v", list[0].StateSince)
	}
	list = []proto.Session{{SessionID: "a", State: proto.StateNeedsYou}}
	h.markStates("m", list, t0.Add(2*time.Minute))
	if !list[0].StateSince.Equal(t0.Add(2 * time.Minute)) {
		t.Fatalf("state change kept old time: %v", list[0].StateSince)
	}
}

func TestPhoneToolsDeviceRouting(t *testing.T) {
	h := newTestHub(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	iphone := &client{hub: h, device: "credential-ios", role: roleFull, out: make(chan []byte, 8)}
	android := &client{hub: h, device: "credential-android", role: roleFull, out: make(chan []byte, 8)}
	h.clients[iphone], h.clients[android] = struct{}{}, struct{}{}
	// Existing iOS omits both new fields; Android supplies its own device ID.
	h.onClientMessage(ctx, iphone, []byte(`{"type":"register_phone","tools":[{"name":"phone_reminders_list"},{"name":"phone_device_info","description":"Detailed device information"}]}`))
	h.onClientMessage(ctx, android, []byte(`{"type":"register_phone","device_id":"android-device","platform":"android","simulator":true,"tools":[{"name":"phone_notifications_list"},{"name":"phone_device_info","description":"Info"}]}`))
	if iphone.phoneID != iphone.device || android.phoneID != "android-device" {
		t.Fatalf("device IDs: %q, %q", iphone.phoneID, android.phoneID)
	}
	tools := h.mcpTools(false)
	if len(tools) != 3 {
		t.Fatalf("merged tools = %+v", tools)
	}
	for _, tool := range tools {
		if tool.Name == "phone_device_info" && tool.Description != "Detailed device information" {
			t.Fatalf("incomplete description: %+v", tool)
		}
	}
	for _, tc := range []struct {
		name string
		want *client
	}{
		{"phone_reminders_list", iphone},
		{"phone_notifications_list", android},
		{"phone_device_info", iphone},
	} {
		done := make(chan proto.ToolResult, 1)
		go func() { done <- h.callPhone(ctx, tc.name, json.RawMessage(`{}`)) }()
		var data []byte
		select {
		case data = <-iphone.out:
			if tc.want != iphone {
				t.Fatalf("%s routed to iOS", tc.name)
			}
		case data = <-android.out:
			if tc.want != android {
				t.Fatalf("%s routed to Android", tc.name)
			}
		case <-ctx.Done():
			t.Fatal("tool call was not routed")
		}
		var call struct {
			Name   string `json:"name"`
			CallID string `json:"call_id"`
		}
		if err := json.Unmarshal(data, &call); err != nil || call.Name != tc.name {
			t.Fatalf("tool call = %s, %v", data, err)
		}
		reply, _ := json.Marshal(map[string]any{"type": "tool_result", "call_id": call.CallID, "content": []proto.Content{{Type: "text", Text: tc.name}}})
		h.onClientMessage(ctx, tc.want, reply)
		select {
		case result := <-done:
			if result.IsError || textOf(result) != tc.name {
				t.Fatalf("result = %+v", result)
			}
		case <-ctx.Done():
			t.Fatal("tool result was not delivered")
		}
	}
	// With both devices real, the last active eligible connection wins.
	h.mu.Lock()
	android.sim = false
	h.mu.Unlock()
	h.onClientMessage(ctx, android, []byte(`{"type":"ping"}`))
	if got := h.phoneClient("phone_device_info"); got != android {
		t.Fatal("recently active device was not selected")
	}
	// An empty registration removes only this device's capabilities.
	h.onClientMessage(ctx, android, []byte(`{"type":"register_phone","device_id":"android-device","platform":"android","tools":[]}`))
	if h.phoneClient("phone_notifications_list") != nil || len(h.mcpTools(false)) != 2 {
		t.Fatal("empty registration did not remove Android tools")
	}
	h.mu.Lock()
	delete(h.clients, iphone)
	h.mu.Unlock()
	h.revoked(iphone.device)
	if len(h.mcpTools(false)) != 0 {
		t.Fatal("revoked credential retained tools")
	}
}

package proc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/connector/serve"
	"github.com/nwverse/persona/server/internal/kb"
	"github.com/nwverse/persona/server/internal/plugin"
	"github.com/nwverse/persona/server/internal/plugin/host"
	pserve "github.com/nwverse/persona/server/internal/plugin/serve"
	"github.com/nwverse/persona/server/internal/proto"
)

// The test binary is also the program: run with JARVIS_PROC_CHILD=1 it serves exampleProgram on stdin / stdout.
const childVersion = "v-test\nchannel dev, build 0"

func TestMain(m *testing.M) {
	if os.Getenv("JARVIS_PROC_CHILD") == "1" {
		if len(os.Args) > 1 && os.Args[1] == "version" {
			fmt.Println(childVersion)
			os.Exit(0)
		}
		err := serve.Main(context.Background(), os.Stdin, os.Stdout, func(cfg json.RawMessage, s connector.Secrets) (connector.Connector, error) {
			return &exampleProgram{secrets: s, exitSoon: os.Getenv("JARVIS_PROC_EXIT") == "1"}, nil
		})
		if err != nil && !strings.Contains(err.Error(), "EOF") {
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// exampleProgram signs in by phone and code, archives one made-up chat, reads, sends (an unlisted action) and tells
// about changes; no inbox.
type exampleProgram struct {
	secrets  connector.Secrets
	exitSoon bool
	once     sync.Once
	sent     int
}

func (*exampleProgram) ID() string   { return "example" }
func (*exampleProgram) Name() string { return "Example" }

func (*exampleProgram) Manifest() connector.Manifest {
	return connector.Manifest{Capabilities: []connector.Capability{connector.CapStatus, connector.CapTools, connector.CapActions,
		connector.CapEvents, connector.CapArchive, connector.CapLogin, connector.CapThreads}, AgentTools: []string{"example_chats"},
		Unlisted: []string{"example_send"}, Poll: 3 * time.Minute}
}

func (p *exampleProgram) Status(ctx context.Context) connector.Status {
	if p.exitSoon {
		p.once.Do(func() { go func() { time.Sleep(50 * time.Millisecond); os.Exit(3) }() })
	}
	if s, _ := p.secrets.Load(ctx); s == nil {
		return connector.Status{State: connector.StateNotSignedIn, Error: "还没登录"}
	}
	return connector.Status{State: connector.StateOK, Account: "示例用户"}
}

func (p *exampleProgram) LoginStep(ctx context.Context, step string, v map[string]string) (connector.Prompt, error) {
	code := connector.Prompt{Step: "code", Fields: []connector.Field{{Name: "code", Label: "验证码"}}}
	switch step {
	case "phone":
		return code, nil
	case "code":
		if v["code"] != "12345" {
			code.Error = "验证码不对"
			return code, nil
		}
		return connector.Prompt{Done: true, Account: "示例用户"}, p.secrets.Save(ctx, []byte("example session"))
	}
	return connector.Prompt{Step: "phone", Fields: []connector.Field{{Name: "phone", Label: "手机号"}}}, nil
}

func (p *exampleProgram) Logout(ctx context.Context) error { return p.secrets.Delete(ctx) }

func (*exampleProgram) Changes(_ context.Context, cursor string, _ int) ([]connector.ChatBatch, string, bool, error) {
	if cursor == "1" {
		return nil, "1", false, nil
	}
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	return []connector.ChatBatch{{Chat: kb.SourceChat{ID: "g1", Name: "示例群", Kind: "group"},
		Messages: []kb.SourceMessage{{ID: "1", SenderName: "示例用户", Text: "示例消息", At: at}}}}, "1", false, nil
}

func (*exampleProgram) Tools() []proto.Tool { return []proto.Tool{{Name: "example_chats"}} }
func (*exampleProgram) Call(_ context.Context, name string, _ json.RawMessage) (string, error) {
	return name + ": 2 chats", nil
}

func (*exampleProgram) Actions() []proto.Tool { return []proto.Tool{{Name: "example_send"}} }
func (*exampleProgram) Describe(_ context.Context, name string, args json.RawMessage) (connector.Action, string, error) {
	var a struct{ Text string }
	if err := json.Unmarshal(args, &a); err != nil || a.Text == "" {
		return connector.Action{}, "", fmt.Errorf("text is required")
	}
	return connector.Action{Name: name, Visible: "示例群的所有人"}, "发到示例群：" + a.Text, nil
}
func (p *exampleProgram) Do(_ context.Context, _ string, args json.RawMessage) (string, error) {
	p.sent++
	return fmt.Sprintf("sent #%d: %s", p.sent, args), nil
}

func (*exampleProgram) Thread(_ context.Context, thread, before string, limit int) (connector.ThreadPage, error) {
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	return connector.ThreadPage{Chat: connector.ThreadChat{ID: thread, Name: "示例群", Kind: "group", Reply: "example_send"},
		Messages: []connector.ThreadMessage{{ID: "7", At: at, Sender: connector.ThreadPerson{ID: "u1", Name: "示例用户", Avatar: "avatar:u1"},
			Text: "示例消息", Media: []connector.ThreadMedia{{Kind: "sticker", Ref: "sticker:7", Emoji: "👍", Width: 512, Height: 512}},
			ReplyTo: &connector.ThreadQuote{ID: "6", From: "我", Text: "示例问题"}}}, Before: before + "6"}, nil
}

func (*exampleProgram) Media(_ context.Context, ref string) (connector.Media, error) {
	return connector.Media{Mime: "image/webp", Data: []byte{0, 1, 2, 255}}, nil
}

func (*exampleProgram) Watch(ctx context.Context, changed func(connector.Scope)) error {
	changed(connector.ScopeChats)
	<-ctx.Done()
	return nil
}

// memSecrets is the hub's store for one connector, in memory.
type memSecrets struct {
	mu    sync.Mutex
	data  []byte
	loads int
}

func (m *memSecrets) Load(context.Context) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.loads++
	return m.data, nil
}
func (m *memSecrets) Save(_ context.Context, d []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data = d
	return nil
}
func (m *memSecrets) Delete(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data = nil
	return nil
}
func (m *memSecrets) get() ([]byte, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.data, m.loads
}

func program(t *testing.T, s connector.Secrets, env ...string) Config {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return Config{ID: "example", Name: "Example", Path: exe, Env: append([]string{"JARVIS_PROC_CHILD=1"}, env...),
		Version: childVersion, Secrets: s, Logf: t.Logf}
}

func waitUntil(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

// The program runs under the hub: it says what it has, signs in a step at a time with its session kept by the hub,
// archives, reads, and its changes reach the hub's Watch.
func TestProgram(t *testing.T) {
	sec := &memSecrets{}
	exercise(t, New(program(t, sec)), sec)
}

// linked is the program run as a plugin by the plugin host, the connector reaching it through its Link (the hub's
// way, docs/plugins.md); the hub's part (passing the program's calls and events to the connector) is done here.
func linked(t *testing.T, s connector.Secrets) *Conn {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var c *Conn
	p := host.Start(context.Background(), host.Spec{ID: "example-plugin", Name: "Example", Command: exe, RPC: true,
		Env: []string{"JARVIS_PROC_CHILD=1"}, CallTimeout: callTimeout, Logf: t.Logf,
		Init: func(context.Context) (plugin.InitializeParams, error) {
			return plugin.InitializeParams{Plugin: plugin.PluginInfo{ID: "example-plugin", Version: "1.0.0"},
				Active: map[string][]string{"connectors": {"example"}}}, nil
		},
		Handle: func(ctx context.Context, method string, params json.RawMessage) (any, error) {
			if pserve.Contribution(params) != "example" {
				return nil, errors.New("another connector's")
			}
			return c.Handle(ctx, strings.TrimPrefix(method, "connector."), params)
		},
		OnEvent: func(event string, params json.RawMessage) { c.Event(strings.TrimPrefix(event, "connector."), params) },
	})
	t.Cleanup(p.Stop)
	cfg := Config{ID: "example", Name: "Example", Manifest: (&exampleProgram{}).Manifest(), Secrets: s, Logf: t.Logf,
		Link: &Link{Proc: p, ID: "example"}}
	c = New(cfg)
	return c
}

func TestProgramAsPlugin(t *testing.T) {
	sec := &memSecrets{}
	exercise(t, linked(t, sec), sec)
}

func exercise(t *testing.T, c *Conn, sec *memSecrets) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if st := c.Status(ctx); st.State != connector.StateError {
		t.Fatalf("before Run: %+v", st)
	}
	go c.Run(ctx)
	waitUntil(t, "the program's status", func() bool { return c.Status(ctx).State == connector.StateNotSignedIn })
	if got := connector.Capabilities(c); !slices.Equal(got, []string{"status", "tools", "actions", "events", "archive", "login", "threads"}) {
		t.Fatalf("the registry lists %v", got)
	}
	if m := c.Manifest(); !m.Proxy || !m.ForAgents("example_chats") || m.Poll != 3*time.Minute || m.Listed("example_send") {
		t.Fatalf("manifest %+v", m)
	}
	if _, ok := connector.As[connector.Inbox](c); ok {
		t.Fatal("no inbox, yet the hub would collect it")
	}

	p, err := c.LoginStep(ctx, "", nil)
	if err != nil || p.Step != "phone" {
		t.Fatalf("start %+v %v", p, err)
	}
	if p, _ = c.LoginStep(ctx, "phone", map[string]string{"phone": "+1 555 0100"}); p.Step != "code" {
		t.Fatalf("phone %+v", p)
	}
	if p, _ = c.LoginStep(ctx, "code", map[string]string{"code": "0"}); p.Error != "验证码不对" {
		t.Fatalf("wrong code %+v", p)
	}
	if p, _ = c.LoginStep(ctx, "code", map[string]string{"code": "12345"}); !p.Done {
		t.Fatalf("signed in %+v", p)
	}
	if d, _ := sec.get(); string(d) != "example session" {
		t.Fatalf("the hub keeps %q", d)
	}
	if st := c.Status(ctx); st.State != connector.StateOK || st.Account != "示例用户" {
		t.Fatalf("after signing in %+v", st)
	}

	batches, next, _, err := c.Changes(ctx, "", 20)
	if err != nil || next != "1" || len(batches) != 1 || batches[0].Messages[0].Text != "示例消息" || !batches[0].Messages[0].At.Equal(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)) {
		t.Fatalf("changes %+v %q %v", batches, next, err)
	}
	if tools := c.Tools(); len(tools) != 1 || tools[0].Name != "example_chats" {
		t.Fatalf("tools %+v", tools)
	}
	if out, err := c.Call(ctx, "example_chats", json.RawMessage(`{}`)); err != nil || out != "example_chats: 2 chats" {
		t.Fatalf("call %q %v", out, err)
	}
	// A write: described for the card, then run with the very args; each run is its own (a new request id).
	if acts := c.Actions(); len(acts) != 1 || acts[0].Name != "example_send" {
		t.Fatalf("actions %+v", acts)
	}
	if act, summary, err := c.Describe(ctx, "example_send", json.RawMessage(`{"text":"示例回复"}`)); err != nil ||
		act.Visible != "示例群的所有人" || summary != "发到示例群：示例回复" {
		t.Fatalf("describe %+v %q %v", act, summary, err)
	}
	if _, _, err := c.Describe(ctx, "example_send", json.RawMessage(`{}`)); err == nil || err.Error() != "text is required" {
		t.Fatalf("describe without text: %v", err)
	}
	for i := 1; i <= 2; i++ {
		if out, err := c.Do(ctx, "example_send", json.RawMessage(`{"text":"示例回复"}`)); err != nil || out != fmt.Sprintf(`sent #%d: {"text":"示例回复"}`, i) {
			t.Fatalf("do %d: %q %v", i, out, err)
		}
	}
	// A thread's page and a media's bytes over the wire, every field as the program gave it.
	page, err := c.Thread(ctx, "g1", "", 20)
	if err != nil || page.Chat.Reply != "example_send" || len(page.Messages) != 1 || page.Before != "6" {
		t.Fatalf("thread %+v %v", page, err)
	}
	if m := page.Messages[0]; m.ReplyTo == nil || m.ReplyTo.Text != "示例问题" || m.Media[0].Emoji != "👍" || m.Sender.Avatar != "avatar:u1" ||
		!m.At.Equal(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)) {
		t.Fatalf("message %+v", m)
	}
	if m, err := c.Media(ctx, "sticker:7"); err != nil || m.Mime != "image/webp" || !bytes.Equal(m.Data, []byte{0, 1, 2, 255}) {
		t.Fatalf("media %+v %v", m, err)
	}
	got := make(chan connector.Scope, 4)
	wctx, wcancel := context.WithCancel(ctx)
	go c.Watch(wctx, func(scope connector.Scope) { got <- scope })
	select {
	case s := <-got:
		if s != connector.ScopeChats {
			t.Fatalf("scope %q", s)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the program's change did not reach the hub")
	}
	wcancel()

	if err := c.Logout(ctx); err != nil {
		t.Fatal(err)
	}
	if d, _ := sec.get(); d != nil {
		t.Fatal("signed out, the session is still kept")
	}
	cancel()
	waitUntil(t, "the program to stop", func() bool { return strings.Contains(c.Status(context.Background()).Error, "已停止") })
}

// As a plugin: signing in while it does not run says to switch it on; signing out runs it just for that.
func TestPluginWhileStopped(t *testing.T) {
	sec := &memSecrets{data: []byte("example session")}
	c := linked(t, sec)
	if p, err := c.LoginStep(context.Background(), "", nil); err != nil || !strings.Contains(p.Error, "先打开") {
		t.Fatalf("login: %+v %v", p, err)
	}
	if err := c.Logout(context.Background()); err != nil {
		t.Fatal(err)
	}
	if d, _ := sec.get(); d != nil {
		t.Fatal("still signed in")
	}
	if st := c.cfg.Link.Proc; st.Ready() {
		waitUntil(t, "it to stop again", func() bool { s, _ := st.Status(); return s == host.StateStopped })
	}
}

// A program from another build is not started: the two must match.
func TestProgramOfAnotherVersion(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sec := &memSecrets{}
	cfg := program(t, sec)
	cfg.Version = "v-other\nchannel release, build 202601010000"
	c := New(cfg)
	go c.Run(ctx)
	waitUntil(t, "the version check", func() bool { return strings.Contains(c.Status(ctx).Error, "不是同一个版本") })
	if _, loads := sec.get(); loads != 0 {
		t.Fatal("the program ran")
	}
	if p, err := c.LoginStep(ctx, "", nil); err != nil || p.Error == "" {
		t.Fatalf("signing in without the program: %+v %v", p, err)
	}
}

// A program that exits is started again; meanwhile its status says it is restarting.
func TestProgramRestarts(t *testing.T) {
	restartFirst = 20 * time.Millisecond
	t.Cleanup(func() { restartFirst = 5 * time.Second })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sec := &memSecrets{}
	c := New(program(t, sec, "JARVIS_PROC_EXIT=1"))
	go c.Run(ctx)
	waitUntil(t, "a restart", func() bool { _, loads := sec.get(); return loads >= 2 })
}

// Signing out while the connector is switched off (the program not running) runs the program just for that.
func TestLogoutWhileStopped(t *testing.T) {
	sec := &memSecrets{data: []byte("example session")}
	c := New(program(t, sec))
	if err := c.Logout(context.Background()); err != nil {
		t.Fatal(err)
	}
	if d, _ := sec.get(); d != nil {
		t.Fatal("still signed in")
	}
}

// A connector whose program never ran (switched off from the start) answers with the manifest it was registered
// with: the capabilities are listed, signing in says to switch it on, signing out runs the program for that (#666).
func TestNeverStartedDeclaresItsManifest(t *testing.T) {
	ctx := context.Background()
	sec := &memSecrets{data: []byte("example session")}
	cfg := program(t, sec)
	cfg.Manifest = (&exampleProgram{}).Manifest()
	c := New(cfg)
	if got := connector.Capabilities(c); !slices.Equal(got, []string{"status", "tools", "actions", "events", "archive", "login", "threads"}) {
		t.Fatalf("before it ever ran: %v", got)
	}
	l, ok := connector.As[connector.Login](c)
	if !ok {
		t.Fatal("no sign-in before it ever ran")
	}
	if p, err := l.LoginStep(ctx, "", nil); err != nil || !strings.Contains(p.Error, "先打开") {
		t.Fatalf("login: %+v %v", p, err)
	}
	if err := l.Logout(ctx); err != nil {
		t.Fatal(err)
	}
	if d, _ := sec.get(); d != nil {
		t.Fatal("still signed in")
	}
}

// The program declaring something else than the hub registered it with is logged; the hub's declaration stays.
func TestProgramDeclaringOtherwiseIsLogged(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg := program(t, &memSecrets{})
	cfg.Manifest = connector.Manifest{Capabilities: []connector.Capability{connector.CapStatus, connector.CapLogin}}
	var mu sync.Mutex
	var logged []string
	cfg.Logf = func(f string, a ...any) { mu.Lock(); logged = append(logged, fmt.Sprintf(f, a...)); mu.Unlock() }
	c := New(cfg)
	go c.Run(ctx)
	waitUntil(t, "the program's status", func() bool { return c.Status(ctx).State == connector.StateNotSignedIn })
	mu.Lock()
	defer mu.Unlock()
	if !slices.ContainsFunc(logged, func(l string) bool { return strings.Contains(l, "the hub registered it with") }) {
		t.Fatalf("not logged: %q", logged)
	}
	if got := connector.Capabilities(c); !slices.Equal(got, []string{"status", "login"}) {
		t.Fatalf("listed %v", got)
	}
	// The same declaration in another order is the same.
	if !sameManifest(connector.Manifest{Capabilities: []connector.Capability{connector.CapLogin, connector.CapStatus}},
		connector.Manifest{Capabilities: []connector.Capability{connector.CapStatus, connector.CapLogin}, Proxy: true}) {
		t.Fatal("order matters")
	}
}

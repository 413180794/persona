package proc

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chyroc/jarvis/server/internal/connector"
	"github.com/chyroc/jarvis/server/internal/connector/serve"
	"github.com/chyroc/jarvis/server/internal/kb"
	"github.com/chyroc/jarvis/server/internal/proto"
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

// exampleProgram signs in by phone and code, archives one made-up chat, reads, and tells about changes; no inbox.
type exampleProgram struct {
	secrets  connector.Secrets
	exitSoon bool
	once     sync.Once
}

func (*exampleProgram) ID() string   { return "example" }
func (*exampleProgram) Name() string { return "Example" }

func (*exampleProgram) Manifest() connector.Manifest {
	return connector.Manifest{Capabilities: []connector.Capability{connector.CapStatus, connector.CapTools, connector.CapEvents,
		connector.CapArchive, connector.CapLogin}, AgentTools: []string{"example_chats"}, Poll: 3 * time.Minute}
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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sec := &memSecrets{}
	c := New(program(t, sec))
	if st := c.Status(ctx); st.State != connector.StateError {
		t.Fatalf("before Run: %+v", st)
	}
	go c.Run(ctx)
	waitUntil(t, "the program's status", func() bool { return c.Status(ctx).State == connector.StateNotSignedIn })
	if got := connector.Capabilities(c); !slices.Equal(got, []string{"status", "tools", "events", "archive", "login"}) {
		t.Fatalf("the registry lists %v", got)
	}
	if m := c.Manifest(); !m.Proxy || !m.ForAgents("example_chats") || m.Poll != 3*time.Minute {
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

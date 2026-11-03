package hub

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/kb"
)

// keepStore is memStore with settings and connector secrets kept in memory.
type keepStore struct {
	*memStore
	mu       sync.Mutex
	settings map[string]string
	secrets  map[string]string
}

func newKeepStore() *keepStore {
	return &keepStore{memStore: newMemStore(), settings: map[string]string{}, secrets: map[string]string{}}
}

func (s *keepStore) Setting(_ context.Context, key string, out any) (bool, error) {
	s.mu.Lock()
	raw, ok := s.settings[key]
	s.mu.Unlock()
	if !ok {
		return false, nil
	}
	return true, json.Unmarshal([]byte(raw), out)
}

func (s *keepStore) PutSetting(_ context.Context, key string, v any) error {
	raw, err := json.Marshal(v)
	s.mu.Lock()
	s.settings[key] = string(raw)
	s.mu.Unlock()
	return err
}

func (s *keepStore) ConnectorSecret(_ context.Context, id string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.secrets[id]
	return v, ok, nil
}

func (s *keepStore) PutConnectorSecret(_ context.Context, id, sealed string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.secrets[id] = sealed
	return nil
}

func (s *keepStore) DeleteConnectorSecret(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.secrets, id)
	return nil
}

// signInConnector signs in by phone, code and (made-up) 2FA password, keeping its session in the hub's secrets.
type signInConnector struct {
	exampleConnector
	secrets connector.Secrets
	phone   string
	out     bool
}

func (*signInConnector) ID() string   { return "example" }
func (*signInConnector) Name() string { return "Example" }
func (*signInConnector) Manifest() connector.Manifest {
	return connector.Manifest{Capabilities: []connector.Capability{connector.CapStatus, connector.CapTools, connector.CapActions,
		connector.CapLogin}}
}

func (c *signInConnector) LoginStep(ctx context.Context, step string, v map[string]string) (connector.Prompt, error) {
	phone := connector.Prompt{Step: "phone", Fields: []connector.Field{{Name: "phone", Label: "手机号", Placeholder: "+1 555 0100"}}}
	code := connector.Prompt{Step: "code", Fields: []connector.Field{{Name: "code", Label: "验证码"}}, Hint: "验证码发到了你别的设备上"}
	password := connector.Prompt{Step: "password", Fields: []connector.Field{{Name: "password", Label: "两步验证密码", Secret: true}}}
	switch step {
	case "":
		return phone, nil
	case "phone":
		c.phone = v["phone"]
		return code, nil
	case "code":
		if v["code"] != "12345" {
			code.Error = "验证码不对"
			return code, nil
		}
		return password, nil
	case "password":
		if v["password"] != "example-password" {
			password.Error = "密码不对"
			return password, nil
		}
		if err := c.secrets.Save(ctx, []byte("session for "+c.phone)); err != nil {
			return connector.Prompt{}, err
		}
		return connector.Prompt{Done: true, Account: "示例用户"}, nil
	}
	return phone, nil
}

func (c *signInConnector) Logout(ctx context.Context) error {
	c.out = true
	return c.secrets.Delete(ctx)
}

// Signing in goes a step at a time through the hub: each answer is the next Prompt, a wrong code asks again with
// a reason; the session ends up sealed in the hub's store, and signing out removes it.
func TestConnectorLoginSteps(t *testing.T) {
	h := newTestHub(t)
	st := newKeepStore()
	h.store = st
	h.connectors = connector.NewRegistry(nil)
	c := &signInConnector{secrets: h.secretsFor("example")}
	if err := h.addConnector(c); err != nil {
		t.Fatal(err)
	}
	mux := h.routes()
	post := func(body string) map[string]any {
		t.Helper()
		code, out := call(t, mux, "POST", "/api/v1/connectors/example/login", h.cfg.Token, body)
		if code != 200 {
			t.Fatalf("%d %v", code, out)
		}
		return out
	}
	if out := post(`{"step":""}`); out["step"] != "phone" {
		t.Fatalf("start %v", out)
	}
	if out := post(`{"step":"phone","values":{"phone":"+1 555 0100"}}`); out["step"] != "code" || out["hint"] == nil {
		t.Fatalf("after the phone %v", out)
	}
	if out := post(`{"step":"code","values":{"code":"00000"}}`); out["step"] != "code" || out["error"] != "验证码不对" {
		t.Fatalf("a wrong code %v", out)
	}
	if out := post(`{"step":"code","values":{"code":"12345"}}`); out["step"] != "password" {
		t.Fatalf("after the code %v", out)
	} else if f := out["fields"].([]any)[0].(map[string]any); f["secret"] != true {
		t.Fatalf("the password field is not secret: %v", f)
	}
	if out := post(`{"step":"password","values":{"password":"example-password"}}`); out["done"] != true || out["account"] != "示例用户" {
		t.Fatalf("signed in %v", out)
	}
	sealed := st.secrets["example"]
	if sealed == "" || strings.Contains(sealed, "session") || strings.Contains(sealed, "555") {
		t.Fatalf("the stored session is not sealed: %q", sealed)
	}
	if got, _ := h.secretsFor("example").Load(context.Background()); string(got) != "session for +1 555 0100" {
		t.Fatalf("load %q", got)
	}
	if got, _ := h.secretsFor("other").Load(context.Background()); got != nil {
		t.Fatalf("another connector reads it: %q", got)
	}
	if code, _ := call(t, mux, "POST", "/api/v1/connectors/example/logout", h.cfg.Token, ""); code != 200 || !c.out {
		t.Fatalf("logout %d", code)
	}
	if _, ok := st.secrets["example"]; ok {
		t.Fatal("the session is still stored after signing out")
	}
	if code, _ := call(t, mux, "POST", "/api/v1/connectors/nobody/login", h.cfg.Token, `{}`); code != 404 {
		t.Fatalf("unknown connector: %d", code)
	}
}

// A connector without a sign-in says so.
func TestConnectorWithoutLogin(t *testing.T) {
	h := newTestHub(t)
	h.connectors = connector.NewRegistry(nil)
	_ = h.addConnector(&exampleConnector{})
	if code, _ := call(t, h.routes(), "POST", "/api/v1/connectors/example/login", h.cfg.Token, `{}`); code != 409 {
		t.Fatalf("%d", code)
	}
}

// The switch in 设置 › 连接器: off stops the connector and is kept, so the next hub starts it switched off.
func TestConnectorSwitchIsKept(t *testing.T) {
	h := newTestHub(t)
	st := newKeepStore()
	h.store = st
	h.connectors = connector.NewRegistry(nil)
	_ = h.addConnector(&exampleConnector{})
	mux := h.routes()
	code, out := call(t, mux, "PUT", "/api/v1/connectors/example/settings", h.cfg.Token, `{"enabled":false}`)
	if code != 200 || out["enabled"] != false || out["status"] != connector.StateDisabled {
		t.Fatalf("%d %v", code, out)
	}
	if len(h.connectors.Tools(context.Background())) != 0 {
		t.Fatal("a switched-off connector offers tools")
	}

	next := newTestHub(t)
	next.store = st
	next.connectors = connector.NewRegistry(nil)
	_ = next.addConnector(&exampleConnector{})
	next.restoreConnectorSwitches(context.Background())
	if next.connectors.Enabled("example") {
		t.Fatal("the next hub forgot the switch")
	}

	if code, out := call(t, mux, "PUT", "/api/v1/connectors/example/settings", h.cfg.Token, `{"enabled":true}`); code != 200 || out["enabled"] != true {
		t.Fatalf("back on: %d %v", code, out)
	}
	if !strings.Contains(st.settings[connectorSettingsKey], `"example":true`) {
		t.Fatalf("the choice to switch it back on is not kept: %s", st.settings[connectorSettingsKey])
	}
}

// A connector added to start off stays off until the user turns it on, and then stays on.
func TestConnectorThatStartsOff(t *testing.T) {
	h := newTestHub(t)
	st := newKeepStore()
	h.store = st
	h.connectors = connector.NewRegistry(nil)
	_ = h.addConnectorOff(&exampleConnector{})
	h.restoreConnectorSwitches(context.Background())
	if h.connectors.Enabled("example") {
		t.Fatal("it started on")
	}
	if code, _ := call(t, h.routes(), "PUT", "/api/v1/connectors/example/settings", h.cfg.Token, `{"enabled":true}`); code != 200 {
		t.Fatal(code)
	}
	next := newTestHub(t)
	next.store = st
	next.connectors = connector.NewRegistry(nil)
	_ = next.addConnectorOff(&exampleConnector{})
	next.restoreConnectorSwitches(context.Background())
	if !next.connectors.Enabled("example") {
		t.Fatal("turned on by the user, it is off again after a restart")
	}
}

// jarvisd-telegram gets the user's time zone by name, and none while only the hub's own is known (the hub runs in
// UTC: its zone is not the user's).
func TestTelegramGetsTheUserZone(t *testing.T) {
	h := newTestHub(t)
	if got := string(h.telegramSettings()); got != `{"zone":""}` {
		t.Fatalf("unknown: %s", got)
	}
	h.userTZ = "Asia/Shanghai"
	if got := string(h.telegramSettings()); got != `{"zone":"Asia/Shanghai"}` {
		t.Fatalf("known: %s", got)
	}
}

// 删除已存的数据: the connector is switched off (and stays off), its chats, people and inbox items go, the rest stays.
func TestConnectorDataDelete(t *testing.T) {
	h := newTestHub(t)
	st := newKeepStore()
	h.store = st
	h.connectors = connector.NewRegistry(nil)
	h.inbox.init()
	_ = h.addConnector(&exampleConnector{})
	mux := h.routes()
	kst, err := kb.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h.kb.eng = &kb.Engine{Store: kst}
	_ = kst.PutChat(&kb.Chat{ID: "example-c1", Source: "example", Name: "示例群"})
	_ = kst.PutChat(&kb.Chat{ID: "lark-f1", Source: "lark", Name: "需求群"})
	_ = kst.PutPerson(&kb.Person{ID: "example-p1", Source: "example", Name: "示例用户"})
	h.inbox.items["example:c1"] = &InboxItem{ID: "example:c1", Source: "example", Title: "示例群"}
	h.inbox.items["lark:f1:1"] = &InboxItem{ID: "lark:f1:1", Source: "lark", Title: "需求群"}

	if code, _ := call(t, mux, "DELETE", "/api/v1/connectors/nope/data", h.cfg.Token, ""); code != 404 {
		t.Fatalf("an unknown connector: %d", code)
	}
	code, out := call(t, mux, "DELETE", "/api/v1/connectors/example/data", h.cfg.Token, "")
	if code != 200 || out["chats"] != 1.0 || out["people"] != 1.0 || out["inbox"] != 1.0 {
		t.Fatalf("delete: %d %v", code, out)
	}
	if h.connectors.Enabled("example") || !strings.Contains(st.settings[connectorSettingsKey], `"example":false`) {
		t.Fatalf("not switched off for good: %s", st.settings[connectorSettingsKey])
	}
	if _, ok := kst.Chat("example-c1"); ok {
		t.Error("the chat is still there")
	}
	if _, ok := kst.Chat("lark-f1"); !ok {
		t.Error("Feishu's chat is gone")
	}
	if _, ok := h.inbox.items["example:c1"]; ok {
		t.Error("the inbox item is still there")
	}
	if _, ok := h.inbox.items["lark:f1:1"]; !ok {
		t.Error("Feishu's inbox item is gone")
	}
}

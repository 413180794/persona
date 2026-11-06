package hub

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/proto"
)

// The admin UI's switch for the assistant's cards (#2023): written into hub.json with everything else kept, live at
// once (Hello, which the assistant's CLAUDE.md is written from), and behind the admin UI's session and CSRF.
func TestAdminAssistantPersonaView(t *testing.T) {
	h, _, mux := adminTestHub(t)
	path := filepath.Join(h.cfg.dir, "hub.json")
	orig := `{
  "token": "placeholder",
  "assistant": {"owner": "示例", "notes": "备注", "x_future": {"a": [1, "<b>"]}, "launcher": "claude"},
  "zzz_other": true
}`
	if err := os.WriteFile(path, []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}
	hello := func() bool {
		t.Helper()
		out, err := assistantLink{h}.Hello(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		return out.PersonaView
	}
	file := func() map[string]json.RawMessage {
		t.Helper()
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var top map[string]json.RawMessage
		if err := json.Unmarshal(b, &top); err != nil {
			t.Fatal(err)
		}
		return top
	}

	// Not signed in: nothing to read or write.
	anon := &adminClient{t: t, h: mux, remote: ip4(192, 168, 1, 20) + ":1"}
	if code, _, _ := anon.do("GET", "/api/v1/admin/assistant", ""); code != 401 {
		t.Fatalf("anonymous read %d", code)
	}
	c := signedIn(t, mux, ip4(192, 168, 1, 20)+":1")
	if code, out, _ := c.do("GET", "/api/v1/admin/assistant", ""); code != 200 || out["persona_view"] != false {
		t.Fatalf("read %d %v", code, out)
	}
	csrf := c.csrf
	c.csrf = "wrong"
	if code, out, _ := c.do("PUT", "/api/v1/admin/assistant", `{"persona_view":true}`); code != 403 || out["code"] != "csrf" {
		t.Fatalf("bad csrf %d %v", code, out)
	}
	c.csrf = csrf
	if code, _, _ := c.do("PUT", "/api/v1/admin/assistant", `{}`); code != 400 {
		t.Fatalf("missing field %d", code)
	}
	if hello() {
		t.Fatal("refused writes switched it on")
	}

	if code, out, _ := c.do("PUT", "/api/v1/admin/assistant", `{"persona_view":true}`); code != 200 || out["persona_view"] != true {
		t.Fatalf("on %d %v", code, out)
	}
	if !hello() {
		t.Fatal("on: Hello still says off")
	}
	top := file()
	if string(top["token"]) != `"placeholder"` || string(top["zzz_other"]) != "true" {
		t.Fatalf("other keys changed: %v", top)
	}
	var a map[string]json.RawMessage
	_ = json.Unmarshal(top["assistant"], &a)
	if string(a["persona_view"]) != "true" || string(a["owner"]) != `"示例"` || string(a["launcher"]) != `"claude"` ||
		!strings.Contains(string(a["x_future"]), `"<b>"`) {
		t.Fatalf("assistant after on: %s", top["assistant"])
	}
	if b, _ := os.ReadFile(path); !strings.Contains(string(b), `"owner": "示例",
    "notes"`) {
		t.Fatalf("key order not kept:\n%s", b)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode())
	}

	// Off: the key goes (its default), the rest of "assistant" stays; persona-card is taught again.
	if code, out, _ := c.do("PUT", "/api/v1/admin/assistant", `{"persona_view":false}`); code != 200 || out["persona_view"] != false {
		t.Fatalf("off %d %v", code, out)
	}
	if hello() {
		t.Fatal("off: Hello still says on")
	}
	a = nil
	_ = json.Unmarshal(file()["assistant"], &a)
	if _, ok := a["persona_view"]; ok || string(a["notes"]) != `"备注"` || len(a) != 4 {
		t.Fatalf("assistant after off: %v", a)
	}
}

func TestSetAssistantPersonaViewFileShapes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hub.json")
	// No file, or no "assistant": on adds just the key; off leaves no empty "assistant" behind.
	if err := setAssistantPersonaView(dir, true); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != "{\n  \"assistant\": {\n    \"persona_view\": true\n  }\n}\n" {
		t.Fatalf("new file:\n%s", b)
	}
	if err := setAssistantPersonaView(dir, false); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != "{}\n" {
		t.Fatalf("off:\n%s", b)
	}
	// An "assistant" that is not an object is not touched.
	bad := `{"assistant": [1]}`
	_ = os.WriteFile(path, []byte(bad), 0o600)
	if err := setAssistantPersonaView(dir, true); err == nil {
		t.Fatal("not an object: no error")
	}
	if b, _ := os.ReadFile(path); string(b) != bad {
		t.Fatalf("changed:\n%s", b)
	}
}

// The switch has one getter: the admin UI's value once it changed it, else hub.json's as read at start. Hub code
// reads it there, never h.cfg.Assistant.PersonaView, or a switch from the admin UI would not reach it.
func TestAssistantPersonaViewGetter(t *testing.T) {
	h := newTestHub(t)
	if h.assistantPersonaView() {
		t.Fatal("default on")
	}
	h.cfg.Assistant.PersonaView = true
	if !h.assistantPersonaView() {
		t.Fatal("hub.json's value not read")
	}
	off := false
	h.adminInit()
	h.admin.personaView = &off
	if h.assistantPersonaView() {
		t.Fatal("the admin UI's value does not win")
	}

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || f == "admin_assistant.go" {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "Assistant.PersonaView") {
			t.Errorf("%s reads cfg.Assistant.PersonaView: use h.assistantPersonaView() (admin_assistant.go)", f)
		}
	}
}

// Live through the assistant process: its card action route asks Hello on every request, so the admin UI's switch
// opens and closes it without a restart (on: past the switch, to the body's check; off: 403 again).
func TestAdminAssistantSwitchReachesTheAssistant(t *testing.T) {
	connection := os.Getenv("JARVIS_TEST_DATABASE_URL")
	if connection == "" {
		t.Skip("JARVIS_TEST_DATABASE_URL not set") // the assistant keeps its settings in the store
	}
	database, err := db.Open(context.Background(), connection, db.AllSets)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	h, store, mux := adminTestHub(t)
	store.sharedStore = database // the admin sign-in stays in memory, the rest is the test database
	connectAssistant(t, h, database)
	app := h.routes()
	action := func(html bool) int {
		t.Helper()
		suffix := "/actions"
		if html {
			suffix = "/html-actions"
		}
		status, _ := call(t, app, "POST", "/api/v1/assistant/conversations/example/views/view_x"+suffix, h.cfg.Token, `not json`)
		return status
	}
	for _, html := range []bool{false, true} {
		if got := action(html); got != 403 {
			t.Fatalf("off html=%v: %d", html, got)
		}
	}
	c := signedIn(t, mux, ip4(192, 168, 1, 20)+":1")
	if code, _, _ := c.do("PUT", "/api/v1/admin/assistant", `{"persona_view":true}`); code != 200 {
		t.Fatalf("on %d", code)
	}
	// manager.updated reaches the assistant a moment later (assistantUpdated gathers bursts).
	waitFor := func(want int, tools bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for {
			got, gotHTML := action(false), action(true)
			has := slices.ContainsFunc(h.assistantToolsNow(), func(t proto.Tool) bool { return t.Name == "view_show" })
			if got == want && gotHTML == want && has == tools {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("want %d, tools %v: got action %d, html %d, tools %v", want, tools, got, gotHTML, has)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	waitFor(400, true) // past the switch, to the body's check; the card tools offered
	if code, _, _ := c.do("PUT", "/api/v1/admin/assistant", `{"persona_view":false}`); code != 200 {
		t.Fatalf("off %d", code)
	}
	waitFor(403, false)
}

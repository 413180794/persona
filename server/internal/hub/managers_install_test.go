package hub

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/bus"
)

// A new manager's answer carries the commands that install it; GET /i/<code> is the script they fetch, a valid
// sh program with the hub and the code in it, and getting it does not use the code up; codes it does not know are
// 404 and count against the source.
func TestManagerInstallScript(t *testing.T) {
	h, srv, _ := managersHub(t)
	t.Setenv("JARVIS_IMAGE", "")
	t.Setenv("PERSONA_IMAGE", "")
	var made struct {
		Manager  managerInfo       `json:"manager"`
		Code     string            `json:"code"`
		Commands map[string]string `json:"commands"`
	}
	if code := managersDo(t, h, srv, "POST", "/api/v1/managers", map[string]any{"name": "工作"}, &made); code != 201 {
		t.Fatalf("make: %d", code)
	}
	id := made.Manager.ID
	if want := "curl -fsSL " + srv.URL + "/i/" + made.Code + " | sh"; made.Commands["service"] != want {
		t.Fatalf("service %q, want %q", made.Commands["service"], want)
	}
	if want := "personad manager join " + srv.URL + " " + made.Code + " --install"; made.Commands["join"] != want {
		t.Fatalf("join %q", made.Commands["join"])
	}
	if _, ok := made.Commands["docker"]; ok {
		t.Fatalf("docker without an image: %q", made.Commands["docker"])
	}

	get := func(path string) (int, string) {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	status, script := get("/i/" + made.Code)
	if status != 200 || !strings.Contains(script, "HUB="+srv.URL+"\n") || !strings.Contains(script, "CODE="+made.Code+"\n") ||
		!strings.Contains(script, `manager join "$HUB" "$CODE"  --install "$@"`) || !strings.Contains(script, id) {
		t.Fatalf("%d:\n%s", status, script)
	}
	f := filepath.Join(t.TempDir(), "install.sh")
	if err := os.WriteFile(f, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("sh", "-n", f).CombinedOutput(); err != nil {
		t.Fatalf("sh -n: %v %s", err, out)
	}
	if status, _ := get("/i/mi_0123456789abcdef0123456789abcdef"); status != 404 {
		t.Fatalf("unknown code: %d", status)
	}
	if status, _ := get("/i/nonsense"); status != 404 {
		t.Fatalf("not a code: %d", status)
	}

	// The script did not use it up: the join trades it; then the script is gone.
	var joined struct{ ID, Token string }
	if code := managersDo(t, h, srv, "POST", bus.JoinPath, map[string]any{"code": made.Code}, &joined); code != 200 || joined.ID != id {
		t.Fatalf("join after the script: %d %+v", code, joined)
	}
	if status, _ := get("/i/" + made.Code); status != 404 {
		t.Fatalf("script of a used code: %d", status)
	}

	// With an image the hub knows, the container's command too, in the manager's own directory of the volume.
	t.Setenv("PERSONA_IMAGE", "registry.example/persona-hub:1")
	var again struct {
		Code     string            `json:"code"`
		Commands map[string]string `json:"commands"`
	}
	if code := managersDo(t, h, srv, "POST", "/api/v1/managers/"+id+"/code", nil, &again); code != 200 {
		t.Fatalf("new code: %d", code)
	}
	d := again.Commands["docker"]
	for _, want := range []string{"docker run -d --name persona-manager-" + id, "registry.example/persona-hub:1 manager run --dir /home/jarvis/.jarvis/managers/" + id,
		"--join " + srv.URL + " " + again.Code, "--database-url 'postgres://USER:PASSWORD@HOST:5432/DBNAME'"} {
		if !strings.Contains(d, want) {
			t.Errorf("docker %q lacks %q", d, want)
		}
	}
}

// A second manager with its own id connects beside the hub's own assistant: both are online in the roster, the
// assistant still answers the hub, the other's reports are not taken yet, and once the second is removed in the app its client stops (bus.ErrRevoked)
// instead of dialing again.
func TestSecondManagerBesideTheAssistant(t *testing.T) {
	h, srv, _ := managersHub(t)
	asst := dialRawBus(t, srv, busToken(t, h, assistantwire.AssistantID, "助手"))
	if err := asst.call("manager.hello", map[string]any{"id": assistantwire.AssistantID}, nil); err != nil {
		t.Fatal(err)
	}
	var made struct {
		Manager managerInfo `json:"manager"`
		Code    string      `json:"code"`
	}
	managersDo(t, h, srv, "POST", "/api/v1/managers", map[string]any{"name": "家"}, &made)
	var joined struct{ ID, Token string }
	if code := managersDo(t, h, srv, "POST", bus.JoinPath, map[string]any{"code": made.Code}, &joined); code != 200 {
		t.Fatalf("join: %d", code)
	}
	c := &bus.Client{Hub: srv.URL, Token: joined.Token, HelloMethod: assistantwire.MethodHello, StopWhenRevoked: true,
		HelloParams: func() any { return map[string]any{"id": joined.ID, "deploy": "service"} },
		Backoff:     10 * time.Millisecond}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	ran := make(chan error, 1)
	go func() { ran <- c.Run(ctx) }()
	if err := c.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	var list managersReply
	managersDo(t, h, srv, "GET", "/api/v1/managers", nil, &list)
	a, _ := list.find(assistantwire.AssistantID)
	m, _ := list.find(joined.ID)
	if !a.Online || !a.Default || !m.Online || m.Default || m.Deploy != "service" {
		t.Fatalf("roster: %+v", list)
	}
	// Its snapshot is kept as its own: the assistant's stays as it was (manager_state.go).
	var res assistantwire.SnapshotResult
	if err := c.Call(ctx, assistantwire.MethodSnapshot, map[string]any{"epoch": "x", "seq": 1,
		"sessions": []map[string]any{{"machine_id": "m1", "session_id": "conv-2"}}}, &res); err != nil || !res.OK {
		t.Fatalf("its snapshot: %+v %v", res, err)
	}
	if got := h.conversationOwner("conv-2"); got != joined.ID {
		t.Fatalf("its conversation is %q's", got)
	}
	if a := h.assistantOf(assistantwire.AssistantID); func() bool { a.mu.RLock(); defer a.mu.RUnlock(); return a.sessions["conv-2"].SessionID != "" }() {
		t.Fatal("its snapshot went into the assistant's")
	}
	if err := c.Call(ctx, "ping", nil, nil); err != nil {
		t.Fatalf("ping: %v", err)
	}
	var out struct {
		Got json.RawMessage `json:"got"`
	}
	if err := h.callBus(ctx, assistantwire.AssistantID, "echo", map[string]any{"x": 1}, &out); err != nil || string(out.Got) != `{"x":1}` {
		t.Fatalf("the assistant beside it: %s %v", out.Got, err)
	}
	if code := managersDo(t, h, srv, "DELETE", "/api/v1/managers/"+joined.ID, nil, nil); code != 200 {
		t.Fatalf("delete: %d", code)
	}
	select {
	case err := <-ran:
		if !errors.Is(err, bus.ErrRevoked) {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the removed manager kept going")
	}
	if err := h.callBus(ctx, assistantwire.AssistantID, "echo", map[string]any{}, nil); err != nil {
		t.Fatalf("the assistant after: %v", err)
	}
}

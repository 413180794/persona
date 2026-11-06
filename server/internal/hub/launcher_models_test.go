package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// modelsNode is a machine whose node answers launchers.models and session.start with fn.
func modelsNode(t *testing.T, h *Hub, cfg MachineConfig, fn func(method string, params map[string]any) (any, error)) {
	t.Helper()
	m := newMachine(h, cfg)
	registerMachine(h, m)
	hubIn, nodeOut := io.Pipe()
	nodeIn, hubOut := io.Pipe()
	hubPeer, nodePeer := rpc.New("h", hubOut), rpc.New("n", nodeOut)
	nodePeer.OnRequest = func(_ context.Context, method string, raw json.RawMessage) (any, error) {
		var p map[string]any
		_ = json.Unmarshal(raw, &p)
		return fn(method, p)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		hubPeer.Close(io.EOF)
		nodePeer.Close(io.EOF)
		_ = hubIn.Close()
		_ = nodeIn.Close()
	})
	go func() { _ = hubPeer.Serve(ctx, hubIn) }()
	go func() { _ = nodePeer.Serve(ctx, nodeIn) }()
	m.peer, m.online = hubPeer, true
}

func modelsRequest(h *Hub, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "https://example.com"+path, strings.NewReader(body))
	r.RemoteAddr = netip.AddrFrom4([4]byte{127, 0, 0, 1}).String() + ":1234"
	r.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w := httptest.NewRecorder()
	h.routes().ServeHTTP(w, r)
	return w
}

// The machine config's word for what a launcher runs goes to the node, and so does a refresh.
func TestLauncherModelsEndpoint(t *testing.T) {
	h := newTestHub(t)
	var got map[string]any
	cfg := MachineConfig{ID: "box", Launchers: []assistantwire.ConfiguredLauncher{{Launcher: proto.Launcher{Name: "work"}, AgentType: "codex"}}}
	modelsNode(t, h, cfg, func(method string, p map[string]any) (any, error) {
		if method != "launchers.models" {
			return map[string]any{}, nil
		}
		got = p
		return proto.LauncherModels{Agent: "codex", Models: []proto.AgentModel{{ID: "model-a", Name: "Model A", Default: true}}}, nil
	})
	connectAssistant(t, h) // the picker's list comes through it (it adds the plugin gateway's models)
	w := modelsRequest(h, "GET", "/api/v1/machines/box/launchers/work/models?refresh=1", "")
	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if got["configured"] != true {
		t.Fatalf("a launcher in hub.json is not vouched for: %v", got)
	}
	if got["name"] != "work" || got["agent_type"] != "codex" || got["refresh"] != true {
		t.Fatalf("node got %+v", got)
	}
	var res proto.LauncherModels
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil || res.Agent != "codex" || len(res.Models) != 1 || !res.Models[0].Default {
		t.Fatalf("answer %s", w.Body)
	}

	// An agent without a list answers models: [], never null (the apps decode a list).
	h2 := newTestHub(t)
	modelsNode(t, h2, MachineConfig{ID: "box"}, func(string, map[string]any) (any, error) {
		return map[string]any{"agent": "traex"}, nil
	})
	connectAssistant(t, h2)
	w = modelsRequest(h2, "GET", "/api/v1/machines/box/launchers/traex/models", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"models":[]`) {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := modelsRequest(h2, "GET", "/api/v1/machines/nowhere/launchers/x/models", ""); w.Code != http.StatusNotFound {
		t.Fatalf("unknown machine: %d", w.Code)
	}

	// A node from before the list answers 404 like an older hub (the apps leave the picker out), not 502 with a retry.
	h3 := newTestHub(t)
	modelsNode(t, h3, MachineConfig{ID: "box"}, func(method string, _ map[string]any) (any, error) {
		return nil, fmt.Errorf("unknown method %s", method)
	})
	connectAssistant(t, h3)
	w = modelsRequest(h3, "GET", "/api/v1/machines/box/launchers/claude/models", "")
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), proto.CodeNodeOutdated) {
		t.Fatalf("old node: %d %s", w.Code, w.Body)
	}
}

// The model picked in "New agent" reaches the node's start (it used to be dropped with the request's other unknown keys).
func TestSessionStartTakesPickedModel(t *testing.T) {
	h, _ := modelTestHub(t)
	var got map[string]any
	modelsNode(t, h, MachineConfig{ID: "box"}, func(method string, p map[string]any) (any, error) {
		if method == "session.start" {
			got = p
			return map[string]any{"session": proto.Session{SessionID: "s1"}}, nil
		}
		return map[string]any{}, nil
	})
	connectAssistant(t, h) // what a new Agent runs on is the manager's to fill in (agents.start_params)
	w := modelsRequest(h, "POST", "/api/v1/sessions", `{"machine_id":"box","cwd":"/code","launcher":"claude","model":"gateway/model-a","effort":"high"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if got["model"] != "gateway/model-a" || got["effort"] != "high" || got["launcher"] != "claude" || got["model_fingerprint"] == nil {
		t.Fatalf("node got %+v", got)
	}
}

// Models' list for a launcher provider comes from a machine that has the launcher and is online, and says which;
// providers without a list to ask for answer 404 (#943).
func TestProviderModelsAsksAMachineWithTheLauncher(t *testing.T) {
	h, mux := modelTestHub(t)
	for _, id := range []string{"a", "c"} {
		registerMachine(h, newMachine(h, MachineConfig{ID: id}))
	}
	var asked []string
	modelsNode(t, h, MachineConfig{ID: "b"}, func(method string, p map[string]any) (any, error) {
		if method == "launchers.list" { // b, online, says what it has (the manager asks it when it first lists)
			return map[string]any{"launchers": []proto.Launcher{{Name: "work"}}}, nil
		}
		if method != "launchers.models" {
			return map[string]any{}, nil
		}
		asked = append(asked, p["name"].(string))
		return proto.LauncherModels{Agent: "claude", Models: []proto.AgentModel{{ID: "opus", Name: "Opus"}}}, nil
	})
	if err := h.store.PutSetting(context.Background(), "launchers.zshrc", map[string][]proto.Launcher{"a": {{Name: "work"}}, "b": {{Name: "work"}},
		"c": {{Name: "elsewhere"}}}); err != nil {
		t.Fatal(err)
	}
	connectAssistant(t, h) // the model configuration's route is its (the machines' launchers the hub's: launchers.zshrc)
	get := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		return w
	}

	w := get("/api/v1/model-config/providers/launcher.work/models?machine=a") // a has it but is offline
	var res struct {
		proto.LauncherModels
		Machine string `json:"machine"`
	}
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &res) != nil || res.Machine != "b" || len(res.Models) != 1 {
		t.Fatalf("work: %d %s", w.Code, w.Body)
	}
	// Claude Code "Default" runs on every machine.
	if w := get("/api/v1/model-config/providers/launcher.claude/models"); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"machine":"b"`) {
		t.Fatalf("claude: %d %s", w.Code, w.Body)
	}
	if len(asked) != 2 || asked[0] != "work" || asked[1] != "claude" {
		t.Fatalf("asked %v", asked)
	}
	// Only on a machine that is not connected.
	if w := get("/api/v1/model-config/providers/launcher.elsewhere/models"); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("elsewhere: %d %s", w.Code, w.Body)
	}
	for _, id := range []string{"launcher.nothing", "plugin.voice", "legacy.ai"} {
		if w := get("/api/v1/model-config/providers/" + id + "/models"); w.Code != http.StatusNotFound {
			t.Fatalf("%s: %d %s", id, w.Code, w.Body)
		}
	}
}

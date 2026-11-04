package hub

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// dirsNode answers node.configure, sessions.list and claude.dirs.list like a node that blocks what it is told to.
type dirsNode struct {
	mu        sync.Mutex
	blocked   []string
	configure []map[string]json.RawMessage
}

func (f *dirsNode) handle(_ context.Context, method string, params json.RawMessage) (any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	const home = "/home/me"
	all := []string{"/home/me/.automation/claude-a", "/home/me/.config/cc/work"}
	switch method {
	case "node.configure":
		var c map[string]json.RawMessage
		_ = json.Unmarshal(params, &c)
		f.configure = append(f.configure, c)
		if raw, ok := c["claude_dirs_blocked"]; ok {
			f.blocked = nil
			_ = json.Unmarshal(raw, &f.blocked)
		}
		return map[string]any{}, nil
	case "sessions.list":
		var list []proto.Session
		for i, d := range all {
			if !proto.ClaudeDirBlocked(d, home, f.blocked) {
				list = append(list, proto.Session{SessionID: []string{"bot", "work"}[i], Cwd: d})
			}
		}
		return map[string]any{"sessions": list}, nil
	case "claude.dirs.list":
		dirs := []proto.ClaudeDir{{Path: "/home/me/.claude", Default: true, Source: "default"}}
		for _, d := range all {
			dirs = append(dirs, proto.ClaudeDir{Path: d, Source: "found", Blocked: proto.ClaudeDirBlocked(d, home, f.blocked)})
		}
		return map[string]any{"dirs": dirs}, nil
	}
	return map[string]any{}, nil
}

func connectDirsNode(t *testing.T, m *Machine) *dirsNode {
	t.Helper()
	f := &dirsNode{}
	toNode, nodeIn := io.Pipe()
	toHub, hubIn := io.Pipe()
	hubPeer := rpc.New("h", nodeIn)
	nodePeer := rpc.New("n", hubIn)
	nodePeer.OnRequest = f.handle
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); toNode.Close(); toHub.Close() })
	go hubPeer.Serve(ctx, toHub)
	go nodePeer.Serve(ctx, toNode)
	m.mu.Lock()
	m.peer, m.online = hubPeer, true
	m.info.Home = "/home/me"
	m.mu.Unlock()
	return f
}

// dirsStore keeps settings and drops the interactions jarvis_settings records.
type dirsStore struct{ *settingsStore }

func (dirsStore) InsertInteraction(context.Context, proto.Interaction) error { return nil }
func (dirsStore) FinishInteraction(context.Context, string, string, *proto.Resolution) error {
	return nil
}

func dirsHub(t *testing.T) (*Hub, *settingsStore, *Machine, http.Handler) {
	t.Helper()
	h := newTestHub(t)
	st := &settingsStore{vals: map[string][]byte{}}
	h.store = dirsStore{st}
	m := newMachine(h, MachineConfig{ID: "m1", Name: "box", ClaudeConfigDirs: []string{"~/.automation/claude-b"}})
	h.byID["m1"], h.machines = m, []*Machine{m}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/machines/{mid}/claude-dirs", h.withMachine(h.getClaudeDirs))
	mux.HandleFunc("PUT /api/v1/machines/{mid}/claude-dirs/blocked", h.withMachine(h.putClaudeDirsBlocked))
	return h, st, m, mux
}

func listedIDs(m *Machine) []string {
	var ids []string
	for _, s := range m.sessionList() {
		ids = append(ids, s.SessionID)
	}
	return ids
}

// Blocking a pattern stores it, forgets the remembered directories it covers, tells the node and takes its new session
// list; a node reporting a blocked directory again is not listened to; unblocking brings the sessions back.
func TestBlockClaudeDirs(t *testing.T) {
	_, st, m, mux := dirsHub(t)
	ctx := context.Background()
	_ = st.PutSetting(ctx, claudeDirsKey("m1"), []string{"/home/me/.automation/claude-a", "/home/me/.config/cc/work"})
	f := connectDirsNode(t, m)

	code, out := put(t, mux, "/api/v1/machines/m1/claude-dirs/blocked", `{"blocked":[" ~/.automation/* ",""]}`)
	if code != 200 {
		t.Fatalf("PUT = %d %v", code, out)
	}
	if b := m.blockedClaudeDirs(ctx); !slices.Equal(b, []string{"~/.automation/*"}) {
		t.Fatalf("stored %v", b)
	}
	var seen []string
	_, _ = st.Setting(ctx, claudeDirsKey("m1"), &seen)
	if !slices.Equal(seen, []string{"/home/me/.config/cc/work"}) {
		t.Fatalf("remembered %v", seen)
	}
	if ids := listedIDs(m); !slices.Equal(ids, []string{"work"}) {
		t.Fatalf("sessions %v", ids)
	}
	f.mu.Lock()
	last := f.configure[len(f.configure)-1]
	f.mu.Unlock()
	if string(last["claude_dirs"]) != `["/home/me/.config/cc/work"]` || string(last["claude_dirs_blocked"]) != `["~/.automation/*"]` {
		t.Fatalf("node told %s / %s", last["claude_dirs"], last["claude_dirs_blocked"])
	}
	// The answer lists every directory, blocked ones marked, the configured one too.
	dirs, _ := out["dirs"].([]any)
	var blocked []string
	for _, d := range dirs {
		if dm := d.(map[string]any); dm["blocked"] == true {
			blocked = append(blocked, dm["path"].(string))
		}
	}
	if out["live"] != true || !slices.Equal(blocked, []string{"/home/me/.automation/claude-a", "~/.automation/claude-b"}) {
		t.Fatalf("state %v", out)
	}

	// An older node reports the blocked directory again: not remembered.
	m.rememberClaudeDirs(json.RawMessage(`{"dirs":["/home/me/.automation/claude-a","/home/me/.config/cc/new"]}`))
	_, _ = st.Setting(ctx, claudeDirsKey("m1"), &seen)
	if !slices.Equal(seen, []string{"/home/me/.config/cc/work", "/home/me/.config/cc/new"}) {
		t.Fatalf("remembered %v", seen)
	}
	// Reconnecting hands the node the patterns.
	m.configureNode(ctx, m.peer)
	f.mu.Lock()
	last = f.configure[len(f.configure)-1]
	f.mu.Unlock()
	if string(last["claude_dirs_blocked"]) != `["~/.automation/*"]` || strings.Contains(string(last["claude_dirs"]), "automation") {
		t.Fatalf("configure on connect: %s / %s", last["claude_dirs"], last["claude_dirs_blocked"])
	}

	if code, _ := put(t, mux, "/api/v1/machines/m1/claude-dirs/blocked", `{"blocked":[]}`); code != 200 {
		t.Fatalf("unblock = %d", code)
	}
	if ids := listedIDs(m); !slices.Equal(ids, []string{"bot", "work"}) {
		t.Fatalf("unblocked sessions %v", ids)
	}
}

func TestBlockClaudeDirsRefusesBadPatterns(t *testing.T) {
	_, _, _, mux := dirsHub(t)
	for _, body := range []string{`{"blocked":["relative/dir"]}`, `{"blocked":["~/["]}`, `nope`} {
		if code, _ := put(t, mux, "/api/v1/machines/m1/claude-dirs/blocked", body); code != 400 {
			t.Errorf("%s: %d", body, code)
		}
	}
}

// Offline: the hub's own record, blocked ones marked.
func TestClaudeDirsOffline(t *testing.T) {
	_, st, _, mux := dirsHub(t)
	ctx := context.Background()
	_ = st.PutSetting(ctx, claudeDirsKey("m1"), []string{"~/.config/cc/work"})
	_ = st.PutSetting(ctx, claudeDirsBlockedKey("m1"), []string{"~/.automation/*"})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/machines/m1/claude-dirs", nil))
	var out claudeDirsState
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if rec.Code != 200 || out.Live || len(out.Dirs) != 2 || !out.Dirs[0].Blocked || out.Dirs[0].Source != "config" || out.Dirs[1].Blocked {
		t.Fatalf("%d %+v", rec.Code, out)
	}
}

// An empty blocklist serializes as [] (not null): the web settings page treats blocked as a JSON array and crashes
// rendering .filter on null (#977).
func TestClaudeDirsEmptyBlocklistIsArray(t *testing.T) {
	_, _, m, mux := dirsHub(t)
	connectDirsNode(t, m)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/machines/m1/claude-dirs", nil))
	if rec.Code != 200 {
		t.Fatalf("GET = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"blocked":[]`) {
		t.Fatalf("blocklist is not an empty JSON array: %s", rec.Body.String())
	}
}

// jarvis_settings blocks a directory only after the phone approves, and removes it the same way.
func TestSelfSettingsBlocksClaudeDirs(t *testing.T) {
	h, _, m, _ := dirsHub(t)
	connectDirsNode(t, m)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	run := func(args string) string {
		t.Helper()
		done := make(chan string, 1)
		go func() {
			out, err := h.toolJarvisSettings(ctx, m, "s1", json.RawMessage(args))
			if err != nil {
				out = "error: " + err.Error()
			}
			done <- out
		}()
		for ctx.Err() == nil {
			if pending := h.pendingInteractions(); len(pending) == 1 {
				allow := true
				_ = h.resolve(pending[0].ID, proto.Resolution{Allow: &allow})
				break
			}
			select {
			case out := <-done:
				return out
			case <-time.After(5 * time.Millisecond):
			}
		}
		return <-done
	}
	if out := run(`{"action":"get","key":"agents.blocked_claude_dirs"}`); !strings.Contains(out, "/home/me/.automation/claude-a (listed") {
		t.Fatalf("get: %s", out)
	}
	if out := run(`{"action":"add","key":"agents.blocked_claude_dirs","value":{"machine":"m1","pattern":"~/.automation/*"}}`); !strings.HasPrefix(out, "Blocked") {
		t.Fatalf("add: %s", out)
	}
	if ids := listedIDs(m); !slices.Equal(ids, []string{"work"}) {
		t.Fatalf("sessions %v", ids)
	}
	if out := run(`{"action":"add","key":"agents.blocked_claude_dirs","value":{"machine":"m1","pattern":"dir"}}`); !strings.Contains(out, "bad pattern") {
		t.Fatalf("bad add: %s", out)
	}
	if out := run(`{"action":"remove","key":"agents.blocked_claude_dirs","id":"m1:~/.automation/*"}`); !strings.HasPrefix(out, "Unblocked") {
		t.Fatalf("remove: %s", out)
	}
	if len(m.blockedClaudeDirs(ctx)) != 0 || len(listedIDs(m)) != 2 {
		t.Fatal("not unblocked")
	}
}

// Connecting tells the node whether to list botmux's sessions: as before unless hub.json turns them off for the
// machine (machines[].botmux false). So does handing it new config directories, which asks for the session list
// next: the node must not take that list for an older hub's.
func TestConfigureNodeSaysBotmux(t *testing.T) {
	_, _, m, _ := dirsHub(t)
	f := connectDirsNode(t, m)
	ctx := context.Background()
	m.configureNode(ctx, m.peer)
	off := false
	m.cfg.Botmux = &off
	m.configureNode(ctx, m.peer)
	m.reconfigureClaudeDirs(ctx)
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.configure) != 3 || string(f.configure[0]["botmux"]) != "true" || string(f.configure[1]["botmux"]) != "false" ||
		string(f.configure[2]["botmux"]) != "false" {
		t.Fatalf("botmux in node.configure: %v", f.configure)
	}
}

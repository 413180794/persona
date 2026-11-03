package hub

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

func startReceiptNode(t *testing.T, h *Hub, start func(context.Context, json.RawMessage) (any, error)) {
	t.Helper()
	m := newMachine(h, MachineConfig{ID: "machine"})
	h.byID[m.cfg.ID] = m
	h.machines = append(h.machines, m)
	hubIn, nodeOut := io.Pipe()
	nodeIn, hubOut := io.Pipe()
	hubPeer, nodePeer := rpc.New("h", hubOut), rpc.New("n", nodeOut)
	nodePeer.OnRequest = func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		if method == "session.start" {
			return start(ctx, params)
		}
		return map[string]any{}, nil
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

func TestSessionStartReceipt(t *testing.T) {
	url := os.Getenv("JARVIS_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("JARVIS_TEST_DATABASE_URL not set")
	}
	store, err := db.Open(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, unknown := range []bool{false, true} {
		t.Run(map[bool]string{false: "completed", true: "unknown"}[unknown], func(t *testing.T) {
			id := "00000000-0000-4000-8000-" + randomHex(6)
			var starts atomic.Int32
			h := newTestHub(t)
			h.store = store
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			startReceiptNode(t, h, func(_ context.Context, params json.RawMessage) (any, error) {
				starts.Add(1)
				cancel() // Losing the caller must not cancel an accepted session start.
				var p map[string]any
				_ = json.Unmarshal(params, &p)
				if p["agent_type"] != "codex" {
					t.Error("selected agent type was not forwarded")
				}
				if unknown {
					return nil, errors.New("start outcome unavailable")
				}
				return map[string]any{"session": proto.Session{SessionID: "created", Agent: "codex"}}, nil
			})
			request := func(h *Hub, body, opID string, ctx context.Context) *httptest.ResponseRecorder {
				r := httptest.NewRequest("POST", "https://example.com/api/v1/sessions", strings.NewReader(body)).WithContext(ctx)
				r.RemoteAddr = netip.AddrFrom4([4]byte{127, 0, 0, 1}).String() + ":1234"
				r.Header.Set("Authorization", "Bearer "+h.cfg.Token)
				if opID != "" {
					r.Header.Set("X-Jarvis-Op-Id", opID)
				}
				w := httptest.NewRecorder()
				h.routes().ServeHTTP(w, r)
				return w
			}
			body := `{"machine_id":"machine","cwd":"/code","launcher":"codex","agent_type":"codex"}`
			first := request(h, body, id, ctx)
			wantStatus, wantState := http.StatusOK, "done"
			if unknown {
				wantStatus, wantState = http.StatusBadGateway, "unknown"
			}
			if first.Code != wantStatus || starts.Load() != 1 {
				t.Fatalf("first = %d %s, starts = %d", first.Code, first.Body, starts.Load())
			}
			// The restarted hub has no node: only the durable database receipt can answer.
			restarted := newTestHub(t)
			restarted.store = store
			second := request(restarted, body, id, context.Background())
			if second.Code != first.Code || second.Body.String() != first.Body.String() || starts.Load() != 1 {
				t.Fatalf("replay = %d %s, starts = %d", second.Code, second.Body, starts.Load())
			}
			r := httptest.NewRequest("GET", "https://example.com/api/v1/ops/"+id, nil)
			r.RemoteAddr = netip.AddrFrom4([4]byte{127, 0, 0, 1}).String() + ":1234"
			r.Header.Set("Authorization", "Bearer "+restarted.cfg.Token)
			w := httptest.NewRecorder()
			restarted.routes().ServeHTTP(w, r)
			var receipt db.ClientOp
			if json.Unmarshal(w.Body.Bytes(), &receipt) != nil || receipt.Kind != "start" || receipt.State != wantState {
				t.Fatalf("receipt = %d %s", w.Code, w.Body)
			}
			conflict := request(restarted, strings.Replace(body, "/code", "/other", 1), id, context.Background())
			if conflict.Code != http.StatusConflict {
				t.Fatalf("changed parameters = %d %s", conflict.Code, conflict.Body)
			}
			if !unknown {
				legacy := request(h, body, "", context.Background())
				if legacy.Code != http.StatusOK || starts.Load() != 2 {
					t.Fatalf("legacy = %d %s, starts = %d", legacy.Code, legacy.Body, starts.Load())
				}
			}
		})
	}
}

func TestSessionLauncherTypes(t *testing.T) {
	list := sessionLaunchers([]sessionLauncher{
		{Launcher: proto.Launcher{Name: "claude", Model: "default"}},
		{Launcher: proto.Launcher{Name: "codex"}},
		{Launcher: proto.Launcher{Name: "wrapper", Model: "configured-model"}},
	}, []sessionLauncher{
		{Launcher: proto.Launcher{Name: "wrapper"}, AgentType: "traex"},
		{Launcher: proto.Launcher{Name: "other-wrapper"}, AgentType: "codex"},
	})
	if len(list) != 4 || list[0].AgentType != "claude" || list[1].AgentType != "codex" ||
		list[2].AgentType != "traex" || list[2].Model != "configured-model" || list[3].AgentType != "codex" {
		t.Fatalf("launchers = %+v", list)
	}
	// The old iOS launcher model ignores the optional type and retains its existing fields.
	raw, _ := json.Marshal(list)
	var legacy []proto.Launcher
	if json.Unmarshal(raw, &legacy) != nil || len(legacy) != 4 || legacy[0].Name != "claude" {
		t.Fatalf("legacy launchers = %s", raw)
	}
}

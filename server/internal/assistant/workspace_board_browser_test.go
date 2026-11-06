package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/apierr"
	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
	"github.com/nwverse/persona/server/internal/workspaceboard"
)

// This opt-in harness serves only the board API on synthetic machines, backed by a disposable Postgres database.
func TestWorkspaceBoardBrowserHarness(t *testing.T) {
	if os.Getenv("WORKSPACE_BOARD_QA") != "1" {
		t.Skip("manual isolated browser harness")
	}
	databaseURL := os.Getenv("WORKSPACE_BOARD_QA_DATABASE_URL")
	if databaseURL == "" {
		databaseURL = os.Getenv("PERSONA_TEST_DATABASE_URL")
	}
	database, err := db.Open(context.Background(), databaseURL, db.ManagerSet)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	makeService := func() *Service {
		d := newBoardDevs()
		d.add("computer", func(_ context.Context, method string, p json.RawMessage) (any, error) {
			switch method {
			case "workspaces.list":
				return proto.WorkspacesResponse{Workspaces: []proto.Workspace{{ID: "design", Source: "herdr", SourceID: "design", Title: "Design", Cwd: "/projects/design", Available: true}, {ID: "api", Source: "herdr", SourceID: "api", Title: "API", Cwd: "/projects/api", Available: true}, {ID: "new", Source: "herdr", SourceID: "new", Title: "New project", Cwd: "/projects/new", Available: true}}}, nil
			case "workspace.resolve":
				var v struct {
					Path string `json:"path"`
				}
				_ = json.Unmarshal(p, &v)
				return proto.Workspace{ID: v.Path, Source: "directory", SourceID: v.Path, Title: "Bound directory", Cwd: v.Path, Available: true}, nil
			case "fs.stat":
				return map[string]any{"stats": []map[string]string{{"path": "/projects/design", "kind": "dir"}, {"path": "/projects/api", "kind": "dir"}}}, nil
			default:
				t.Errorf("metadata UI unexpectedly invoked %s", method)
				return nil, &rpc.Error{Code: "unsupported", Message: "harness does not run agents"}
			}
		})
		d.setSessions("computer", []proto.Session{{SessionID: "one", Cwd: "/projects/design", SpaceID: "design", SpaceName: "Design", Agent: "codex"}, {SessionID: "two", Cwd: "/projects/design", SpaceID: "design", SpaceName: "Design", Agent: "codex"}})
		d.add("offline", nil)
		s, _ := newBoardService(t, database, d)
		return s
	}
	initial, err := database.WorkspaceBoard(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(initial.Cards) == 0 {
		state := initial
		for _, item := range []struct{ id, mid, space, title, cwd, col string }{{"design", "computer", "design", "Design refresh", "/projects/design", "doing"}, {"api", "computer", "api", "API cleanup", "/projects/api", "todo"}, {"report", "offline", "report", "Report follow-up", "/projects/report", "review"}} {
			w := qualifyWorkspace(item.mid, proto.Workspace{Source: "herdr", SourceID: item.space})
			title := item.title
			state, err = workspaceboard.Apply(state, workspaceboard.Mutation{Action: "add_card", ID: item.id, MachineID: item.mid, WorkspaceID: w.ID, Source: "herdr", SourceID: item.space, Title: &title, Cwd: item.cwd, ColumnID: item.col})
			if err != nil {
				t.Fatal(err)
			}
		}
		if _, err = database.SaveWorkspaceBoard(context.Background(), initial.Revision, state); err != nil {
			t.Fatal(err)
		}
	}
	var mu sync.RWMutex
	// The hub's part: token "mock" is a full device, "mock-readonly" an answer-only one (the role the relay passes on).
	newHandler := func() http.Handler {
		svc := makeService()
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Header.Get("Authorization") {
			case "Bearer mock":
				r.Header.Set(RoleHeader, roleFull)
			case "Bearer mock-readonly":
				r.Header.Set(RoleHeader, roleAnswer)
			default:
				apierr.Write(w, http.StatusUnauthorized, errors.New("invalid token"))
				return
			}
			svc.ServeHTTP(w, r)
		})
	}
	handler := newHandler()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /_qa/restart", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		handler = newHandler()
		mu.Unlock()
		apierr.WriteJSON(w, map[string]bool{"restarted": true})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		mu.RLock()
		current := handler
		mu.RUnlock()
		current.ServeHTTP(w, r)
	})
	listener, err := net.Listen("tcp", "127.0.0.1:18770")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	defer srv.Close()
	go srv.Serve(listener)
	if info := os.Getenv("WORKSPACE_BOARD_QA_INFO"); info != "" {
		raw, _ := json.Marshal(map[string]string{"url": "http://127.0.0.1:18770", "token": "mock"})
		if err := os.WriteFile(info, raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Log("isolated board API ready on loopback port 18770")
	timer := time.NewTimer(20 * time.Minute)
	defer timer.Stop()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-timer.C:
			t.Fatal("browser harness deadline reached")
		case <-ticker.C:
			if _, err := os.Stat(os.Getenv("WORKSPACE_BOARD_QA_STOP")); err == nil {
				return
			}
		}
	}
}

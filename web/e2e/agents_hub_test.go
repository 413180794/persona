package hub

// Browser-only fixture, copied by agents-performance.mjs into a disposable server source copy.
// This exercises the real hub HTTP/WebSocket implementation without changing server/ or starting a real node.
import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestWebAgentsFixture(t *testing.T) {
	dist, port := os.Getenv("WEB_AGENTS_DIST"), os.Getenv("WEB_AGENTS_PORT")
	if dist == "" || port == "" {
		t.Skip("browser fixture requires explicit dist and port")
	}
	h := syntheticAgentHub(t)
	h.cfg.Token = "synthetic-browser-token"
	for _, m := range h.machines {
		m.online = true
		m.cfg.Name = "Demo computer " + m.cfg.ID
		m.sessions[0].Queued = []string{"Oldest demo queued instruction"}
		m.sessions[1999].Queued = []string{"Newest demo queued instruction"}
	}
	routes := h.routes()
	stop := make(chan struct{}, 1)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/__fixture/stop" {
			stop <- struct{}{}
			w.WriteHeader(204)
			return
		}
		if r.URL.Path == "/__fixture/agents" {
			h.broadcast(map[string]any{"type": "agents"})
			w.WriteHeader(204)
			return
		}
		if r.URL.Path == "/__fixture/change" {
			row, _ := strconv.Atoi(r.URL.Query().Get("row"))
			m := h.machines[0]
			if row < 0 || row >= len(m.sessions) {
				http.Error(w, "invalid synthetic row", 400)
				return
			}
			m.mu.Lock()
			m.sessions[row].Title = r.URL.Query().Get("title")
			if r.URL.Query().Get("clear_tasks") == "1" {
				m.sessions[row].Queued = nil
				m.sessions[row].Background = nil
				m.sessions[row].Subagents = nil
				m.sessions[row].Workflows = nil
			}
			m.mu.Unlock()
			h.sessionsChanged(m)
			w.WriteHeader(204)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/v1/sessions/") && (strings.HasSuffix(r.URL.Path, "/items") || strings.HasSuffix(r.URL.Path, "/info")) {
			w.Header().Set("Content-Type", "application/json")
			if strings.HasSuffix(r.URL.Path, "/items") {
				fmt.Fprint(w, `{"items":[{"seq":1,"role":"assistant","kind":"text","text":"Synthetic chat transcript","id":"demo-message"}],"has_more":false,"history":"demo","to_seq":1}`)
			} else {
				fmt.Fprint(w, `{"model":"demo-model","permission_mode":"default"}`)
			}
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			if strings.HasPrefix(r.URL.Path, "/api/v1/sessions") || r.URL.Path == "/api/v1/ws" || r.URL.Path == "/api/v1/machines" || r.URL.Path == "/api/v1/mutes" || r.URL.Path == "/api/v1/pins" {
				routes.ServeHTTP(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/api/v1/agents/sessions":
				if r.URL.Query().Get("counts") == "1" {
					fmt.Fprint(w, `{"total":0,"machine":{},"project":{},"group":{},"state":{},"revision":"demo-personal-1"}`)
				} else {
					fmt.Fprint(w, `{"sessions":[],"remove":[],"has_more":false,"revision":"demo-personal-1","total":0}`)
				}
			case "/api/v1/assistant":
				fmt.Fprint(w, `{"conversations":[]}`)
			case "/api/v1/interactions":
				fmt.Fprint(w, `{"interactions":[]}`)
			case "/api/v1/todos":
				fmt.Fprint(w, `{"todos":[]}`)
			case "/api/v1/agents":
				fmt.Fprint(w, `{"sources":[]}`)
			default:
				fmt.Fprint(w, `{}`)
			}
			return
		}
		path := filepath.Join(dist, filepath.Clean("/"+r.URL.Path))
		if _, err := os.Stat(path); err != nil {
			path = filepath.Join(dist, "index.html")
		}
		http.ServeFile(w, r, path)
	})
	listener, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(handler)
	srv.Listener.Close()
	srv.Listener = listener
	srv.Start()
	defer srv.Close()
	fmt.Printf("WEB_AGENTS_READY %s\n", srv.URL)
	select {
	case <-stop:
		fmt.Println("WEB_AGENTS_FIXTURE PASS")
	case <-time.After(15 * time.Minute):
		t.Fatal("WEB_AGENTS_FIXTURE TIMEOUT")
	}
}

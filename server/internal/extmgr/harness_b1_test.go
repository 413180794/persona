package extmgr

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/coder/websocket"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/extwire"
)

// Group B1's helpers (the extensions' own tests).

// b1Pushed is how many messages of type typ the apps were sent that mention text.
func b1Pushed(h *Manager, typ, text string) int {
	n := 0
	for _, b := range hubOf(h).broadcastsOf(typ) {
		raw, _ := json.Marshal(b)
		if strings.Contains(string(raw), text) {
			n++
		}
	}
	return n
}

// b1Launcher puts a launcher added in Persona into the assistant's model configuration: its gateway at base, and the
// key a request to it is sent with (models.launcher_http).
func b1Launcher(h *Manager, name, base, key string) { modelsOf(h).launcher(name, base, key) }

// b1Emit is the hub telling the extensions of something it saw (extensions.emit: agent.finished, agent.needs_you).
func b1Emit(h *Manager, event string, params map[string]any) {
	raw, _ := json.Marshal(params)
	b, _ := json.Marshal(extwire.Emit{Event: event, Params: raw})
	h.Event(extwire.EventEmit, b)
}

// b1DevServer is a dev server on a computer: a page, a cookie sign-in, a WebSocket echo (the hub's ports test's).
func b1DevServer(t *testing.T) (*httptest.Server, int) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, "<title>Dev</title><script src=\"/@vite/client\"></script> host=%s", r.Host)
	})
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "sid", Value: "s3cr3t", Path: "/"})
		http.Redirect(w, r, "/me", http.StatusFound)
	})
	mux.HandleFunc("/me", func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("sid")
		if err != nil {
			http.Error(w, "no cookie", http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, "hello "+c.Value)
	})
	mux.HandleFunc("/caller", func(w http.ResponseWriter, r *http.Request) { // who is calling (extension_caller.go)
		_ = json.NewEncoder(w).Encode(personaHeaders(r))
	})
	mux.HandleFunc("/hmr", func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer c.CloseNow()
		c.SetReadLimit(1 << 20)
		for {
			typ, b, err := c.Read(r.Context())
			if err != nil {
				return
			}
			if err := c.Write(r.Context(), typ, append([]byte("echo:"), b...)); err != nil {
				return
			}
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, srv.Listener.Addr().(*net.TCPAddr).Port
}

// b1Items are the inbox's items as the assistant keeps them (inbox.item answers them, in this process).
type b1Items struct {
	mu    sync.Mutex
	items map[string]json.RawMessage
}

func b1InboxItems(h *Manager) *b1Items {
	fm := &b1Items{items: map[string]json.RawMessage{}}
	a := withAssistant(h)
	a.on(assistantwire.MethodInboxItem, func(p json.RawMessage) (any, error) {
		var in struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(p, &in)
		fm.mu.Lock()
		defer fm.mu.Unlock()
		it, ok := fm.items[in.ID]
		return assistantwire.InboxItemResult{Item: it, OK: ok}, nil
	})
	return fm
}

// b1Admin is the admin UI as the hub relays it from its admin mux: who signed in the hub checked (its session and
// CSRF: hub/admin*.go), and it says so (X-Persona-Admin); a client that is not signed in is relayed without it.
type b1Admin struct {
	t        *testing.T
	h        http.Handler // the manager (its ServeHTTP: what the hub relays to)
	remote   string
	signedIn bool
}

// b1AdminLAN is an address in the hub's LAN.
const b1AdminLAN = "[fd00::20]:1"

func b1SignedIn(t *testing.T, h *Manager) *b1Admin {
	h.routes() // its routes registered
	return &b1Admin{t: t, h: h, remote: b1AdminLAN, signedIn: true}
}

func (c *b1Admin) request(req *http.Request) *httptest.ResponseRecorder {
	req.RemoteAddr = c.remote
	req.Header.Set(extwire.HeaderRole, roleFull)
	req.Header.Set(extwire.HeaderRemote, strings.Split(c.remote, ":")[0])
	if c.signedIn {
		req.Header.Set(extwire.HeaderAdmin, "1")
	}
	w := httptest.NewRecorder()
	c.h.ServeHTTP(w, req)
	return w
}

func (c *b1Admin) do(method, path, body string) (int, map[string]any, *httptest.ResponseRecorder) {
	c.t.Helper()
	w := c.request(httptest.NewRequest(method, path, strings.NewReader(body)))
	out := map[string]any{}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out, w
}

// upload posts a package body as the browser does.
func (c *b1Admin) upload(query string, body []byte, contentType string) (int, map[string]any) {
	c.t.Helper()
	req := httptest.NewRequest("POST", adminUploadPath+"?"+query, bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	w := c.request(req)
	out := map[string]any{}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

// b1TriageModel is whether the assistant's inbox.triage can be called (models.usable).
func b1TriageModel(h *Manager) *atomic.Bool { return &modelsOf(h).usable }

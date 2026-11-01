package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/chyroc/jarvis/server/internal/proto"
	"github.com/chyroc/jarvis/server/internal/rpc"
	"github.com/chyroc/jarvis/server/internal/streams"
)

// portNode is a node as the hub sees it for ports: port.dial connects to 127.0.0.1 and streams it, port.probe says
// whether anything is there.
func portNode(t *testing.T, h *Hub, sessions []proto.Session) *Machine {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	h2nR, h2nW := io.Pipe()
	n2hR, n2hW := io.Pipe()
	hubSide, nodeSide := rpc.New("h", h2nW), rpc.New("n", n2hW)
	hubMux, nodeMux := streams.New(hubSide.Notify), streams.New(nodeSide.Notify)
	hubSide.OnEvent = func(e string, p json.RawMessage) { hubMux.Event(e, p) }
	nodeSide.OnEvent = func(e string, p json.RawMessage) { nodeMux.Event(e, p) }
	nodeSide.OnRequest = func(_ context.Context, method string, params json.RawMessage) (any, error) {
		var p struct {
			ID   string `json:"id"`
			Port int    `json:"port"`
		}
		_ = json.Unmarshal(params, &p)
		c, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(p.Port), 2*time.Second)
		switch method {
		case "port.probe":
			if err != nil {
				return map[string]any{"port": p.Port, "listening": false}, nil
			}
			c.Close()
			return map[string]any{"port": p.Port, "listening": true, "http": true}, nil
		case "port.dial":
			if err != nil {
				return nil, &rpc.Error{Code: "port_gone", Message: "nothing is listening"}
			}
			if err := nodeMux.Open(p.ID); err != nil {
				c.Close()
				return nil, err
			}
			nodeMux.Start(p.ID, c)
			return map[string]any{}, nil
		}
		return nil, fmt.Errorf("unknown method %s", method)
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _ = hubSide.Serve(ctx, n2hR) }()
	go func() { defer wg.Done(); _ = nodeSide.Serve(ctx, h2nR) }()
	t.Cleanup(func() { cancel(); h2nW.Close(); n2hW.Close(); wg.Wait() })

	m := newMachine(h, MachineConfig{ID: "m1", Name: "dev box"})
	m.peer, m.online, m.streams = hubSide, true, hubMux
	m.sessions = sessions
	h.byID["m1"] = m
	return m
}

// devServer is what an agent runs: a page, a login that redirects and sets a cookie, a page reading it, and a
// WebSocket that echoes (HMR's channel).
func devServer(t *testing.T) (*httptest.Server, int) {
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

// phone listens on loopback the way the app does and opens one WebSocket to the hub per connection.
func phone(t *testing.T, hubURL, token string, port int) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	ws := "ws" + strings.TrimPrefix(hubURL, "http") + "/api/v1/machines/m1/ports/" + strconv.Itoa(port) + "/connect"
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				wc, _, err := websocket.Dial(ctx, ws, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + token}}})
				if err != nil {
					return
				}
				nc := websocket.NetConn(ctx, wc, websocket.MessageBinary)
				defer nc.Close()
				go func() { _, _ = io.Copy(nc, c); nc.Close() }()
				_, _ = io.Copy(c, nc)
			}()
		}
	}()
	return ln.Addr().String()
}

func portHub(t *testing.T) (*Hub, *httptest.Server) {
	t.Helper()
	h := newTestHub(t)
	h.cfg.Token = "test-token"
	srv := httptest.NewServer(h.routes())
	t.Cleanup(srv.Close)
	return h, srv
}

// An agent's dev server through the hub: the page with the browser's own Host, a redirect setting a cookie that the
// next page reads, and a WebSocket both ways.
func TestForwardedPortCarriesHTTPAndWebSocket(t *testing.T) {
	h, srv := portHub(t)
	_, port := devServer(t)
	portNode(t, h, []proto.Session{{SessionID: "s1", Ports: []proto.Port{{Port: port, Title: "Dev"}}}})
	local := phone(t, srv.URL, "test-token", port)

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 10 * time.Second}
	res, err := client.Get("http://" + local + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if !strings.Contains(string(body), "<title>Dev</title>") || !strings.Contains(string(body), "host="+local) {
		t.Fatalf("page = %q (the Host must be the one the browser used)", body)
	}

	res, err = client.Get("http://" + local + "/login")
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if res.Request.URL.Path != "/me" || string(body) != "hello s3cr3t" {
		t.Fatalf("after the redirect: %s %q", res.Request.URL, body)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws://"+local+"/hmr", nil)
	if err != nil {
		t.Fatalf("websocket through the forward: %v", err)
	}
	defer c.CloseNow()
	c.SetReadLimit(1 << 20)
	for _, msg := range []string{"update", strings.Repeat("x", 200<<10)} {
		if err := c.Write(ctx, websocket.MessageText, []byte(msg)); err != nil {
			t.Fatal(err)
		}
		_, got, err := c.Read(ctx)
		if err != nil || string(got) != "echo:"+msg {
			t.Fatalf("echo of %.10q: %.20q %v", msg, got, err)
		}
	}
}

// The hub refuses before the upgrade: a bad token, a read-only device, a port no agent listens on and nobody opened,
// and an allowed port with nothing on it. Opening a port that answers allows it.
func TestForwardedPortRefusals(t *testing.T) {
	h, srv := portHub(t)
	_, agentPort := devServer(t)
	_, ownPort := devServer(t) // the user's own server: no agent's
	portNode(t, h, []proto.Session{{SessionID: "s1", Ports: []proto.Port{{Port: agentPort}}}})
	connect := func(port int, token string) int {
		req, _ := http.NewRequest("GET", srv.URL+"/api/v1/machines/m1/ports/"+strconv.Itoa(port)+"/connect", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Connection", "Upgrade")
		req.Header.Set("Upgrade", "websocket")
		req.Header.Set("Sec-WebSocket-Version", "13")
		req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	post := func(path string) int {
		req, _ := http.NewRequest("POST", srv.URL+path, nil)
		req.Header.Set("Authorization", "Bearer test-token")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}

	if got := connect(agentPort, "wrong"); got != http.StatusUnauthorized {
		t.Errorf("bad token: %d", got)
	}
	if got := connect(agentPort, "test-token"); got != http.StatusSwitchingProtocols {
		t.Errorf("the agent's port: %d", got)
	}
	if got := connect(ownPort, "test-token"); got != http.StatusForbidden {
		t.Errorf("a port nobody opened: %d", got)
	}
	if got := connect(22, "test-token"); got != http.StatusForbidden {
		t.Errorf("ssh: %d", got)
	}
	if got := post("/api/v1/machines/m1/ports/" + strconv.Itoa(ownPort) + "/open"); got != http.StatusOK {
		t.Errorf("open the user's server: %d", got)
	}
	if got := connect(ownPort, "test-token"); got != http.StatusSwitchingProtocols {
		t.Errorf("an opened port: %d", got)
	}
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	closed := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	if got := post("/api/v1/machines/m1/ports/" + strconv.Itoa(closed) + "/open"); got != http.StatusNotFound {
		t.Errorf("open a closed port: %d", got)
	}
	h.byID["m1"].sessions[0].Ports = append(h.byID["m1"].sessions[0].Ports, proto.Port{Port: closed}) // listed, then stopped
	if got := connect(closed, "test-token"); got != http.StatusNotFound {
		t.Errorf("a listed port that is gone: %d", got)
	}
	if got := post("/api/v1/machines/m1/ports/0/open"); got != http.StatusBadRequest {
		t.Errorf("port 0: %d", got)
	}

	// A read-only device (the watch) cannot connect even to an agent's port.
	ro := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/machines/m1/ports/"+strconv.Itoa(agentPort)+"/connect", nil)
	req.SetPathValue("mid", "m1")
	req.SetPathValue("port", strconv.Itoa(agentPort))
	req = req.WithContext(context.WithValue(req.Context(), principalKey{}, principal{Role: roleAnswer}))
	h.withMachine(h.connectPort)(ro, req)
	if ro.Code != http.StatusForbidden {
		t.Errorf("read-only device: %d", ro.Code)
	}
}

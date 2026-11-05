//go:build herdrlive && !windows

package node

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/herdr"
	"github.com/nwverse/persona/server/internal/host"
	"github.com/nwverse/persona/server/internal/rpc"
)

// Against a throwaway herdr session you made yourself (never the one you work in), with at least two tabs in a
// workspace and a second workspace:
//
//	PERSONA_LIVE_FULL_SESSION=persona-focus-test go test -tags herdrlive -run TestLiveFocusSync ./internal/node/
//
// It runs the node's focus watcher on that session and checks what the hub would be told for a focus moved by another
// client of the API ("other"), by Persona's own request ("persona"), and by keys typed into a real full client
// ("client"): herdr's prefix + a tab number, in the PTY.
func TestLiveFocusSync(t *testing.T) {
	name := os.Getenv("PERSONA_LIVE_FULL_SESSION")
	if name == "" || name == "default" {
		t.Skip("set PERSONA_LIVE_FULL_SESSION to a throwaway session's name (not the default one)")
	}
	out, err := exec.Command("herdr", "session", "list", "--json").Output()
	if err != nil {
		t.Fatal(err)
	}
	var list struct {
		Sessions []struct {
			Name       string `json:"name"`
			SocketPath string `json:"socket_path"`
		} `json:"sessions"`
	}
	_ = json.Unmarshal(out, &list)
	socket := ""
	for _, s := range list.Sessions {
		if s.Name == name {
			socket = s.SocketPath
		}
	}
	if socket == "" {
		t.Fatalf("no session %q", name)
	}
	cli := func(args ...string) {
		t.Helper()
		c := exec.Command("herdr", args...)
		c.Env = append(os.Environ(), "HERDR_SOCKET_PATH="+socket)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("herdr %v: %v: %s", args, err, out)
		}
	}

	client := &herdr.Client{Bin: "herdr", Socket: socket}
	n := &Node{agents: nil, state: newState(), hosts: herdrHosts(client)}
	ctx, cancel := context.WithCancel(context.Background())
	n2hR, n2hW := io.Pipe()
	h2nR, h2nW := io.Pipe()
	n.peer = rpc.New("n", n2hW)
	hub := rpc.New("h", h2nW)
	var mu sync.Mutex
	var got []map[string]any
	hub.OnEvent = func(event string, params json.RawMessage) {
		if event != "herdr.focus.changed" {
			return
		}
		var p map[string]any
		_ = json.Unmarshal(params, &p)
		mu.Lock()
		got = append(got, p)
		mu.Unlock()
	}
	go func() { _ = hub.Serve(ctx, n2hR) }()
	go func() { _ = n.peer.Serve(ctx, h2nR) }()
	t.Cleanup(func() { n.closeFulls(); cancel(); _ = n2hW.Close(); _ = h2nW.Close() })

	snap, err := client.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	tabsOf := map[string][]string{}
	for _, tb := range snap.Tabs {
		tabsOf[tb.WorkspaceID] = append(tabsOf[tb.WorkspaceID], tb.ID)
	}
	var ws, tab1, tab2 string
	for w, tabs := range tabsOf {
		if len(tabs) >= 2 {
			ws, tab1, tab2 = w, tabs[0], tabs[1]
		}
	}
	if ws == "" || len(snap.Workspaces) < 2 {
		t.Fatalf("the session needs a workspace with two tabs and another workspace: %+v", tabsOf)
	}
	var otherPane, tab2Pane string
	for _, p := range snap.Panes {
		if p.WorkspaceID != ws && otherPane == "" {
			otherPane = p.PaneID
		}
		if p.TabID == tab2 {
			tab2Pane = p.PaneID
		}
	}

	cli("tab", "focus", tab1) // a known start: the first read is only the baseline
	f := n.hosts.Get(host.Herdr).(host.Focuser)
	go n.focusStream(ctx, f)
	fullWait(t, 10*time.Second, "the focus baseline", func() bool {
		n.focus.mu.Lock()
		defer n.focus.mu.Unlock()
		return n.focus.known
	})
	next := func(what string) map[string]any {
		t.Helper()
		var p map[string]any
		fullWait(t, 10*time.Second, what, func() bool {
			mu.Lock()
			defer mu.Unlock()
			if len(got) == 0 {
				return false
			}
			p, got = got[0], got[1:]
			return true
		})
		return p
	}
	quiet := func(what string) {
		t.Helper()
		time.Sleep(500 * time.Millisecond)
		mu.Lock()
		defer mu.Unlock()
		if len(got) != 0 {
			t.Fatalf("%s: unexpected %v", what, got)
		}
	}

	// another client of the API moves the focus: nobody here did
	cli("tab", "focus", tab2)
	p := next("a focus moved by the API")
	if p["source"] != "other" || p["tab_id"] != tab2 || p["pane_id"] != tab2Pane || p["client_id"] != nil {
		t.Fatalf("api change = %v", p)
	}
	quiet("after one change")

	// Persona's own request comes back as its own
	n.mu.Lock()
	n.fulls = map[string]*fullClient{"hc_live_req": {id: "hc_live_req"}}
	n.mu.Unlock()
	n.onFocusRequest(json.RawMessage(fmt.Sprintf(`{"client_id":"hc_live_req","session_id":"pane:%s"}`, otherPane)))
	p = next("Persona's focus request")
	if p["source"] != "persona" || p["client_id"] != "hc_live_req" || p["pane_id"] != otherPane {
		t.Fatalf("persona change = %v", p)
	}
	quiet("after the echo")
	n.mu.Lock()
	n.fulls = nil
	n.mu.Unlock()

	// back to the first tab of the workspace, then a key typed into a real full client switches tab
	cli("tab", "focus", tab2)
	next("the API again")
	if _, err := n.fullOpen(ctx, json.RawMessage(`{"client_id":"hc_live","cols":120,"rows":40}`)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1500 * time.Millisecond) // herdr draws its screen; the client's startup answers are not user input
	quiet("while the client starts")
	n.onFullEvent("herdr.client.input", json.RawMessage(`{"client_id":"hc_live","data":"\u0002"}`)) // herdr's prefix
	n.onFullEvent("herdr.client.input", json.RawMessage(`{"client_id":"hc_live","data":"1"}`))      // tab 1
	p = next("a key typed into the full client")
	if p["source"] != "client" || p["client_id"] != "hc_live" || p["tab_id"] != tab1 {
		t.Fatalf("client change = %v (want tab %s)", p, tab1)
	}
	t.Logf("live: other=%s persona=%s client=%s", tab2, otherPane, tab1)
}

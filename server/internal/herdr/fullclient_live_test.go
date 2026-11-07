//go:build herdrlive && !windows

package herdr

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// Against a throwaway herdr session you made yourself (never the one you work in):
//
//	herdr --session persona-fullui-test      # start it, detach (or stop the client)
//	PERSONA_LIVE_FULL_SESSION=persona-fullui-test go test -tags herdrlive -run TestLiveFullClient ./internal/herdr/
//
// It starts the full client for that session, checks herdr's own screen comes out and that resizing the client resizes
// the session's layout (the finding in docs/integrations/terminal.md), then closes it and checks nothing is left running.
func TestLiveFullClient(t *testing.T) {
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
	var socket string
	for _, s := range list.Sessions {
		if s.Name == name {
			socket = s.SocketPath
		}
	}
	if socket == "" {
		t.Fatalf("no session %q", name)
	}
	c := &Client{Bin: "herdr", Socket: socket}
	ctx := context.Background()
	got, err := c.SessionName(ctx)
	if err != nil || got != name {
		t.Fatalf("SessionName = %q, %v", got, err)
	}
	snap, err := c.Snapshot(ctx)
	if err != nil || len(snap.Panes) == 0 {
		t.Fatalf("snapshot: %v, %d panes", err, len(snap.Panes))
	}
	pane := snap.Panes[0].PaneID

	var mu sync.Mutex
	var buf strings.Builder
	fc, err := c.StartFull(FullOptions{Session: name, Cols: 100, Rows: 30}, func(b []byte) { mu.Lock(); buf.Write(b); mu.Unlock() })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(fc.Close)
	text := func() string { mu.Lock(); defer mu.Unlock(); return buf.String() }
	wait := func(what string, ok func() bool) {
		t.Helper()
		for deadline := time.Now().Add(15 * time.Second); !ok(); time.Sleep(50 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s", what)
			}
		}
	}
	wait("herdr's screen", func() bool { return strings.Contains(text(), "\x1b[?1049h") && strings.Contains(text(), "spaces") })
	for _, q := range []string{"\x1b[?996n", "\x1b]11;?", "\x1b[?2031h"} { // what it asks of the terminal (the web answers these)
		if !strings.Contains(text(), q) {
			t.Errorf("the client did not ask %q", q)
		}
	}
	cols, rows := func() (int, int) { c, r, _ := c.PaneSize(ctx, pane); return c, r }()
	t.Logf("pane %s is %dx%d with a 100x30 client", pane, cols, rows)
	if cols >= 100 || rows >= 30 {
		t.Errorf("the pane (%dx%d) should be inside the client's 100x30 (sidebar and bars take some)", cols, rows)
	}
	if err := fc.Resize(140, 40); err != nil {
		t.Fatal(err)
	}
	wait("the layout to follow the client", func() bool { c2, r2, _ := c.PaneSize(ctx, pane); return c2 > cols && r2 > rows })

	pid := fc.cmd.Process.Pid
	fc.Close()
	if err := syscall.Kill(pid, 0); err == nil {
		t.Errorf("the client process %d is still running", pid)
	}
	if fc.Err() != nil {
		t.Errorf("Err after Close = %v", fc.Err())
	}
	// the server and its panes are still there
	if _, err := c.Snapshot(ctx); err != nil {
		t.Errorf("the session did not survive its client: %v", err)
	}
}

// FocusPane shows the tab of a pane (herdr's focus is shared by all its clients).
func TestLiveFocusPane(t *testing.T) {
	name := os.Getenv("PERSONA_LIVE_FULL_SESSION")
	if name == "" || name == "default" {
		t.Skip("set PERSONA_LIVE_FULL_SESSION to a throwaway session's name (not the default one)")
	}
	out, _ := exec.Command("herdr", "session", "list", "--json").Output()
	var list struct {
		Sessions []struct {
			Name       string `json:"name"`
			SocketPath string `json:"socket_path"`
		} `json:"sessions"`
	}
	_ = json.Unmarshal(out, &list)
	c := &Client{Bin: "herdr"}
	for _, s := range list.Sessions {
		if s.Name == name {
			c.Socket = s.SocketPath
		}
	}
	ctx := context.Background()
	snap, err := c.Snapshot(ctx)
	if err != nil || len(snap.Tabs) < 2 {
		t.Skipf("needs a session with two tabs: %v, %d tabs", err, len(snap.Tabs))
	}
	focused := func() string {
		var res struct {
			Tabs []struct {
				ID      string `json:"tab_id"`
				Focused bool   `json:"focused"`
			} `json:"tabs"`
		}
		if err := c.call(ctx, "tab.list", map[string]any{}, &res, "tab", "list"); err != nil {
			t.Fatal(err)
		}
		for _, tab := range res.Tabs {
			if tab.Focused {
				return tab.ID
			}
		}
		return ""
	}
	for _, tab := range snap.Tabs {
		var pane string
		for _, p := range snap.Panes {
			if p.TabID == tab.ID {
				pane = p.PaneID
				break
			}
		}
		if pane == "" {
			continue
		}
		if err := c.FocusPane(ctx, pane); err != nil {
			t.Fatal(err)
		}
		if got := focused(); got != tab.ID {
			t.Fatalf("after FocusPane(%s) the focused tab is %q, want %q", pane, got, tab.ID)
		}
	}
}

package herdr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// snapshotFixture has an oversized whole snapshot, while each workspace and tab fits in herdr's socket buffer.
// Agents carry names and metadata absent from pane.list, and inactive workspaces remember their own local focus.
func snapshotFixture() *Snapshot {
	s := &Snapshot{
		Workspaces: []Workspace{
			{ID: "w1", Label: "Main project", Number: 1, Focused: true, Cwd: "/tmp/project", Branch: "main"},
			{ID: "w2", Label: "Review project", Number: 2},
			{ID: "w3", Label: "Empty workspace", Number: 3},
		},
		Tabs: []Tab{
			{ID: "w1:t1", WorkspaceID: "w1", Label: "Build", Number: 1},
			{ID: "w1:t2", WorkspaceID: "w1", Label: "Tests", Number: 2, Focused: true},
			{ID: "w2:t1", WorkspaceID: "w2", Label: "Review", Number: 1, Focused: true},
		},
		FocusedWorkspaceID: "w1", FocusedTabID: "w1:t2", FocusedPaneID: "w1:p4",
	}
	for i, tab := range s.Tabs {
		first, second := fmt.Sprintf("%s:p%d", tab.WorkspaceID, 2*i+1), fmt.Sprintf("%s:p%d", tab.WorkspaceID, 2*i+2)
		s.Layouts = append(s.Layouts, TabLayout{
			TabID: tab.ID, WorkspaceID: tab.WorkspaceID, FocusedPaneID: second, Zoomed: i == 1,
			Panes: []LayoutPane{
				{PaneID: second, Focused: true, Rect: Rect{X: 80, Width: 80, Height: 24}},
				{PaneID: first, Rect: Rect{Width: 80, Height: 24}},
			},
			Splits: []LayoutSplit{{ID: fmt.Sprintf("split_%d", i), Direction: "right", Ratio: 0.5, Rect: Rect{Width: 160, Height: 24}}},
		})
		for j, pane := range []string{first, second} {
			p := Agent{PaneID: pane, TabID: tab.ID, WorkspaceID: tab.WorkspaceID,
				Cwd: "/tmp/project", ForegroundCwd: "/tmp/project/src", AgentStatus: "working",
				Title: strings.Repeat("terminal output ", 44), RawTitle: strings.Repeat("terminal output ", 44)}
			if j == 0 || i == 2 {
				p.Agent = "claude"
				p.AgentSession = &AgentSession{Kind: "id", Value: fmt.Sprintf("session-%d-%d", i, j)}
				a := p
				a.Name = fmt.Sprintf("agent-%d-%d", i, j)
				a.Tokens = map[string]string{"task": fmt.Sprintf("task-%d", i)}
				s.Agents = append(s.Agents, a)
			}
			s.Panes = append(s.Panes, p)
		}
	}
	return s
}

func snapshotResult(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return `"result":` + string(b)
}

func snapshotReply(s *Snapshot, method string, params map[string]any) string {
	switch method {
	case "session.snapshot":
		return snapshotResult(map[string]any{"snapshot": s})
	case "workspace.list":
		return snapshotResult(map[string]any{"workspaces": s.Workspaces})
	case "tab.list":
		tabs := []Tab{}
		for _, tab := range s.Tabs {
			if tab.WorkspaceID == params["workspace_id"] {
				tabs = append(tabs, tab)
			}
		}
		return snapshotResult(map[string]any{"tabs": tabs})
	case "pane.list":
		panes := []Agent{}
		for _, pane := range s.Panes {
			if pane.WorkspaceID == params["workspace_id"] {
				panes = append(panes, pane)
			}
		}
		return snapshotResult(map[string]any{"panes": panes})
	case "agent.get":
		for _, a := range s.Agents {
			if a.PaneID == params["target"] {
				return snapshotResult(map[string]any{"agent": a})
			}
		}
	case "pane.layout":
		for _, layout := range s.Layouts {
			for _, pane := range layout.Panes {
				if pane.PaneID == params["pane_id"] {
					return snapshotResult(map[string]any{"layout": layout})
				}
			}
		}
	}
	return `"error":{"code":"not_found","message":"no fixture for this call"}`
}

func TestTruncatedSnapshotRebuildsTheWholeLayout(t *testing.T) {
	want := snapshotFixture()
	if len(snapshotReply(want, "session.snapshot", nil)) <= 8192 {
		t.Fatal("the fixture must exceed the socket buffer")
	}
	var mu sync.Mutex
	calls := map[string]int{}
	sock := fakeServerLimited(t, 8192, func(method string, params map[string]any) string {
		mu.Lock()
		calls[method]++
		mu.Unlock()
		return snapshotReply(want, method, params)
	})
	c := &Client{Socket: sock}
	got, err := c.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		g, _ := json.Marshal(got)
		w, _ := json.Marshal(want)
		t.Fatalf("reconstructed snapshot differs\n got: %s\nwant: %s", g, w)
	}
	mu.Lock()
	counts := map[string]int{"session.snapshot": 1, "workspace.list": 1, "tab.list": 3, "pane.list": 3, "pane.layout": 3, "agent.get": 4}
	if !reflect.DeepEqual(calls, counts) {
		t.Errorf("calls = %v, want %v (one layout per tab, no global agent list)", calls, counts)
	}
	mu.Unlock()
	if indexes := PaneIndexes(got.Layouts); indexes["w1:p3"] != 0 || indexes["w1:p4"] != 1 {
		t.Errorf("split pane indexes: %v", indexes)
	}
	focus, err := c.Focused(context.Background())
	if err != nil || focus != (Focus{Workspace: "w1", Tab: "w1:t2", Pane: "w1:p4"}) {
		t.Fatalf("focused = %+v, %v", focus, err)
	}
}

func TestSnapshotOnlyRebuildsTruncatedAnswers(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprintf("error=%v", fail), func(t *testing.T) {
			want := snapshotFixture()
			var calls atomic.Int32
			sock := fakeServer(t, func(method string, params map[string]any) string {
				calls.Add(1)
				if method != "session.snapshot" {
					t.Errorf("unexpected fallback call: %s", method)
				}
				if fail {
					return `"error":{"code":"internal","message":"snapshot unavailable"}`
				}
				return snapshotReply(want, method, params)
			})
			got, err := (&Client{Socket: sock}).Snapshot(context.Background())
			if fail {
				var he *Error
				if got != nil || !errors.As(err, &he) || he.Code != "internal" {
					t.Fatalf("snapshot = %+v, err = %v", got, err)
				}
			} else if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("whole snapshot changed: %+v, %v", got, err)
			}
			if calls.Load() != 1 {
				t.Fatalf("made %d calls, want one", calls.Load())
			}
		})
	}
}

func TestTruncatedSnapshotRejectsFailedParts(t *testing.T) {
	for _, method := range []string{"workspace.list", "tab.list", "pane.list", "agent.get", "pane.layout"} {
		for _, cut := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/cut=%v", method, cut), func(t *testing.T) {
				want := snapshotFixture()
				var failed atomic.Bool
				sock := fakeServerLimited(t, 8192, func(m string, p map[string]any) string {
					if m == method {
						failed.Store(true)
						if cut {
							return snapshotResult(map[string]any{"padding": strings.Repeat("x", 9000)})
						}
						return `"error":{"code":"internal","message":"part unavailable"}`
					}
					return snapshotReply(want, m, p)
				})
				got, err := (&Client{Socket: sock}).Snapshot(context.Background())
				if !failed.Load() || got != nil || !errors.Is(err, ErrTruncated) {
					t.Fatalf("failed=%v, snapshot=%+v, err=%v", failed.Load(), got, err)
				}
				if !cut {
					var he *Error
					if !errors.As(err, &he) || he.Message != "part unavailable" {
						t.Fatalf("lost subcall error: %v", err)
					}
				}
			})
		}
	}
}

func TestTruncatedSnapshotRejectsMovedLayout(t *testing.T) {
	want := snapshotFixture()
	sock := fakeServerLimited(t, 8192, func(method string, params map[string]any) string {
		if method == "pane.layout" {
			layout := want.Layouts[0]
			layout.TabID = "w1:t9"
			return snapshotResult(map[string]any{"layout": layout})
		}
		return snapshotReply(want, method, params)
	})
	got, err := (&Client{Socket: sock}).Snapshot(context.Background())
	if got != nil || !errors.Is(err, ErrTruncated) || !strings.Contains(err.Error(), "pane moved") {
		t.Fatalf("snapshot = %+v, err = %v", got, err)
	}
}

func TestSnapshotTruncationLogsOnceUntilAWholeSnapshotSucceeds(t *testing.T) {
	want := snapshotFixture()
	var whole atomic.Bool
	sock := fakeServerLimited(t, 8192, func(method string, params map[string]any) string {
		if method == "session.snapshot" && whole.Load() {
			return snapshotResult(map[string]any{"snapshot": &Snapshot{}})
		}
		return snapshotReply(want, method, params)
	})
	var logs atomic.Int32
	c := &Client{Socket: sock, Log: func(format string, args ...any) {
		logs.Add(1)
		if !strings.Contains(format, "session.snapshot answer cut short") {
			t.Errorf("unexpected log: %s", fmt.Sprintf(format, args...))
		}
	}}
	for range 3 {
		if _, err := c.Snapshot(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if logs.Load() != 1 {
		t.Fatalf("one streak logged %d times", logs.Load())
	}
	whole.Store(true)
	if _, err := c.Snapshot(context.Background()); err != nil {
		t.Fatal(err)
	}
	whole.Store(false)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.Snapshot(context.Background()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if logs.Load() != 2 {
		t.Fatalf("two streaks logged %d times", logs.Load())
	}
}

package orcart_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chyroc/jarvis/server/internal/orcart"
	"github.com/chyroc/jarvis/server/internal/orcart/orcarttest"
)

// logs collects a client's log lines.
type logs struct {
	mu    sync.Mutex
	lines []string
}

func (l *logs) logf(format string, args ...any) {
	l.mu.Lock()
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
	l.mu.Unlock()
}

func (l *logs) all() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.lines, "\n")
}

func client(f *orcarttest.Fake, l *logs) *orcart.Client {
	c := &orcart.Client{MetaPath: f.MetaPath}
	if l != nil {
		c.Log = l.logf
	}
	return c
}

// A call checks the runtime's protocol once (status.get), then lists; keepalive lines before an answer are skipped.
func TestCallChecksCompatOnceAndSkipsKeepalives(t *testing.T) {
	f := orcarttest.New(t)
	f.SetTerminals(orcarttest.Terminal("term_1", "tab1", "leaf1", "wt1", "/tmp/p", "refs/heads/main", "x", "claude"))
	f.Keepalives(2)
	c := client(f, nil)
	for range 3 {
		l, err := c.Terminals(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(l.Terminals) != 1 || l.Terminals[0].PaneKey() != "tab1:leaf1" || !l.Terminals[0].Local() {
			t.Fatalf("terminals: %+v", l.Terminals)
		}
		if len(l.Tabs) != 1 || l.Tabs[0].ID != "tab1" || l.Tabs[0].WorktreeID != "wt1" || l.Tabs[0].Title != "tab tab1" {
			t.Fatalf("tabs: %+v", l.Tabs)
		}
	}
	if n := f.Count("status.get"); n != 1 {
		t.Errorf("status.get %d times", n)
	}
	if tr := f.Trace("terminal.list"); len(tr) != 3 || !strings.Contains(tr[0], `"includePreview":false`) {
		t.Errorf("list calls: %v", tr)
	}
}

// Orca without its metadata file, or whose socket is gone, is not running: quiet, ErrNotRunning.
func TestNotRunning(t *testing.T) {
	l := &logs{}
	c := &orcart.Client{MetaPath: filepath.Join(t.TempDir(), "orca-runtime.json"), Log: l.logf}
	if _, err := c.Terminals(context.Background()); !errors.Is(err, orcart.ErrNotRunning) {
		t.Fatalf("no metadata: %v", err)
	}
	f := orcarttest.New(t)
	f.Stop()
	c = client(f, l)
	if _, err := c.Terminals(context.Background()); !errors.Is(err, orcart.ErrNotRunning) {
		t.Fatalf("socket gone: %v", err)
	}
	if got := l.all(); got != "" {
		t.Errorf("logged for an Orca never seen: %q", got)
	}
	if v := c.Version(context.Background()); v != "" {
		t.Errorf("version of a stopped Orca: %q", v)
	}
}

// Orca restarting (new socket, new token) is picked up by reading the metadata again: on a refused connection and on
// unauthorized. The token never shows in the log.
func TestRestartReadsTheMetadataAgain(t *testing.T) {
	f := orcarttest.New(t)
	l := &logs{}
	c := client(f, l)
	if _, err := c.Worktrees(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := f.Restart("test-token-2"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Worktrees(context.Background()); err != nil {
		t.Fatalf("after restart: %v", err)
	}
	// The same socket, a new token (the metadata file rewritten in place).
	f.Stop()
	if err := f.Restart("test-token-3"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Worktrees(context.Background()); err != nil {
		t.Fatalf("after a token change: %v", err)
	}
	for _, tok := range []string{"test-token-1", "test-token-2", "test-token-3"} {
		if strings.Contains(l.all(), tok) {
			t.Errorf("token in the log: %q", l.all())
		}
	}
}

// A wrong token in a metadata file that does not change stays an error (unauthorized), tried once more only.
func TestUnauthorized(t *testing.T) {
	f := orcarttest.New(t)
	b, _ := os.ReadFile(f.MetaPath)
	if err := os.WriteFile(f.MetaPath, []byte(strings.Replace(string(b), f.Token(), "wrong-token", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	c := client(f, nil)
	if _, err := c.Terminals(context.Background()); !orcart.IsCode(err, "unauthorized") {
		t.Fatalf("wrong token: %v", err)
	}
}

// A runtime whose protocol range does not include this client's is incompatible: every call fails with a reason.
func TestIncompatible(t *testing.T) {
	for _, st := range []map[string]any{
		{"runtimeProtocolVersion": 2, "minCompatibleRuntimeClientVersion": 1},
		{"runtimeProtocolVersion": 9, "minCompatibleRuntimeClientVersion": 7},
	} {
		f := orcarttest.New(t)
		f.SetStatus(st)
		c := client(f, nil)
		if _, err := c.Terminals(context.Background()); !errors.Is(err, orcart.ErrIncompatible) {
			t.Fatalf("%v: %v", st, err)
		}
		if f.Count("terminal.list") != 0 {
			t.Errorf("listed an incompatible runtime")
		}
		s, err := c.Probe(context.Background())
		if !errors.Is(err, orcart.ErrIncompatible) || s == nil || s.AppVersion != "1.4.245" {
			t.Errorf("probe: %+v %v", s, err)
		}
		if v := c.Version(context.Background()); v != "" {
			t.Errorf("version of an incompatible runtime: %q", v)
		}
	}
}

// Sends: raw text without enter, an agent prompt with enter; a send Orca turns down is a Refusal; an error answer is
// an *Error with its code.
func TestSend(t *testing.T) {
	f := orcarttest.New(t)
	f.SetTerminals(orcarttest.Terminal("term_1", "tab1", "leaf1", "wt1", "/tmp/p", "", "x", "claude"))
	c := client(f, nil)
	ctx := context.Background()
	if err := c.Send(ctx, "term_1", "\x1b[Z"); err != nil {
		t.Fatal(err)
	}
	if err := c.Prompt(ctx, "term_1", "hello"); err != nil {
		t.Fatal(err)
	}
	want := []string{
		orcarttest.SendCall("term_1", map[string]any{"text": "\x1b[Z"}),
		orcarttest.SendCall("term_1", map[string]any{"text": "hello", "enter": true, "agentPrompt": true}),
	}
	if got := f.Trace("terminal.send"); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("sends:\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	f.Handle("terminal.send", func(map[string]any) any {
		return map[string]any{"send": map[string]any{"handle": "term_1", "accepted": false, "bytesWritten": 0,
			"refusedReason": "permission"}}
	})
	var r *orcart.Refusal
	if err := c.Prompt(ctx, "term_1", "hello"); !errors.As(err, &r) || r.Reason != "permission" ||
		!errors.Is(err, orcart.ErrRefused) {
		t.Errorf("refused: %v", err)
	}
	f.Handle("terminal.send", nil)
	if err := c.Send(ctx, "term_gone", "x"); !orcart.IsCode(err, "terminal_handle_stale") || !orcart.Refused(err) {
		t.Errorf("stale handle: %v", err)
	}
}

// The screen comes back as text with soft-wrapped rows joined; a screen Orca cannot give is an error.
func TestRead(t *testing.T) {
	f := orcarttest.New(t)
	f.SetTerminals(orcarttest.Terminal("term_1", "tab1", "leaf1", "wt1", "/tmp/p", "", "x", "claude"))
	f.SetScreenRows("term_1", []string{"a long li", "ne", "❯ next"}, []bool{true, false, false})
	c := client(f, nil)
	s, err := c.Read(context.Background(), "term_1")
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Text(); got != "a long line\n❯ next\n" || s.Source != "screen" {
		t.Errorf("text %q source %q", got, s.Source)
	}
	if tr := f.Trace("terminal.read"); len(tr) != 1 || tr[0] != `terminal.read {"screen":true,"terminal":"term_1"}` {
		t.Errorf("read: %v", tr)
	}
	f.Handle("terminal.read", func(map[string]any) any {
		return map[string]any{"terminal": map[string]any{"handle": "term_1", "tail": []string{}, "source": "screen-unavailable"}}
	})
	if _, err := c.Read(context.Background(), "term_1"); !errors.Is(err, orcart.ErrNoScreen) {
		t.Errorf("no screen: %v", err)
	}
}

// A wedged Orca (accepts, never answers) costs a call its timeout, not more; the failure is logged once per episode.
func TestTimeoutAndEpisodeLog(t *testing.T) {
	f := orcarttest.New(t)
	l := &logs{}
	c := client(f, l)
	if _, err := c.Worktrees(context.Background()); err != nil {
		t.Fatal(err)
	}
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	f.Handle("worktree.ps", func(map[string]any) any { <-block; return nil })
	for range 2 {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		start := time.Now()
		_, err := c.Worktrees(ctx)
		cancel()
		if err == nil || time.Since(start) > 2*time.Second {
			t.Fatalf("wedged: %v after %s", err, time.Since(start))
		}
	}
	// A caller giving up is not Orca failing: nothing logged.
	if got := l.all(); got != "" {
		t.Errorf("logged a caller's timeout: %q", got)
	}
}

// A read-only client never sends a method that could change Orca.
func TestReadOnly(t *testing.T) {
	f := orcarttest.New(t)
	f.SetTerminals(orcarttest.Terminal("term_1", "tab1", "leaf1", "wt1", "/tmp/p", "", "x", "claude"))
	c := &orcart.Client{MetaPath: f.MetaPath, ReadOnly: true}
	ctx := context.Background()
	if _, err := c.Terminals(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Read(ctx, "term_1"); err != nil {
		t.Fatal(err)
	}
	for _, err := range []error{c.Send(ctx, "term_1", "x"), c.Prompt(ctx, "term_1", "x"), c.Close(ctx, "term_1")} {
		if err == nil || !strings.Contains(err.Error(), "read-only") {
			t.Errorf("read-only client: %v", err)
		}
	}
	if n := f.Count("terminal.send") + f.Count("terminal.close"); n != 0 {
		t.Errorf("%d writes reached Orca", n)
	}
}

package transcript

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chyroc/jarvis/server/internal/proto"
)

// Fixtures are real transcripts / rollouts run through scripts/redact_transcript.py (testdata/README.md). Each one has
// a golden file with what the parser makes of it; `go test ./internal/transcript -run Fixture -update` rewrites them,
// and the diff is the review.

var update = flag.Bool("update", false, "rewrite the golden files in testdata")

type fixture struct {
	name string
	path string
	sub  bool // a subagent's transcript (every line a sidechain)
}

func fixtures(t *testing.T) []fixture {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("testdata", "*.jsonl"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no fixtures: %v", err)
	}
	var out []fixture
	for _, p := range paths {
		name := strings.TrimSuffix(filepath.Base(p), ".jsonl")
		out = append(out, fixture{name: name, path: p, sub: strings.Contains(name, "subagent")})
	}
	return out
}

func (fx fixture) open(path string) *File {
	if fx.sub {
		return NewSubagentFile(path)
	}
	return NewFile(path)
}

// golden is what a fixture's golden file holds: every item, and the session-level facts the apps show.
type golden struct {
	Items []proto.Item `json:"items"`
	Meta  goldenMeta   `json:"meta"`
}

type goldenMeta struct {
	Title          string                 `json:"title,omitempty"`
	FirstUser      string                 `json:"first_user,omitempty"`
	LastText       string                 `json:"last_text,omitempty"`
	Model          string                 `json:"model,omitempty"`
	Cwd            string                 `json:"cwd,omitempty"`
	PermissionMode string                 `json:"permission_mode,omitempty"`
	Effort         string                 `json:"effort,omitempty"`
	Turns          int                    `json:"turns"`
	Compactions    int                    `json:"compactions"`
	ContextTokens  int                    `json:"context_tokens"`
	OutputTokens   int                    `json:"output_tokens"`
	Queue          []string               `json:"queue,omitempty"`
	Background     []proto.BackgroundTask `json:"background,omitempty"`
	TurnOpen       bool                   `json:"turn_open,omitempty"`
	LastTurnWake   bool                   `json:"last_turn_wake,omitempty"`
	LastActivity   time.Time              `json:"last_activity"`
}

func snapshot(f *File) golden {
	m := f.Meta()
	items, _ := f.Page(0, 1<<30)
	return golden{Items: items, Meta: goldenMeta{
		Title: m.Title, FirstUser: m.FirstUser, LastText: m.LastText, Model: m.Model, Cwd: m.Cwd,
		PermissionMode: m.PermissionMode, Effort: m.Effort, Turns: m.Turns, Compactions: m.Compactions,
		ContextTokens: m.ContextTokens, OutputTokens: m.OutputTokens, Queue: m.Queue, Background: m.Background,
		TurnOpen: m.TurnOpen, LastTurnWake: m.LastTurnWake, LastActivity: m.LastActivity,
	}}
}

func marshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(b, '\n')
}

func TestFixtureGolden(t *testing.T) {
	for _, fx := range fixtures(t) {
		t.Run(fx.name, func(t *testing.T) {
			f := fx.open(fx.path)
			if _, err := f.Refresh(); err != nil {
				t.Fatal(err)
			}
			got := marshal(t, snapshot(f))
			goldenPath := filepath.Join("testdata", fx.name+".golden.json")
			if *update {
				if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("%v (run with -update to create it)", err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("%s differs from %s; run with -update and review the diff", fx.name, goldenPath)
			}
		})
	}
}

// Reading a transcript as it is written (any split, mid-line included) must give exactly what reading it whole gives.
func TestFixtureIncrementalEqualsCold(t *testing.T) {
	for _, fx := range fixtures(t) {
		t.Run(fx.name, func(t *testing.T) {
			data, err := os.ReadFile(fx.path)
			if err != nil {
				t.Fatal(err)
			}
			cold := fx.open(fx.path)
			if _, err := cold.Refresh(); err != nil {
				t.Fatal(err)
			}
			want := marshal(t, snapshot(cold))
			for _, step := range []int{1 << 30, 4096, 977, 61} {
				p := filepath.Join(t.TempDir(), "t.jsonl")
				if err := os.WriteFile(p, nil, 0o600); err != nil {
					t.Fatal(err)
				}
				f := fx.open(p)
				var streamed []proto.Item
				for off := 0; off < len(data); off += step {
					end := min(off+step, len(data))
					fh, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
					if err != nil {
						t.Fatal(err)
					}
					_, _ = fh.Write(data[off:end])
					fh.Close()
					fresh, err := f.Refresh()
					if err != nil {
						t.Fatal(err)
					}
					streamed = append(streamed, fresh...)
				}
				if got := marshal(t, snapshot(f)); !bytes.Equal(got, want) {
					t.Fatalf("step %d: incremental read differs from a cold read", step)
				}
				all, _ := f.Page(0, 1<<30)
				if !bytes.Equal(marshal(t, streamed), marshal(t, all)) {
					t.Fatalf("step %d: the items Refresh returned are not the items the file holds", step)
				}
			}
		})
	}
}

// Paging back from the end, by rows or by exchanges, visits every item exactly once and never splits a line.
func TestFixturePagingCoversEverything(t *testing.T) {
	for _, fx := range fixtures(t) {
		t.Run(fx.name, func(t *testing.T) {
			f := fx.open(fx.path)
			if _, err := f.Refresh(); err != nil {
				t.Fatal(err)
			}
			all, _ := f.Page(0, 1<<30)
			walk := func(name string, page func(before int) ([]proto.Item, bool)) {
				var got []proto.Item
				before := 0
				for i := 0; ; i++ {
					items, more := page(before)
					if len(items) > 0 && before > 0 && items[len(items)-1].Seq >= before {
						t.Fatalf("%s: page before %d reaches seq %d", name, before, items[len(items)-1].Seq)
					}
					got = append(append([]proto.Item(nil), items...), got...)
					if !more || len(items) == 0 {
						break
					}
					before = items[0].Seq
					if i > 10000 {
						t.Fatalf("%s: does not end", name)
					}
				}
				if !bytes.Equal(marshal(t, got), marshal(t, all)) {
					t.Fatalf("%s: pages hold %d items, the file %d", name, len(got), len(all))
				}
			}
			walk("Page", func(before int) ([]proto.Item, bool) { return f.Page(before, 3) })
			walk("PageTurns", func(before int) ([]proto.Item, bool) { return f.PageTurns(before, 1, 2000) })
			// Forward, the way catch-up and background sync go: resume after the last seq received.
			for _, limit := range []int{1, 3} {
				var got []proto.Item
				after := 0
				for {
					items, more := f.After(after, limit)
					got = append(got, items...)
					if len(items) > 0 {
						after = items[len(items)-1].Seq
					}
					if !more {
						break
					}
				}
				if !bytes.Equal(marshal(t, got), marshal(t, all)) {
					t.Fatalf("After by %d: %d items, the file %d", limit, len(got), len(all))
				}
			}
		})
	}
}

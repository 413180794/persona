package node

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

func usageFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "usagewindows", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestLastCodexSampleTakesTheNewestOfCodexsOwnLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	if err := os.WriteFile(path, usageFixture(t, "codex-rollout.jsonl"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, ok := lastCodexSample(path)
	if !ok {
		t.Fatal("no sample")
	}
	// the 08:20 event is another limit ("premium") and the 08:30 one is the newest of Codex's own
	if want := time.Date(2026, 11, 4, 8, 30, 0, 0, time.UTC); !s.at.Equal(want) {
		t.Errorf("sampled at %v, want %v", s.at, want)
	}
	if s.plan != "plus" || len(s.windows) != 2 {
		t.Fatalf("plan %q windows %+v", s.plan, s.windows)
	}
	w := s.windows[0]
	if w.Label != "5h" || w.WindowMinutes != 300 || w.UsedPercent != 42.5 || w.ResetsAt == nil || w.ResetsAt.Unix() != 1793780000 {
		t.Errorf("5h window = %+v", w)
	}
	if w := s.windows[1]; w.Label != "7d" || w.UsedPercent != 61 {
		t.Errorf("7d window = %+v", w)
	}
}

func TestLastCodexSampleWeeklyOnlyAccount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	if err := os.WriteFile(path, usageFixture(t, "codex-weekly-only.jsonl"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, ok := lastCodexSample(path)
	if !ok || s.plan != "pro" || len(s.windows) != 1 || s.windows[0].Label != "7d" || s.windows[0].UsedPercent != 16 {
		t.Fatalf("got %+v %v", s, ok)
	}
}

func TestParseCodexRateLimitsIgnoresWhatIsNotAWindow(t *testing.T) {
	for name, line := range map[string]string{
		"no windows":    string(usageFixture(t, "codex-no-windows.jsonl")),
		"not token":     `{"timestamp":"2026-11-04T09:00:00.000Z","type":"event_msg","payload":{"type":"agent_message","rate_limits":{"limit_id":"codex","primary":{"used_percent":1,"window_minutes":300}}}}`,
		"no length":     `{"timestamp":"2026-11-04T09:00:00.000Z","type":"event_msg","payload":{"type":"token_count","rate_limits":{"limit_id":"codex","primary":{"used_percent":1}}}}`,
		"bad timestamp": `{"timestamp":"yesterday","type":"event_msg","payload":{"type":"token_count","rate_limits":{"limit_id":"codex","primary":{"used_percent":1,"window_minutes":300}}}}`,
		"not json":      `{"rate_limits": nope`,
	} {
		if s, ok := parseCodexRateLimits([]byte(line)); ok {
			t.Errorf("%s: parsed %+v", name, s)
		}
	}
	// a limit without an id is Codex's own (older Codex versions)
	line := `{"timestamp":"2026-11-04T09:00:00.000Z","type":"event_msg","payload":{"type":"token_count","rate_limits":{"primary":{"used_percent":-3,"window_minutes":300,"resets_at":0}}}}`
	s, ok := parseCodexRateLimits([]byte(line))
	if !ok || s.windows[0].UsedPercent != 0 || s.windows[0].ResetsAt != nil {
		t.Errorf("no id: %+v %v", s, ok)
	}
}

func TestParseCodexRateLimitsCapsOverusedAtAHundred(t *testing.T) {
	line := `{"timestamp":"2026-11-04T09:00:00.000Z","type":"event_msg","payload":{"type":"token_count","rate_limits":{"limit_id":"codex","primary":{"used_percent":130,"window_minutes":300}}}}`
	s, ok := parseCodexRateLimits([]byte(line))
	if !ok || s.windows[0].UsedPercent != 100 {
		t.Errorf("got %+v %v", s, ok)
	}
}

func TestLastCodexSampleReadsOnlyTheTailAndSkipsTheCutLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	old := `{"timestamp":"2026-11-03T08:00:00.000Z","type":"event_msg","payload":{"type":"token_count","rate_limits":{"limit_id":"codex","primary":{"used_percent":1,"window_minutes":300}}}}` + "\n"
	pad := `{"timestamp":"2026-11-04T08:00:00.000Z","type":"response_item","payload":{"text":"` + strings.Repeat("x", usageWindowsTail) + `"}}` + "\n"
	if err := os.WriteFile(path, []byte(old+pad), 0o600); err != nil {
		t.Fatal(err)
	}
	if s, ok := lastCodexSample(path); ok {
		t.Errorf("found %+v beyond the tail", s)
	}
	// a sample after the long line is found though the line before the tail's start is cut
	newer := strings.Replace(old, "2026-11-03", "2026-11-04", 1)
	if err := os.WriteFile(path, []byte(old+pad+newer), 0o600); err != nil {
		t.Fatal(err)
	}
	if s, ok := lastCodexSample(path); !ok || s.at.Day() != 4 {
		t.Errorf("got %+v %v", s, ok)
	}
}

func TestParseClaudeStatusLimits(t *testing.T) {
	now := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)
	mod := now.Add(-time.Minute)
	a, ok := parseClaudeStatusLimits(usageFixture(t, "claude-statusline.json"), mod, now.Add(-100*24*time.Hour))
	if !ok || a.Provider != "claude" || a.Name != "Claude" || !a.SampledAt.Equal(mod) || len(a.Windows) != 2 {
		t.Fatalf("got %+v %v", a, ok)
	}
	if w := a.Windows[0]; w.Label != "5h" || w.UsedPercent != 23.5 || w.ResetsAt == nil || w.ResetsAt.Unix() != 1793780000 {
		t.Errorf("5h = %+v", w)
	}
	if w := a.Windows[1]; w.Label != "7d" || w.UsedPercent != 41 {
		t.Errorf("7d = %+v", w)
	}

	// only rate_limits, a string time and milliseconds; the gateway's spend_limit is not a window
	a, ok = parseClaudeStatusLimits(usageFixture(t, "claude-rate-limits-only.json"), mod, now.Add(-100*24*time.Hour))
	if !ok || len(a.Windows) != 2 {
		t.Fatalf("got %+v %v", a, ok)
	}
	if got := a.Windows[0].ResetsAt; got == nil || !got.Equal(time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("RFC 3339 resets_at = %v", got)
	}
	if got := a.Windows[1].ResetsAt; got == nil || got.Unix() != 1794300000 {
		t.Errorf("millisecond resets_at = %v", got)
	}

	for name, in := range map[string]string{
		"no field":   `{"model":{}}`,
		"no percent": `{"rate_limits":{"five_hour":{"resets_at":1793780000}}}`,
		"not json":   `statusline`,
		"empty":      `{"rate_limits":{}}`,
	} {
		if a, ok := parseClaudeStatusLimits([]byte(in), mod, now); ok {
			t.Errorf("%s: parsed %+v", name, a)
		}
	}
}

func TestParseClaudeStatusLimitsCapsOverusedAtAHundred(t *testing.T) {
	now := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)
	in := `{"rate_limits":{"five_hour":{"used_percentage":130},"seven_day":{"used_percentage":100}}}`
	a, ok := parseClaudeStatusLimits([]byte(in), now.Add(-time.Minute), now)
	if !ok || len(a.Windows) != 2 || a.Windows[0].UsedPercent != 100 || a.Windows[1].UsedPercent != 100 {
		t.Errorf("got %+v %v", a, ok)
	}
}

func TestWindowsThatAlreadyResetAreDropped(t *testing.T) {
	now := time.Unix(1794000000, 0)
	past, future := now.Add(-time.Minute), now.Add(time.Hour)
	got := liveWindows([]proto.UsageWindow{
		{Label: "7d", WindowMinutes: 10080, UsedPercent: 50, ResetsAt: &future},
		{Label: "5h", WindowMinutes: 300, UsedPercent: 90, ResetsAt: &past},
		{Label: "1h", WindowMinutes: 60, UsedPercent: 5},
	}, now)
	if len(got) != 2 || got[0].Label != "1h" || got[1].Label != "7d" {
		t.Errorf("got %+v", got)
	}
}

// rolloutAt writes a rollout of a Codex home for today, with the given lines.
func rolloutAt(t *testing.T, home string, mtime time.Time, id string, lines ...string) {
	t.Helper()
	dir := filepath.Join(home, "sessions", mtime.Format("2006"), mtime.Format("01"), mtime.Format("02"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "rollout-"+mtime.Format("2006-01-02T15-04-05")+"-"+id+".jsonl")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

func tokenCount(at time.Time, used float64, mins int, resets time.Time) string {
	return fmt.Sprintf(`{"timestamp":%q,"type":"event_msg","payload":{"type":"token_count","rate_limits":{"limit_id":"codex","primary":{"used_percent":%v,"window_minutes":%d,"resets_at":%d},"secondary":null,"plan_type":"plus"}}}`,
		at.UTC().Format(time.RFC3339Nano), used, mins, resets.Unix())
}

func TestCollectUsageWindowsReadsCodexAndTheClaudeDrop(t *testing.T) {
	dir := t.TempDir()
	n := &Node{home: filepath.Join(dir, "home"), dir: filepath.Join(dir, "node")}
	now := time.Now()

	// Codex: the newest rollout has no rate_limits yet, the one before it has
	older := now.Add(-2 * time.Hour)
	rolloutAt(t, n.codexHome(), older, "22222222-2222-4222-8222-222222222222", tokenCount(older, 33, 300, now.Add(time.Hour)))
	rolloutAt(t, n.codexHome(), now.Add(-time.Minute), "33333333-3333-4333-8333-333333333333", `{"type":"session_meta","payload":{}}`)

	// Claude: a wrapper's drop, a file too old to count, and a window that has reset
	if err := os.MkdirAll(filepath.Join(n.dir, "usage"), 0o700); err != nil {
		t.Fatal(err)
	}
	drop := fmt.Sprintf(`{"rate_limits":{"five_hour":{"used_percentage":12,"resets_at":%d},"seven_day":{"used_percentage":40,"resets_at":%d}}}`,
		now.Add(-time.Hour).Unix(), now.Add(48*time.Hour).Unix())
	write := func(name, body string, age time.Duration) {
		p := filepath.Join(n.dir, "usage", name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, now.Add(-age), now.Add(-age)); err != nil {
			t.Fatal(err)
		}
	}
	write("claude.json", drop, time.Minute)
	write("claude-old.json", drop, usageWindowsMaxAge+time.Hour)
	write("other.json", drop, time.Minute) // not claude*.json

	got := n.collectUsageWindows(now)
	if len(got) != 2 {
		t.Fatalf("sources: %+v", got)
	}
	c := got["codex:"+n.codexHome()]
	if c.Name != "Codex Plus" || len(c.Windows) != 1 || c.Windows[0].UsedPercent != 33 || now.Sub(c.SampledAt) < time.Hour {
		t.Errorf("codex = %+v", c)
	}
	cl := got["claude:claude.json"]
	if len(cl.Windows) != 1 || cl.Windows[0].Label != "7d" {
		t.Errorf("claude = %+v (the reset 5h window must be gone)", cl)
	}
}

func TestUsageSnapshotKeepsTheLastSampleAndHonoursTheOffSwitch(t *testing.T) {
	dir := t.TempDir()
	n := &Node{home: filepath.Join(dir, "home"), dir: filepath.Join(dir, "node")}
	now := time.Now()
	rolloutAt(t, n.codexHome(), now.Add(-time.Hour), "44444444-4444-4444-8444-444444444444", tokenCount(now.Add(-time.Hour), 7, 10080, now.Add(24*time.Hour)))

	snap := n.usageSnapshot(true)
	if !snap.Enabled || len(snap.Accounts) != 1 || snap.Accounts[0].Windows[0].UsedPercent != 7 {
		t.Fatalf("first poll: %+v", snap)
	}

	// the rollout goes away (cleaned up): the last sample stays
	if err := os.RemoveAll(filepath.Join(n.codexHome(), "sessions")); err != nil {
		t.Fatal(err)
	}
	rolloutCaches.Lock()
	delete(rolloutCaches.byHome, n.codexHome())
	rolloutCaches.Unlock()
	if snap = n.usageSnapshot(true); len(snap.Accounts) != 1 {
		t.Fatalf("the kept sample is gone: %+v", snap)
	}

	// off: nothing is answered, and what was kept is forgotten
	n.setUsageWindows(false)
	if snap = n.usageSnapshot(true); snap.Enabled || len(snap.Accounts) != 0 {
		t.Fatalf("off: %+v", snap)
	}
	n.setUsageWindows(true)
	if snap = n.usageSnapshot(true); !snap.Enabled || len(snap.Accounts) != 0 {
		t.Fatalf("back on, nothing to read: %+v", snap)
	}
}

func TestMergeUsageForgetsSamplesOlderThanThreeDays(t *testing.T) {
	now := time.Now()
	kept := map[string]proto.UsageAccount{
		"fresh": {Name: "a", SampledAt: now.Add(-time.Hour)},
		"old":   {Name: "b", SampledAt: now.Add(-usageWindowsMaxAge - time.Minute)},
	}
	found := map[string]proto.UsageAccount{"fresh": {Name: "a2", SampledAt: now}}
	got := mergeUsage(kept, found, now)
	if len(got) != 1 || got["fresh"].Name != "a2" {
		t.Errorf("got %+v", got)
	}
}

func TestUsageSnapshotWithNothingToReadAnswersAnEmptyList(t *testing.T) {
	n := &Node{home: t.TempDir(), dir: t.TempDir()}
	if snap := n.usageSnapshot(true); !snap.Enabled || snap.Accounts == nil || len(snap.Accounts) != 0 {
		t.Errorf("an empty answer must be [] not null: %+v", snap)
	}
}

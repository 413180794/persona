package transcript

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

func ultracodeOutputLine(text, at string) []byte {
	line, _ := json.Marshal(map[string]any{
		"type": "system", "subtype": "local_command", "uuid": "output", "timestamp": at,
		"content": "<local-command-stdout>" + text + "</local-command-stdout>",
	})
	return line
}

func TestClaudeUltracodeOutput(t *testing.T) {
	const at = "2026-01-01T08:00:00Z"
	for _, tt := range []struct {
		name, output string
		known, on    bool
		notice       string
	}{
		{"on", "Ultracode on (this session only): dynamic workflows on every task. Effort stays high.", true, true, UltracodeOnNotice},
		{"off", "Ultracode off. Effort stays high.", true, false, UltracodeOffNotice},
		{"status on", "Current effort level: high (session) · Ultracode on", true, true, ""},
		{"status off", "Current effort level: high (session)", true, false, ""},
		{"auto on", "Effort level: auto (currently high) · Ultracode on", true, true, ""},
		{"auto off", "Effort level: auto (currently high)", true, false, ""},
		{"workflows disabled", "Ultracode needs dynamic workflows enabled (see /config).", false, false, ""},
		{"model unsupported", "Ultracode isn't available on claude-haiku-4-5.", false, false, ""},
		{"invalid", "Invalid argument: ultracode on", false, false, ""},
		{"override", "CLAUDE_CODE_EFFORT_LEVEL overrides this session.", false, false, ""},
		{"usage", "Usage: /effort [low|medium|high|xhigh|max|auto|ultracode [on|off]]", false, false, ""},
		{"quoted", "The output may say Ultracode on (this session only): ...", false, false, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var m Meta
			items := ParseLine(1, ultracodeOutputLine(tt.output, at), &m)
			if (m.Ultracode != nil) != tt.known || tt.known && *m.Ultracode != tt.on {
				t.Fatalf("state = %v; want known=%v on=%v", m.Ultracode, tt.known, tt.on)
			}
			if tt.known && m.UltracodeAt.Format(time.RFC3339) != at {
				t.Fatalf("observation time = %v", m.UltracodeAt)
			}
			want := tt.notice
			if want == "" {
				want = tt.output
			}
			if len(items) != 1 || items[0].Kind != proto.KindNotice || items[0].Text != want {
				t.Fatalf("items = %+v; want one notice %q", items, want)
			}
		})
	}
}

func TestClaudeUltracodeLifecycle(t *testing.T) {
	data, err := os.ReadFile("testdata/claude-ultracode.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	var m Meta
	var notices []string
	for seq, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		for _, it := range ParseLine(seq+1, []byte(line), &m) {
			if it.Kind == proto.KindNotice {
				notices = append(notices, it.Text)
			}
		}
	}
	if got, want := strings.Join(notices, "|"), strings.Join([]string{UltracodeOnNotice, UltracodeOffNotice, UltracodeOnNotice, UltracodeOffNotice}, "|"); got != want {
		t.Fatalf("notices = %q; want %q", got, want)
	}
	if m.Ultracode == nil || *m.Ultracode || m.Turns != 2 || m.LastPromptAt.Format(time.RFC3339) != "2026-01-01T08:02:00Z" {
		t.Fatalf("final state = %+v", m)
	}
}

func TestClaudeUltracodeLatestAndProcessBoundary(t *testing.T) {
	start, _ := time.Parse(time.RFC3339, "2026-01-01T09:00:00Z")
	m := Meta{UltracodeSince: start}
	ParseLine(1, ultracodeOutputLine("Ultracode on (this session only): enabled", "2026-01-01T08:00:00Z"), &m)
	if m.Ultracode != nil {
		t.Fatal("resumed process inherited old state")
	}
	ParseLine(2, ultracodeOutputLine("Ultracode off. Effort stays high.", "2026-01-01T09:00:00Z"), &m)
	ParseLine(3, ultracodeOutputLine("Ultracode on (this session only): enabled", "2026-01-01T08:30:00Z"), &m)
	if m.Ultracode == nil || *m.Ultracode || !m.UltracodeAt.Equal(start) {
		t.Fatalf("old appended record changed state: %+v", m)
	}
	ParseLine(4, ultracodeOutputLine("Ultracode on (this session only): enabled", "2026-01-01T10:00:00Z"), &m)
	ParseLine(5, ultracodeOutputLine("Ultracode off. Effort stays high.", "2026-01-01T09:30:00Z"), &m)
	if m.Ultracode == nil || !*m.Ultracode || m.UltracodeAt.Hour() != 10 {
		t.Fatalf("latest timestamp did not win: %+v", m)
	}
}

func TestClaudeUltracodeLegacyOutputAndStatusNotices(t *testing.T) {
	for _, sidechain := range []bool{false, true} {
		m := Meta{Sidechain: true}
		line := fmt.Sprintf(`{"type":"user","isSidechain":%t,"timestamp":"2026-01-01T08:00:00Z","message":{"role":"user","content":"<local-command-stdout>Ultracode on (this session only): dynamic workflows on every task. Effort stays high.</local-command-stdout>"}}`, sidechain)
		items := ParseLine(1, []byte(line), &m)
		if sidechain {
			if m.Ultracode != nil {
				t.Fatal("sidechain command output changed the session's state")
			}
			continue
		}
		if m.Ultracode == nil || !*m.Ultracode || len(items) != 1 || items[0].Text != UltracodeOnNotice {
			t.Fatalf("legacy command output: %+v, %+v", items, m)
		}
		ParseLine(2, ultracodeOutputLine("Ultracode needs dynamic workflows enabled (see /config).", "2026-01-01T08:01:00Z"), &m)
		if !*m.Ultracode || m.UltracodeAt.Minute() != 0 {
			t.Fatal("a rejected command changed the last confirmed state")
		}
		ParseLine(3, ultracodeOutputLine("Current effort level: high (session)", "2026-01-01T08:02:00Z"), &m)
		if *m.Ultracode {
			t.Fatal("status without the suffix did not confirm off")
		}
		items = ParseLine(4, []byte(`{"type":"attachment","timestamp":"2026-01-01T08:03:00Z","attachment":{"type":"ultra_effort_enter","reminderType":"full"}}`), &m)
		if len(items) != 1 || items[0].Text != UltracodeOnNotice {
			t.Fatalf("status observed an intervening off, so the next on needs a notice: %+v", items)
		}
	}
}

func TestClaudeUltracodeNoticePairsOnlyNextAttachment(t *testing.T) {
	const onOutput = "Ultracode on (this session only): dynamic workflows on every task. Effort stays high."
	const offOutput = "Ultracode off. Effort stays high."
	attachment := func(kind, reminder, at string) []byte {
		return []byte(fmt.Sprintf(`{"type":"attachment","timestamp":%q,"attachment":{"type":%q,"reminderType":%q}}`, at, kind, reminder))
	}
	for _, tt := range []struct {
		name  string
		lines [][]byte
		want  []int
	}{
		{"repeat commands and later full", [][]byte{
			ultracodeOutputLine(onOutput, "2026-01-01T08:00:00Z"),
			ultracodeOutputLine(onOutput, "2026-01-01T08:01:00Z"),
			attachment("ultra_effort_enter", "full", "2026-01-01T08:02:00Z"),
			attachment("ultra_effort_enter", "full", "2026-01-01T09:00:00Z"),
		}, []int{1, 1, 0, 1}},
		{"sparse consumes command", [][]byte{
			ultracodeOutputLine(onOutput, "2026-01-01T08:00:00Z"),
			attachment("ultra_effort_enter", "sparse", "2026-01-01T08:01:00Z"),
			attachment("ultra_effort_enter", "full", "2026-01-01T09:00:00Z"),
		}, []int{1, 0, 1}},
		{"status reversal clears command", [][]byte{
			ultracodeOutputLine(onOutput, "2026-01-01T08:00:00Z"),
			ultracodeOutputLine("Current effort level: high (session)", "2026-01-01T08:01:00Z"),
			attachment("ultra_effort_enter", "full", "2026-01-01T09:00:00Z"),
		}, []int{1, 1, 1}},
		{"older attachment leaves command", [][]byte{
			ultracodeOutputLine(onOutput, "2026-01-01T09:00:00Z"),
			attachment("ultra_effort_exit", "", "2026-01-01T08:00:00Z"),
			attachment("ultra_effort_enter", "full", "2026-01-01T10:00:00Z"),
		}, []int{1, 0, 0}},
		{"older command keeps newer pairing", [][]byte{
			ultracodeOutputLine(offOutput, "2026-01-01T09:00:00Z"),
			ultracodeOutputLine(onOutput, "2026-01-01T08:00:00Z"),
			attachment("ultra_effort_exit", "", "2026-01-01T10:00:00Z"),
		}, []int{1, 1, 0}},
		{"attachment reversal clears command", [][]byte{
			ultracodeOutputLine(onOutput, "2026-01-01T08:00:00Z"),
			attachment("ultra_effort_exit", "", "2026-01-01T09:00:00Z"),
			attachment("ultra_effort_enter", "full", "2026-01-01T10:00:00Z"),
		}, []int{1, 1, 1}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var m Meta
			for i, line := range tt.lines {
				if items := ParseLine(i+1, line, &m); len(items) != tt.want[i] {
					t.Fatalf("line %d: items = %+v; want %d notices", i+1, items, tt.want[i])
				}
			}
		})
	}
}

// The window's marker index also sees command strings copied into huge request blobs. Seeding must not allocate
// those earlier lines just to discover they have no metadata; otherwise a cold open decodes the whole history.
func TestUltracodeWindowSeedSkipsHugeRequestBlob(t *testing.T) {
	blob := `{"type":"api-request-blob","content":"local-command-stdout ultra_effort_enter ` + strings.Repeat("x", 2<<20) + `"}` + "\n"
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte(blob), 0o600); err != nil {
		t.Fatal(err)
	}
	fh, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer fh.Close()
	w := &window{starts: []int64{0, int64(len(blob))}, ultracode: []int{1}}
	f := NewFile(path)
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	m := f.seeded(fh, w, 2, int64(len(blob)), nil)
	runtime.ReadMemStats(&after)
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 512<<10 {
		t.Fatalf("window seed allocated %d bytes for a request blob outside the window", allocated)
	}
	if m.Ultracode != nil || m.ultracodeCommand != nil {
		t.Fatalf("request blob invented ultracode evidence: %+v", m)
	}
}

func TestClaudeUltracodeSparseAndUntrustedRecords(t *testing.T) {
	for _, tt := range []struct {
		name, line string
		known      bool
	}{
		{"sparse", `{"type":"attachment","timestamp":"2026-01-01T08:00:00Z","attachment":{"type":"ultra_effort_enter","reminderType":"sparse"}}`, true},
		{"unknown reminder", `{"type":"attachment","timestamp":"2026-01-01T08:00:00Z","attachment":{"type":"ultra_effort_enter","reminderType":"future"}}`, false},
		{"undated", `{"type":"attachment","attachment":{"type":"ultra_effort_enter","reminderType":"full"}}`, false},
		{"sidechain", `{"type":"attachment","timestamp":"2026-01-01T08:00:00Z","isSidechain":true,"attachment":{"type":"ultra_effort_enter","reminderType":"full"}}`, false},
		{"assistant field", `{"type":"assistant","timestamp":"2026-01-01T08:00:00Z","ultracode":true,"effort":"high"}`, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := Meta{Sidechain: true, Updated: time.Now()}
			items := ParseLine(1, []byte(tt.line), &m)
			if len(items) != 0 || (m.Ultracode != nil) != tt.known || tt.known && !*m.Ultracode {
				t.Fatalf("items = %+v; meta = %+v", items, m)
			}
		})
	}
	for _, prefix := range []string{"", `"isSidechain":true,`} {
		m := Meta{Sidechain: true, Updated: time.Now()}
		line := fmt.Sprintf(`{%s"type":"user","message":{"role":"user","content":"<local-command-stdout>Ultracode on (this session only): enabled</local-command-stdout>"}}`, prefix)
		ParseLine(1, []byte(line), &m)
		if m.Ultracode != nil {
			t.Fatalf("undated output inherited the previous line's timestamp: %+v", m)
		}
	}
}

// The version comes with when it was written: a node only trusts one written by the process it asks about (#5510).
func TestVersionAtIsTheLineThatSaidIt(t *testing.T) {
	var m Meta
	for _, line := range []string{
		`{"type":"user","timestamp":"2026-01-01T00:00:00Z","version":"2.1.283","message":{"role":"user","content":"hi"}}`,
		`{"type":"system","subtype":"turn_duration","timestamp":"2026-01-01T00:01:00Z","durationMs":1}`,
		`{"type":"user","timestamp":"2026-01-02T00:00:00Z","version":"2.1.318","message":{"role":"user","content":"again"}}`,
		`{"type":"system","subtype":"turn_duration","timestamp":"2026-01-02T00:01:00Z","durationMs":1}`,
	} {
		ParseLine(0, []byte(line), &m)
	}
	if m.Version != "2.1.318" || m.VersionAt.Format(time.RFC3339) != "2026-01-02T00:00:00Z" {
		t.Fatalf("version %q at %v", m.Version, m.VersionAt)
	}
}

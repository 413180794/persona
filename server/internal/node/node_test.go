package node

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

func TestParseLaunchersSimple(t *testing.T) {
	// `_claude_launchers` pads the name to 26 columns and the base URL to 40.
	out := "claude                     " + strings.Repeat(" ", 40) + "默认\n" +
		"cc_ds                      " + strings.Repeat(" ", 40) + "deepseek\n"
	got := parseLaunchers(out)
	want := []proto.Launcher{
		{Name: "claude", Model: "默认"},
		{Name: "cc_ds", Model: "deepseek"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestParseLaunchersWithBaseURL(t *testing.T) {
	line := "cc_kimi"
	if len(line) < 26 {
		line += strings.Repeat(" ", 26-len(line))
	}
	base := "https://example.com/anthropic/api"
	line += base + strings.Repeat(" ", 40-len(base)) + "kimi-latest"

	got := parseLaunchers(line + "\n")
	if len(got) != 1 {
		t.Fatalf("got %+v", got)
	}
	if got[0].Name != "cc_kimi" || got[0].BaseURL != base || got[0].Model != "kimi-latest" {
		t.Fatalf("unexpected launcher %+v", got[0])
	}
}

// When the base URL overflows its 40-column field the two values run together; the parser keeps them as one string
// rather than losing the line, and the name is still usable.
func TestParseLaunchersBaseURLTooLong(t *testing.T) {
	name := "cc_long" + strings.Repeat(" ", 26-len("cc_long"))
	line := name + strings.Repeat("u", 50) + " model-x"
	got := parseLaunchers(line + "\n")
	if len(got) != 1 {
		t.Fatalf("got %+v", got)
	}
	if got[0].Name != "cc_long" {
		t.Fatalf("name = %q", got[0].Name)
	}
	if !strings.HasPrefix(got[0].BaseURL, "uuuu") {
		t.Fatalf("base url = %q", got[0].BaseURL)
	}
	// Everything after column 66 is the model, including the tail of the overflowed URL.
	if got[0].Model != "uuuuuuuuuu model-x" {
		t.Fatalf("model = %q", got[0].Model)
	}
}

func TestParseLaunchersDashMeansNoURL(t *testing.T) {
	line := "claude"
	if len(line) < 26 {
		line += strings.Repeat(" ", 26-len(line))
	}
	line += "-" + strings.Repeat(" ", 39) + "sonnet"
	got := parseLaunchers(line)
	if len(got) != 1 || got[0].BaseURL != "" || got[0].Model != "sonnet" {
		t.Fatalf("got %+v", got)
	}
}

func TestParseLaunchersSkipsJunk(t *testing.T) {
	out := "\n  \n_loading…\n123bad model\nok_1 \n"
	got := parseLaunchers(out)
	for _, l := range got {
		if l.Name != "ok_1" && !nameRe.MatchString(l.Name) {
			t.Fatalf("junk line accepted: %+v", l)
		}
	}
	if len(got) != 1 || got[0].Name != "ok_1" {
		t.Fatalf("got %+v", got)
	}
}

func TestTrustKeysCursorAlreadyOnYes(t *testing.T) {
	screen := strings.Join([]string{
		"Do you trust this folder?",
		"  > Yes, I trust this folder",
		"    No, exit",
	}, "\n")
	if got := trustKeys(screen); !reflect.DeepEqual(got, []string{"enter"}) {
		t.Fatalf("got %v", got)
	}
}

func TestTrustKeysCursorOnNoMovesDown(t *testing.T) {
	screen := strings.Join([]string{
		"Do you trust this folder?",
		"  ❯ 1. Yes, proceed",
		"    2. No, exit",
	}, "\n")
	// "❯" sits on the Yes line here, so no movement is needed.
	if got := trustKeys(screen); !reflect.DeepEqual(got, []string{"enter"}) {
		t.Fatalf("got %v", got)
	}

	screen = strings.Join([]string{
		"Do you trust this folder?",
		"   1. Yes, I trust this folder",
		"  ❯ 2. No, exit",
	}, "\n")
	if got := trustKeys(screen); !reflect.DeepEqual(got, []string{"up", "enter"}) {
		t.Fatalf("got %v", got)
	}
}

func TestTrustKeysMultiStepCursor(t *testing.T) {
	screen := strings.Join([]string{
		"So, do you trust this folder?",
		"   1. Yes, I trust this folder",
		"   2. No, exit",
		"   3. Something else",
		"  ❯ 4. Another choice",
	}, "\n")
	if got := trustKeys(screen); !reflect.DeepEqual(got, []string{"up", "up", "up", "enter"}) {
		t.Fatalf("got %v", got)
	}
}

func TestTrustKeysNoDialog(t *testing.T) {
	for _, screen := range []string{
		"",
		"⏺ Hello\n> \n  ? for shortcuts",
		"Do you trust this folder?", // dialog but no Yes option rendered yet
	} {
		if got := trustKeys(screen); got != nil {
			t.Fatalf("screen %q produced %v", screen, got)
		}
	}
}

func TestInputBoxVisible(t *testing.T) {
	visible := []string{
		"⏺ done\n\n> \n  ? for shortcuts",
		"  shift+tab to cycle",
		"mode: bypass permissions",
		"esc to interrupt",
		"────\n❯ \n────\n  ⏸ manual mode on · ← for agents", // 2.1.206, default permission mode
	}
	for _, s := range visible {
		if !inputBoxVisible(s) {
			t.Fatalf("should be visible: %q", s)
		}
	}
	for _, s := range []string{"", "Compiling…", "⏺ Reading file", "Downloading"} {
		if inputBoxVisible(s) {
			t.Fatalf("should not be visible: %q", s)
		}
	}
}

func TestToolSummary(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"Bash", `{"command":"ls -la","description":"list"}`, "ls -la"},
		{"Read", `{"file_path":"/tmp/a.go"}`, "/tmp/a.go"},
		{"Edit", `{"file_path":"/tmp/b.go","old_string":"x"}`, "/tmp/b.go"},
		{"Write", `{"file_path":"/tmp/c.go","content":"..."}`, "/tmp/c.go"},
		{"NotebookEdit", `{"notebook_path":"/tmp/n.ipynb"}`, "/tmp/n.ipynb"},
		{"WebFetch", `{"url":"https://example.com","prompt":"hi"}`, "https://example.com"},
		{"WebSearch", `{"query":"golang io.Pipe"}`, "golang io.Pipe"},
		{"mcp__jarvis__mail_send", `{"description":"send mail"}`, "send mail"},
		{"SomeTool", `{"description":"","path":"/p"}`, "/p"},
		{"SomeTool", `{"other":"value"}`, `{"other":"value"}`},
		{"Bash", ``, ""},
	}
	for _, c := range cases {
		if got := toolSummary(c.name, json.RawMessage([]byte(c.input))); got != c.want {
			t.Fatalf("%s %s: got %q, want %q", c.name, c.input, got, c.want)
		}
	}
}

func TestToolSummaryTruncatesLongInput(t *testing.T) {
	long := strings.Repeat("x", 1000)
	got := toolSummary("SomeTool", json.RawMessage(`{"payload":"`+long+`"}`))
	if len([]rune(got)) > 201 {
		t.Fatalf("summary too long: %d runes", len([]rune(got)))
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("expected an ellipsis, got %q", got[len(got)-10:])
	}
}

func TestShellQuote(t *testing.T) {
	cases := map[string]string{
		"/usr/local/bin/jarvisd": "/usr/local/bin/jarvisd",
		"":                       "''",
		"with space":             "'with space'",
		"it's":                   `'it'\''s'`,
		"$HOME":                  "'$HOME'",
		"a;b":                    "'a;b'",
		"a-b_c.d/e":              "a-b_c.d/e",
	}
	for in, want := range cases {
		if got := shellQuote(in); got != want {
			t.Fatalf("shellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidID(t *testing.T) {
	for _, id := range []string{"0f5b2c1e-9a44-4b8f-9c31-2f4f1f4b8e11", "abcdef12", "ABCDEF0123"} {
		if !validID(id) {
			t.Fatalf("%q should be valid", id)
		}
	}
	for _, id := range []string{"", "short", "abcdefgh", "../../etc/passwd", "abc def", strings.Repeat("a", 65)} {
		if validID(id) {
			t.Fatalf("%q should be rejected", id)
		}
	}
}

func TestNewUUIDShape(t *testing.T) {
	id := newUUID()
	if !validID(id) {
		t.Fatalf("uuid %q is not a valid id", id)
	}
	if len(id) != 36 || id[14] != '4' {
		t.Fatalf("not a v4 uuid: %q", id)
	}
	if newUUID() == id {
		t.Fatal("uuids must not repeat")
	}
}

func TestSnippetCollapsesWhitespaceAndTruncates(t *testing.T) {
	if got := snippet("a\n\n  b\tc"); got != "a b c" {
		t.Fatalf("got %q", got)
	}
	long := strings.Repeat("汉", 250)
	got := snippet(long)
	if len([]rune(got)) != 201 || !strings.HasSuffix(got, "…") {
		t.Fatalf("unexpected snippet length %d", len([]rune(got)))
	}
}

func TestListKeyIgnoresSmallTimeChurn(t *testing.T) {
	base := proto.Session{SessionID: "s1", Status: proto.StatusWorking, Title: "t",
		UpdatedAt: time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC)}
	same := base
	same.UpdatedAt = base.UpdatedAt.Add(4 * time.Second) // under the 10s bucket
	if listKey([]proto.Session{base}) != listKey([]proto.Session{same}) {
		t.Fatal("a few seconds of churn must not count as a change")
	}
	changed := base
	changed.Status = proto.StatusDone
	if listKey([]proto.Session{base}) == listKey([]proto.Session{changed}) {
		t.Fatal("a status change must be visible")
	}
	withSpawn := base
	withSpawn.SpawnFrom = "todo_1@macbook/parent_s"
	if listKey([]proto.Session{base}) == listKey([]proto.Session{withSpawn}) {
		t.Fatal("a spawn_from change must be visible in listKey (#873)")
	}
	withReason, withLink := base, base
	withReason.AttentionReason = "本轮执行失败，再发一条消息继续"
	withLink.ExternalURL = "https://applink.example.com/thread/t1"
	if listKey([]proto.Session{base}) == listKey([]proto.Session{withReason}) ||
		listKey([]proto.Session{base}) == listKey([]proto.Session{withLink}) {
		t.Fatal("an attention reason or topic link that changes alone must be visible in listKey (#1225)")
	}
}

func TestLastLines(t *testing.T) {
	if got := lastLines("a\nb\nc\nd\n", 2); got != "c\nd" {
		t.Fatalf("got %q", got)
	}
	if got := lastLines("only", 5); got != "only" {
		t.Fatalf("got %q", got)
	}
}

func TestParseClaudeVersion(t *testing.T) {
	for out, want := range map[string]string{
		"2.1.309 (Claude Code)\n": "2.1.309",
		"[oh-my-zsh] autojump not found. Please install it first.\n2.1.303 (Claude Code)\n": "2.1.303",
		"-----\n  欢迎使用 开发机\n║ 1.2.3 is not this ║\n2.1.304 (Claude Code)":                   "2.1.304",
		"zsh: command not found: claude": "",
		"\x1b]1337;RemoteHost=me@box\x07\x1b]1337;ShellIntegrationVersion=14;shell=zsh\x072.1.283 (Claude Code)\n": "2.1.283",
		"\x1b[31mERROR\x1b[39m: gitstatus failed\n\x1b[32m2.1.300\x1b[39m (Claude Code)":                           "2.1.300",
	} {
		if got := parseClaudeVersion(out); got != want {
			t.Errorf("%q: %q, want %q", out, got, want)
		}
	}
}

// A new step shows at once: an agent that only runs tools changes no text, and its updated_at only every 10 s bucket.
func TestListKeySeesANewStep(t *testing.T) {
	at := time.Date(2026, 11, 4, 1, 20, 13, 0, time.UTC)
	base := proto.Session{SessionID: "s1", Status: proto.StatusWorking, Title: "t", UpdatedAt: at, LastText: "先看代码",
		Activity: "在读 tasks.go", ActivityAt: &at}
	next := base
	next.Activity = "在跑测试"
	if listKey([]proto.Session{base}) == listKey([]proto.Session{next}) {
		t.Fatal("a new step must be pushed")
	}
	later := at.Add(3 * time.Second)
	again := base
	again.ActivityAt = &later // the same kind of step again (another file read)
	if listKey([]proto.Session{base}) == listKey([]proto.Session{again}) {
		t.Fatal("the same step started again must be pushed (its time resets on the card)")
	}
}

// The list's activity is the latest tool call no text came after, read off the transcript's tail.
func TestQuickMetaActivityWhileOnlyToolsRun(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	lines := []string{
		`{"type":"user","timestamp":"2026-11-04T01:19:57Z","message":{"role":"user","content":"修一下登录"}}`,
		`{"type":"assistant","timestamp":"2026-11-04T01:20:00Z","message":{"id":"m1","role":"assistant","content":[{"type":"text","text":"先看代码"}]}}`,
		`{"type":"assistant","timestamp":"2026-11-04T01:20:01Z","message":{"id":"m2","role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"/a/tasks.go"}}]}}`,
		`{"type":"user","timestamp":"2026-11-04T01:20:02Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"` + strings.Repeat("x", 20<<10) + `"}]}}`,
		`{"type":"assistant","timestamp":"2026-11-04T01:20:13Z","message":{"id":"m3","role":"assistant","content":[{"type":"tool_use","id":"t2","name":"Bash","input":{"command":"go test ./..."}}]}}`,
	}
	write := func() os.FileInfo {
		t.Helper()
		if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		st, _ := os.Stat(path)
		return st
	}
	m := readQuickMeta(path, write())
	if m.lastText != "先看代码" || m.doing != "在跑测试" || !m.doingAt.Equal(time.Date(2026, 11, 4, 1, 20, 13, 0, time.UTC)) {
		t.Fatalf("doing %q at %v, last text %q", m.doing, m.doingAt, m.lastText)
	}
	doing, at := m.activityOf()
	if doing != "在跑测试" || at == nil || !at.Equal(m.doingAt) {
		t.Fatalf("activityOf %q %v", doing, at)
	}
	lines = append(lines, `{"type":"assistant","timestamp":"2026-11-04T01:24:05Z","message":{"id":"m4","role":"assistant","content":[{"type":"text","text":"测试过了"}]}}`)
	m = readQuickMeta(path, write())
	if doing, at := m.activityOf(); doing != "" || at != nil || m.lastText != "测试过了" {
		t.Fatalf("words come first again: %q %v %q", doing, at, m.lastText)
	}
}

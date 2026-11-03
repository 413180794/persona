package tgcli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// fakeNode answers every call with res (or err) and keeps what was asked.
type fakeNode struct {
	name string
	args string
	res  proto.ToolResult
	err  error
}

func (f *fakeNode) install(t *testing.T) {
	t.Helper()
	old, oldNow := call, now
	call = func(_ context.Context, name string, args map[string]any) (proto.ToolResult, error) {
		b, _ := json.Marshal(args)
		f.name, f.args = name, string(b)
		return f.res, f.err
	}
	now = func() time.Time { return time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { call, now = old, oldNow })
}

func run(args string, stdin string) (int, string, string) {
	var out, errOut bytes.Buffer
	code := Main(context.Background(), strings.Fields(args), strings.NewReader(stdin), &out, &errOut)
	return code, out.String(), errOut.String()
}

// Each command calls its tool with the arguments the connector reads; flags go anywhere, times are turned into what
// the connector takes, a message's text may come from stdin.
func TestCommandsBuildTheirCalls(t *testing.T) {
	f := &fakeNode{res: proto.TextResult(`{"chats":[]}`, false)}
	f.install(t)
	for _, c := range []struct{ line, stdin, tool, args string }{
		{"chats", "", "telegram_list_chats", `{"unread_only":false}`},
		{"chats --unread --limit 5", "", "telegram_list_chats", `{"limit":5,"unread_only":true}`},
		{"unread", "", "telegram_list_chats", `{"unread_only":true}`},
		{"messages c12 --limit=20 --before 991 --since 2h", "", "telegram_read_messages",
			`{"before":991,"chat":"c12","limit":20,"since":"2026-11-03T10:00:00Z"}`},
		{"messages --until 2026-11-01 u7", "", "telegram_read_messages", `{"chat":"u7","until":"2026-11-01"}`},
		{"messages u7 --since 1793696400", "", "telegram_read_messages", `{"chat":"u7","since":"2026-11-03T09:00:00Z"}`},
		{"search 示例 方案 --chat c12 --since 2026-11-01T09:00:00+08:00", "", "telegram_search_messages",
			`{"chat":"c12","query":"示例 方案","since":"2026-11-01T09:00:00+08:00"}`},
		{"message u7 991", "", "telegram_get_message", `{"chat":"u7","id":991}`},
		{"chat c12", "", "telegram_get_chat", `{"chat":"c12"}`},
		{"members c12 --limit 30", "", "telegram_list_members", `{"chat":"c12","limit":30}`},
		{"send u7 好的 周五见", "", "telegram_send", `{"chat":"u7","text":"好的 周五见"}`},
		{"send @example -", "第一行\n第二行\n", "telegram_send", `{"chat":"@example","text":"第一行\n第二行"}`},
		{"reply c12 991 收到", "", "telegram_send", `{"chat":"c12","reply_to":991,"text":"收到"}`},
		{"forward c12 991,992 --to u7", "", "telegram_forward", `{"chat":"c12","ids":[991,992],"to":"u7"}`},
		{"react c12 991 👍", "", "telegram_react", `{"chat":"c12","emoji":"👍","id":991}`},
		{"read u7 --up-to 991", "", "telegram_mark_read", `{"chat":"u7","up_to":991}`},
		{"read u7", "", "telegram_mark_read", `{"chat":"u7"}`},
	} {
		code, _, errOut := run(c.line, c.stdin)
		if code != exitOK || f.name != c.tool || f.args != c.args {
			t.Errorf("tg %s: exit %d %s, called %s %s; want %s %s", c.line, code, errOut, f.name, f.args, c.tool, c.args)
		}
	}
}

// A wrong command line is said on stderr with exit 2, and nothing is called.
func TestUsage(t *testing.T) {
	f := &fakeNode{}
	f.install(t)
	for _, line := range []string{"", "nope", "messages", "message u7 x", "chats --limit 0", "chats --limitt 5", "send u7",
		"forward c12 991", "forward c12 a,b --to u7", "messages u7 --since yesterday", "chats --unread=yes", "chats --format xml",
		"reply u7 0 hi", "chat c1 c2"} {
		f.name = ""
		code, _, errOut := run(line, "")
		if code != exitUsage || f.name != "" {
			t.Errorf("tg %q: exit %d, called %q", line, code, f.name)
		}
		if line != "" && !strings.Contains(errOut, `"kind":"usage"`) {
			t.Errorf("tg %q: stderr %q", line, errOut)
		}
	}
	if code, out, _ := run("--help", ""); code != exitOK || !strings.Contains(out, "tg send <chat>") {
		t.Fatalf("help: %d %q", code, out)
	}
	if code, _, _ := run("send u7 help", ""); code != exitOK {
		t.Fatal("a message that says help was taken for --help")
	}
}

// A failure's kind is its exit code, said as JSON on stderr; a rate limit says how long to wait.
func TestFailures(t *testing.T) {
	f := &fakeNode{}
	f.install(t)
	for _, c := range []struct {
		res   proto.ToolResult
		err   error
		code  int
		retry string
	}{
		{res: proto.TextResult("Telegram 限流中，30 秒后再试", true), code: exitRateLimited, retry: `"retry_after":30`},
		{res: proto.TextResult("Telegram is not available: Telegram 限流：14:05:01 起要求等 300 秒，还要 248 秒（14:10:02 恢复）。", true),
			code: exitRateLimited, retry: `"retry_after":248`},
		{res: proto.TextResult("Telegram is not available: not_signed_in", true), code: exitUnavailable},
		{res: proto.TextResult("tg 只能在助手的对话里用：这个进程不是 Persona 为助手起的", true), code: exitUnavailable},
		{res: proto.TextResult("The user declined telegram_send.", true), code: exitDeclined},
		{res: proto.TextResult("The hub's rules (connector_gate) do not allow telegram_send.", true), code: exitDeclined},
		{res: proto.TextResult("不认识会话 \"u9\"：先用 tg chats 或 tg search 找到它", true), code: exitFailed},
		{err: errors.New("dial unix: no such file"), code: exitUnavailable},
	} {
		f.res, f.err = c.res, c.err
		code, out, errOut := run("send u7 示例", "")
		if code != c.code || out != "" || !strings.Contains(errOut, `"kind":"`+kinds[c.code]+`"`) || !strings.Contains(errOut, c.retry) {
			t.Errorf("%+v %v: exit %d, stdout %q, stderr %q", c.res, c.err, code, out, errOut)
		}
		var line map[string]any
		if err := json.Unmarshal([]byte(errOut), &line); err != nil || line["error"] == "" {
			t.Errorf("stderr is not one JSON line: %q", errOut)
		}
	}
	f.res, f.err = proto.TextResult("The user declined telegram_send.", true), nil
	if code, _, errOut := run("send u7 示例 --format text", ""); code != exitDeclined || errOut != "tg: The user declined telegram_send.\n" {
		t.Fatalf("text: %d %q", code, errOut)
	}
}

// JSON goes out as the hub gave it, one line; --format text reads it for people.
func TestOutput(t *testing.T) {
	answer := `{"chats":[{"id":"c12","name":"示例群","kind":"group","unread":3,"mentions":1,` +
		`"last":{"id":991,"at":"2026-11-03T09:40:00+08:00","from":"示例用户","text":"周末的\n示例活动"}},{"id":"u7","name":"示例用户","kind":"dm"}],"more":true}`
	f := &fakeNode{res: proto.TextResult(answer, false)}
	f.install(t)
	if code, out, _ := run("chats", ""); code != exitOK || out != answer+"\n" {
		t.Fatalf("json: %d %q", code, out)
	}
	code, out, _ := run("chats --format text", "")
	want := "c12  示例群  群  未读 3（@你 1）\n    2026-11-03 09:40 [991] 示例用户：周末的 示例活动\nu7  示例用户  私聊\n（还有更多：--limit 加大）\n"
	if code != exitOK || out != want {
		t.Fatalf("text:\n%s\nwant\n%s", out, want)
	}
	f.res = proto.TextResult(`{"chat":{"id":"u7","name":"示例用户","kind":"dm"},"messages":[{"id":990,"at":"2026-11-03T09:00:00+08:00","mine":true,"text":"在吗"},`+
		`{"id":991,"at":"2026-11-03T09:40:00+08:00","from":"示例用户","text":"在","reply_to":990}],"next_before":990}`, false)
	if _, out, _ = run("messages u7 --format=text", ""); out != "示例用户（u7）\n2026-11-03 09:00 [990] 我：在吗\n2026-11-03 09:40 [991] 示例用户（回复 990）：在\n（更早的：--before 990）\n" {
		t.Fatalf("messages as text:\n%s", out)
	}
	f.res = proto.TextResult(`{"sent":{"id":992,"at":"2026-11-03T09:41:00+08:00","mine":true,"text":"好的"},"chat":"u7"}`, false)
	if _, out, _ = run("send u7 好的 --format text", ""); out != "已发送到 u7（消息 992）\n" {
		t.Fatalf("send as text: %q", out)
	}
}

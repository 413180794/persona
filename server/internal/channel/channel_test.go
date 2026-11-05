package channel

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
)

func TestValidID(t *testing.T) {
	for id, ok := range map[string]bool{"wechat": true, "example_chat": true, "slack2": true, "deadbeef": false, "1abc": false,
		"Wechat": false, "": false, "a-b": false, strings.Repeat("a", 33): false} {
		if ValidID(id) != ok {
			t.Errorf("ValidID(%q) = %v", id, !ok)
		}
	}
}

// The source mark: a client_msg_id's prefix is a channel only when it is one of those there are.
func TestFromClientMsgID(t *testing.T) {
	known := []string{"feishu", "wechat"}
	for id, want := range map[string]string{"wechat-message:12": "wechat", "feishu-om_1": "feishu", "3f2a9c1e-1111-4222-8333-444455556666": "",
		"telegram-9": "", "wechat": "", "": ""} {
		if got := FromClientMsgID(id, known); got != want {
			t.Errorf("FromClientMsgID(%q) = %q, want %q", id, got, want)
		}
	}
	if ClientMsgID("wechat", "message:1") != "wechat-message:1" {
		t.Fatal(ClientMsgID("wechat", "message:1"))
	}
}

func TestItemValidate(t *testing.T) {
	good := Item{ID: "examplechat", Name: "Example Chat", Kind: KindIM, Connector: "examplechat", Bind: BindQR,
		Capabilities: Capabilities{Format: FormatPlain, MaxText: 4000, Asks: AsksText}}
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	bad := map[string]func(*Item){
		"reserved id":  func(i *Item) { i.ID = "feishu" },
		"uuid-like id": func(i *Item) { i.ID = "cafebabe" },
		"no name":      func(i *Item) { i.Name = " " },
		"kind":         func(i *Item) { i.Kind = KindApp },
		"format":       func(i *Item) { i.Capabilities.Format = "html" },
		"asks":         func(i *Item) { i.Capabilities.Asks = "cards" },
		"max_text":     func(i *Item) { i.Capabilities.MaxText = 50 },
		"bind alone":   func(i *Item) { i.Connector = "" },
		"bind value":   func(i *Item) { i.Bind = BindNone },
	}
	for what, f := range bad {
		it := good
		f(&it)
		if it.Validate() == nil {
			t.Errorf("%s: accepted", what)
		}
	}
}

func TestTargetCheck(t *testing.T) {
	for _, tc := range []struct {
		t  Target
		ok bool
	}{{Target{Kind: TargetMain}, true}, {Target{Kind: TargetNew}, true}, {Target{Kind: TargetConversation, MachineID: "m", SessionID: "s"}, true},
		{Target{Kind: TargetConversation}, false}, {Target{Kind: TargetMain, SessionID: "s"}, false}, {Target{Kind: TargetAgent}, false}} {
		if err := tc.t.Check(); (err == nil) != tc.ok {
			t.Errorf("%+v: %v", tc.t, err)
		}
	}
}

func TestPluginFeatures(t *testing.T) {
	ids := func(fs []Feature) string {
		var out []string
		for _, f := range fs {
			out = append(out, f.ID)
		}
		return strings.Join(out, ",")
	}
	if got := ids(PluginFeatures("", Capabilities{Asks: AsksText, Push: true}, nil)); got != "asks,all_asks,pushes,sync" {
		t.Fatal(got)
	}
	if got := ids(PluginFeatures("", Capabilities{Asks: AsksNone}, nil)); got != "" {
		t.Fatal(got)
	}
}

var t0 = time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)

// Check: a working conversation marks the run seen; a quiet one waits a little; a gone one is given up on.
func TestRunCheck(t *testing.T) {
	r := &Run{Started: t0}
	if s := r.Check(Look{Now: t0.Add(5 * time.Second), Exists: true}); s.Do != Wait {
		t.Fatalf("quiet at 5 s: %+v", s)
	}
	if s := r.Check(Look{Now: t0.Add(25 * time.Second), Exists: true}); s.Do != Read {
		t.Fatalf("quiet at 25 s: %+v", s)
	}
	if s := r.Check(Look{Now: t0.Add(time.Second), Exists: true, Working: true}); s.Do != Wait || !r.Seen {
		t.Fatalf("working: %+v %v", s, r.Seen)
	}
	if s := r.Check(Look{Now: t0.Add(2 * time.Second), Exists: true}); s.Do != Read {
		t.Fatalf("seen, then idle: %+v", s)
	}
	if s := r.Check(Look{Now: t0.Add(2 * time.Second), Exists: true, Waiting: true}); s.Do != Wait {
		t.Fatalf("asking the owner: %+v", s)
	}
	if s := r.Check(Look{Now: t0.Add(4 * time.Minute), Online: true}); s.Do != Finish || !strings.Contains(s.Note, "不见了") {
		t.Fatalf("gone: %+v", s)
	}
	if s := r.Check(Look{Now: t0.Add(5 * time.Hour), Exists: true}); s.Do != Finish || !strings.Contains(s.Note, "先不等了") {
		t.Fatalf("too long: %+v", s)
	}
	held := &Run{Started: t0, Held: true}
	if s := held.Check(Look{Now: t0.Add(3 * time.Hour), Exists: true}); s.Do != Finish || s.Note != "" {
		t.Fatalf("held too long: %+v", s)
	}
}

// Decide: background work answers now and holds; a held run whose work ended quietly says nothing; no words yet waits.
func TestRunDecide(t *testing.T) {
	r := &Run{Started: t0, Seen: true}
	res := r.Decide(Reading{Answer: "做完了", Found: true, Last: 20}, Look{Now: t0.Add(time.Minute), Background: 2})
	if res == nil || !res.Held || !strings.Contains(res.Note, "后台还有 2 个任务") {
		t.Fatalf("background: %+v", res)
	}
	r.Hold(res.Last, t0.Add(time.Minute))
	if !r.Held || r.Base != 20 || r.MsgIDs != nil {
		t.Fatalf("held %+v", r)
	}
	if res := r.Decide(Reading{Found: true, Last: 21}, Look{Now: t0.Add(2 * time.Minute)}); res != nil {
		t.Fatalf("held, quiet, soon: %+v", res)
	}
	if res := r.Decide(Reading{Found: true, Last: 21}, Look{Now: t0.Add(15 * time.Minute)}); res == nil || !res.Quiet {
		t.Fatalf("held, quiet, later: %+v", res)
	}
	fresh := &Run{Started: t0}
	if res := fresh.Decide(Reading{Found: true}, Look{Now: t0.Add(30 * time.Second)}); res != nil {
		t.Fatalf("no words, not seen: %+v", res)
	}
	if res := fresh.Decide(Reading{Found: true}, Look{Now: t0.Add(3 * time.Minute)}); res == nil || res.Answer != "" || res.Quiet {
		t.Fatalf("no words at all: %+v", res)
	}
}

func TestFindAndFold(t *testing.T) {
	items := []proto.Item{{Seq: 1, Role: "user", Kind: proto.KindText, Text: "上一个"}, {Seq: 2, Role: "user", Kind: proto.KindText, Text: "第二个问题",
		ClientMsgID: "wechat-b"}, {Seq: 3, Role: "user", Kind: proto.KindText, Text: "没有 id 的消息"}}
	if i, ok := (&Run{MsgIDs: []string{"wechat-b"}}).Find(items, nil); !ok || i != 1 {
		t.Fatal(i, ok)
	}
	if i, ok := (&Run{MsgIDs: []string{"wechat-x"}, Probe: "没有 id"}).Find(items, nil); !ok || i != 2 {
		t.Fatal(i, ok)
	}
	if _, ok := (&Run{MsgIDs: []string{"wechat-x"}}).Find(items, nil); ok {
		t.Fatal("found a message that is not there")
	}
	if i, ok := (&Run{}).Find(items, nil); !ok || i != 0 {
		t.Fatal("a run naming nothing takes everything", i, ok)
	}
	a := &Run{ReplyTo: "m1", Base: 5, Started: t0, MsgIDs: []string{"x-m1"}, Probe: "p1", Ext: map[string]string{"card": "c1"}}
	b := &Run{ReplyTo: "m2", Base: 8, Started: t0.Add(time.Minute), MsgIDs: []string{"x-m2"}}
	b.Fold(a)
	if b.Base != 5 || !b.Started.Equal(t0) || len(b.MsgIDs) != 2 || b.Probe != "p1" || len(b.Folded) != 1 || b.Folded[0].Ext["card"] != "c1" {
		t.Fatalf("folded %+v", b)
	}
}

func TestAnswerIsTheLastWords(t *testing.T) {
	items := []proto.Item{
		{Role: "assistant", Kind: proto.KindText, Text: "先看看"},
		{Role: "assistant", Kind: proto.KindToolUse, ToolName: "Bash"},
		{Role: "user", Kind: proto.KindToolResult},
		{Role: "assistant", Kind: proto.KindText, Text: "结论一"},
		{Role: "assistant", Kind: proto.KindText, Text: "结论二"},
	}
	if got := Answer(items); len(got) != 2 || got[0].Text != "结论一" {
		t.Fatalf("%+v", got)
	}
	if got := Answer(items[:3]); len(got) != 1 || got[0].Text != "先看看" {
		t.Fatalf("%+v", got)
	}
}

func TestSplitKeepsCodeBlocksWhole(t *testing.T) {
	long := "开头\n```go\n" + strings.Repeat("fmt.Println(1)\n", 40) + "```\n结尾"
	parts := Split(long, 200)
	if len(parts) < 2 {
		t.Fatal(len(parts))
	}
	for _, p := range parts {
		if n := len([]rune(p)); n > 200 {
			t.Fatalf("a piece of %d", n)
		}
		if strings.Count(p, "```")%2 != 0 {
			t.Fatalf("an open fence: %q", p)
		}
	}
	if got := Split("短", 0); len(got) != 1 {
		t.Fatal(got)
	}
}

// Asked in text, answered by a reply: numbers, words, one question at a time.
func TestAskAndReadAnswer(t *testing.T) {
	ix := proto.Interaction{ID: "q", Kind: proto.IxQuestion, SessionTitle: "周报", Questions: []proto.Question{
		{Question: "发给谁？", Options: []proto.QuestionOption{{Label: "张三"}, {Label: "李四"}}},
		{Question: "哪些部分？", MultiSelect: true, Options: []proto.QuestionOption{{Label: "进展"}, {Label: "风险"}, {Label: "计划"}}}}}
	a := NewAsk("wechat", ix, t0)
	text, ok := AskText("", ix, a)
	if !ok || !strings.HasPrefix(text, "【周报】（1/2）") || !strings.Contains(text, "2. 李四") {
		t.Fatalf("%q", text)
	}
	if _, done, again := ReadAnswer("", a, "2"); done || again != "" || a.Step != 1 {
		t.Fatalf("first answer: done=%v again=%q step=%d", done, again, a.Step)
	}
	r, done, _ := ReadAnswer("", a, "1 3")
	if !done || r.Answers["发给谁？"] != "李四" || r.Answers["哪些部分？"] != "进展, 计划" {
		t.Fatalf("%+v", r)
	}
	perm := proto.Interaction{Kind: proto.IxPermission, ToolName: "Bash", ToolInput: json.RawMessage(`{"command":"make deploy"}`)}
	pa := NewAsk("wechat", perm, t0)
	if text, _ := AskText("", perm, pa); !strings.Contains(text, "make deploy") {
		t.Fatalf("%q", text)
	}
	if _, _, again := ReadAnswer("", pa, "也许"); again == "" {
		t.Fatal("a reply that is not one was taken")
	}
	if r, done, _ := ReadAnswer("", pa, "拒绝。"); !done || r.Allow == nil || *r.Allow {
		t.Fatalf("%+v", r)
	}
	plan := &Ask{Kind: proto.IxPlan}
	if r, _, _ := ReadAnswer("", plan, "先别改数据库"); r.Approve == nil || *r.Approve || r.Feedback != "先别改数据库" {
		t.Fatalf("%+v", r)
	}
	if _, ok := AskText("", proto.Interaction{Kind: proto.IxPermission, Fields: []proto.Field{{Key: "pw"}}}, &Ask{}); ok {
		t.Fatal("a card with fields is answerable in text")
	}
}

// The texts read in the user's language; a reply is understood in any of the three, whatever that language is.
func TestAskInLanguages(t *testing.T) {
	perm := proto.Interaction{Kind: proto.IxPermission, ToolName: "Bash", ToolInput: json.RawMessage(`{"command":"make deploy"}`)}
	if text, _ := AskText(i18n.En, perm, NewAsk("wechat", perm, t0)); !strings.Contains(text, "Wants your approval for “Bash”") {
		t.Fatalf("en: %q", text)
	}
	if text, _ := AskText(i18n.Ja, perm, NewAsk("wechat", perm, t0)); !strings.Contains(text, "「Bash」の承認") {
		t.Fatalf("ja: %q", text)
	}
	if text, _ := AskText("", perm, NewAsk("wechat", perm, t0)); !strings.Contains(text, "要你批准「Bash」") {
		t.Fatalf("no language: %q", text)
	}
	for text, allow := range map[string]bool{"允许": true, "Allow": true, "yes!": true, "許可": true, "はい。": true,
		"拒绝": false, "deny": false, "No": false, "拒否": false, "いいえ": false} {
		r, done, _ := ReadAnswer(i18n.En, &Ask{Kind: proto.IxPermission}, text)
		if !done || r.Allow == nil || *r.Allow != allow {
			t.Errorf("%q: done=%v %+v", text, done, r)
		}
	}
	for text, mode := range map[string]string{"1": "bypassPermissions", "自动执行": "bypassPermissions", "approve and run": "bypassPermissions",
		"自動実行": "bypassPermissions", "2": "acceptEdits", "批准": "acceptEdits", "Approve": "acceptEdits", "承認": "acceptEdits"} {
		r, _, _ := ReadAnswer(i18n.Ja, &Ask{Kind: proto.IxPlan}, text)
		if r.Approve == nil || !*r.Approve || r.Mode != mode {
			t.Errorf("%q: %+v", text, r)
		}
	}
	if _, _, again := ReadAnswer(i18n.En, &Ask{Kind: proto.IxPermission}, "maybe"); again != "Reply “allow” or “deny”." {
		t.Fatalf("%q", again)
	}
	if got := Answered(i18n.Ja, proto.Resolution{Allow: new(bool)}); got != "拒否しました。" {
		t.Fatal(got)
	}
	if got := Duration(i18n.En, 3*time.Minute+4*time.Second) + " / " + Duration(i18n.Ja, 2*time.Hour+5*time.Minute) + " / " +
		Duration("", 12*time.Second); got != "3m 4s / 2時間5分 / 12 秒" {
		t.Fatal(got)
	}
	r := &Run{Started: t0}
	if s := r.Check(Look{Now: t0.Add(5 * time.Hour), Exists: true, Lang: i18n.En}); !strings.Contains(s.Note, "Still not done after 5h 0m") {
		t.Fatal(s.Note)
	}
	if res := r.Decide(Reading{Answer: "done"}, Look{Now: t0.Add(time.Minute), Background: 1, Lang: i18n.En}); res.Note !=
		"1 background task is still running. I'll tell you when it's done." {
		t.Fatal(res.Note)
	}
	if f := PluginFeatures(i18n.Ja, Capabilities{Asks: AsksText}, nil); f[0].Title != "質問と承認" {
		t.Fatal(f[0].Title)
	}
	if f := PluginFeatures("", Capabilities{Asks: AsksText}, nil); f[0].Title != "提问和批准" {
		t.Fatal(f[0].Title)
	}
}

// An option number typed on a Chinese keyboard (full-width) or with a full stop is still that number: a plan is not
// sent back with "１" as the note, a terminal dialog is answered, a question's option becomes its label (#3413).
func TestOptionNumbersFullWidthAndPunctuated(t *testing.T) {
	for text, mode := range map[string]string{"１": "bypassPermissions", "1。": "bypassPermissions", " 1. ": "bypassPermissions",
		"２": "acceptEdits", "2！": "acceptEdits", "２。": "acceptEdits", "1、": "bypassPermissions", "１．": "bypassPermissions"} {
		r, done, _ := ReadAnswer("", &Ask{Kind: proto.IxPlan}, text)
		if !done || r.Approve == nil || !*r.Approve || r.Mode != mode || r.Feedback != "" {
			t.Errorf("plan %q: %+v", text, r)
		}
	}
	for _, text := range []string{"3", "1?", "１？", "01", "10"} {
		if r, _, _ := ReadAnswer("", &Ask{Kind: proto.IxPlan}, text); r.Approve == nil || *r.Approve || r.Feedback != text {
			t.Errorf("plan %q is not a choice: %+v", text, r)
		}
	}
	if _, done, again := ReadAnswer("", &Ask{Kind: proto.IxScreen, Options: []string{"yes", "no"}}, "３"); done || again == "" {
		t.Error("screen: ３ of 2 options was taken")
	}
	if _, done, again := ReadAnswer("", &Ask{Kind: proto.IxPermission}, "1"); done || again == "" {
		t.Error("permission: a number was taken")
	}
	for _, text := range []string{"２", "2。", "2."} {
		r, done, again := ReadAnswer("", &Ask{Kind: proto.IxScreen, Options: []string{"yes", "no"}}, text)
		if !done || again != "" || r.Option != "no" {
			t.Errorf("screen %q: done=%v again=%q %+v", text, done, again, r)
		}
	}
	q := []proto.Question{{Question: "哪些？", MultiSelect: true, Options: []proto.QuestionOption{{Label: "进展"}, {Label: "风险"}, {Label: "计划"}}}}
	r, done, _ := ReadAnswer("", &Ask{Kind: proto.IxQuestion, Questions: q}, "１，３")
	if !done || r.Answers["哪些？"] != "进展, 计划" {
		t.Errorf("question: %+v", r)
	}
	if r, _, _ := ReadAnswer("", &Ask{Kind: proto.IxQuestion, Questions: q}, "1，3 。"); r.Answers["哪些？"] != "进展, 计划" {
		t.Errorf("question with a stray full stop: %+v", r)
	}
	one := []proto.Question{{Question: "发给谁？", Options: []proto.QuestionOption{{Label: "张三"}, {Label: "李四"}}}}
	if r, _, _ := ReadAnswer("", &Ask{Kind: proto.IxQuestion, Questions: one}, "２。"); r.Answers["发给谁？"] != "李四" {
		t.Errorf("question: %+v", r)
	}
	for _, text := range []string{"３", "1 2"} {
		if r, _, _ := ReadAnswer("", &Ask{Kind: proto.IxQuestion, Questions: one}, text); r.Answers["发给谁？"] != text {
			t.Errorf("question %q is the answer itself: %+v", text, r)
		}
	}
}

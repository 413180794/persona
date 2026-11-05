package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"unicode/utf8"
)

// What a task's prompt opens with for the agent is not its todo's title (#2775).
func TestCleanTodoTitle(t *testing.T) {
	for in, want := range map[string]string{
		"ultrathink ultracode 主人 11-05 19:47 的反馈，已授权直接开工：待办标题改成短标题": "待办标题改成短标题",
		"秘书派活（主人授权过，直接做）：查 CI 为什么挂了":                                "查 CI 为什么挂了",
		"think hard 修登录崩溃":       "修登录崩溃",
		"【派活】11/05 08:30 的消息：周报": "周报",
		"看看 CI 为什么挂了":            "看看 CI 为什么挂了",
		"修复 3.2 的崩溃":             "修复 3.2 的崩溃", // a version number is not a date
		"v2.0 发布说明":              "v2.0 发布说明",
		"11-05 的反馈：周报":           "周报",
		"ultrathink":             "",
		"把收件箱的分拣改成按会话合并，再给每件事写一句总结，最后跑一遍回放":                     "把收件箱的分拣改成按会话合并，再给每件事",
		"ultrathink ultracode 主人 11-05 19:47 反馈，直接做：把待办标题改成短标题": "把待办标题改成短标题",
		"一二三四五六七八九十一二三四五👨‍👩‍👧‍👦 家庭相册":                           "一二三四五六七八九十一二三四五",
		"🚀 把首页加载速度提上来": "🚀 把首页加载速度提上来",
		// # * > and a backtick mid-line are text, not noise (#3869).
		"修复 C# 编译错误":               "修复 C# 编译错误",
		"A* 寻路":                    "A* 寻路",
		"跟进 #3471 的回归":             "跟进 #3471 的回归",
		"检查 a>b 的边界":               "检查 a>b 的边界",
		"### 修复 C# 编译错误":           "修复 C# 编译错误",
		"- 跟进 #3471":               "跟进 #3471",
		"+ 列表待办":                   "列表待办",
		"`code` 路径也要保留":            "code 路径也要保留",
		"**加粗**的待办":                "加粗的待办",
		"**只有开头两个星号":               "**只有开头两个星号", // a lone marker is not a wrap
		`<at id="u1">某人</at> 跟进回归`: "跟进回归",
	} {
		if got := cleanTodoTitle(in, maxTodoTitle); got != want {
			t.Errorf("cleanTodoTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

// The assistant's own title stays when it is a real one; one that is the prompt, too long or a prompt's opening is
// cleaned and named again by the model.
func TestTaskTitle(t *testing.T) {
	s := newTestService(t, newFakeHub())
	ctx := context.Background()
	long := "ultrathink 主人 11-05 19:47 的反馈，已授权直接开工：修登录崩溃，顺便把日志也整理一下再发版"
	for _, c := range []struct {
		given, prompt, want string
		retitle             bool
	}{
		{"修登录崩溃", long, "修登录崩溃", false},
		{"", long, "修登录崩溃，顺便把日志也整理一下再发版", true},
		{"ultrathink 修登录崩溃", long, "修登录崩溃", true},
		{long, long, "修登录崩溃，顺便把日志也整理一下再发版", true},
	} {
		got, retitle := s.taskTitle(ctx, c.given, c.prompt)
		if got != c.want || retitle != c.retitle {
			t.Errorf("taskTitle(%q) = %q %v, want %q %v", c.given, got, retitle, c.want, c.retitle)
		}
		if n := utf8.RuneCountInString(got); n > maxTodoTitle {
			t.Errorf("%q has %d characters", got, n)
		}
	}
}

// The model's title replaces the cleaned one in the background; a title the user gave first stays; without a model
// the cleaned one stays.
func TestTodoTitledByTheModel(t *testing.T) {
	e := newTodoEnv(t, nil)
	ctx := context.Background()
	prompt := "ultrathink 主人 11-05 19:47 的反馈，已授权直接开工：agent_start 建的待办标题太难看了，改成短标题"

	e.f.mu.Lock()
	e.f.todoTitleReply = "「待办改用短标题」\n"
	e.f.mu.Unlock()
	title, _ := e.s.taskTitle(ctx, "", prompt)
	todo, err := e.s.createTodo(ctx, title, prompt, "conv1", "")
	if err != nil {
		t.Fatal(err)
	}
	e.s.nameTodoLater(ctx, todo.ID, title, prompt)
	e.s.todos.titling.Wait()
	if got := e.get(todo.ID).Title; got != "待办改用短标题" {
		t.Fatalf("title = %q", got)
	}
	e.f.mu.Lock()
	asked := e.f.todoTitleAsked
	e.f.mu.Unlock()
	if len(asked) != 1 || !strings.Contains(asked[0].User, "agent_start 建的待办标题") {
		t.Fatalf("asked %+v", asked)
	}

	// Renamed by the user before the model answers: the user's stays.
	todo2, _ := e.s.createTodo(ctx, title, prompt, "conv1", "")
	e.must("PATCH", "/api/v1/todos/"+todo2.ID, `{"title":"我自己起的"}`)
	e.s.nameTodoLater(ctx, todo2.ID, title, prompt)
	e.s.todos.titling.Wait()
	if got := e.get(todo2.ID).Title; got != "我自己起的" {
		t.Fatalf("the user's title was replaced: %q", got)
	}

	// No model: the cleaned title stays.
	e.f.mu.Lock()
	e.f.todoTitleErr = errors.New("no model")
	e.f.mu.Unlock()
	todo3, _ := e.s.createTodo(ctx, title, prompt, "conv1", "")
	e.s.nameTodoLater(ctx, todo3.ID, title, prompt)
	e.s.todos.titling.Wait()
	if got := e.get(todo3.ID).Title; got != title {
		t.Fatalf("title = %q, want %q", got, title)
	}
}

// task_plan's todo: a prompt-like title from the assistant is cleaned at once and named by the model.
func TestPlanTaskTodoTitle(t *testing.T) {
	s := newTestService(t, newFakeHub())
	ctx := context.Background()
	got, retitle := s.taskTitle(ctx, "秘书派活（主人授权过）：把周报发到群里", "秘书派活（主人授权过）：把周报发到群里，先给我看一眼")
	if got != "把周报发到群里" || !retitle {
		t.Fatalf("%q %v", got, retitle)
	}
}

// POST /api/v1/todos/retitle renames the tasks' todos titled with their prompt, and only those; ?dry_run=1 lists them.
func TestTodosRetitle(t *testing.T) {
	e := newTodoEnv(t, nil)
	ctx := context.Background()
	prompt := "ultrathink ultracode 主人 11-05 19:47 的反馈，已授权直接开工：收件箱的已读同步到飞书"
	old := clip(oneLine(prompt), 40)
	fromPrompt, _ := e.s.createTodo(ctx, old, prompt, "conv1", "")
	good, _ := e.s.createTodo(ctx, "已读同步到飞书", prompt, "conv1", "")
	byHand, _ := e.s.createTodo(ctx, old, prompt, "", "")
	renamed, _ := e.s.createTodo(ctx, "这是我改过的一个特别特别特别长的待办标题哦", prompt, "conv1", "")
	// Typed by the user, only looking like a prompt: a bracket, a date, or the prompt's opening but short.
	looks, _ := e.s.createTodo(ctx, "【紧急】修登录", prompt, "conv1", "")
	dated, _ := e.s.createTodo(ctx, "11/05 反馈：修登录", prompt, "conv1", "")
	short, _ := e.s.createTodo(ctx, "ultrathink ultracode", prompt, "conv1", "")
	e.f.mu.Lock()
	e.f.todoTitleReply = "收件箱已读同步飞书"
	e.f.mu.Unlock()

	if w := e.doAs("watch", "POST", "/api/v1/todos/retitle", ""); w.Code != http.StatusForbidden {
		t.Fatalf("watch: %d", w.Code)
	}
	var res struct {
		DryRun bool          `json:"dry_run"`
		Todos  []TodoRetitle `json:"todos"`
	}
	_ = json.Unmarshal(e.must("POST", "/api/v1/todos/retitle?dry_run=1", "").Body.Bytes(), &res)
	if !res.DryRun || len(res.Todos) != 1 || res.Todos[0] != (TodoRetitle{fromPrompt.ID, old, "收件箱已读同步飞书"}) {
		t.Fatalf("dry run = %+v", res)
	}
	if got := e.get(fromPrompt.ID).Title; got != old {
		t.Fatalf("a dry run renamed it: %q", got)
	}

	_ = json.Unmarshal(e.must("POST", "/api/v1/todos/retitle", "").Body.Bytes(), &res)
	if res.DryRun || len(res.Todos) != 1 || e.get(fromPrompt.ID).Title != "收件箱已读同步飞书" {
		t.Fatalf("retitle = %+v, title %q", res, e.get(fromPrompt.ID).Title)
	}
	for _, id := range []string{good.ID, byHand.ID, renamed.ID, looks.ID, dated.ID, short.ID} {
		if before, after := map[string]string{good.ID: "已读同步到飞书", byHand.ID: old, renamed.ID: "这是我改过的一个特别特别特别长的待办标题哦",
			looks.ID: "【紧急】修登录", dated.ID: "11/05 反馈：修登录", short.ID: "ultrathink ultracode"}[id], e.get(id).Title; before != after {
			t.Errorf("%s renamed to %q", id, after)
		}
	}
	// Again: nothing left to do.
	_ = json.Unmarshal(e.must("POST", "/api/v1/todos/retitle", "").Body.Bytes(), &res)
	if len(res.Todos) != 0 {
		t.Fatalf("second run = %+v", res)
	}
}

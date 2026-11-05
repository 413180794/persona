package assistant

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// testReportDelay is the tests' reportDelay: short, set on each service (never the shared constant).
const testReportDelay = 50 * time.Millisecond

// reportService is a task service whose Feishu bot is paired, with reports on (or off), recording the posts.
func reportService(t *testing.T, on bool) (*Service, *fakeHub, func() []string) {
	t.Helper()
	s, f := newTaskService(t)
	s.reports.mu.Lock()
	s.reports.delay = testReportDelay
	s.reports.mu.Unlock()
	var mu sync.Mutex
	var posts []string
	f.answerApp(AppFeishu, FeishuStatusGet, func(json.RawMessage) (any, error) {
		return FeishuStatus{Ready: true, Enabled: true, Owner: "ou_me", PublicURL: "https://hub.example.com",
			Features: FeishuFeatures{Main: true, Cards: true, Reports: on}}, nil
	})
	f.answerApp(AppFeishu, FeishuPost, func(p json.RawMessage) (any, error) {
		var m struct {
			Card string `json:"card"`
		}
		_ = json.Unmarshal(p, &m)
		mu.Lock()
		posts = append(posts, m.Card)
		mu.Unlock()
		return map[string]any{"message_id": "om_r"}, nil
	})
	return s, f, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), posts...)
	}
}

// A task's turn that ends with a merged PR is one card in the chat: the task, its stage, what was said, links to the
// task's page, the PR and the agent; never a bare id. The todo carries the stage and the PR for the apps.
func TestTaskDoneIsReportedInFeishu(t *testing.T) {
	s, f, posts := reportService(t, true)
	todo := startedTask(t, s, f)
	agentTurn(s, f, proto.StatusWorking, "", 0)
	agentTurn(s, f, proto.StatusDone, "修好了，https://github.com/o/r/pull/901 已合并", 0)
	waitFor(t, "the report", func() bool { return len(posts()) == 1 })
	card := posts()[0]
	for _, want := range []string{"修登录崩溃", "✅开发", "🔵**合入**", "⚪部署", "已合并",
		"https://hub.example.com/todos?id=" + todo.ID, "https://github.com/o/r/pull/901", "https://hub.example.com/s/box/s1"} {
		if !strings.Contains(card, want) {
			t.Errorf("card lacks %q:\n%s", want, card)
		}
	}
	if got := onlyTodo(s); got.Stage != "merge" || got.PRURL != "https://github.com/o/r/pull/901" {
		t.Fatalf("todo stage %q pr %q", got.Stage, got.PRURL)
	}
	time.Sleep(4 * testReportDelay)
	if n := len(posts()); n != 1 {
		t.Fatalf("%d posts", n)
	}
}

// Changes within the delay are one card, the decision on top with its own answerable card; with reports off nothing
// is posted.
func TestTaskReportsMergeAndPutDecisionsFirst(t *testing.T) {
	s, f, posts := reportService(t, true)
	todo := startedTask(t, s, f)
	agentTurn(s, f, proto.StatusBlocked, "", 1)
	ix := proto.Interaction{ID: "ix1", MachineID: "box", SessionID: "s1", Kind: proto.IxQuestion,
		Questions: []proto.Question{{Question: "用哪个分支？", Options: []proto.QuestionOption{{Label: "main"}, {Label: "dev"}}}}}
	f.answerApp(AppInteractions, InteractionsGet, func(json.RawMessage) (any, error) { return map[string]any{"ok": true, "interaction": ix}, nil })
	s.taskReport(todo, reportFailed, "box", [2]string{}, "那台电脑离线", nil)
	s.taskReport(todo, reportNeeds, "box", [2]string{"box", "s1"}, noticeAsk(ix), &ix)
	waitFor(t, "the report and the question's card", func() bool { return len(posts()) == 2 })
	card := posts()[0]
	if !strings.Contains(card, "1 件事等你拍板") || !strings.Contains(card, "等你拍板") || !strings.Contains(card, "用哪个分支") {
		t.Fatalf("report:\n%s", card)
	}
	if !strings.Contains(posts()[1], "main") || !strings.Contains(posts()[1], `"a":"ix"`) {
		t.Fatalf("question card:\n%s", posts()[1])
	}

	s2, f2, posts2 := reportService(t, false)
	startedTask(t, s2, f2)
	agentTurn(s2, f2, proto.StatusWorking, "", 0)
	agentTurn(s2, f2, proto.StatusDone, "做完了", 0)
	time.Sleep(6 * testReportDelay)
	if got := posts2(); len(got) != 0 {
		t.Fatalf("reports off, posted %q", got)
	}
}

func TestStageOf(t *testing.T) {
	for text, want := range map[string]string{
		"改好了，还在跑测试":                                  "develop",
		"提了 https://github.com/o/r/pull/1":           "review",
		"https://github.com/o/r/pull/1 已合入":          "merge",
		"合入 https://github.com/o/r/pull/1 并已部署到 n37": "deploy",
		"已部署，实测通过，截图为证":                              "verify",
		// Not deployed yet, waiting for it, or asking: no 请验收.
		"https://github.com/o/r/pull/1 已合并，还没部署到 hub，等你部署":       "merge",
		"Merged https://github.com/o/r/pull/2, not deployed yet": "merge",
		"已合入。未部署（这台连不上 hub）":                                     "develop",
		"部署了吗？还没有":                                               "develop",
		"等部署完再验":                                                 "develop",
		"Ready to be deployed once CI passes":                    "develop",
		"deploy.sh 跑完，已上线":                                       "deploy",
		"已部署到 hub，还没验证":                                          "deploy",
		"已部署到 hub。未部署的部分：网页":                                     "deploy",
	} {
		if got, _ := stageOf(text); got != want {
			t.Errorf("stageOf(%q) = %q, want %q", text, got, want)
		}
	}
	if got := stageBar("zh-Hans", ""); strings.Contains(got, "✅") || strings.Contains(got, "🔵") {
		t.Errorf("no stage: %s", got)
	}
	_ = context.Background
}

// A decision whose question card the bot already posted (cards / all_cards) is not told again as waiting.
func TestTaskReportSkipsAskAlreadyInChat(t *testing.T) {
	s, f, posts := reportService(t, true)
	todo := startedTask(t, s, f)
	agentTurn(s, f, proto.StatusBlocked, "", 1)
	ix := proto.Interaction{ID: "ix2", MachineID: "box", SessionID: "s1", Kind: proto.IxQuestion,
		Questions: []proto.Question{{Question: "用哪个分支？", Options: []proto.QuestionOption{{Label: "main"}}}}}
	s.fbot.mu.Lock()
	if s.fbot.cards == nil {
		s.fbot.cards = map[string]botCard{}
	}
	s.fbot.cards[ix.ID] = botCard{}
	s.fbot.mu.Unlock()
	s.taskReport(todo, reportNeeds, "box", [2]string{"box", "s1"}, noticeAsk(ix), &ix)
	time.Sleep(6 * testReportDelay)
	if got := posts(); len(got) != 0 {
		t.Fatalf("posted %q", got)
	}
}

// Realistic replies: told as deployed or not (negated, deferred, asked). A false 已部署 sends a bogus 请验收 card.
func TestSaidDeployed(t *testing.T) {
	for text, want := range map[string]bool{
		"已完成部署，请验收": true, "部署好了": true, "已经部署，不需要你再看": true, "已部署到 hub，不需要你再操作": true,
		"部署完成，链接 …": true, "上线了，可以去看看": true, "Deployed to staging; nothing pending.": true,
		"shipped and deployed, nothing pending": true, "It is live now": true,
		"not yet deployed": false, "hasn't been deployed yet": false, "it isn't fully deployed yet": false,
		"nothing has been deployed yet": false, "it has not been deployed": false, "先别部署到生产，等我确认": false,
		"无需部署到生产": false, "等你确认后再部署到生产": false, "待验证后再部署到生产": false, "还没部署到 hub，等我打声招呼": false,
		"尚未部署到 hub，先别验收": false, "准备部署到生产，等 CI": false, "等部署完再说": false, "部署了吗？": false,
		"Not deployed yet.": false, "we haven't deployed it yet": false, "never deployed": false,
		"awaiting deploy, will ping you": false, "It will be deployed after review": false,
		"还没合入。已部署到 hub": true, "PR 已合并，未部署（连不上 hub）": false,
	} {
		if got := said(deployWords, text); got != want {
			t.Errorf("said(%q) = %v, want %v", text, got, want)
		}
	}
}

package assistant

import (
	"path/filepath"
	"strings"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
)

// The node writes a session's activity, a background command's stand-in and a queued message's sender in Chinese
// (internal/transcript): it does not know the user's language. The session lists the apps get have them in the user's
// language (docs/i18n.md); zh-Hans, and anything not recognized (an older or newer node's wording), go out as the node
// wrote them. The transcripts' own lines the hub rewrites for each app (hub/transcript_text.go).

const transcriptNS = "hub-transcript_text."

func tt(l i18n.Lang, key string, args ...i18n.Args) string {
	return i18n.T(l, transcriptNS+key, args...)
}

// fromName is the sender's name in l when it is the node's stand-in for a sender without one.
func fromName(l i18n.Lang, from string) string {
	switch from {
	case "另一个会话":
		return tt(l, "from_session")
	case "队友":
		return tt(l, "from_teammate")
	}
	return from
}

// ---- a session's activity, background tasks and queue ----

// activityPhrases are transcript.Activity's fixed lines, activityVerbs its verbs (「在读 tasks.go」, or alone), by key.
var (
	activityPhrases = map[string]string{
		"在更新计划": "act_plan", "在问你问题": "act_asking", "在等你批准计划": "act_plan_approval", "在等待": "act_waiting",
		"在等后台任务": "act_waiting_task", "在停后台任务": "act_stopping_task", "在找工具": "act_finding_tools", "在查代码": "act_lsp",
		"在等 CI": "act_ci", "在提 PR": "act_pr_create", "在合并 PR": "act_pr_merge", "在发版": "act_release", "在部署": "act_deploy",
		"在跑测试": "act_test", "在检查代码": "act_lint", "在编译": "act_build", "在装依赖": "act_install", "在推送代码": "act_push",
		"在提交代码": "act_commit", "在同步代码": "act_sync", "在看改动": "act_diff", "在用模拟器": "act_simulator",
		"在搜代码": "act_search_code", "在读文件": "act_read_files", "在跑命令": "act_command",
	}
	activityVerbs = map[string]string{
		"在读": "act_read", "在改": "act_edit", "在写": "act_write", "在搜": "act_grep", "在找文件": "act_glob", "在看目录": "act_ls",
		"在看网页": "act_fetch", "在搜网页": "act_web_search", "在派子任务": "act_subtask", "在用技能": "act_skill", "在调用": "act_tool",
		"在跑": "act_run",
	}
)

// activityLen is transcript's: an activity is one short line on a task card.
const activityLen = 40

// activityText is transcript.Activity's line in l; one it does not know stays as it is.
func activityText(l i18n.Lang, s string) string {
	if key, ok := activityPhrases[s]; ok {
		return tt(l, key)
	}
	if key, ok := activityVerbs[s]; ok {
		return tt(l, key)
	}
	verb, what, ok := strings.Cut(s, " ")
	if !ok || what == "" {
		return s
	}
	key, ok := activityVerbs[verb]
	if !ok {
		return s
	}
	out := tt(l, key+"_what", i18n.Args{"what": what})
	if r := []rune(out); len(r) > activityLen {
		out = strings.TrimSuffix(string(r[:activityLen-1]), "…") + "…"
	}
	return out
}

// queuedFrom starts a queued message another session sent (transcript.go applyQueue).
const queuedFrom = "【来自 "

// localizeSession rewrites s's node-written lines in l: its activity, a background command's stand-in description, the
// sender of a queued message. The node's slices are shared: changed ones are copied.
func localizeSession(l i18n.Lang, s *proto.Session) {
	if l == i18n.ZhHans || !i18n.Valid(l) {
		return
	}
	s.Activity = activityText(l, s.Activity)
	s.RuntimeFallback = runtimeFallbackText(l, s.RuntimeFallback)
	copied := false
	for i, b := range s.Background {
		if b.Description == "超时转到后台的命令" {
			if !copied {
				s.Background, copied = append([]proto.BackgroundTask(nil), s.Background...), true
			}
			s.Background[i].Description = tt(l, "background_moved")
		}
	}
	copied = false
	for i, q := range s.Queued {
		rest, ok := strings.CutPrefix(q, queuedFrom)
		if !ok {
			continue
		}
		from, body, ok := strings.Cut(rest, "】")
		if !ok {
			continue
		}
		if !copied {
			s.Queued, copied = append([]string(nil), s.Queued...), true
		}
		s.Queued[i] = tt(l, "queued_from", i18n.Args{"from": fromName(l, from)}) + body
	}
}

// workLogLine is what the knowledge base's project timeline gets for a finished turn: its conclusion when the Agents
// business found it done, else its last words (kbAgentDone).
func workLogLine(sess proto.Session) string {
	if w := sess.Work; w != nil && w.Phase == proto.WorkDone && w.Line != "" {
		line := "完成：" + w.Line
		for _, a := range w.Artifacts {
			if a.Kind == "pr" || a.Kind == "doc" {
				line += " " + filepath.ToSlash(a.Ref)
			}
		}
		return line
	}
	return sess.LastText
}

// runtimeFallbacks are the node's reasons for running an Agent SDK conversation on the CLI (proto.Fallback*) and their
// keys; the cause after one that takes it stays as it is.
var runtimeFallbacks = []struct {
	text, key string
	cause     bool
}{
	{proto.FallbackReadBridge, "fallback_read_bridge", true},
	{proto.FallbackNoBridge, "fallback_no_bridge", false},
	{proto.FallbackWriteBridge, "fallback_write_bridge", true},
	{proto.FallbackNoBridgeScript, "fallback_no_bridge_script", false},
	{proto.FallbackNoNode, "fallback_no_node", true},
	{proto.FallbackNoClaude, "fallback_no_claude", true},
}

// shellLauncherHead, shellLauncherTail: proto.FallbackShellLauncher around the launcher's name.
var shellLauncherHead, shellLauncherTail, _ = strings.Cut(proto.FallbackShellLauncher, "%s")

// runtimeFallbackText is the node's RuntimeFallback in l; one it does not know stays as it is.
func runtimeFallbackText(l i18n.Lang, s string) string {
	if s == "" {
		return s
	}
	for _, f := range runtimeFallbacks {
		if cause, ok := strings.CutPrefix(s, f.text); ok && (cause == "" || f.cause) {
			return tt(l, f.key, i18n.Args{"cause": cause})
		}
	}
	if rest, ok := strings.CutPrefix(s, shellLauncherHead); ok {
		if name, ok := strings.CutSuffix(rest, shellLauncherTail); ok {
			return tt(l, "fallback_shell_launcher", i18n.Args{"launcher": name})
		}
	}
	return s
}

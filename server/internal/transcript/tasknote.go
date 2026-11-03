package transcript

import (
	"html"
	"regexp"
	"strconv"
	"strings"

	"github.com/nwverse/persona/server/internal/proto"
)

// What a <task-notification> summary says, in the shapes Claude Code 2.1.3xx writes (the corpus):
//
//	Background command "<description or command>" completed (exit code 0) / failed with exit code 1 / was stopped
//	Monitor event: "<description>"            (the event itself in <event>)
//	Monitor "<description>" stream ended / stopped
//	Agent "<description>" finished / failed: … / is waiting on its own background work
var (
	taskSummaryRe = regexp.MustCompile(`(?s)^(Background command|Monitor event:|Monitor|Agent)\s+"(.*)$`)
	// Where the quoted description ends: the last quote followed by how it went (descriptions may hold quotes).
	taskOutcomeRe = regexp.MustCompile(`"\s+(completed|failed|was stopped|was killed|stream ended|stopped|finished|is waiting|timed out)`)
	exitCodeRe    = regexp.MustCompile(`exit code (-?\d+)`)
)

// noteTitleLen: a title is one line of at most this many characters (the whole command is the detail).
const noteTitleLen = 100

// taskNote reads a task notification: what it is, and the one line a notice shows for it (apps that do not know
// TaskNote show that line; it used to be the whole summary — a background heredoc filled screens with it).
func taskNote(xml string) (*proto.TaskNote, string) {
	n := &proto.TaskNote{
		Kind:       "task",
		TaskID:     firstTag(xml, "task-id"),
		ToolUseID:  firstTag(xml, "tool-use-id"),
		OutputFile: firstTag(xml, "output-file"),
		Status:     firstTag(xml, "status"),
	}
	summary := html.UnescapeString(firstTag(xml, "summary"))
	full, outcome := summary, ""
	if m := taskSummaryRe.FindStringSubmatch(summary); m != nil {
		rest := m[2]
		if loc := taskOutcomeRe.FindAllStringSubmatchIndex(rest, -1); loc != nil {
			last := loc[len(loc)-1]
			full, outcome = rest[:last[0]], strings.TrimSpace(rest[last[0]+1:])
		} else if q := strings.LastIndex(rest, `"`); q >= 0 { // "Monitor event: "…"", or words not known yet
			full, outcome = rest[:q], strings.TrimSpace(rest[q+1:])
		}
		switch m[1] {
		case "Background command":
			n.Kind = "shell"
		case "Monitor event:":
			n.Kind, n.Status = "monitor", "event"
		case "Monitor":
			n.Kind = "monitor"
		case "Agent":
			n.Kind = "agent"
		}
	}
	switch {
	case n.Kind == "monitor" && strings.HasPrefix(outcome, "stream ended"):
		n.Status = "ended"
	case strings.HasPrefix(outcome, "stopped") || strings.HasPrefix(outcome, "was stopped"):
		if n.Status == "" {
			n.Status = "stopped"
		}
	case strings.HasPrefix(outcome, "finished") && n.Status == "":
		n.Status = "completed"
	case strings.HasPrefix(outcome, "failed") && n.Status == "":
		n.Status = "failed"
	case strings.HasPrefix(outcome, "is waiting"):
		n.Status = "waiting"
	}
	if m := exitCodeRe.FindStringSubmatch(outcome); m != nil {
		if c, err := strconv.Atoi(m[1]); err == nil {
			n.ExitCode = &c
		}
	}
	n.Title = oneLine(full, noteTitleLen)
	switch {
	case firstTag(xml, "event") != "":
		n.Detail = clipTo(html.UnescapeString(firstTag(xml, "event")), pageOutputLen)
	case firstTag(xml, "result") != "":
		n.Detail = clipTo(html.UnescapeString(firstTag(xml, "result")), pageOutputLen)
	case n.Title != strings.TrimSpace(full):
		n.Detail = clipTo(full, pageOutputLen) // a long or multi-line command: the title is its first line
	}
	return n, noteText(n)
}

// noteText is the notice's one line: "后台命令完成 · <title>", "Monitor 事件 · <the event's first line>"….
func noteText(n *proto.TaskNote) string {
	failed := "失败"
	if n.ExitCode != nil {
		failed = "失败（退出码 " + strconv.Itoa(*n.ExitCode) + "）"
	}
	var head string
	switch n.Kind {
	case "shell":
		head = map[string]string{"completed": "后台命令完成", "failed": "后台命令" + failed, "killed": "后台命令已停止",
			"stopped": "后台命令已停止"}[n.Status]
		if head == "" {
			head = "后台命令"
		}
	case "monitor":
		if n.Status == "event" {
			if ev := oneLine(n.Detail, noteTitleLen); ev != "" {
				return "Monitor 事件 · " + ev
			}
			head = "Monitor 事件"
		} else {
			head = map[string]string{"ended": "Monitor 结束", "stopped": "Monitor 已停止", "killed": "Monitor 已停止",
				"failed": "Monitor " + failed, "completed": "Monitor 结束"}[n.Status]
			if head == "" {
				head = "Monitor"
			}
		}
	case "agent":
		head = map[string]string{"completed": "子 Agent 完成", "failed": "子 Agent 失败", "waiting": "子 Agent 在等它的后台任务",
			"killed": "子 Agent 已停止", "stopped": "子 Agent 已停止"}[n.Status]
		if head == "" {
			head = "子 Agent"
		}
	default:
		if n.Title == "" {
			return "后台任务有更新"
		}
		return n.Title
	}
	if n.Title == "" {
		return head
	}
	return head + " · " + n.Title
}

// oneLine is the first non-blank line of s, cut to n characters.
func oneLine(s string, n int) string {
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			if r := []rune(l); len(r) > n {
				return string(r[:n-1]) + "…"
			}
			return l
		}
	}
	return ""
}

package transcript

import (
	"encoding/json"
	"strings"
)

// teammateNote turns an agent-teams protocol note (the JSON Claude Code puts inside <teammate-message> when a teammate
// goes idle, is handed a task, asks to shut down or for plan approval) into the Markdown the chat draws: what it says,
// not its raw fields. Anything else, plain words and JSON of an unknown type, comes back as it is.
func teammateNote(body string) string {
	t := strings.TrimSpace(body)
	if !strings.HasPrefix(t, "{") {
		return body
	}
	var n struct {
		Type          string          `json:"type"`
		Summary       string          `json:"summary"`
		Result        string          `json:"result"`
		IdleReason    string          `json:"idleReason"`
		FailureReason string          `json:"failureReason"`
		TaskID        json.RawMessage `json:"taskId"` // a string today; a number would do too
		Subject       string          `json:"subject"`
		Description   string          `json:"description"`
		Reason        string          `json:"reason"`
		PlanContent   string          `json:"planContent"`
		Approved      *bool           `json:"approved"`
		Feedback      string          `json:"feedback"`
	}
	if json.Unmarshal([]byte(t), &n) != nil {
		return body
	}
	var parts []string
	add := func(s string) {
		if s = strings.TrimSpace(s); s != "" {
			parts = append(parts, s)
		}
	}
	bold := func(s string) string { return "**" + strings.Join(strings.Fields(s), " ") + "**" } // one line, or it is not bold
	labeled := func(label, s string) string {
		if s = strings.TrimSpace(s); s != "" {
			return bold(label) + "：" + s
		}
		return bold(label)
	}
	switch n.Type {
	case "idle_notification":
		if strings.TrimSpace(n.Summary) != "" {
			add(bold(n.Summary))
		}
		add(n.Result)
		switch {
		case n.IdleReason == "failed" || n.FailureReason != "":
			add(labeled("出错停下", n.FailureReason))
		case n.IdleReason == "interrupted":
			add(bold("被中断"))
		}
		if len(parts) == 0 {
			add("空闲，等待新的任务")
		}
	case "task_assignment":
		title := "分配任务"
		if id := strings.Trim(string(n.TaskID), `"`); id != "" && id != "null" {
			title += " #" + id
		}
		add(labeled(title, n.Subject))
		add(n.Description)
	case "shutdown_request":
		add(labeled("请求结束", n.Reason))
	case "shutdown_approved":
		add(bold("同意结束"))
	case "shutdown_rejected":
		add(labeled("拒绝结束", n.Reason))
	case "plan_approval_request":
		add(bold("提交了计划，等批准"))
		add(n.PlanContent)
	case "plan_approval_response":
		if n.Approved == nil {
			return body
		}
		if *n.Approved {
			add(bold("计划已批准"))
		} else {
			add(bold("计划被退回"))
		}
		add(n.Feedback)
	default:
		return body
	}
	return strings.Join(parts, "\n\n")
}

package transcript

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// The agent's goal (docs/integrations/agent-goal.md): Claude Code's /goal writes goal_status attachments and the command's own
// output lines; a Codex rollout writes thread_goal_updated events. Meta.Goal is the latest of them; a goal met (and a
// Codex goal that stops for another reason) also gets a notice in the chat.

// Claude Code's command output for /goal <condition> and /goal clear (older versions write only these).
const (
	goalSetPrefix     = "Goal set: "
	goalClearedPrefix = "Goal cleared"
)

// GoalMarkers are the bytes a line that can change the goal contains, for readers that skip the other lines.
var GoalMarkers = [][]byte{[]byte(`goal_status`), []byte(goalSetPrefix), []byte(goalClearedPrefix), []byte(`thread_goal_updated`)}

// claudeGoalStatus is a goal_status attachment: {condition, met, failed?, sentinel?, reason?, iterations?, durationMs?,
// tokens?}. The sentinel ones are written by the command: met false is /goal <condition>, met true is /goal clear.
// failed (with met false) is the check judging the condition impossible: the hook is removed and the agent stops.
type claudeGoalStatus struct {
	Condition  string `json:"condition"`
	Met        bool   `json:"met"`
	Failed     bool   `json:"failed"`
	Sentinel   bool   `json:"sentinel"`
	Reason     string `json:"reason"`
	DurationMS int64  `json:"durationMs"`
	Tokens     int    `json:"tokens"`
}

// codexGoal is thread_goal_updated's goal: the whole current state, written again when its usage changes.
type codexGoal struct {
	Objective       string `json:"objective"`
	Status          string `json:"status"`
	TokensUsed      int    `json:"tokensUsed"`
	TokenBudget     int    `json:"tokenBudget"`
	TimeUsedSeconds int    `json:"timeUsedSeconds"`
}

// claudeGoal applies a goal_status line; a goal met, or judged impossible, comes back as a notice with the check's
// reason. An impossible one is shown as blocked: it no longer goes on, and only the user can unblock it.
func (m *Meta) claudeGoal(g claudeGoalStatus, at time.Time, notice func() proto.Item) []proto.Item {
	objective := strings.TrimSpace(g.Condition)
	switch {
	case g.Sentinel && g.Met: // /goal clear
		m.Goal = nil
		return nil
	case g.Sentinel:
		// Written again after a compaction: the same goal keeps its latest check.
		if m.Goal == nil || m.Goal.Objective != objective || m.Goal.Status != proto.GoalActive {
			m.Goal = &proto.GoalInfo{Objective: objective, Status: proto.GoalActive, UpdatedAt: at}
		}
		return nil
	}
	was := m.Goal
	next := proto.GoalInfo{Objective: objective, Status: proto.GoalActive, Reason: strings.TrimSpace(g.Reason), UpdatedAt: at}
	var text string
	switch {
	case g.Met:
		next.Status, text = proto.GoalComplete, "目标已达成"
	case g.Failed:
		next.Status, text = proto.GoalBlocked, "目标达不成，已停下"
	default:
		m.Goal = &next
		return nil
	}
	next.TokensUsed, next.Seconds = g.Tokens, int(g.DurationMS/1000)
	m.Goal = &next
	if was != nil && was.Status == next.Status && was.Objective == objective {
		return nil // the same check again: one notice
	}
	it := notice()
	it.Text = text
	if next.Reason != "" {
		it.Text += "：" + next.Reason
	}
	it.Text = clip(it.Text)
	return []proto.Item{it}
}

// claudeGoalOutput applies /goal's command output ("Goal set: …" / "Goal cleared: …"), which older Claude Code
// versions write without a goal_status line. Newer ones write both; the output then changes nothing.
func (m *Meta) claudeGoalOutput(text string, at time.Time) {
	switch {
	case strings.HasPrefix(text, goalSetPrefix):
		objective := strings.TrimSpace(strings.TrimPrefix(text, goalSetPrefix))
		if m.Goal == nil || m.Goal.Objective != objective || m.Goal.Status != proto.GoalActive {
			m.Goal = &proto.GoalInfo{Objective: objective, Status: proto.GoalActive, UpdatedAt: at}
		}
	case strings.HasPrefix(text, goalClearedPrefix):
		m.Goal = nil
	}
}

// codexGoalUpdated applies a thread_goal_updated event (a null goal clears it). A goal that stops (done, paused,
// blocked, out of budget) gets a notice, once per change of status.
func (m *Meta) codexGoalUpdated(raw json.RawMessage, at time.Time, notice func() proto.Item) []proto.Item {
	var g *codexGoal
	if json.Unmarshal(raw, &g) != nil {
		return nil
	}
	if g == nil {
		m.Goal = nil
		return nil
	}
	was := m.Goal
	status := g.Status
	if status == "" {
		status = proto.GoalActive
	}
	m.Goal = &proto.GoalInfo{Objective: strings.TrimSpace(g.Objective), Status: status, TokensUsed: g.TokensUsed,
		TokenBudget: g.TokenBudget, Seconds: g.TimeUsedSeconds, UpdatedAt: at}
	text := GoalStatusNotice(status)
	if text == "" || was != nil && was.Status == status {
		return nil
	}
	it := notice()
	it.Text = text
	return []proto.Item{it}
}

// GoalStatusNotice is the chat's line for a goal reaching a status, "" for one that needs none (still going).
func GoalStatusNotice(status string) string {
	switch status {
	case proto.GoalComplete:
		return "目标已达成"
	case proto.GoalPaused:
		return "目标已暂停"
	case proto.GoalBlocked:
		return "目标受阻，暂停推进"
	case proto.GoalUsageLimited:
		return "目标暂停：用量到了上限"
	case proto.GoalBudgetLimited:
		return "目标暂停：token 预算用完了"
	}
	return ""
}

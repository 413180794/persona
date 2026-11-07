package transcript

import (
	"fmt"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

// The lines Claude Code 2.1 writes for /goal (docs/integrations/agent-goal.md), in the order it writes them.
func TestClaudeGoalLifecycle(t *testing.T) {
	const cond = "finish the plan in docs/plan.md and open a PR"
	var m Meta
	step := func(line string) []proto.Item { return ParseLine(1, []byte(line), &m) }

	if items := step(`{"type":"attachment","uuid":"a1","timestamp":"2026-11-05T08:00:00Z","attachment":{"type":"goal_status","met":false,"sentinel":true,"condition":"` + cond + `"}}`); len(items) != 0 {
		t.Fatalf("setting a goal: items %+v", items)
	}
	if m.Goal == nil || m.Goal.Objective != cond || m.Goal.Status != proto.GoalActive {
		t.Fatalf("after /goal: %+v", m.Goal)
	}
	// The command and its output: the output is a notice as before, and changes nothing.
	step(`{"type":"system","subtype":"local_command","uuid":"a2","timestamp":"2026-11-05T08:00:01Z","content":"<command-name>/goal</command-name>\n<command-args>` + cond + `</command-args>"}`)
	if items := step(`{"type":"system","subtype":"local_command","uuid":"a3","timestamp":"2026-11-05T08:00:01Z","content":"<local-command-stdout>Goal set: ` + cond + `</local-command-stdout>"}`); len(items) != 1 || items[0].Text != "Goal set: "+cond {
		t.Fatalf("command output: %+v", items)
	}

	// A check that does not hold yet: still active, with its reason; no notice.
	if items := step(`{"type":"attachment","uuid":"a4","timestamp":"2026-11-05T08:30:00Z","attachment":{"type":"goal_status","met":false,"condition":"` + cond + `","reason":"The PR is not open yet."}}`); len(items) != 0 {
		t.Fatalf("unmet check: items %+v", items)
	}
	if m.Goal.Status != proto.GoalActive || m.Goal.Reason != "The PR is not open yet." {
		t.Fatalf("after an unmet check: %+v", m.Goal)
	}
	// The sentinel again (after a compaction) keeps the check.
	step(`{"type":"attachment","uuid":"a5","timestamp":"2026-11-05T08:40:00Z","attachment":{"type":"goal_status","met":false,"sentinel":true,"condition":"` + cond + `"}}`)
	if m.Goal.Reason != "The PR is not open yet." {
		t.Fatalf("sentinel again dropped the reason: %+v", m.Goal)
	}

	// Met: complete, with the usage, and one notice.
	met := `{"type":"attachment","uuid":"a6","timestamp":"2026-11-05T09:00:00Z","attachment":{"type":"goal_status","met":true,"condition":"` + cond + `","reason":"PR #1 is open.","iterations":2,"durationMs":3600000,"tokens":12345}}`
	items := step(met)
	if len(items) != 1 || items[0].Kind != proto.KindNotice || items[0].Text != "目标已达成：PR #1 is open." {
		t.Fatalf("met: items %+v", items)
	}
	if g := m.Goal; g.Status != proto.GoalComplete || g.TokensUsed != 12345 || g.Seconds != 3600 || g.Reason != "PR #1 is open." {
		t.Fatalf("after met: %+v", g)
	}
	if again := ParseLine(2, []byte(strings.Replace(met, `"a6"`, `"a7"`, 1)), &m); len(again) != 0 {
		t.Fatalf("the same check twice: %+v", again)
	}

	// /goal clear: the sentinel with met true, then the command's output.
	step(`{"type":"attachment","uuid":"a8","timestamp":"2026-11-05T09:10:00Z","attachment":{"type":"goal_status","met":true,"sentinel":true,"condition":"` + cond + `"}}`)
	if m.Goal != nil {
		t.Fatalf("after clear: %+v", m.Goal)
	}
}

// Older Claude Code writes only the command's output lines (user lines).
func TestClaudeGoalFromOutputOnly(t *testing.T) {
	var m Meta
	ParseLine(1, []byte(`{"type":"user","uuid":"b1","timestamp":"2026-10-22T08:00:00Z","message":{"role":"user","content":"<local-command-stdout>Goal set: audit the layers</local-command-stdout>"}}`), &m)
	if m.Goal == nil || m.Goal.Objective != "audit the layers" || m.Goal.Status != proto.GoalActive {
		t.Fatalf("Goal set: %+v", m.Goal)
	}
	ParseLine(2, []byte(`{"type":"user","uuid":"b2","timestamp":"2026-10-22T09:00:00Z","message":{"role":"user","content":"<local-command-stdout>Goal cleared: audit the layers</local-command-stdout>"}}`), &m)
	if m.Goal != nil {
		t.Fatalf("Goal cleared: %+v", m.Goal)
	}
}

// A subagent's own transcript (all sidechain) has no goal of the session's.
func TestClaudeGoalSidechainIgnored(t *testing.T) {
	m := Meta{Sidechain: true}
	ParseLine(1, []byte(`{"type":"attachment","isSidechain":true,"uuid":"c1","attachment":{"type":"goal_status","met":false,"sentinel":true,"condition":"x"}}`), &m)
	if m.Goal != nil {
		t.Fatalf("sidechain goal: %+v", m.Goal)
	}
}

// Codex 0.166 writes thread_goal_updated with the whole goal; a stop gets one notice per change of status.
func TestCodexGoalUpdated(t *testing.T) {
	line := func(status string, tokens int) []byte {
		return fmt.Appendf(nil, `{"timestamp":"2026-11-04T04:15:19.062Z","type":"event_msg","payload":{"type":"thread_goal_updated","threadId":"t1","goal":{"threadId":"t1","objective":"write the test plan","status":%q,"tokensUsed":%d,"timeUsedSeconds":60,"tokenBudget":500000,"createdAt":1793765719,"updatedAt":1793765719}}}`, status, tokens)
	}
	var m Meta
	if items := ParseLine(1, line("active", 0), &m); len(items) != 0 {
		t.Fatalf("active: %+v", items)
	}
	if !m.Codex || m.Goal == nil || m.Goal.Objective != "write the test plan" || m.Goal.Status != proto.GoalActive || m.Goal.TokenBudget != 500000 || m.Goal.Seconds != 60 {
		t.Fatalf("active goal: %+v (codex %v)", m.Goal, m.Codex)
	}
	if items := ParseLine(2, line("active", 10000), &m); len(items) != 0 || m.Goal.TokensUsed != 10000 {
		t.Fatalf("usage update: %+v %+v", items, m.Goal)
	}
	items := ParseLine(3, line("budget_limited", 500000), &m)
	if len(items) != 1 || items[0].Text != "目标暂停：token 预算用完了" {
		t.Fatalf("budget limited: %+v", items)
	}
	if again := ParseLine(4, line("budget_limited", 500000), &m); len(again) != 0 {
		t.Fatalf("same status again: %+v", again)
	}
	if items := ParseLine(5, line("complete", 500000), &m); len(items) != 1 || items[0].Text != "目标已达成" || m.Goal.Status != proto.GoalComplete {
		t.Fatalf("complete: %+v %+v", items, m.Goal)
	}
	ParseLine(6, []byte(`{"timestamp":"2026-11-04T05:00:00Z","type":"event_msg","payload":{"type":"thread_goal_updated","threadId":"t1","goal":null}}`), &m)
	if m.Goal != nil {
		t.Fatalf("cleared: %+v", m.Goal)
	}
}

// Claude Code 2.1.318 writes failed: true when the check judges the condition impossible: the hook is gone and the
// agent stops, so the goal is no longer active (#3905).
func TestClaudeGoalImpossible(t *testing.T) {
	var m Meta
	ParseLine(1, []byte(`{"type":"attachment","uuid":"f1","timestamp":"2026-11-05T08:00:00Z","attachment":{"type":"goal_status","met":false,"sentinel":true,"condition":"make the flaky test pass"}}`), &m)
	failed := `{"type":"attachment","uuid":"f2","timestamp":"2026-11-05T09:00:00Z","attachment":{"type":"goal_status","met":false,"failed":true,"condition":"make the flaky test pass","reason":"The test depends on a service that is down.","iterations":3,"durationMs":120000,"tokens":5000}}`
	items := ParseLine(2, []byte(failed), &m)
	if len(items) != 1 || items[0].Kind != proto.KindNotice || items[0].Text != "目标达不成，已停下：The test depends on a service that is down." {
		t.Fatalf("impossible: items %+v", items)
	}
	if g := m.Goal; g == nil || g.Status != proto.GoalBlocked || g.Reason != "The test depends on a service that is down." || g.TokensUsed != 5000 || g.Seconds != 120 {
		t.Fatalf("after impossible: %+v", g)
	}
	if again := ParseLine(3, []byte(strings.Replace(failed, `"f2"`, `"f3"`, 1)), &m); len(again) != 0 {
		t.Fatalf("the same check twice: %+v", again)
	}
	// A new /goal after it starts over as active.
	ParseLine(4, []byte(`{"type":"attachment","uuid":"f4","timestamp":"2026-11-05T09:10:00Z","attachment":{"type":"goal_status","met":false,"sentinel":true,"condition":"make the flaky test pass"}}`), &m)
	if m.Goal == nil || m.Goal.Status != proto.GoalActive || m.Goal.Reason != "" {
		t.Fatalf("set again after impossible: %+v", m.Goal)
	}
}

// A failed check with no goal seen before (the set line was before what was read) still gives one notice.
func TestClaudeGoalImpossibleWithoutSet(t *testing.T) {
	var m Meta
	items := ParseLine(1, []byte(`{"type":"attachment","uuid":"h1","timestamp":"2026-11-05T09:00:00Z","attachment":{"type":"goal_status","met":false,"failed":true,"condition":"x"}}`), &m)
	if len(items) != 1 || items[0].Text != "目标达不成，已停下" || m.Goal == nil || m.Goal.Status != proto.GoalBlocked {
		t.Fatalf("impossible without a set line: %+v %+v", items, m.Goal)
	}
	// An unmet check (no failed) after it is the agent going on: active again, no notice.
	if items := ParseLine(2, []byte(`{"type":"attachment","uuid":"h2","timestamp":"2026-11-05T09:05:00Z","attachment":{"type":"goal_status","met":false,"condition":"x","reason":"not yet"}}`), &m); len(items) != 0 || m.Goal.Status != proto.GoalActive {
		t.Fatalf("unmet check: %+v %+v", items, m.Goal)
	}
}

package transcript

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// planLines writes synthetic Claude Code lines: a plan tool's call (an assistant line) and its result (a user line).
type planLines struct {
	n int
}

func (p *planLines) call(id, tool, input string, at time.Time) string {
	p.n++
	return fmt.Sprintf(`{"type":"assistant","uuid":"u%d","timestamp":%q,"message":{"role":"assistant","id":"m%d","content":[{"type":"tool_use","id":%q,"name":%q,"input":%s}]}}`,
		p.n, at.Format(time.RFC3339), p.n, id, tool, input)
}

func (p *planLines) result(id, text string, isError bool, at time.Time) string {
	p.n++
	return fmt.Sprintf(`{"type":"user","uuid":"u%d","timestamp":%q,"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":%q,"content":%q,"is_error":%v}]}}`,
		p.n, at.Format(time.RFC3339), id, text, isError)
}

func todos(items ...string) string {
	return `{"todos":[` + strings.Join(items, ",") + `]}`
}

func todo(content, status, activeForm string) string {
	return fmt.Sprintf(`{"content":%q,"status":%q,"activeForm":%q}`, content, status, activeForm)
}

func TestPlanFromTodoWrite(t *testing.T) {
	var m Meta
	var p planLines
	t0 := time.Date(2026, 11, 6, 8, 0, 0, 0, time.UTC)
	parse := func(line string) { ParseLine(p.n, []byte(line), &m) }

	list := todos(todo("Read the crash log", "completed", "Reading the crash log"),
		todo("Find the view", "completed", "Finding the view"),
		todo("Write a test", "completed", "Writing a test"),
		todo("Reproduce the crash", "in_progress", "Reproducing the crash in landscape"),
		todo("Fix it", "pending", "Fixing it"))
	parse(p.call("toolu_1", "TodoWrite", list, t0))
	if m.Plan != nil {
		t.Fatalf("a call without its result changed the plan: %+v", m.Plan)
	}
	parse(p.result("toolu_1", "Todos have been modified successfully.", false, t0.Add(time.Second)))
	want := proto.PlanInfo{Done: 3, Total: 5, Current: "Reproducing the crash in landscape", At: t0.Add(time.Second)}
	if m.Plan == nil || *m.Plan != want {
		t.Fatalf("plan %+v, want %+v", m.Plan, want)
	}

	// The same list again: nothing it shows changed, At stays.
	parse(p.call("toolu_2", "TodoWrite", list, t0.Add(time.Minute)))
	parse(p.result("toolu_2", "Todos have been modified successfully.", false, t0.Add(time.Minute)))
	if *m.Plan != want {
		t.Fatalf("the same list moved the plan: %+v", m.Plan)
	}

	// A failed call changes nothing.
	parse(p.call("toolu_3", "TodoWrite", todos(todo("Other", "pending", "Othering")), t0.Add(2*time.Minute)))
	parse(p.result("toolu_3", "InputValidationError", true, t0.Add(2*time.Minute)))
	if *m.Plan != want {
		t.Fatalf("a failed call changed the plan: %+v", m.Plan)
	}

	// The next list replaces the whole list; no item in progress: no current step. An item without an activeForm
	// shows its content, on one line.
	parse(p.call("toolu_4", "TodoWrite", todos(todo("Ship it", "completed", ""), `{"content":"Tell the user\nabout it","status":"in_progress"}`), t0.Add(3*time.Minute)))
	parse(p.result("toolu_4", "ok", false, t0.Add(3*time.Minute)))
	if p := m.Plan; p == nil || p.Done != 1 || p.Total != 2 || p.Current != "Tell the user" || !p.At.Equal(t0.Add(3*time.Minute)) {
		t.Fatalf("second list: %+v", p)
	}
	parse(p.call("toolu_5", "TodoWrite", todos(todo("Ship it", "completed", "Shipping"), todo("Tell the user", "completed", "Telling")), t0.Add(4*time.Minute)))
	parse(p.result("toolu_5", "ok", false, t0.Add(4*time.Minute)))
	if p := m.Plan; p == nil || p.Done != 2 || p.Total != 2 || p.Current != "" {
		t.Fatalf("all done stays as done N/N: %+v", p)
	}

	// An empty list is no plan.
	parse(p.call("toolu_6", "TodoWrite", todos(), t0.Add(5*time.Minute)))
	parse(p.result("toolu_6", "ok", false, t0.Add(5*time.Minute)))
	if m.Plan != nil {
		t.Fatalf("an empty list: %+v", m.Plan)
	}
}

func TestPlanFromTaskTools(t *testing.T) {
	var m Meta
	var p planLines
	t0 := time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)
	parse := func(line string) { ParseLine(p.n, []byte(line), &m) }
	step := func(id, tool, input, output string) {
		parse(p.call(id, tool, input, t0))
		t0 = t0.Add(time.Second)
		parse(p.result(id, output, false, t0))
	}

	step("toolu_a", "TaskCreate", `{"subject":"Reproduce the crash","description":"In landscape","activeForm":"Reproducing the crash"}`, "Task #1 created successfully: Reproduce the crash")
	step("toolu_b", "TaskCreate", `{"subject":"Fix the layout","description":"","activeForm":"Fixing the layout"}`, "Task #2 created successfully: Fix the layout")
	step("toolu_c", "TaskCreate", `{"subject":"Open a PR","description":""}`, "Task #3 created successfully: Open a PR")
	if p := m.Plan; p == nil || p.Done != 0 || p.Total != 3 || p.Current != "" {
		t.Fatalf("created: %+v", p)
	}
	step("toolu_d", "TaskUpdate", `{"taskId":"1","status":"in_progress"}`, "Updated task #1 status")
	if p := m.Plan; p == nil || p.Current != "Reproducing the crash" {
		t.Fatalf("in progress keeps the activeForm from TaskCreate: %+v", p)
	}
	step("toolu_e", "TaskUpdate", `{"taskId":"1","status":"completed"}`, "Updated task #1 status")
	step("toolu_f", "TaskUpdate", `{"taskId":"3","status":"in_progress"}`, "Updated task #3 status")
	if p := m.Plan; p == nil || p.Done != 1 || p.Total != 3 || p.Current != "Open a PR" {
		t.Fatalf("an item without an activeForm shows its subject: %+v", p)
	}
	step("toolu_x", "TaskUpdate", `{"taskId":"9","status":"completed"}`, "Task #9 not found")
	if p := m.Plan; p == nil || p.Done != 1 || p.Total != 3 {
		t.Fatalf("an unknown id changed the plan: %+v", p)
	}
	step("toolu_g", "TaskUpdate", `{"taskId":"2","status":"deleted"}`, "Updated task #2 deleted")
	if p := m.Plan; p == nil || p.Done != 1 || p.Total != 2 {
		t.Fatalf("a deleted task goes: %+v", p)
	}
	// TaskList is the whole list: tasks it does not name are gone.
	step("toolu_h", "TaskList", `{}`, "#3 [in_progress] Open a PR (team-lead) [blocked by #1]")
	if p := m.Plan; p == nil || p.Done != 0 || p.Total != 1 || p.Current != "Open a PR" {
		t.Fatalf("after TaskList: %+v", p)
	}
}

// A subagent's plan (sidechain lines in the parent's transcript) and a Codex rollout are not the agent's plan.
func TestPlanIgnoresSidechainAndCodex(t *testing.T) {
	var m Meta
	line := `{"type":"assistant","isSidechain":true,"uuid":"s1","timestamp":"2026-11-06T08:00:00Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_s","name":"TodoWrite","input":` + todos(todo("Sub step", "in_progress", "Subbing")) + `}]}}`
	res := `{"type":"user","isSidechain":true,"uuid":"s2","timestamp":"2026-11-06T08:00:01Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_s","content":"ok"}]}}`
	ParseLine(1, []byte(line), &m)
	ParseLine(2, []byte(res), &m)
	if m.Plan != nil {
		t.Fatalf("sidechain plan: %+v", m.Plan)
	}

	c := Meta{Codex: true}
	c.trackPlan(proto.Item{Kind: proto.KindToolUse, ToolUseID: "x", ToolName: "TodoWrite", ToolInput: []byte(todos(todo("a", "pending", "")))})
	c.trackPlan(proto.Item{Kind: proto.KindToolResult, ToolUseID: "x", Text: "ok"})
	if c.Plan != nil {
		t.Fatalf("codex plan: %+v", c.Plan)
	}
}

func TestPlanCurrentClipped(t *testing.T) {
	var m Meta
	var p planLines
	long := strings.Repeat("é", 300)
	ParseLine(1, []byte(p.call("toolu_1", "TodoWrite", todos(todo("x", "in_progress", long)), time.Now())), &m)
	if len(m.PlanPending()) != 1 {
		t.Fatalf("pending: %q", m.PlanPending())
	}
	ParseLine(2, []byte(p.result("toolu_1", "ok", false, time.Now())), &m)
	if r := []rune(m.Plan.Current); len(r) != planCurrentLen || r[len(r)-1] != '…' {
		t.Fatalf("current %d runes: %q", len(r), m.Plan.Current)
	}
	if len(m.PlanPending()) != 0 {
		t.Fatalf("still pending: %q", m.PlanPending())
	}
}

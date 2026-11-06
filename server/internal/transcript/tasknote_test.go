package transcript

import (
	"os"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

// Background tasks reporting back (testdata/claude-background.jsonl, redacted from real sessions): a heredoc command
// run in the background, a Monitor's events and its end. Each becomes a notice carrying what it is (proto.TaskNote) and
// one short line of text — the whole heredoc used to be the notice, its &gt; / &lt; never decoded.
func TestBackgroundTaskNotices(t *testing.T) {
	data, err := os.ReadFile("testdata/claude-background.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	var meta Meta
	var notes []proto.Item
	for i, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		for _, it := range ParseLine(i+1, []byte(line), &meta) {
			if it.Kind == proto.KindNotice {
				notes = append(notes, it)
			}
		}
	}
	if len(notes) != 4 {
		t.Fatalf("%d notices, want 4 (a command, a Monitor event, two Monitors ending): %+v", len(notes), notes)
	}
	for _, n := range notes {
		if n.Task == nil {
			t.Fatalf("notice without its task: %q", n.Text)
		}
		if strings.ContainsAny(n.Text, "\n") || len([]rune(n.Text)) > 140 {
			t.Fatalf("a notice's text is one short line, got %d runes: %q", len([]rune(n.Text)), n.Text)
		}
		for _, s := range []string{n.Text, n.Task.Title, n.Task.Detail} {
			if strings.Contains(s, "&gt;") || strings.Contains(s, "&lt;") || strings.Contains(s, "&amp;") {
				t.Fatalf("entities left in %q", s)
			}
		}
	}

	cmd := notes[0].Task
	if cmd.Kind != "shell" || cmd.Status != "completed" || cmd.ExitCode == nil || *cmd.ExitCode != 0 ||
		cmd.TaskID == "" || !strings.HasPrefix(cmd.ToolUseID, "toolu_") || !strings.HasSuffix(cmd.OutputFile, ".output") {
		t.Fatalf("command: %+v", cmd)
	}
	// The title is the command's first line; the whole command, decoded, is the detail.
	if strings.Contains(cmd.Title, "\n") || !strings.Contains(cmd.Title, "cat > ") || !strings.Contains(cmd.Title, "<<'EOF'") {
		t.Fatalf("command title: %q", cmd.Title)
	}
	if !strings.Contains(cmd.Detail, "<<'EOF'\n#!/usr/bin/env bash\n") || strings.Count(cmd.Detail, "\n") < 5 {
		t.Fatalf("command detail: %q", cmd.Detail)
	}
	if !strings.HasPrefix(notes[0].Text, "后台命令完成") {
		t.Fatalf("command text: %q", notes[0].Text)
	}

	ev := notes[1]
	if ev.Task.Kind != "monitor" || ev.Task.Status != "event" || ev.Task.Detail == "" || ev.Task.Title == "" ||
		!strings.HasPrefix(ev.Text, "Monitor 事件 · "+strings.SplitN(ev.Task.Detail, "\n", 2)[0]) {
		t.Fatalf("monitor event: %q %+v", ev.Text, ev.Task)
	}
	// A Monitor ending carries the last lines it saw.
	for _, end := range notes[2:] {
		if end.Task.Kind != "monitor" || end.Task.Status != "ended" || end.Task.ToolUseID == "" || end.Task.Detail == "" ||
			!strings.HasPrefix(end.Text, "Monitor 结束 · ") {
			t.Fatalf("monitor end: %q %+v", end.Text, end.Task)
		}
	}
	if ev.Task.TaskID != notes[2].Task.TaskID || ev.Task.Title != notes[2].Task.Title {
		t.Fatal("a Monitor's event and its end carry its task id and description")
	}
}

// Completed workflow notifications were observed as both user messages and queued-command attachments.
// The fixture preserves those envelopes and XML tags, with every id, path, timestamp and payload replaced.
func TestWorkflowTaskNotices(t *testing.T) {
	data, err := os.ReadFile("testdata/claude-workflow-notices.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	wantTaskIDs := []string{"workflow-task-1", "workflow-task-2"}
	wantToolIDs := []string{"toolu_workflow_1", "toolu_workflow_2"}
	if len(lines) != len(wantTaskIDs) {
		t.Fatalf("fixture has %d records, want %d", len(lines), len(wantTaskIDs))
	}
	var meta Meta
	for i, line := range lines {
		items := ParseLine(i+1, []byte(line), &meta)
		if len(items) != 1 || items[0].Kind != proto.KindNotice || items[0].Task == nil {
			t.Fatalf("record %d: expected one task notice, got %+v", i+1, items)
		}
		n := items[0].Task
		if n.Kind != "workflow" || n.Status != "completed" || n.Title != "Example workflow" ||
			n.TaskID != wantTaskIDs[i] || n.ToolUseID != wantToolIDs[i] ||
			n.OutputFile != "/tmp/example/tasks/"+wantTaskIDs[i]+".output" || n.Detail != "Example workflow result" ||
			items[0].Text != "后台 workflow 完成 · Example workflow" {
			t.Fatalf("record %d: %q %+v", i+1, items[0].Text, n)
		}
		if duplicate := ParseLine(i+10, []byte(line), &meta); len(duplicate) != 0 {
			t.Fatalf("record %d: repeated notification produced %+v", i+1, duplicate)
		}
	}
}

// Failed/stopped wording comes from the issue; only completed was available in the local corpus.
func TestWorkflowTaskNoteOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name, status, outcome, taskID, toolUseID, wantStatus, text string
	}{
		{"completed", "completed", "completed", "workflow-task", "toolu_workflow", "completed", "后台 workflow 完成"},
		{"failed", "failed", "failed", "workflow-task", "toolu_workflow", "failed", "后台 workflow 失败"},
		{"killed", "killed", "was stopped", "workflow-task", "", "killed", "后台 workflow 已停止"},
		{"completed without status", "", "completed", "", "toolu_workflow", "completed", "后台 workflow 完成"},
		{"failed without status", "", "failed", "workflow-task", "", "failed", "后台 workflow 失败"},
		{"stopped without status", "", "was stopped", "workflow-task", "", "stopped", "后台 workflow 已停止"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			xml := "<task-notification><task-id>" + tc.taskID + "</task-id><tool-use-id>" + tc.toolUseID +
				"</tool-use-id><status>" + tc.status + `</status><summary>Dynamic workflow "Review &quot;quoted&quot; changes &amp; tests" ` +
				tc.outcome + "</summary></task-notification>"
			n, text := taskNote(xml)
			const title = `Review "quoted" changes & tests`
			if n.Kind != "workflow" || n.Status != tc.wantStatus || n.Title != title ||
				n.TaskID != tc.taskID || n.ToolUseID != tc.toolUseID || text != tc.text+" · "+title {
				t.Fatalf("got %+v %q, want workflow %s with its original task/tool ids and %q", n, text, tc.wantStatus, tc.text+" · "+title)
			}
		})
	}
}

// The shapes the corpus holds besides the fixture's: failures, stops, subagents.
func TestTaskNoteShapes(t *testing.T) {
	for _, tc := range []struct {
		xml                 string
		kind, status, title string
		exit                int // -1: none
		text                string
	}{
		{`<task-id>a</task-id><tool-use-id>toolu_1</tool-use-id><status>failed</status><summary>Background command "npm test" failed with exit code 1</summary>`,
			"shell", "failed", "npm test", 1, "后台命令失败（退出码 1） · npm test"},
		{`<task-id>a</task-id><status>killed</status><summary>Background command "tail -f x" was stopped</summary>`,
			"shell", "killed", "tail -f x", -1, "后台命令已停止 · tail -f x"},
		{`<task-id>a</task-id><status>completed</status><summary>Agent "Review the diff" finished</summary><result>All good &amp; merged</result>`,
			"agent", "completed", "Review the diff", -1, "子 Agent 完成 · Review the diff"},
		{`<task-id>a</task-id><status>failed</status><summary>Agent "Fix "quoted" bug" failed: Agent terminated early</summary>`,
			"agent", "failed", `Fix "quoted" bug`, -1, `子 Agent 失败 · Fix "quoted" bug`},
		{`<task-id>m</task-id><summary>Monitor event: "Watch CI"</summary><event>build 42 &gt; passed
second line</event>`,
			"monitor", "event", "Watch CI", -1, "Monitor 事件 · build 42 > passed"},
		{`<task-id>m</task-id><status>stopped</status><summary>Monitor "Watch CI" stopped</summary>`,
			"monitor", "stopped", "Watch CI", -1, "Monitor 已停止 · Watch CI"},
		{`<task-id>a</task-id><status>completed</status><summary>Agent "Survey" says something new</summary>`,
			"agent", "completed", "Survey", -1, "子 Agent 完成 · Survey"},
		{`<task-id>z</task-id><summary>Something new happened</summary>`,
			"task", "", "Something new happened", -1, "Something new happened"},
	} {
		n, text := taskNote("<task-notification>" + tc.xml + "</task-notification>")
		exit := -1
		if n.ExitCode != nil {
			exit = *n.ExitCode
		}
		if n.Kind != tc.kind || n.Status != tc.status || n.Title != tc.title || exit != tc.exit || text != tc.text {
			t.Errorf("%s\n got %+v exit %d %q\nwant %s %s %q exit %d %q", tc.xml, *n, exit, text, tc.kind, tc.status, tc.title, tc.exit, tc.text)
		}
	}
}

// Claude Code 2.1.318 subagents hand their report back with a SubagentHandback call (#5886); lines shaped like what it
// writes, the words made up.
func TestSubagentHandbackIsItsLastWords(t *testing.T) {
	lines := []string{
		`{"type":"assistant","uuid":"s1","isSidechain":true,"timestamp":"2026-01-02T10:00:01Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_run","name":"Bash","input":{"command":"echo one"}}]}}`,
		`{"type":"user","uuid":"s2","isSidechain":true,"timestamp":"2026-01-02T10:00:02Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_run","content":"one"}]}}`,
		`{"type":"assistant","uuid":"s3","isSidechain":true,"timestamp":"2026-01-02T10:00:03Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_back","name":"SubagentHandback","input":{"message":"Done: it printed one."}}]}}`,
		`{"type":"user","uuid":"s4","isSidechain":true,"timestamp":"2026-01-02T10:00:04Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_back","content":[{"type":"text","text":"{\"success\":true,\"message\":\"Report delivered to your caller.\"}"}]}]},"toolUseResult":{"success":true,"message":"Report delivered to your caller."}}`,
	}
	meta := Meta{Sidechain: true}
	var got []string
	for i, l := range lines {
		for _, it := range ParseLine(i+1, []byte(l), &meta) {
			got = append(got, it.Kind+":"+it.ToolName+":"+it.Text)
		}
	}
	want := []string{"tool_use:Bash:", "tool_result::one", "text::Done: it printed one."}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("items %q, want %q: the hand-back is the subagent's words, its result not shown", got, want)
	}
	if meta.LastText != "Done: it printed one." {
		t.Fatalf("last text %q, want the report", meta.LastText)
	}
}

func TestAgentNotificationTakesTheHandedBackReport(t *testing.T) {
	const pointer = `This agent's report was delivered to you as a message from "a1b2c3" (its SubagentHandback call). Read it there; it is not repeated here.`
	notify := func(uuid, id string) string {
		return `{"type":"user","uuid":"` + uuid + `","message":{"role":"user","content":"<task-notification>\n<task-id>` + id + `</task-id>\n<tool-use-id>toolu_launch</tool-use-id>\n<status>completed</status>\n<summary>Agent \"Check the tests\" finished</summary>\n<result>` + strings.ReplaceAll(pointer, `"`, `\"`) + `\n</result>\n</task-notification>"}}`
	}
	handback := `{"type":"user","uuid":"h1","isMeta":true,"origin":{"kind":"peer","from":"a1b2c3","name":"general-purpose","handback":true,"handbackReportText":"All tests pass."},"message":{"role":"user","content":"Another Claude session sent a message:\n<agent-message from=\"a1b2c3\">…</agent-message>"}}`

	var meta Meta
	var notes []proto.Item
	for i, l := range []string{handback, notify("n1", "a1b2c3")} {
		for _, it := range ParseLine(i+1, []byte(l), &meta) {
			notes = append(notes, it)
		}
	}
	if len(notes) != 1 || notes[0].Task == nil || notes[0].Task.Kind != "agent" {
		t.Fatalf("items %+v, want the agent's notice alone (the hand-back line is hidden)", notes)
	}
	if d := notes[0].Task.Detail; d != "All tests pass." {
		t.Fatalf("detail %q, want the report it handed back", d)
	}
	// a notification whose report came before what was read: the pointer alone is not shown as the report
	var later Meta
	items := ParseLine(1, []byte(notify("n2", "a1b2c3")), &later)
	if len(items) != 1 || items[0].Task == nil || items[0].Task.Detail != "" {
		t.Fatalf("items %+v, want the notice with no detail", items)
	}
	// an origin of a shape not known yet loses nothing
	if items := ParseLine(1, []byte(`{"type":"user","uuid":"u9","origin":"elsewhere","message":{"role":"user","content":"hello"}}`), &Meta{}); len(items) != 1 || items[0].Text != "hello" {
		t.Fatalf("items %+v, want the message", items)
	}
}

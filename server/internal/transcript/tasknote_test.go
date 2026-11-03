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

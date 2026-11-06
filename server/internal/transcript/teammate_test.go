package transcript

import (
	"encoding/json"
	"testing"
)

// The shapes are the ones Claude Code's agent teams write (idle_notification and task_assignment seen in real
// transcripts; the shutdown and plan approval ones from the CLI's own code).
func TestTeammateNote(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"idle with summary and report",
			`{"type":"idle_notification","from":"worker-1","timestamp":"2026-11-04T16:19:30.319Z","idleReason":"available","summary":" web branch\nrebased ","result":"Checks pass.\n\n- **Fix:** kept both lines"}`,
			"**web branch rebased**\n\nChecks pass.\n\n- **Fix:** kept both lines"},
		{"idle with a report only",
			`{"type":"idle_notification","from":"worker-1","timestamp":"t","result":"Done; committed locally."}`,
			"Done; committed locally."},
		{"stopped on an error",
			`{"type":"idle_notification","from":"worker-1","timestamp":"t","idleReason":"failed","failureReason":"API Error: 503 try again"}`,
			"**出错停下**：API Error: 503 try again"},
		{"interrupted", `{"type":"idle_notification","from":"w","timestamp":"t","idleReason":"interrupted"}`, "**被中断**"},
		{"idle and nothing to say", `{"type":"idle_notification","from":"w","timestamp":"t","idleReason":"available"}`, "空闲，等待新的任务"},
		{"task assignment",
			`{"type":"task_assignment","taskId":"23","subject":"Templates batch 1","description":"Presenters and tests.","assignedBy":"lead","timestamp":"t"}`,
			"**分配任务 #23**：Templates batch 1\n\nPresenters and tests."},
		{"numeric task id", `{"type":"task_assignment","taskId":7,"subject":"Docs"}`, "**分配任务 #7**：Docs"},
		{"shutdown request", `{"type":"shutdown_request","requestId":"r1","from":"lead","reason":"all done"}`, "**请求结束**：all done"},
		{"shutdown approved", `{"type":"shutdown_approved","requestId":"r1","from":"w"}`, "**同意结束**"},
		{"shutdown rejected", `{"type":"shutdown_rejected","requestId":"r1","from":"w","reason":"still testing"}`, "**拒绝结束**：still testing"},
		{"plan for approval", `{"type":"plan_approval_request","from":"w","planContent":"1. Do A\n2. Do B","requestId":"r"}`, "**提交了计划，等批准**\n\n1. Do A\n2. Do B"},
		{"plan approved", `{"type":"plan_approval_response","requestId":"r","approved":true}`, "**计划已批准**"},
		{"plan sent back", `{"type":"plan_approval_response","requestId":"r","approved":false,"feedback":"Split step 2."}`, "**计划被退回**\n\nSplit step 2."},
		// Not a note: as it is.
		{"plain words", "Report is in /tmp/r.md", "Report is in /tmp/r.md"},
		{"unknown type", `{"type":"something_new","x":1}`, `{"type":"something_new","x":1}`},
		{"not JSON", `{not json} but words`, `{not json} but words`},
		{"unexpected field type", `{"type":"idle_notification","result":42}`, `{"type":"idle_notification","result":42}`},
	}
	for _, c := range cases {
		if got := teammateNote(c.in); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
}

// A teammate's JSON note in the transcript reads as its words in the "From" card.
func TestTeammateNoteInTranscript(t *testing.T) {
	note := `{"type":"idle_notification","from":"docs","timestamp":"t","summary":"done","result":"Report is in /tmp/r.md"}`
	content, _ := json.Marshal("<teammate-message teammate_id=\"docs\" color=\"blue\" summary=\"done\">\n" + note + "\n</teammate-message>")
	items, _ := parse(t, `{"type":"user","uuid":"t2","message":{"role":"user","content":`+string(content)+`}}`)
	if it := only(t, items); it.From != "docs" || it.Text != "**done**\n\nReport is in /tmp/r.md" {
		t.Fatalf("teammate note: %+v", it)
	}
}

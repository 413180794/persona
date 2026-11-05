package hub

import (
	"encoding/json"
	"testing"
)

// The same completion must not push twice: a Stop whose reply was already pushed for, with no prompt since, is the same
// turn end seen again (the status flapping around it). A new prompt, a different reply, or a Stop with no reply
// (nothing to tell it by) is a new one.
func TestStopDuplicate(t *testing.T) {
	h := newTestHub(t)
	m, _ := newCodexMachine(t, h)
	event := func(sid, name, sig string) {
		t.Helper()
		b, _ := json.Marshal(map[string]any{"session_id": sid, "event": name, "stop_sig": sig})
		m.onEvent("hook.event", b)
	}
	for _, step := range []struct {
		name string
		do   func()
		sid  string
		dup  bool // does the push that follows count as a repeat
	}{
		{"a session without hooks", func() {}, "plain", false},
		{"the first Stop", func() { event("s1", "Stop", "aaaa") }, "s1", false},
		{"the same Stop again", func() { event("s1", "Stop", "aaaa") }, "s1", true},
		{"another session is its own", func() { event("s2", "Stop", "aaaa") }, "s2", false},
		{"the same reply after a new prompt", func() { event("s1", "UserPromptSubmit", ""); event("s1", "Stop", "aaaa") }, "s1", false},
		{"and again", func() {}, "s1", true},
		{"a different reply", func() { event("s1", "Stop", "bbbb") }, "s1", false},
		{"a Stop with no reply is never held back", func() { event("s1", "Stop", "") }, "s1", false},
		{"nor again", func() {}, "s1", false},
	} {
		step.do()
		if got := m.stopDuplicate(step.sid); got != step.dup {
			t.Errorf("%s: duplicate %v, want %v", step.name, got, step.dup)
		}
	}
}

// A report-only hook (a session the user started, node/globalhooks.go) is not Persona's hooks running in the session:
// it must not make the hub hold screen cards back (hasHooks), but its Stop still feeds the duplicate check.
func TestReportOnlyStopDedupesWithoutMarkingHooks(t *testing.T) {
	h := newTestHub(t)
	m, _ := newCodexMachine(t, h)
	b, _ := json.Marshal(map[string]any{"session_id": "s1", "event": "Stop", "stop_sig": "aaaa", "report_only": true})
	m.onEvent("hook.event", b)
	if m.hooked["s1"] {
		t.Error("a report-only event marked the session as having Persona's hooks")
	}
	if m.stopDuplicate("s1") {
		t.Error("the first push was held back")
	}
	if !m.stopDuplicate("s1") {
		t.Error("the same reply pushed twice")
	}
}

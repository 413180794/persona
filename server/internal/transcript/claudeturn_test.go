package transcript

import "testing"

// A Claude turn is open from its prompt until its turn_duration line or an interrupt (#2069: the node shows a Claude
// at work from this when herdr says done); a typed-ahead (queued) prompt does not start one, and a Codex rollout's
// turns are its own.
func TestClaudeTurnOpenAndClosed(t *testing.T) {
	var m Meta
	feed := func(lines ...string) {
		for _, l := range lines {
			ParseLine(0, []byte(l), &m)
		}
	}
	check := func(when string, open, closed bool) {
		t.Helper()
		if m.TurnOpen != open || m.TurnClosed != closed {
			t.Fatalf("%s: open %v closed %v, want %v %v", when, m.TurnOpen, m.TurnClosed, open, closed)
		}
	}
	check("nothing read", false, false)
	feed(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"mid-turn tail"}]},"uuid":"a0","timestamp":"2026-01-01T00:00:00Z"}`)
	check("a tail from mid-turn", false, false)
	feed(`{"type":"user","message":{"role":"user","content":"look for the bug"},"uuid":"u1","timestamp":"2026-01-01T00:00:01Z"}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Grep","input":{"pattern":"x"}}]},"uuid":"a1","parentUuid":"u1","timestamp":"2026-01-01T00:00:02Z"}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"x.go:1"}]},"uuid":"r1","parentUuid":"a1","timestamp":"2026-01-01T00:00:03Z"}`)
	check("a prompt, a tool call and its result", true, false)
	feed(`{"type":"system","subtype":"turn_duration","uuid":"d1","parentUuid":"a1","durationMs":2000,"timestamp":"2026-01-01T00:00:04Z"}`)
	check("turn_duration", false, true)
	feed(`{"type":"user","message":{"role":"user","content":"and the other one"},"uuid":"u2","timestamp":"2026-01-01T00:00:05Z"}`)
	check("the next prompt", true, false)
	feed(`{"type":"user","message":{"role":"user","content":"[Request interrupted by user]"},"uuid":"u3","timestamp":"2026-01-01T00:00:06Z"}`)
	check("an interrupt", false, true)

	var c Meta
	ParseLine(0, []byte(`{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"x","cwd":"/work/project"}}`), &c)
	ParseLine(0, []byte(`{"timestamp":"2026-01-01T00:00:01Z","type":"event_msg","payload":{"type":"task_complete"}}`), &c)
	ParseLine(0, []byte(`{"timestamp":"2026-01-01T00:00:02Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}}`), &c)
	if c.TurnOpen || !c.TurnClosed {
		t.Fatalf("Codex: open %v closed %v, want its own turn (closed)", c.TurnOpen, c.TurnClosed)
	}
}

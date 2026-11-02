package transcript

import (
	"fmt"
	"testing"
)

func TestRecordedTurnDuration(t *testing.T) {
	for _, raw := range []string{"0", "12345"} {
		t.Run(raw, func(t *testing.T) {
			claude := ParseLine(3, []byte(fmt.Sprintf(`{"type":"system","subtype":"turn_duration","uuid":"duration","parentUuid":"reply","durationMs":%s}`, raw)), &Meta{})
			if len(claude) != 1 || claude[0].TurnDuration.ParentUUID != "reply" || claude[0].TurnDuration.Source != "claude_turn_duration" {
				t.Fatalf("Claude duration: %+v", claude)
			}
			m := &Meta{Codex: true}
			ParseLine(1, []byte(`{"type":"event_msg","payload":{"type":"task_started","turn_id":"turn-example"}}`), m)
			ParseLine(2, []byte(`{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"UserMessage","id":"prompt","content":[{"type":"text","text":"Example"}]}}}`), m)
			codex := ParseLine(3, []byte(fmt.Sprintf(`{"type":"event_msg","payload":{"type":"task_complete","turn_id":"turn-example","duration_ms":%s}}`, raw)), m)
			if len(codex) != 1 || codex[0].TurnDuration.UserItemID != "prompt#0" || codex[0].TurnDuration.TurnID != "turn-example" || codex[0].TurnDuration.DurationMS != claude[0].TurnDuration.DurationMS {
				t.Fatalf("Codex duration: %+v", codex)
			}
		})
	}
	for _, raw := range []string{"null", `"12345"`, "-1", "1.5", "true", "9223372036854775808"} {
		line := fmt.Sprintf(`{"type":"system","subtype":"turn_duration","parentUuid":"reply","durationMs":%s}`, raw)
		if got := ParseLine(1, []byte(line), &Meta{}); len(got) != 0 {
			t.Fatalf("accepted invalid duration %s: %+v", raw, got)
		}
	}
	for _, line := range []string{
		`{"type":"system","subtype":"turn_duration","parentUuid":"reply"}`,
		`{"type":"system","subtype":"turn_duration","durationMs":1000}`,
		`{"type":"system","subtype":"turn_duration","parentUuid":"reply","durationMs":1000,"isSidechain":true}`,
		`{"type":"system","subtype":"compact_boundary","compactMetadata":{"durationMs":1000}}`,
		`{"type":"event_msg","payload":{"type":"task_complete","turn_id":"turn-example","started_at":"2026-01-01T00:00:00Z","completed_at":"2026-01-01T00:01:00Z"}}`,
		`{"type":"event_msg","payload":{"type":"task_complete","duration_ms":1000}}`,
		`{"type":"event_msg","payload":{"type":"task_complete","turn_id":"other-turn","duration_ms":1000}}`,
	} {
		got := ParseLine(3, []byte(line), &Meta{codexTurnID: "turn-example"})
		for _, it := range got {
			if it.TurnDuration != nil {
				t.Fatalf("invented a turn duration: %+v", it)
			}
		}
	}
}

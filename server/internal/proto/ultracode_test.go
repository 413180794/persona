package proto

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestSessionUltracodeThreeStates(t *testing.T) {
	on, off := true, false
	for _, value := range []*bool{nil, &on, &off} {
		at := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
		original := Session{Ultracode: value, UltracodeNote: "需要开启 dynamic workflows", UltracodePending: true, UltracodeLostAt: &at}
		raw, err := json.Marshal(original)
		if err != nil {
			t.Fatal(err)
		}
		var decoded Session
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), `"ultracode":`) != (value != nil) || (decoded.Ultracode == nil) != (value == nil) ||
			(value != nil && *decoded.Ultracode != *value) || decoded.UltracodeNote != original.UltracodeNote || !decoded.UltracodePending ||
			decoded.UltracodeLostAt == nil || !decoded.UltracodeLostAt.Equal(at) {
			t.Fatalf("round trip: %s", raw)
		}
	}
	if !slices.Contains(Features, "ultracode") {
		t.Fatal("ultracode is not advertised")
	}
}

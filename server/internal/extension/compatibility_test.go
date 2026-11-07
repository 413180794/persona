package extension

import (
	"encoding/json"
	"github.com/nwverse/persona/server/internal/compat"
	"testing"
)

func TestCompatibleRetiredFallbackKeepsPackageBytesAndReservedIDs(t *testing.T) {
	for _, id := range []string{"persona-visor", "persona-circuit"} {
		legacy := compat.LegacyLook(id)
		if !ReservedLookID(legacy) {
			t.Fatalf("legacy retired id became available: %q", legacy)
		}
		item := LookItem{ID: "fixture-look", Name: "Fixture", AppIconFallback: legacy, Avatar: LookAvatar{Sizes: map[string]string{"128": "avatar.png"}}}
		before, _ := json.Marshal(item)
		if err := item.check(); err != nil {
			t.Fatal("pre-cutover fallback was rejected", err)
		}
		after, _ := json.Marshal(item)
		if string(before) != string(after) {
			t.Fatal("validation rewrote package bytes")
		}
		item.AppIconFallback = id
		if err := item.check(); err == nil {
			t.Fatal("new retired fallback was reenabled")
		}
	}
}

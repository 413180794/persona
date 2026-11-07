package extmgr

import (
	"testing"

	"github.com/nwverse/persona/server/internal/extension"
)

func TestPageLegacyRenderInputSeparatesSourceAndTreeVersions(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		s := &pageSession{legacy: legacy, params: extension.UIParams{UIVersion: 1, State: "synthetic-state", Locale: "ja"}}
		got := pageRenderInput(s)
		want := 1
		if legacy {
			want = extension.UIVersion
		}
		if got.params.UIVersion != want || s.params.UIVersion != 1 {
			t.Fatalf("legacy=%v: tree version=%d source version=%d", legacy, got.params.UIVersion, s.params.UIVersion)
		}
		if got.params.State != s.params.State || got.params.Locale != s.params.Locale {
			t.Fatal("render fitting changed source state or language")
		}
	}
}

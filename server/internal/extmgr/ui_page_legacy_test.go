package extmgr

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func TestPageSourceLegacyHTTPKeepsCallbacksAndFittedPresence(t *testing.T) {
	h, process, _ := pageProcessManager(t)
	var raw json.RawMessage
	var in uiRequest
	in.Context.UIVersion = 6
	in.nativeResult = &raw
	result, err := h.uiCall(t.Context(), "example-page", "home", in)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(`"truncated":false`)) || !bytes.Contains(raw, []byte(`"text":""`)) {
		t.Fatal("HTTP fitted snapshot lost native presence")
	}
	state := result.View.State
	action := ""
	for _, item := range result.View.Sections[0].Items {
		if item.Key == "no-ui" {
			action = item.Action.ID
		}
	}
	in.State = state
	in.Action = action
	result, err = h.uiCall(t.Context(), "example-page", "home", in)
	if err != nil {
		t.Fatal(err)
	}
	if result.View.State == state || pageStats(t, process)["writes"] != 1 {
		t.Fatalf("legacy callback state/fence not retained: old=%s new=%s stats=%v", state, result.View.State, pageStats(t, process))
	}
	if _, err := h.uiCall(t.Context(), "example-page", "home", in); err == nil {
		t.Fatal("old HTTP checkpoint repeated a write")
	}
	// A snapshot-only older client still sees a fitted page and retains the
	// server session. Draft values stay in the action request, not a source claim.
	in.State = result.View.State
	in.Action = ""
	in.Context.UIVersion = 1
	result, err = h.uiCall(t.Context(), "example-page", "home", in)
	if err != nil {
		t.Fatal(err)
	}
	for _, section := range result.View.Sections {
		for _, item := range section.Items {
			if item.Type == "code" || item.Control != nil {
				t.Fatal("legacy client received an unsupported native control")
			}
		}
	}
	before := pageStats(t, process)
	time.Sleep(30 * time.Millisecond)
	after := pageStats(t, process)
	if after["uiTicks"] != before["uiTicks"] {
		t.Fatal("snapshot-only legacy session kept UI polling")
	}
	wrong := withPrincipal(t.Context(), principal{ID: "other-device", Role: roleFull})
	if _, err := h.uiCall(wrong, "example-page", "home", in); err == nil {
		t.Fatal("legacy state token granted another principal access")
	}
}

func TestPageSourceLegacyHTTPPreservesNavigationVersion(t *testing.T) {
	for _, version := range []int{1, 2, 3, 6} {
		t.Run(fmt.Sprintf("version-%d", version), func(t *testing.T) {
			h, process, _ := pageProcessManager(t)
			if err := process.Call(t.Context(), "test.navigation", map[string]any{"enabled": true}, nil); err != nil {
				t.Fatal(err)
			}
			var raw json.RawMessage
			var in uiRequest
			in.Context.UIVersion = version
			in.nativeResult = &raw
			result, err := h.uiCall(t.Context(), "example-page", "home", in)
			if err != nil {
				t.Fatal(err)
			}
			if got := pageStats(t, process)["mountUIVersion"]; got != version {
				t.Fatalf("source received UI version %d, caller requested %d", got, version)
			}
			action := func() string {
				for _, section := range result.View.Sections {
					for _, item := range section.Items {
						if item.Title == "Synthetic navigation" && item.Action != nil {
							return item.Action.ID
						}
					}
				}
				t.Fatal("version-aware navigation action missing")
				return ""
			}
			in.State, in.Action = result.View.State, action()
			result, err = h.uiCall(t.Context(), "example-page", "home", in)
			if err != nil {
				t.Fatal(err)
			}
			if result.View.State == in.State {
				t.Fatal("navigation callback did not advance the checkpoint fence")
			}
			if version < 3 {
				if result.Push != nil || result.Pop || !bytes.Contains(raw, []byte(`"text":"detail"`)) {
					t.Fatalf("old client did not navigate in its server session: %s", raw)
				}
			} else if result.Push == nil || result.Push.State != "detail" || !bytes.Contains(raw, []byte(`"text":"home"`)) {
				t.Fatalf("modern native push changed: %s", raw)
			}
			if _, err := h.uiCall(t.Context(), "example-page", "home", in); err == nil {
				t.Fatal("old navigation checkpoint replayed")
			}
			if version < 3 {
				in.State, in.Action = result.View.State, action()
				result, err = h.uiCall(t.Context(), "example-page", "home", in)
				if err != nil || !bytes.Contains(raw, []byte(`"text":"home"`)) || result.Push != nil || result.Pop {
					t.Fatalf("old client did not navigate back: err=%v result=%s", err, raw)
				}
			}
		})
	}
}

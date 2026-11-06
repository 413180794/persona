package uistream

import (
	"bytes"
	"testing"
)

func TestAcceptedEventEmptyDeltaFence(t *testing.T) {
	view := `{"sections":[{"key":"main","items":[{"type":"code","key":"code","text":"","truncated":false}]}]}`
	before := snapshot(t, view, 1)
	after := snapshot(t, view, 2)
	change, recommend, err := Diff(before, after)
	if err != nil || recommend || len(change.ops) != 0 {
		t.Fatalf("fence must be empty delta: %v %v", recommend, err)
	}
	next, err := Apply(before, change)
	if err != nil || next.Version() != 2 || !bytes.Equal(next.ViewJSON(), before.ViewJSON()) {
		t.Fatal("no-op fence lost native presence or failed to advance")
	}
	if _, err := Apply(next, change); err == nil {
		t.Fatal("accepted fence replayed")
	}
	gap := delta(t, `[]`)
	gap.frame.Version = 3
	if _, err := Apply(before, gap); err == nil {
		t.Fatal("empty fence skipped a version")
	}
	same := snapshot(t, view, 1)
	unchanged, _, err := Diff(before, same)
	if err != nil {
		t.Fatal(err)
	}
	if next, err := Apply(before, unchanged); err != nil || next.Version() != 1 {
		t.Fatal("unchanged non-event snapshots cannot retain version")
	}
}

func TestClientEnvelopeRejectsRecipientClaimsAndMissingRequest(t *testing.T) {
	for _, raw := range []string{
		`{"type":"extension_ui_stream"}`,
		`{"type":"extension_ui_stream","owner":"someone","request":{"request":"close","protocol":1,"request_id":"close","session":"s","epoch":"e"}}`,
		`{"type":"extension_ui_stream","request":{"request":"close","protocol":1,"request_id":"close","session":"s","epoch":"e"},"type":"extension_ui_stream"}`,
	} {
		if _, err := DecodeClientEnvelope([]byte(raw)); err == nil {
			t.Fatal("untrusted envelope accepted")
		}
	}
	valid := `{"type":"extension_ui_stream","request":{"request":"close","protocol":1,"request_id":"close","session":"s","epoch":"e"}}`
	request, err := DecodeClientEnvelope([]byte(valid))
	if err != nil || request.Request == nil || request.Request.Session != "s" {
		t.Fatalf("decoded request lost fields: %+v %v", request, err)
	}
}

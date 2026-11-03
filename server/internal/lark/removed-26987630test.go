package lark

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOLKProjectionAndQuotedMedia(t *testing.T) {
	var raw rawMessage
	err := json.Unmarshal([]byte(`{"id":"m","olk":{"version":1,"state":"decrypted","bodyParts":[{"kind":"text","text":"body"}],"publicParts":[{"kind":"emotion","url":"/api/media/cover"}],"keyHandout":true},"replyTo":{"id":"q","olk":{"version":1,"state":"decrypted","bodyParts":[{"kind":"text","text":"quote"}],"publicParts":[]}}}`), &raw)
	if err != nil {
		t.Fatal(err)
	}
	m := raw.normalize()
	if m.OLK == nil || !m.OLK.KeyHandout || !strings.Contains(string(m.OLK.PublicParts), "/api/v1/lark/media/cover") || !strings.Contains(string(m.OLK.BodyParts), "body") {
		t.Fatalf("missing projection: %+v", m.OLK)
	}
	if m.ReplyTo == nil || m.ReplyTo.OLK == nil || !strings.Contains(string(m.ReplyTo.OLK.BodyParts), "quote") {
		t.Fatal("missing quote projection")
	}
	if strings.Contains(string(raw.OLK.PublicParts), "/api/v1/") {
		t.Fatal("mutated upstream")
	}
}

func TestOLKClosedOrUnknownVersionDiscardsUnexpectedBody(t *testing.T) {
	for _, state := range []string{"locked", "forbidden", "missing-key", "unavailable"} {
		p := (&OLKPresentation{Version: 1, State: state, BodyParts: json.RawMessage(`[{"kind":"text","text":"secret"}]`), KeyHandout: true}).normalize()
		if p.BodyParts != nil || p.KeyHandout {
			t.Fatalf("closed state %s retained plaintext", state)
		}
	}
	if p := (&OLKPresentation{Version: 2, State: "decrypted", BodyParts: json.RawMessage(`[]`)}).normalize(); p.BodyParts != nil {
		t.Fatal("unknown version retained body")
	}
	if m := (rawMessage{TextPreview: "ordinary"}).normalize(); m.OLK != nil || m.Text != "ordinary" {
		t.Fatal("changed unencrypted message")
	}
}

func TestOLKPartialProjectionDoesNotBreakMessagePage(t *testing.T) {
	for _, projection := range []string{
		`{"version":1,"state":"locked"}`,
		`{"version":1,"state":"locked","publicParts":null}`,
		`{"version":1,"state":"locked","publicParts":{}}`,
		`{"state":"decrypted","bodyParts":[{"kind":"text","text":"private"}]}`,
		`{"version":"future","state":"decrypted","bodyParts":[{"kind":"text","text":"private"}]}`,
		`{"version":1,"bodyParts":[{"kind":"text","text":"private"}]}`,
		`"invalid"`,
	} {
		var raw []rawMessage
		if err := json.Unmarshal([]byte(`[{"id":"plain","textPreview":"ordinary"},{"id":"encrypted","olk":`+projection+`}]`), &raw); err != nil {
			t.Fatal(err)
		}
		if len(raw) != 2 || raw[0].normalize().Text != "ordinary" {
			t.Fatal("dropped neighboring message")
		}
		normalized := raw[1].normalize().OLK
		if normalized == nil || string(normalized.PublicParts) != "[]" || normalized.BodyParts != nil || normalized.KeyHandout {
			t.Fatalf("unsafe projection: %+v", normalized)
		}
		encoded, err := json.Marshal(normalized)
		if err != nil || !strings.Contains(string(encoded), `"publicParts":[]`) {
			t.Fatalf("missing array default: %s %v", encoded, err)
		}
	}
}

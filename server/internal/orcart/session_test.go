package orcart_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/orcart"
	"github.com/nwverse/persona/server/internal/orcart/orcarttest"
)

const chat = "claude_0b0e9c8e_6f1a_4c3e_9d2b_4a5f6e7d8c9b"

// The fingerprint of a plain "hello" is the one Orca itself computes for it (the same canonical text gave the
// fingerprint Orca stored for a live session's message; the session id here is a placeholder).
func TestFingerprintMatchesOrca(t *testing.T) {
	fields := map[string]any{"body": map[string]any{"kind": "message", "role": "user",
		"blocks": []any{map[string]any{"type": "text", "text": "hello"}}}}
	got, err := orcart.Fingerprint("agentSession.send", chat, fields)
	if err != nil {
		t.Fatal(err)
	}
	if want := "6960bc9ad67080004eaddcc7cff600cf8043ab224aec8730355443f83a931cd8"; got != want {
		t.Errorf("fingerprint %s, want %s", got, want)
	}
}

// Strings are written as JavaScript writes them: no HTML escapes, U+2028 raw, controls escaped.
func TestFingerprintEscapesLikeJavaScript(t *testing.T) {
	got, err := orcart.Fingerprint("m", "s", map[string]any{"t": "<a&b>\u2028\n\x01", "n": 2})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(`{"fields":{"n":2,"t":"<a&b>` + "\u2028" + `\n\u0001"},"method":"m","sessionId":"s"}`))
	if want := hex.EncodeToString(sum[:]); got != want {
		t.Errorf("fingerprint %s, want %s", got, want)
	}
}

func TestOperationID(t *testing.T) {
	at := time.UnixMilli(1793850000000)
	re := regexp.MustCompile(`^\d{13}-[0-9a-f]{32}$`)
	a, b := orcart.OperationID(at, "msg-1"), orcart.OperationID(at, "msg-1")
	if !re.MatchString(a) || a != b || !strings.HasPrefix(a, "1793850000000-") {
		t.Errorf("keyed ids: %s %s", a, b)
	}
	if r1, r2 := orcart.OperationID(at, ""), orcart.OperationID(at, ""); !re.MatchString(r1) || r1 == r2 {
		t.Errorf("random ids: %s %s", r1, r2)
	}
}

func TestSessionOf(t *testing.T) {
	if id, ok := orcart.SessionOf("structured-agent-session-" + chat + ":8b55721b"); !ok || id != chat {
		t.Errorf("structured: %q %v", id, ok)
	}
	if _, ok := orcart.SessionOf("tab-1:leaf-1"); ok {
		t.Error("a terminal's pane is not a session")
	}
}

// currentRequests comes in pieces under a marker: the first call learns it, the pieces are joined, and only pending
// prompts are kept.
func TestPromptsReadsThePieces(t *testing.T) {
	f := orcarttest.New(t)
	items := `[{"itemId":"i1","revision":3,"body":{"kind":"approval","title":"Allow Bash?","detail":"ls",` +
		`"options":[{"id":"allow","label":"Allow"},{"id":"deny","label":"Deny"}],"resolution":{"state":"pending"}}},` +
		`{"itemId":"i0","revision":1,"body":{"kind":"question","question":"q","options":[],"resolution":{"state":"resolved"}}}]`
	half := len(items) / 2
	f.Handle("agentSession.currentRequests", func(p map[string]any) any {
		marker := map[string]any{"version": "v7"}
		if p["version"] != "v7" {
			return map[string]any{"marker": marker, "json": "", "nextCursor": 0, "restart": true}
		}
		if p["cursor"] == float64(0) {
			return map[string]any{"marker": marker, "json": items[:half], "nextCursor": 1}
		}
		return map[string]any{"marker": marker, "json": items[half:], "nextCursor": nil}
	})
	c := &orcart.Client{MetaPath: f.MetaPath}
	got, err := c.Prompts(context.Background(), chat)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ItemID != "i1" || got[0].Revision != 3 || got[0].Body.Detail == nil || *got[0].Body.Detail != "ls" {
		t.Fatalf("prompts: %+v", got)
	}
	if n := f.Count("agentSession.currentRequests"); n != 3 {
		t.Errorf("%d calls, want 3 (probe, two pieces)", n)
	}
}

// A write carries the envelope (operation id, fence, the fingerprint of exactly its fields); a refusal is an error
// that says Orca did nothing.
func TestWriteEnvelopeAndRefusal(t *testing.T) {
	f := orcarttest.New(t)
	var got map[string]any
	f.Handle("agentSession.send", func(p map[string]any) any {
		got = p
		return map[string]any{"ok": true, "replayed": false, "value": map[string]any{}}
	})
	c := &orcart.Client{MetaPath: f.MetaPath}
	w := orcart.Write{SessionID: chat, Fence: 2, OperationID: orcart.OperationID(time.Now(), "m1")}
	if err := c.SendSession(context.Background(), w, "hi there\n", []string{"/tmp/a.png"}); err != nil {
		t.Fatal(err)
	}
	env, _ := got["envelope"].(map[string]any)
	body := map[string]any{"kind": "message", "role": "user", "blocks": []any{
		map[string]any{"type": "text", "text": "hi there"}, map[string]any{"type": "image-ref", "path": "/tmp/a.png"}}}
	want, _ := orcart.Fingerprint("agentSession.send", chat, map[string]any{"body": body, "delivery": "queue-if-active"})
	if env["sessionId"] != chat || env["clientOperationId"] != w.OperationID || env["expectedRuntimeFence"] != float64(2) ||
		env["payloadFingerprint"] != want {
		b, _ := json.Marshal(got)
		t.Fatalf("envelope: %s", b)
	}

	f.Handle("agentSession.respondToApproval", func(p map[string]any) any {
		return map[string]any{"ok": false, "refusal": map[string]any{"code": "agent_session_already_resolved", "message": "already"}}
	})
	var p orcart.Prompt
	p.ItemID, p.Revision = "i1", 1
	err := c.Approve(context.Background(), w, p, "allow")
	if !orcart.IsCode(err, "agent_session_already_resolved") || !orcart.Refused(err) {
		t.Errorf("refusal: %v", err)
	}
	f.Handle("agentSession.cancel", func(p map[string]any) any {
		return map[string]any{"ok": false, "refusal": map[string]any{"code": "agent_session_operation_unknown"}}
	})
	if err := c.CancelSession(context.Background(), w); err == nil || orcart.Refused(err) {
		t.Errorf("an unknown outcome is not a refusal: %v", err)
	}
}

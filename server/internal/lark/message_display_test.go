package lark

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRetainedAndEditedMessageProjection(t *testing.T) {
	var raw rawMessage
	err := json.Unmarshal([]byte(`{"id":"m","isRecalled":true,"hasRetainedRecallContent":true,"parts":[{"kind":"text","text":"retained"}],"replyTo":{"id":"q","isRecalled":true,"hasRetainedRecallContent":true},"editHistory":[{"editedAt":"2026-01-01T00:00:00Z","textPreview":"legacy","parts":[{"kind":"image","url":"/agent/v1/media/a"}],"olk":{"version":1,"state":"locked","bodyParts":[{"kind":"text","text":"secret"}],"publicParts":[]}}]}`), &raw)
	if err != nil {
		t.Fatal(err)
	}
	m := raw.normalize()
	if !m.Recalled || !m.Retained || !m.ReplyTo.Retained || !m.ReplyTo.Recalled || len(m.EditHistory) != 1 {
		t.Fatalf("lost semantics: %+v", m)
	}
	v := m.EditHistory[0]
	if !strings.Contains(string(v.Parts), "/api/v1/lark/media/a") || len(v.OLK.BodyParts) != 0 {
		t.Fatalf("unsafe or unrewritten history: %+v", v)
	}
	raw.IsSystem = true
	if raw.normalize().Retained {
		t.Fatal("system recall stub mistaken for retained original")
	}
}

func TestMessageDisplayAccountChangesDuringRead(t *testing.T) {
	reads := 0
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/accounts" {
			reads++
			fmt.Fprintf(w, `{"activeId":"1","accounts":[{"id":"1","signedIn":true,"userId":"user-%d"}]}`, reads)
			return
		}
		fmt.Fprint(w, `{"antiRecall":true,"showEditHistory":true}`)
	}))
	defer up.Close()
	c := New(Config{BaseURL: up.URL, Token: "test-token"})
	got, err := c.MessageDisplay(context.Background(), nil)
	if err != ErrDisplayAccountChanged || got.AccountID != "" {
		t.Fatalf("mixed account snapshot: %+v %v", got, err)
	}
}

func TestLocalSettingsChangeIsRelayed(t *testing.T) {
	ev, ok := parseEvent([]byte(`{"type":"local.settings.updated"}`))
	if !ok || ev.Type != "local.settings.updated" || len(ev.FeedIDs) != 0 {
		t.Fatalf("settings event dropped: %+v %v", ev, ok)
	}
}

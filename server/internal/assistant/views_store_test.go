package assistant

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/proto"
)

func TestViewsPersistAcrossAssistantRestart(t *testing.T) {
	url := os.Getenv("PERSONA_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("PERSONA_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	database, err := db.Open(ctx, url, db.ManagerSet)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	f := newFakeHub().withMachine()
	f.hello.PersonaView = true
	f.setSessions("server", proto.Session{SessionID: "view-test-" + randomHex(8), Assistant: true})
	f.mu.Lock()
	conversation := f.sessions["server"][0].SessionID
	f.mu.Unlock()
	s := newServiceOn(t, f, database)
	out, err := s.toolView(ctx, "server", conversation, "view_show", json.RawMessage(`{"view":{"fallback":"Before","sections":[]}}`))
	if err != nil {
		t.Fatal(err)
	}
	var made struct{ ID string }
	if err := json.Unmarshal([]byte(out), &made); err != nil {
		t.Fatal(err)
	}
	update := json.RawMessage(`{"id":"` + made.ID + `","view":{"fallback":"After","sections":[]}}`)
	if _, err := s.toolView(ctx, "server", conversation, "view_update", update); err != nil {
		t.Fatal(err)
	}
	resumed := newServiceOn(t, f, database)
	got, err := resumed.toolView(ctx, "server", conversation, "view_get", json.RawMessage(`{"id":"`+made.ID+`"}`))
	if err != nil || !strings.Contains(got, `"version":2`) || !strings.Contains(got, `"fallback":"After"`) {
		t.Fatalf("%s %v", got, err)
	}
	views, err := resumed.conversationViews(ctx, conversation)
	if err != nil || len(views) != 1 || views[made.ID].Version != 2 {
		t.Fatalf("%+v %v", views, err)
	}
	items, _, err := resumed.feed(ctx, conversation, time.Time{})
	if err != nil || len(items) != 1 || items[0].Kind != feedView || items[0].Ref != made.ID {
		t.Fatalf("%+v %v", items, err)
	}
}

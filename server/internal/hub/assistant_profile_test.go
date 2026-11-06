package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistant"
	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/proto"
)

// assistantView is GET /api/v1/assistant as the apps read it (the assistant answers it, profile.go).
type assistantView struct {
	MachineID string  `json:"machine_id"`
	Launcher  string  `json:"launcher"`
	Timezone  string  `json:"timezone"`
	Notes     *string `json:"notes"`
	Main      string  `json:"main"`
	Former    []struct {
		MachineID string    `json:"machine_id"`
		SessionID string    `json:"session_id"`
		Until     time.Time `json:"until"`
	} `json:"former_main"`
	Cwd            string          `json:"cwd"`
	Conversations  []proto.Session `json:"conversations"`
	NotesEffective string          `json:"notes_effective"`
	MainTasksQuiet bool            `json:"main_tasks_quiet"`
}

// putAssistantSettings saves the assistant's own settings in h's store the way the assistant keeps them (a store
// in memory when h has none); a connected assistant reads them again and reports them (helloChanged).
func putAssistantSettings(t *testing.T, h *Hub, v assistant.Settings) {
	t.Helper()
	if h.store == nil {
		h.store = &settingsStore{vals: map[string][]byte{}}
	}
	if err := h.store.PutSetting(context.Background(), "assistant", v); err != nil {
		t.Fatal(err)
	}
	if _, ok := testAssistants.Load(h); ok {
		helloChanged(t, h)
	}
}

// assistantViewOf is GET /api/v1/assistant (query: "?archived=1" or ""), relayed to the assistant.
func assistantViewOf(t *testing.T, h *Hub, query string) assistantView {
	t.Helper()
	mux := http.NewServeMux()
	h.assistantRoutesRelayed(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/assistant"+query, nil))
	var v assistantView
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/assistant: %d %s", rec.Code, rec.Body)
	}
	return v
}

// The hub keeps the profile the assistant last reported as its own (hub.assistant_profile) and starts with it, so its
// features have the user's zone and the main conversation before the assistant reports again. It never reads the
// assistant's own settings (the manager's).
func TestTheHubStartsWithTheProfileItKept(t *testing.T) {
	ctx := context.Background()
	h := newTestHub(t)
	store := &settingsStore{vals: map[string][]byte{}}
	h.store = store
	if err := store.PutSetting(ctx, "assistant", map[string]any{"machine_id": "gone", "main": "main-1", "timezone": "Asia/Tokyo",
		"notes": "关于我"}); err != nil {
		t.Fatal(err)
	}
	h.loadAssistantProfile(ctx)
	// The assistant's own settings are the manager's: the hub does not read them (hub.json's defaults until it reports).
	if p := h.assistantProfile(); p.Main != "" || p.Timezone == "Asia/Tokyo" {
		t.Fatalf("read from the assistant's settings: %+v", p)
	}

	// A report is what the next start begins with (the hub's own settings).
	h.assistant.profile = nil
	snap := assistantwire.Snapshot{Epoch: "e", Seq: 1, Profile: &assistantwire.Profile{MachineID: "server", Main: "main-2",
		Launcher: "claude", Timezone: "Europe/Paris", Keep: []assistantwire.SessionRef{{MachineID: "server", SessionID: "main-2"}}}}
	if _, err := (assistantLink{h}).Snapshot(ctx, snap); err != nil {
		t.Fatal(err)
	}
	const other = "m-0a0b0c"
	if _, err := (assistantLink{h}).snapshotFrom(ctx, other, assistantwire.Snapshot{
		Epoch: "e", Seq: 1, Profile: &assistantwire.Profile{MachineID: "laptop", Timezone: "Asia/Tokyo"},
	}); err != nil {
		t.Fatal(err)
	}
	if p := h.assistantProfileOf(other); p.MachineID != "laptop" || p.Timezone != "Asia/Tokyo" {
		t.Fatalf("another manager's report: %+v", p)
	}
	next := newTestHub(t)
	next.store = store
	next.loadAssistantProfile(ctx)
	if p := next.assistantProfile(); p.Main != "main-2" || p.Timezone != "Europe/Paris" || len(p.Keep) != 1 {
		t.Fatalf("after a restart: %+v", p)
	}
	if keep := next.assistantProfile().Keep; len(keep) != 1 || keep[0].SessionID != "main-2" {
		t.Fatalf("kept: %v", keep)
	}
	// Only the built-in profile survives a restart; use it until the other manager reports again.
	if p := next.assistantProfileOf(other); p.MachineID != "server" || p.Timezone != "Europe/Paris" {
		t.Fatalf("another manager's startup fallback: %+v", p)
	}
}

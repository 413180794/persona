package hub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/compat"
)

func TestLegacyHeaderAdmissionAndConflict(t *testing.T) {
	calls := 0
	h := compatibilityRequests(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("X-Persona-Op-Id") != "same" {
			t.Fatal("legacy receipt header was lost")
		}
		w.WriteHeader(204)
	}))
	r := httptest.NewRequest("POST", "/api/v1/sessions", nil)
	r.Header.Set("X-Jarvis-Op-Id", "same")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 204 || calls != 1 {
		t.Fatal(w.Code, calls)
	}
	r = httptest.NewRequest("POST", "/api/v1/sessions", nil)
	r.Header.Set("X-Jarvis-Op-Id", "old")
	r.Header.Set("X-Persona-Op-Id", "new")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 400 || calls != 1 {
		t.Fatal("conflicting identities reached the operation", w.Code, calls)
	}
}
func TestPushStartUsesPersistedDeviceCapability(t *testing.T) {
	p := startPayload(FleetState{NeedsYou: 1}, time.Unix(1, 0), false)
	old := compatibleStartPayload(Device{}, p)
	if old["aps"].(map[string]any)["attributes-type"] != "JarvisActivityAttributes" {
		t.Fatal(old)
	}
	if p["aps"].(map[string]any)["attributes-type"] != "PersonaActivityAttributes" {
		t.Fatal("shared payload mutated")
	}
	current := compatibleStartPayload(Device{Features: []string{compat.Feature}}, p)
	if current["aps"].(map[string]any)["attributes-type"] != "PersonaActivityAttributes" {
		t.Fatal(current)
	}
	if got := cleanFeatures([]string{compat.Feature, "unknown"}); len(got) != 1 || got[0] != compat.Feature {
		t.Fatal(got)
	}
}
func TestLookWireAndPersistenceKeepBaselineIdentity(t *testing.T) {
	v := lookView{ID: "persona-visor", AppIcon: "persona-visor", Source: "builtin", Name: "Keep name"}
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var old struct {
		ID   string `json:"id"`
		Icon string `json:"app_icon"`
	}
	json.Unmarshal(raw, &old)
	if old.ID != "jarvis" || old.Icon != "jarvis" {
		t.Fatal(string(raw))
	}
	var current lookView
	json.Unmarshal(raw, &current)
	if current.ID != "persona-visor" || current.Name != "Keep name" {
		t.Fatal(current)
	}
	raw, _ = json.Marshal(lookChoice{ID: "persona-circuit"})
	var kept map[string]any
	json.Unmarshal(raw, &kept)
	if kept["id"] != "jarvis-circuit" {
		t.Fatal(kept)
	}
}

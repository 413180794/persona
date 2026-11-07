package hub

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/compat"
	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/extwire"
)

func TestLegacyExtensionIconFallbackUsesMochi(t *testing.T) {
	for _, icon := range append(slices.Clone(extension.RetiredLookIDs), "unknown-icon") {
		t.Run(icon, func(t *testing.T) {
			h := newTestHub(t)
			lp := lookReport(t, exampleLooksDir(t), "pebble")[0]
			var item extension.LookItem
			_ = json.Unmarshal(lp.Item, &item)
			item.AppIconFallback = icon
			lp.Item, _ = json.Marshal(item)
			reportLooks(h, []extwire.LookPackage{lp})
			_, out := call(t, h.routes(), "GET", "/api/v1/looks", h.cfg.Token, "")
			look := lookByID(t, out, "pebble")
			if look == nil || look["app_icon"] != "persona" || look["manifest"].(map[string]any)["app_icon_fallback"] != "persona" {
				t.Fatalf("legacy icon must not hide an otherwise valid extension: %v", out)
			}
		})
	}
}

func TestBundledLooksOnlyMochiAndMilkFrog(t *testing.T) {
	h := newTestHub(t)
	h.builtinLooks = builtinPersonaFS()
	_, out := call(t, h.routes(), "GET", "/api/v1/looks", h.cfg.Token, "")
	if len(out["looks"].([]any)) != 2 {
		t.Fatalf("bundled catalog: %v", out)
	}
	for _, id := range []string{"persona", "nwverse"} {
		if lookByID(t, out, id) == nil {
			t.Fatalf("missing %s: %v", id, out)
		}
	}
	t.Log("GET /api/v1/looks: ids=[persona nwverse], current.effective=persona")
	if lookByID(t, out, "nwverse")["source"] != "hub" {
		t.Fatalf("Milk Frog must remain a downloadable bundled pack: %v", out)
	}
}

// Old settings retain their timestamp and raw ID, but all clients receive the safe drawing to display.
func TestRemovedAndUnknownLookChoicesFallBack(t *testing.T) {
	for _, id := range append(slices.Clone(extension.RetiredLookIDs), "unknown-look") {
		t.Run(id, func(t *testing.T) {
			h := newTestHub(t)
			h.builtinLooks = builtinPersonaFS()
			h.store = &settingsStore{vals: map[string][]byte{}}
			at := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
			if err := h.store.PutSetting(context.Background(), lookKey, lookChoice{ID: id, UpdatedAt: at}); err != nil {
				t.Fatal(err)
			}
			mux := h.routes()
			_, out := call(t, mux, "GET", "/api/v1/looks", h.cfg.Token, "")
			cur := out["current"].(map[string]any)
			if cur["id"] != compat.LegacyLook(id) || cur["effective"] != "persona" || cur["updated_at"] != at.Format(time.RFC3339) {
				t.Fatalf("saved choice: %v", cur)
			}
			t.Logf("GET /api/v1/looks: saved=%s, effective=%s", cur["id"], cur["effective"])
			// A stale device's migration cannot revive or overwrite a removed choice.
			code, cur := call(t, mux, "PUT", "/api/v1/looks/current", h.cfg.Token, `{"id":"`+id+`","if_unset":true}`)
			if code != 200 || cur["effective"] != "persona" {
				t.Fatalf("migration: %d %v", code, cur)
			}
			if code, out := call(t, mux, "PUT", "/api/v1/looks/current", h.cfg.Token, `{"id":"`+id+`"}`); code != 404 {
				t.Fatalf("removed choice accepted: %d %v", code, out)
			}
		})
	}
}

// The manager may still report an old installed extension. Neither it nor a disk pack may reclaim retired IDs.
func TestRetiredLookIDsCannotBeReclaimed(t *testing.T) {
	for _, id := range extension.RetiredLookIDs {
		t.Run(id, func(t *testing.T) {
			h := newTestHub(t)
			h.builtinLooks = builtinPersonaFS()
			h.store = &settingsStore{vals: map[string][]byte{}}
			_ = h.store.PutSetting(context.Background(), lookKey, lookChoice{ID: id})
			lp := lookReport(t, exampleLooksDir(t), "pebble")[0]
			var item extension.LookItem
			if err := json.Unmarshal(lp.Item, &item); err != nil {
				t.Fatal(err)
			}
			item.ID = id
			lp.Item, _ = json.Marshal(item)
			reportLooks(h, []extwire.LookPackage{lp})
			writePersona(t, h.personasDir(), id, personaManifestJSON(id), personaFilesOK())
			mux := h.routes()
			_, out := call(t, mux, "GET", "/api/v1/looks", h.cfg.Token, "")
			if len(out["looks"].([]any)) != 2 || lookByID(t, out, id) != nil || out["current"].(map[string]any)["effective"] != "persona" {
				t.Fatalf("retired ID reclaimed: %v", out)
			}
			srv := httptest.NewServer(mux)
			defer srv.Close()
			if code, _ := getPersonaFile(t, srv, h.cfg.Token, id+"/persona.json"); code != 404 {
				t.Fatalf("retired files served: %d", code)
			}
		})
	}
}

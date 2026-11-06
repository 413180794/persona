package hub

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/bus"
	"github.com/nwverse/persona/server/internal/db"
)

// profileManagerStore keeps only the roster and settings needed to select a profile.
type profileManagerStore struct {
	managerStore
	settings settingsStore
	ids      map[string]bool
}

func (s *profileManagerStore) Setting(ctx context.Context, key string, out any) (bool, error) {
	return s.settings.Setting(ctx, key, out)
}

func (s *profileManagerStore) PutSetting(ctx context.Context, key string, value any) error {
	return s.settings.PutSetting(ctx, key, value)
}

func (s *profileManagerStore) BusIdentityByID(_ context.Context, id string) (*db.BusIdentity, error) {
	if !s.ids[id] {
		return nil, nil
	}
	return &db.BusIdentity{ID: id, Role: bus.RoleManager}, nil
}

func newProfileTestHub(t *testing.T, ids ...string) (*Hub, *profileManagerStore) {
	t.Helper()
	h := newTestHub(t)
	s := &profileManagerStore{settings: settingsStore{vals: map[string][]byte{}}, ids: map[string]bool{assistantwire.AssistantID: true}}
	for _, id := range ids {
		s.ids[id] = true
	}
	h.bus.idents = s
	return h, s
}

func TestSkillsUsesRequestManager(t *testing.T) {
	const other = "m-0a0b0c"
	const unreported = "m-0d0e0f"
	h, store := newProfileTestHub(t, other, unreported)
	for _, id := range []string{"builtin-box", "other-box"} {
		boardNode(t, h, id, func(_ context.Context, method string, _ json.RawMessage) (any, error) {
			if method != "skills.list" {
				return nil, errors.New("unexpected method: " + method)
			}
			return map[string]any{"skills": []skill{{Name: id + "-skill", Source: "user"}}}, nil
		})
	}
	for id, mid := range map[string]string{assistantwire.AssistantID: "builtin-box", other: "other-box"} {
		if _, err := (assistantLink{h}).snapshotFrom(context.Background(), id, assistantwire.Snapshot{
			Epoch: "e", Seq: 1, Profile: &assistantwire.Profile{MachineID: mid},
		}); err != nil {
			t.Fatal(err)
		}
	}
	check := func(path, manager, wantMachine string, wantCode int) {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, path, nil)
		if manager != "" {
			r.Header.Set(ManagerHeader, manager)
		}
		w := httptest.NewRecorder()
		h.serveSkills(w, r)
		var out struct {
			MachineID string  `json:"machine_id"`
			Machine   string  `json:"machine"`
			Skills    []skill `json:"skills"`
			Code      string  `json:"code"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || w.Code != wantCode {
			t.Fatalf("%s manager=%q: %d %s, want %d", path, manager, w.Code, w.Body, wantCode)
		}
		if wantCode == http.StatusNotFound && out.Code != "no_manager" {
			t.Fatalf("unknown manager: %s", w.Body)
		}
		if wantCode == http.StatusOK && (out.MachineID != wantMachine || out.Machine != wantMachine || len(out.Skills) != 1 || out.Skills[0].Name != wantMachine+"-skill") {
			t.Fatalf("%s manager=%q: %s, want skills from %s", path, manager, w.Body, wantMachine)
		}
	}
	check("/api/v1/skills", "", "builtin-box", http.StatusOK)
	check("/api/v1/skills", other, "other-box", http.StatusOK)
	check("/api/v1/skills?manager="+other, "", "other-box", http.StatusOK)
	check("/api/v1/skills?manager="+other, assistantwire.AssistantID, "builtin-box", http.StatusOK)
	check("/api/v1/skills", unreported, "builtin-box", http.StatusOK)
	check("/api/v1/skills", "m-ffffff", "", http.StatusNotFound)

	if err := store.PutSetting(context.Background(), managerDefaultKey, other); err != nil {
		t.Fatal(err)
	}
	h.ownersChanged()
	check("/api/v1/skills", "", "other-box", http.StatusOK)
	check("/api/v1/skills", assistantwire.AssistantID, "builtin-box", http.StatusOK)

	// A reported profile with no usable machine must not borrow the built-in machine.
	for i, mid := range []string{"missing-box", ""} {
		if _, err := (assistantLink{h}).snapshotFrom(context.Background(), other, assistantwire.Snapshot{
			Epoch: "e", Seq: uint64(i + 2), Profile: &assistantwire.Profile{MachineID: mid},
		}); err != nil {
			t.Fatal(err)
		}
		check("/api/v1/skills", other, "", http.StatusConflict)
	}
}

func TestSkillsKeepsEmptyListAndNodeErrors(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty", true: "node error"}[fail], func(t *testing.T) {
			h := newTestHub(t)
			boardNode(t, h, "box", func(_ context.Context, _ string, _ json.RawMessage) (any, error) {
				if fail {
					return nil, errors.New("skills unavailable")
				}
				return map[string]any{"skills": nil}, nil
			})
			h.assistant.profile = &assistantwire.Profile{MachineID: "box"}
			w := httptest.NewRecorder()
			h.serveSkills(w, httptest.NewRequest(http.MethodGet, "/api/v1/skills", nil))
			if fail {
				if w.Code != http.StatusBadGateway {
					t.Fatalf("node error: %d %s", w.Code, w.Body)
				}
				return
			}
			var out struct {
				Skills json.RawMessage `json:"skills"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || w.Code != http.StatusOK || string(out.Skills) != "[]" {
				t.Fatalf("empty list: %d %s", w.Code, w.Body)
			}
		})
	}
}

package extmgr

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
)

// flakyAppsStore is the settings in memory whose read of computerAppsKey fails the next failReads times.
type flakyAppsStore struct {
	*memStore
	mu        sync.Mutex
	failReads int
}

func (s *flakyAppsStore) Setting(ctx context.Context, key string, out any) (bool, error) {
	s.mu.Lock()
	fail := key == computerAppsKey && s.failReads > 0
	if fail {
		s.failReads--
	}
	s.mu.Unlock()
	if fail {
		return false, errors.New("connection reset")
	}
	return s.memStore.Setting(ctx, key, out)
}

func (s *flakyAppsStore) stored(t *testing.T) map[string][]approvedApp {
	t.Helper()
	all := map[string][]approvedApp{}
	if _, err := s.memStore.Setting(context.Background(), computerAppsKey, &all); err != nil {
		t.Fatal(err)
	}
	return all
}

// twoMacs is a manager whose settings keep apps always allowed on Macs a and b.
func twoMacs(t *testing.T) (*Manager, *flakyAppsStore) {
	t.Helper()
	st := &flakyAppsStore{memStore: newMemStore()}
	all := map[string][]approvedApp{
		"a": {{BundleID: "com.apple.calculator", Name: "Calculator"}, {BundleID: "com.apple.notes", Name: "Notes"}},
		"b": {{BundleID: "com.apple.safari", Name: "Safari"}},
	}
	if err := st.memStore.PutSetting(context.Background(), computerAppsKey, all); err != nil {
		t.Fatal(err)
	}
	h := newTestManager(t)
	h.store = st
	return h, st
}

// A failed read of the apps always allowed must not be taken for "none": removing one app from Mac a then wrote
// back a list holding only a's entry, dropping every other Mac's apps.
func TestDeleteComputerAppKeepsOtherMacsOnReadError(t *testing.T) {
	h, st := twoMacs(t)
	st.failReads = 1
	r := httptest.NewRequest(http.MethodDelete, "/api/v1/machines/a/computer/apps/com.apple.calculator", nil)
	r.SetPathValue("bundle", "com.apple.calculator")
	w := httptest.NewRecorder()
	h.deleteComputerApp(w, r, &Machine{cfg: MachineConfig{ID: "a"}})
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("read error answered %d, want 500: %s", w.Code, w.Body)
	}
	all := st.stored(t)
	if len(all["b"]) != 1 || len(all["a"]) != 2 {
		t.Fatalf("apps lost on a failed read: %+v", all)
	}

	// Once the read works the app is removed, and only that one.
	w = httptest.NewRecorder()
	h.deleteComputerApp(w, r, &Machine{cfg: MachineConfig{ID: "a"}})
	if w.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", w.Code, w.Body)
	}
	all = st.stored(t)
	if len(all["b"]) != 1 || len(all["a"]) != 1 || all["a"][0].BundleID != "com.apple.notes" {
		t.Fatalf("after delete: %+v", all)
	}
}

// The machine page shows an error, not an empty list, when the apps cannot be read.
func TestGetComputerAppsReadError(t *testing.T) {
	h, st := twoMacs(t)
	st.failReads = 1
	r := httptest.NewRequest(http.MethodGet, "/api/v1/machines/b/computer/apps", nil)
	w := httptest.NewRecorder()
	h.getComputerApps(w, r, &Machine{cfg: MachineConfig{ID: "b"}})
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("read error answered %d, want 500: %s", w.Code, w.Body)
	}
	w = httptest.NewRecorder()
	h.getComputerApps(w, r, &Machine{cfg: MachineConfig{ID: "b"}})
	var out struct {
		Apps []approvedApp `json:"apps"`
	}
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &out) != nil || len(out.Apps) != 1 {
		t.Fatalf("get: %d %s", w.Code, w.Body)
	}
}

// "Always allow" after a failed read keeps nothing new rather than writing a list without the other Macs' apps.
func TestRememberComputerAppsKeepsOtherMacsOnReadError(t *testing.T) {
	h, st := twoMacs(t)
	st.failReads = 1
	m := &Machine{cfg: MachineConfig{ID: "a"}}
	maps := []approvedApp{{BundleID: "com.apple.maps", Name: "Maps"}}
	if err := h.rememberApps(context.Background(), m, maps); err == nil {
		t.Fatal("read error not reported")
	}
	all := st.stored(t)
	if len(all["b"]) != 1 || len(all["a"]) != 2 {
		t.Fatalf("apps lost on a failed read: %+v", all)
	}
	if err := h.rememberApps(context.Background(), m, maps); err != nil {
		t.Fatal(err)
	}
	all = st.stored(t)
	if len(all["b"]) != 1 || len(all["a"]) != 3 || all["a"][2].BundleID != "com.apple.maps" || all["a"][2].At.IsZero() {
		t.Fatalf("after remember: %+v", all)
	}
}

// A stored value that does not decode is not a read error: what decodes stands, and the next write replaces the bad
// part, instead of every read and write failing for good.
func TestComputerAppsRecoverFromAnUndecodableValue(t *testing.T) {
	for name, raw := range map[string]string{
		"one Mac's entry is not a list": `{"a": [{"bundle_id": "com.apple.notes", "name": "Notes"}], "b": 7}`,
		"not JSON":                      `{"a": [`,
	} {
		t.Run(name, func(t *testing.T) {
			st := &flakyAppsStore{memStore: newMemStore()}
			st.memStore.settings[computerAppsKey] = json.RawMessage(raw)
			h := newTestManager(t)
			h.store = st
			m := &Machine{cfg: MachineConfig{ID: "a"}}
			if _, err := h.approvedApps(context.Background()); err != nil {
				t.Fatalf("undecodable value answered %v", err)
			}
			if err := h.rememberApps(context.Background(), m, []approvedApp{{BundleID: "com.apple.maps", Name: "Maps"}}); err != nil {
				t.Fatal(err)
			}
			all := st.stored(t)
			if n := len(all["a"]); n == 0 || all["a"][n-1].BundleID != "com.apple.maps" {
				t.Fatalf("after remember: %+v", all)
			}
		})
	}
}

// An app asked about again because the first read failed is kept once, not added next to the entry already there.
func TestRememberComputerAppsSkipsAppsAlreadyKept(t *testing.T) {
	h, st := twoMacs(t)
	m := &Machine{cfg: MachineConfig{ID: "a"}}
	apps := []approvedApp{{BundleID: "com.apple.notes", Name: "Notes"}, {BundleID: "com.apple.maps", Name: "Maps"}}
	if err := h.rememberApps(context.Background(), m, apps); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, a := range st.stored(t)["a"] {
		ids = append(ids, a.BundleID)
	}
	if want := []string{"com.apple.calculator", "com.apple.notes", "com.apple.maps"}; !slices.Equal(ids, want) {
		t.Fatalf("kept %v, want %v", ids, want)
	}
}

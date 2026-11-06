package assistant

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/nwverse/persona/server/internal/workspaceboard"
)

// settingsBoardStore keeps the settings table in memory next to the board.
type settingsBoardStore struct {
	*memoryBoardStore
	smu      sync.Mutex
	settings map[string][]byte
}

func newSettingsBoardStore() *settingsBoardStore {
	return &settingsBoardStore{memoryBoardStore: newMemoryBoardStore(), settings: map[string][]byte{}}
}
func (s *settingsBoardStore) Setting(_ context.Context, key string, out any) (bool, error) {
	s.smu.Lock()
	defer s.smu.Unlock()
	raw, ok := s.settings[key]
	if !ok {
		return false, nil
	}
	return true, json.Unmarshal(raw, out)
}
func (s *settingsBoardStore) PutSetting(_ context.Context, key string, v any) error {
	raw, err := json.Marshal(v)
	s.smu.Lock()
	defer s.smu.Unlock()
	s.settings[key] = raw
	return err
}
func (s *settingsBoardStore) WorkspaceBoardInUse(context.Context) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state.Revision > 0, nil
}

func boardEnabled(t *testing.T, s *Service, role string) (bool, bool) {
	t.Helper()
	w := boardCall(s, "GET", "/api/v1/workspace-board/settings", "", role)
	if w.Code != 200 {
		t.Fatalf("HTTP %d: %s", w.Code, w.Body)
	}
	var v struct {
		Enabled  bool `json:"enabled"`
		CanWrite bool `json:"can_write"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	return v.Enabled, v.CanWrite
}

func TestWorkspaceBoardOffByDefaultAndSwitchKeepsBoard(t *testing.T) {
	store := newSettingsBoardStore()
	h, _ := newBoardService(t, store, nil)
	// an untouched board (opened once, revision 0) is not "in use"
	if on, can := boardEnabled(t, h, roleFull); on || !can {
		t.Fatalf("new hub: enabled=%v can_write=%v, want off and writable", on, can)
	}
	if w := boardCall(h, "PUT", "/api/v1/workspace-board/settings", `{"enabled":true}`, roleFull); w.Code != 200 {
		t.Fatalf("turn on: HTTP %d %s", w.Code, w.Body)
	}
	if on, _ := boardEnabled(t, h, roleFull); !on {
		t.Fatal("switch not kept")
	}
	before := boardState(t, boardCall(h, "POST", "/api/v1/workspace-board", `{"revision":0,"action":"add_column","title":"Later"}`, roleFull))
	if w := boardCall(h, "PUT", "/api/v1/workspace-board/settings", `{"enabled":false}`, roleFull); w.Code != 200 {
		t.Fatalf("turn off: HTTP %d %s", w.Code, w.Body)
	}
	// the stored switch wins over the board being used, and turning it off keeps the board
	if on, _ := boardEnabled(t, h, roleFull); on {
		t.Fatal("explicit off overridden by board use")
	}
	after := boardState(t, boardCall(h, "GET", "/api/v1/workspace-board", "", roleFull))
	if after.Revision != before.Revision || len(after.Columns) != len(before.Columns) {
		t.Fatalf("board changed by the switch: %+v -> %+v", before, after)
	}
}

func TestWorkspaceBoardUsedBeforeUpgradeCountsAsOn(t *testing.T) {
	store := newSettingsBoardStore()
	store.state = workspaceboard.Default()
	store.state.Revision = 3
	h, _ := newBoardService(t, store, nil)
	if on, _ := boardEnabled(t, h, roleFull); !on {
		t.Fatal("a board used before the switch existed must stay on")
	}
	if _, found := store.settings[workspaceBoardSettingsKey]; found {
		t.Fatal("reading the inferred value must not store it")
	}
}

func TestWorkspaceBoardSettingsRoles(t *testing.T) {
	h, _ := newBoardService(t, newSettingsBoardStore(), nil)
	if _, can := boardEnabled(t, h, roleAnswer); can {
		t.Fatal("answer device reported writable")
	}
	if w := boardCall(h, "PUT", "/api/v1/workspace-board/settings", `{"enabled":true}`, roleAnswer); w.Code != 403 {
		t.Fatalf("answer PUT: HTTP %d", w.Code)
	}
	for _, body := range []string{`{}`, `{"enabled":null}`, `nope`} {
		if w := boardCall(h, "PUT", "/api/v1/workspace-board/settings", body, roleFull); w.Code != 400 {
			t.Fatalf("PUT %s: HTTP %d", body, w.Code)
		}
	}
	if w := boardCall(func() *Service { s, _ := newBoardService(t, nil, nil); return s }(), "GET", "/api/v1/workspace-board/settings", "", roleFull); w.Code != 503 {
		t.Fatalf("no store: HTTP %d", w.Code)
	}
}

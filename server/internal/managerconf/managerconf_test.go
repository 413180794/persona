package managerconf

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// oldHubJSON is a hub.json of the layout before manager.json: the hub's own keys and every manager section.
const oldHubJSON = `{
  "listen": ":18660",
  "token": "hub-token",
  "database_url": "postgres://u:p@[fd00::20]:5432/jarvis",
  "machines": [{"id": "server", "kind": "local"}],
  "assistant": {"owner": "Example"},
  "lark": {"account": 2},
  "lark_gate": [{"method": "POST", "path": "/x", "action": "allow"}],
  "connector_gate": [],
  "github": {"proxy": "http://[fd00::20]:3128"},
  "imessage": {"machine": "mac"},
  "google": {"client_id": "example.apps.googleusercontent.com"},
  "microsoft": {"client_id": "00000000-0000-0000-0000-000000000000"},
  "bots": {"feishu": {"app_id": "cli_example"}},
  "extensions": [{"id": "example"}],
  "plugins": [{"id": "older"}],
  "trusted_extension_markets": ["https://market.example.com"],
  "usage": {"sources": [{"id": "relay", "key": "secret-key"}]},
  "sent_ledger_days": 30
}
`

func readObject(t *testing.T, path string) map[string]json.RawMessage {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]json.RawMessage{}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return out
}

func compact(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// An old-layout home: the first start moves every manager section into manager.json, once, with a marker and a copy
// of hub.json; hub.json is not changed; a second start moves nothing and writes nothing.
func TestMoveTakesTheManagerSectionsOnce(t *testing.T) {
	home := t.TempDir()
	hubPath := filepath.Join(home, "hub.json")
	if err := os.WriteFile(hubPath, []byte(oldHubJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	file := DefaultFile(home)
	rep, err := Move(home, file)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(rep.Moved, Moved) {
		t.Fatalf("moved %v, want every manager section %v", rep.Moved, Moved)
	}
	hub := readObject(t, hubPath)
	mine := readObject(t, file)
	for _, k := range Moved {
		if compact(t, mine[k]) != compact(t, hub[k]) {
			t.Errorf("manager.json's %s = %s, hub.json had %s", k, mine[k], hub[k])
		}
	}
	for _, k := range []string{"listen", "token", "database_url", "machines", "assistant"} {
		if _, ok := mine[k]; ok {
			t.Errorf("manager.json took the hub's %s", k)
		}
	}
	if b, _ := os.ReadFile(hubPath); string(b) != oldHubJSON {
		t.Fatalf("hub.json changed:\n%s", b)
	}
	var m Marker
	if err := json.Unmarshal(mine[MarkerKey], &m); err != nil || !slices.Equal(m.Keys, Moved) || m.At.IsZero() {
		t.Fatalf("marker %s (%v)", mine[MarkerKey], err)
	}
	if b, err := os.ReadFile(rep.Backup); err != nil || string(b) != oldHubJSON || filepath.Dir(rep.Backup) != DefaultDir(home) {
		t.Fatalf("backup %s: %v", rep.Backup, err)
	}
	for _, p := range []string{file, rep.Backup} {
		if st, err := os.Stat(p); err != nil || st.Mode().Perm() != 0o600 {
			t.Fatalf("%s: %v %v", p, st, err)
		}
	}
	if st, err := os.Stat(DefaultDir(home)); err != nil || st.Mode().Perm() != 0o700 {
		t.Fatalf("directory: %v %v", st, err)
	}
	before, _ := os.ReadFile(file)
	stBefore, _ := os.Stat(file)
	// The manager changes its own copy (the admin page's usage sources); the next start keeps it.
	if err := Edit(file, func(o *Object) error { return o.Set("usage", map[string]any{"enabled": false}) }); err != nil {
		t.Fatal(err)
	}
	edited, _ := os.ReadFile(file)
	rep, err = Move(home, file)
	if err != nil || len(rep.Moved) != 0 || rep.Backup != "" || len(rep.Stale) != 0 {
		t.Fatalf("second start: %+v %v", rep, err)
	}
	if after, _ := os.ReadFile(file); string(after) != string(edited) {
		t.Fatalf("second start rewrote manager.json:\n%s", after)
	}
	if string(before) == string(edited) || stBefore == nil {
		t.Fatal("the edit did not land")
	}
	backups, _ := filepath.Glob(filepath.Join(DefaultDir(home), backupPrefix+"*"))
	if len(backups) != 1 {
		t.Fatalf("backups %v", backups)
	}
}

// hub.json edited by hand after the move (as the old docs said), or by an older build switched back: a section
// moved before is not read from hub.json again, and Move says so; one hub.json did not have then is moved now.
func TestMoveAfterHubJSONEdits(t *testing.T) {
	home := t.TempDir()
	hubPath := filepath.Join(home, "hub.json")
	if err := os.WriteFile(hubPath, []byte(`{"token":"t","lark":{"account":2}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	file := DefaultFile(home)
	if _, err := Move(home, file); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hubPath, []byte(`{"token":"t2","lark":{"account":3},"usage":{"sources":[{"id":"x"}]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	rep, err := Move(home, file)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(rep.Stale, []string{"lark"}) || !slices.Equal(rep.Moved, []string{"usage"}) {
		t.Fatalf("report %+v", rep)
	}
	mine := readObject(t, file)
	if compact(t, mine["lark"]) != `{"account":2}` || mine["usage"] == nil {
		t.Fatalf("manager.json %v", mine)
	}
	var m Marker
	if err := json.Unmarshal(mine[MarkerKey], &m); err != nil || m.Backups["lark"] == m.Backups["usage"] || m.Backups["lark"] == "" {
		t.Fatalf("marker %s", mine[MarkerKey])
	}
	// Each section is compared with the copy kept when it moved: the later copy does not hide the lark edit.
	if rep, err := Move(home, file); err != nil || !slices.Equal(rep.Stale, []string{"lark"}) || len(rep.Moved) != 0 {
		t.Fatalf("third start %+v %v", rep, err)
	}
}

// A manager made in the app: its manager.json already has who it is and maybe a section of its own; a hub.json in
// its directory (an older build read its sections there) moves in, without overwriting either.
func TestMoveKeepsWhatManagerJSONHas(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, File)
	if err := os.WriteFile(file, []byte(`{"id":"work","hub":"https://hub.example.com","token":"bus","google":{"client_id":"mine"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hub.json"), []byte(`{"google":{"client_id":"old"},"bots":{"feishu":{}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	rep, err := Move(dir, file)
	if err != nil || !slices.Equal(rep.Moved, []string{"bots"}) {
		t.Fatalf("%+v %v", rep, err)
	}
	mine := readObject(t, file)
	if compact(t, mine["google"]) != `{"client_id":"mine"}` || compact(t, mine["token"]) != `"bus"` || mine["bots"] == nil {
		t.Fatalf("manager.json %v", mine)
	}
	b, _ := os.ReadFile(file)
	if !strings.HasPrefix(strings.TrimSpace(string(b)), `{
  "id": "work"`) {
		t.Fatalf("key order changed:\n%s", b)
	}
}

// No hub.json (a manager elsewhere, or a new one): nothing is written.
func TestMoveWithoutHubJSONWritesNothing(t *testing.T) {
	home := t.TempDir()
	rep, err := Move(home, DefaultFile(home))
	if err != nil || len(rep.Moved) != 0 {
		t.Fatalf("%+v %v", rep, err)
	}
	if _, err := os.Stat(DefaultDir(home)); !os.IsNotExist(err) {
		t.Fatalf("made %s: %v", DefaultDir(home), err)
	}
}

// A section added to Moved later is moved by the next start; those moved before are not taken again.
func TestMoveTakesNewSectionsLater(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "hub.json"), []byte(`{"google":{"client_id":"g"},"later":{"x":1}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	file := DefaultFile(home)
	if _, err := Move(home, file); err != nil {
		t.Fatal(err)
	}
	if err := Edit(file, func(o *Object) error { o.Del("google"); return nil }); err != nil { // the manager dropped it
		t.Fatal(err)
	}
	old := Moved
	Moved = append(slices.Clone(old), "later")
	t.Cleanup(func() { Moved = old })
	rep, err := Move(home, file)
	if err != nil || !slices.Equal(rep.Moved, []string{"later"}) {
		t.Fatalf("%+v %v", rep, err)
	}
	if mine := readObject(t, file); mine["google"] != nil || mine["later"] == nil {
		t.Fatalf("manager.json %v", mine)
	}
}

// Sections hands a manager's struct only the sections: who it is (its bus token) and the marker stay out.
func TestSectionsLeaveIdentityOut(t *testing.T) {
	file := filepath.Join(t.TempDir(), File)
	if err := os.WriteFile(file, []byte(`{"id":"work","token":"bus","lark":{"account":2},"moved_from_hub_json":{"keys":[]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	b, err := Sections(file)
	if err != nil || string(b) != `{"lark":{"account":2}}` {
		t.Fatalf("%s %v", b, err)
	}
	if b, err := Sections(filepath.Join(t.TempDir(), File)); err != nil || string(b) != "{}" {
		t.Fatalf("missing file: %s %v", b, err)
	}
}

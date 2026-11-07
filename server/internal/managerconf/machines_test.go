package managerconf

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The hub's hand-over is kept as the manager's own list, with when it was first taken over; other keys stay, an
// unchanged hand-over writes nothing, and only a list that reads is taken.
func TestKeepMachines(t *testing.T) {
	file := filepath.Join(t.TempDir(), File)
	if _, _, ok, err := OwnMachines(file); ok || err != nil {
		t.Fatalf("no file: ok %v err %v", ok, err)
	}
	if err := os.WriteFile(file, []byte(`{"lark":{"app_id":"x"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, ok, err := OwnMachines(file); ok || err != nil {
		t.Fatalf("nothing taken over yet: ok %v err %v", ok, err)
	}
	list := []json.RawMessage{json.RawMessage(`{"id":"box", "name":"Box","kind":"ssh","ssh_host":"box.example"}`),
		json.RawMessage(`{"id":"lap","name":"Lap","kind":"link"}`)}
	changed, _, replaced, err := KeepMachines(file, list, MachinesFromHub{TailcatDERPMapURL: "https://derp.example/map.json"})
	if err != nil || !changed || replaced != nil {
		t.Fatalf("first: changed %v replaced %v err %v", changed, replaced, err)
	}
	got, from, ok, err := OwnMachines(file)
	if err != nil || !ok || len(got) != 2 || from.At.IsZero() || from.TailcatDERPMapURL != "https://derp.example/map.json" {
		t.Fatalf("read back: %s %+v %v %v", got, from, ok, err)
	}
	b, _ := os.ReadFile(file)
	if !strings.Contains(string(b), `"app_id": "x"`) {
		t.Fatalf("another key was lost:\n%s", b)
	}
	if st, _ := os.Stat(file); st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode())
	}
	before, _ := os.Stat(file)
	if changed, _, _, err := KeepMachines(file, list, MachinesFromHub{TailcatDERPMapURL: "https://derp.example/map.json"}); changed || err != nil {
		t.Fatalf("the same again: changed %v err %v", changed, err)
	}
	if after, _ := os.Stat(file); !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("the same hand-over was written again")
	}
	first := from.At
	if changed, _, _, err := KeepMachines(file, list[:1], MachinesFromHub{Mesh: true}); !changed || err != nil {
		t.Fatalf("a changed one: changed %v err %v", changed, err)
	}
	got, from, _, _ = OwnMachines(file)
	if len(got) != 1 || !from.At.Equal(first) || !from.Mesh {
		t.Fatalf("after the change: %s %+v", got, from)
	}
	// Entries that do not read are left out, the others kept.
	bad := []json.RawMessage{json.RawMessage(`{"name":"no id"}`), json.RawMessage(`{"id":"a"}`), json.RawMessage(`{"id":"a"}`),
		json.RawMessage(`"box"`), json.RawMessage(`{"id":"b"}`)}
	if changed, skipped, _, err := KeepMachines(file, bad, MachinesFromHub{}); !changed || skipped == nil || err != nil {
		t.Fatalf("with entries that do not read: changed %v skipped %v err %v", changed, skipped, err)
	}
	if got, _, ok, err := OwnMachines(file); !ok || err != nil || len(got) != 2 {
		t.Fatalf("kept without them: %s %v %v", got, ok, err)
	}
	// A list edited into something that does not read: an error to read, written over by the next hand-over.
	_ = Edit(file, func(o *Object) error { o.SetRaw(MachinesKey, json.RawMessage(`{"id":"box"}`)); return nil })
	if _, _, _, err := OwnMachines(file); err == nil {
		t.Fatal("a list that does not read was read")
	}
	if changed, _, replaced, err := KeepMachines(file, list, MachinesFromHub{}); !changed || replaced == nil || err != nil {
		t.Fatalf("over a broken one: changed %v replaced %v err %v", changed, replaced, err)
	}
	if got, _, ok, err := OwnMachines(file); !ok || err != nil || len(got) != 2 {
		t.Fatalf("after: %s %v %v", got, ok, err)
	}
	// Another hub's list (the manager joined another one since) is not read.
	if _, _, _, err := KeepMachines(file, list, MachinesFromHub{HubID: "hub-a"}); err != nil {
		t.Fatal(err)
	}
	_ = Edit(file, func(o *Object) error { return o.Set("hub_id", "hub-b") })
	if _, _, ok, err := OwnMachines(file); ok || err != nil {
		t.Fatalf("another hub's list read: ok %v err %v", ok, err)
	}
}

// A manager.json that is a link (to a volume) is written where it leads; the link stays.
func TestKeepMachinesThroughALink(t *testing.T) {
	dir := t.TempDir()
	vol := filepath.Join(dir, "volume", File)
	if err := os.MkdirAll(filepath.Dir(vol), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(vol, []byte(`{"lark":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "managers", File)
	if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(vol, link); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := KeepMachines(link, []json.RawMessage{json.RawMessage(`{"id":"box"}`)}, MachinesFromHub{}); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Lstat(link); err != nil || st.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the link was replaced: %v %v", st, err)
	}
	if b, _ := os.ReadFile(vol); !strings.Contains(string(b), `"box"`) {
		t.Fatalf("not written where the link leads:\n%s", b)
	}
}

// The hub's edits of a manager's own list: put adds or replaces by id, remove takes one out; only a list taken over
// as the manager's own and readable is edited, and each edit gives the list back.
func TestPutAndRemoveMachine(t *testing.T) {
	file := filepath.Join(t.TempDir(), File)
	if _, err := PutMachine(file, json.RawMessage(`{"id":"box"}`)); !errors.Is(err, ErrNotOwnList) {
		t.Fatalf("put into a list never taken over: %v", err)
	}
	box := []json.RawMessage{json.RawMessage(`{"id":"box","name":"Box"}`)}
	if _, _, _, err := KeepMachines(file, box, MachinesFromHub{}); err != nil {
		t.Fatal(err)
	}
	if _, err := PutMachine(file, json.RawMessage(`{"id":"lap"}`)); !errors.Is(err, ErrNotOwnList) {
		t.Fatalf("put into a list not taken over as its own: %v", err)
	}
	if _, _, _, err := KeepMachines(file, box, MachinesFromHub{Own: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := PutMachine(file, json.RawMessage(`{"id":"lap","name":"Lap"}`)); err != nil {
		t.Fatal(err)
	}
	list, err := PutMachine(file, json.RawMessage(`{"id":"box","name":"Box 2"}`))
	if err != nil || len(list) != 2 || !strings.Contains(string(list[0]), `"Box 2"`) || entryID(list[1]) != "lap" {
		t.Fatalf("after the puts: %s %v", list, err)
	}
	for _, bad := range []string{`{"name":"no id"}`, `{"id":" "}`, `{"id":" box"}`, `{"ID":"caps"}`} {
		if _, err := PutMachine(file, json.RawMessage(bad)); err == nil {
			t.Fatalf("put: %s", bad)
		}
	}
	if list, removed, err := RemoveMachine(file, "box"); !removed || err != nil || len(list) != 1 || entryID(list[0]) != "lap" {
		t.Fatalf("remove: %s %v %v", list, removed, err)
	}
	if list, removed, err := RemoveMachine(file, "box"); removed || err != nil || len(list) != 1 {
		t.Fatalf("remove again: %s %v %v", list, removed, err)
	}
	_ = Edit(file, func(o *Object) error { o.SetRaw(MachinesKey, json.RawMessage(`{"id":"x"}`)); return nil })
	if _, err := PutMachine(file, json.RawMessage(`{"id":"lap"}`)); err == nil {
		t.Fatal("put into a list that does not read")
	}
	if _, _, err := RemoveMachine(file, "lap"); err == nil {
		t.Fatal("removed from a list that does not read")
	}
}

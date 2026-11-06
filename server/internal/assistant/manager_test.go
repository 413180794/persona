package assistant

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// manager.json is written 0600 in a 0700 directory and read back; what it must have is checked both ways.
func TestManagerConfigReadWrite(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "managers", "m-abc123")
	c := ManagerConfig{ID: "m-abc123", Name: "工作", Hub: "http://[fd00::20]:18660", HubID: "h1", Token: "jb_x"}
	if err := WriteManagerConfig(dir, c); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(filepath.Join(dir, ManagerFile))
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("file: %v %v", st.Mode(), err)
	}
	if st, _ := os.Stat(dir); st.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode %v", st.Mode())
	}
	got, err := ReadManagerConfig(dir)
	if err != nil || *got != c {
		t.Fatalf("read %+v %v", got, err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("left behind: %v", entries)
	}
	for _, bad := range []ManagerConfig{
		{ID: "assistant", Hub: c.Hub, Token: "t"}, // the hub's own
		{ID: "M-1", Hub: c.Hub, Token: "t"},
		{ID: "m-1", Hub: "[fd00::20]:18660", Token: "t"},
		{ID: "m-1", Hub: c.Hub},
	} {
		if err := WriteManagerConfig(t.TempDir(), bad); err == nil {
			t.Errorf("wrote %+v", bad)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, ManagerFile), []byte(`{"id":"m-1","hub":"http://x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadManagerConfig(dir); err == nil || !strings.Contains(err.Error(), "token") {
		t.Fatalf("no token read as %v", err)
	}
}

// A manager's database is never the hub's: SameDatabase tells the hub's own apart from another on the same server.
func TestSameDatabase(t *testing.T) {
	hub := "postgres://persona:pw@[fd00::20]:5432/persona?sslmode=disable"
	if !SameDatabase("postgres://other@[FD00::20]:5432/persona", hub) {
		t.Fatal("the hub's own database on the same server")
	}
	if SameDatabase("postgres://persona:pw@[fd00::20]:5432/mine", hub) || SameDatabase("postgres://persona@[fd00::21]:5432/persona", hub) ||
		SameDatabase("postgres://persona@[fd00::20]:5433/persona", hub) {
		t.Fatal("another database taken for the hub's")
	}
	// Every name of this machine is one, and 5432 is the port when none is written.
	local := "postgres://persona:pw@127.0.0.1:5432/persona?sslmode=disable"
	for _, same := range []string{"postgres://x@localhost/persona", "postgres://x@[::1]:5432/persona", "postgres://x@127.0.0.2/persona",
		"postgres://x@LOCALHOST:5432/persona"} {
		if !SameDatabase(same, local) {
			t.Errorf("%s is the hub's own database too", same)
		}
	}
	if SameDatabase("postgres://x@localhost:18661/persona", local) {
		t.Error("another port on this machine")
	}
}

// The manager says its own id in manager.hello; the assistant the hub starts says assistant, as before.
func TestBusHelloSaysTheManagersID(t *testing.T) {
	for id, want := range map[string]string{"": AssistantID, "m-0a1b2c": "m-0a1b2c"} {
		b := NewBusHub(BusOptions{ID: id, Hub: "http://[fd00::20]:18660", Token: "t"})
		p, _ := b.client.HelloParams().(map[string]any)
		if p["id"] != want {
			t.Errorf("id %q says %v", id, p["id"])
		}
		if b.client.StopWhenRevoked {
			t.Errorf("StopWhenRevoked without asking")
		}
	}
}

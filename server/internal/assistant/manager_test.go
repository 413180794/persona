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

// Its database: the one manager.json names, else persona_mgr_<id> next to the hub's; never the hub's own, and none
// at all on a computer without a hub and without one named.
func TestManagerDatabase(t *testing.T) {
	hub := "postgres://jarvis:pw@[fd00::20]:5432/jarvis?sslmode=disable"
	c := ManagerConfig{ID: "m-0a1b2c"}
	if ManagerDatabase(c.ID) != "persona_mgr_m_0a1b2c" {
		t.Fatal(ManagerDatabase(c.ID))
	}
	got, err := c.Database(hub)
	if err != nil || got != "postgres://jarvis:pw@[fd00::20]:5432/persona_mgr_m_0a1b2c?sslmode=disable" {
		t.Fatalf("derived %q %v", got, err)
	}
	if _, err := c.Database(""); err == nil || !strings.Contains(err.Error(), "--database-url") {
		t.Fatalf("no hub: %v", err)
	}
	c.DatabaseURL = "postgres://me@[fd00::21]:5432/mine"
	if got, err := c.Database(""); err != nil || got != c.DatabaseURL {
		t.Fatalf("own %q %v", got, err)
	}
	c.DatabaseURL = "postgres://other@[FD00::20]:5432/jarvis"
	if _, err := c.Database(hub); err == nil {
		t.Fatal("the hub's own database was taken")
	}
	if ManagerDatabase("m-0a1b2c") == ManagerDatabase("m-0a1b2d") {
		t.Fatal("two managers, one database")
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

//go:build !windows

package node

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The manager-folder check never hangs or opens what it should not: a FIFO named manager.json beside a project's
// key is not waited on; a manager.json that does not read counts as a manager's (fail closed); a link into the folder
// is refused like the folder; a link into iCloud Drive is refused before anything behind it is opened. The folder is
// read-only to clients whole, unless it is the home itself.
func TestPathGuardManagerDirEdges(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PERSONA_HOME", filepath.Join(t.TempDir(), "persona-state"))
	t.Setenv("PERSONA_HOME", "")
	n := &Node{home: home}
	write := func(p, body string) {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	certs := filepath.Join(home, "code", "proj", "certs")
	write(filepath.Join(certs, "dev.pem"), "pem")
	if err := syscall.Mkfifo(filepath.Join(certs, "manager.json"), 0o600); err != nil {
		t.Skip("no FIFOs here:", err)
	}
	done := make(chan error, 1)
	go func() { _, err := n.readFile(filepath.Join(certs, "dev.pem"), "", 0, false); done <- err }()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("a project's key beside a FIFO named manager.json: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("hung on a FIFO named manager.json")
	}

	// An empty manager.json, or a list, is a project's: its folder's keys stay readable and the folder writable.
	stray := filepath.Join(home, "code", "stray")
	write(filepath.Join(stray, "dev.pem"), "pem")
	for _, body := range []string{"", "[1]", "// notes"} {
		write(filepath.Join(stray, "manager.json"), body)
		if _, err := n.readFile(filepath.Join(stray, "dev.pem"), "", 0, false); err != nil {
			t.Errorf("%q: a project's key beside a stray manager.json: %v", body, err)
		}
		if err := n.writable(stray); err != nil {
			t.Errorf("%q: a project folder with a stray manager.json: %v", body, err)
		}
	}

	broken := filepath.Join(home, "mgr-broken")
	write(filepath.Join(broken, "manager.json"), `{"id":"m-1","hub":"https://hub.example.com","token":"jb_secret",`)
	write(filepath.Join(broken, "secret.key"), "k")
	for _, p := range []string{filepath.Join(broken, "secret.key"), filepath.Join(broken, "manager.json")} {
		if _, err := n.readFile(p, "", 0, false); !errors.Is(err, errOffLimits) {
			t.Errorf("read %s: %v, want it refused", p, err)
		}
	}

	// A manager.json longer than is read, its token past it: a manager's.
	big := filepath.Join(home, "mgr-big")
	write(filepath.Join(big, "manager.json"), `{"extensions":[{"config":{"x":"`+strings.Repeat("a", 1<<20)+`"}}],"hub":"h","token":"t"}`)
	write(filepath.Join(big, "secret.key"), "k")
	if _, err := n.readFile(filepath.Join(big, "secret.key"), "", 0, false); !errors.Is(err, errOffLimits) {
		t.Errorf("a big manager.json's folder: %v", err)
	}

	mgr := filepath.Join(t.TempDir(), "mgr2")
	write(filepath.Join(mgr, "manager.json"), `{"id":"m-2","hub":"https://hub.example.com","token":"jb_secret"}`)
	write(filepath.Join(mgr, "secret.key"), "k")
	if err := os.Symlink(mgr, filepath.Join(home, "mgr-link")); err != nil {
		t.Fatal(err)
	}
	if _, err := n.readFile("~/mgr-link/secret.key", "", 0, false); !errors.Is(err, errOffLimits) {
		t.Errorf("read through a link into a manager's folder: %v", err)
	}
	// Its tls/ a link elsewhere: not followed.
	certs2 := filepath.Join(t.TempDir(), "certs")
	write(filepath.Join(certs2, "hub.pem"), "pem")
	if err := os.Symlink(certs2, filepath.Join(mgr, "tls")); err != nil {
		t.Fatal(err)
	}
	if _, err := n.readFile(filepath.Join(mgr, "tls", "hub.pem"), "", 0, false); !errors.Is(err, errOffLimits) {
		t.Errorf("a manager's tls/ that is a link: %v", err)
	}
	if err := os.Remove(filepath.Join(mgr, "tls")); err != nil {
		t.Fatal(err)
	}
	// A path that is not there, climbing into iCloud Drive once cleaned: nothing there is opened.
	if err := os.Symlink("nope/../../Library/Mobile Documents/notes/secret.key", filepath.Join(home, "code", "l")); err != nil {
		t.Fatal(err)
	}
	if _, err := n.realPath(filepath.Join(home, "code", "l")); errors.Is(err, errOffLimits) {
		t.Errorf("a missing path that climbs was looked behind: %v", err)
	}
	// A path in it that is not there (a Git panel reading a commit's file gone from the working tree) is refused by
	// name too.
	if _, err := n.realPath(filepath.Join(mgr, "tls", "hub.pem")); !errors.Is(err, errOffLimits) {
		t.Errorf("a missing tls/ of a manager's folder: %v", err)
	}

	cloud := filepath.Join(home, "Library", "Mobile Documents", "notes")
	write(filepath.Join(cloud, "dev.pem"), "pem")
	if err := os.Symlink(cloud, filepath.Join(home, "code", "notes")); err != nil {
		t.Fatal(err)
	}
	if _, err := n.readFile("~/code/notes/dev.pem", "", 0, false); !errors.Is(err, errICloud) {
		t.Errorf("read through a link into iCloud Drive: %v", err)
	}

	inHome := filepath.Join(home, "mgr3")
	write(filepath.Join(inHome, "manager.json"), `{"id":"m-3","hub":"https://hub.example.com","token":"jb_secret"}`)
	if err := os.MkdirAll(filepath.Join(inHome, "extensions", "x"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := n.writable(filepath.Join(inHome, "extensions", "x")); err == nil {
		t.Error("a manager's folder in the home was writable")
	}
	if err := n.writable(filepath.Join(home, "code", "proj")); err != nil {
		t.Errorf("a project folder: %v", err)
	}
	// A name to write behind a link into iCloud Drive is not looked into; nor may a client write a manager.json.
	cloudMgr := filepath.Join(home, "Library", "Mobile Documents", "mgr")
	write(filepath.Join(cloudMgr, "manager.json"), `{"id":"m-5","hub":"https://hub.example.com","token":"jb_secret"}`)
	if err := os.Symlink(cloudMgr, filepath.Join(home, "code", "proj", "cloudmgr")); err != nil {
		t.Fatal(err)
	}
	if err := n.writable(filepath.Join(home, "code", "proj", "cloudmgr")); err != nil {
		t.Errorf("a link's name in a project folder was looked through: %v", err)
	}
	if err := n.writable(filepath.Join(home, "code", "proj", "manager.json")); err == nil {
		t.Error("a client may write a manager.json")
	}
	write(filepath.Join(home, "manager.json"), `{"id":"m-4","hub":"https://hub.example.com","token":"jb_secret"}`)
	if err := n.writable(filepath.Join(home, "code", "proj")); err != nil {
		t.Errorf("a manager whose folder is the home: %v", err)
	}
}

// The extensions' data is refused wherever it is kept: extensions/data/, what an uninstall or an upgrade sets aside in
// extensions/staging/, the plugins/ folder of a hub from before #1844; the folders themselves too (their listing names
// the extensions); and a link standing where such a folder, or a folder on the way to one, is named is not followed.
func TestPathGuardExtensionsData(t *testing.T) {
	home := t.TempDir()
	state := filepath.Join(t.TempDir(), "persona-state")
	t.Setenv("PERSONA_HOME", state)
	t.Setenv("JARVIS_HOME", "")
	n := &Node{home: home}
	write := func(p string) {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(`{"token":"x"}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	mgr := filepath.Join(t.TempDir(), "mgr2")
	write(filepath.Join(mgr, "manager.json"))
	if err := os.WriteFile(filepath.Join(mgr, "manager.json"), []byte(`{"id":"m","hub":"https://hub.example.com","token":"jb"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	elsewhere := filepath.Join(t.TempDir(), "elsewhere")
	write(filepath.Join(elsewhere, "data", "notes", "token.json"))
	refused := []string{
		filepath.Join(state, "extensions", "data", "notes", "token.json"),
		filepath.Join(state, "extensions", "staging", "uninstall-notes-1", "0", "token.json"),
		filepath.Join(state, "extensions", "staging", "plan-2", "data", "token.json"),
		filepath.Join(state, "plugins", "data", "notes", "token.json"),
		filepath.Join(state, "plugins", "notes", "data", "token.json"), // a node's from before #1921
		filepath.Join(state, "node-plugins", "notes", "data", "session.json"),
		filepath.Join(mgr, "extensions", "data", "notes", "token.json"),
		filepath.Join(mgr, "extensions", "staging", "uninstall-notes-1", "0", "token.json"),
	}
	for _, p := range refused {
		write(p)
		if _, err := n.readFile(p, "", 0, false); !errors.Is(err, errOffLimits) {
			t.Errorf("read %s: %v, want it refused", p, err)
		}
	}
	for _, d := range []string{filepath.Join(state, "extensions", "data"), filepath.Join(mgr, "extensions", "data")} {
		if _, err := n.listDir(d, ""); !errors.Is(err, errOffLimits) {
			t.Errorf("list %s: %v, want it refused", d, err)
		}
	}
	if _, err := n.listDir(filepath.Join(state, "extensions"), ""); err != nil {
		t.Errorf("list the extensions folder: %v", err)
	}
	// Links standing where extensions/ or extensions/data/ are named, in the data folder and in a manager's.
	if err := os.RemoveAll(filepath.Join(mgr, "extensions")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, filepath.Join(mgr, "extensions")); err != nil {
		t.Fatal(err)
	}
	if _, err := n.readFile(filepath.Join(mgr, "extensions", "data", "notes", "token.json"), "", 0, false); !errors.Is(err, errOffLimits) {
		t.Errorf("read through a manager's extensions link: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(state, "extensions", "data")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(elsewhere, "data"), filepath.Join(state, "extensions", "data")); err != nil {
		t.Fatal(err)
	}
	if _, err := n.readFile(filepath.Join(state, "extensions", "data", "notes", "token.json"), "", 0, false); !errors.Is(err, errOffLimits) {
		t.Errorf("read through the data folder's extensions/data link: %v", err)
	}
	// A project's own folders of these names, and a link named data elsewhere, stay readable.
	project := filepath.Join(home, "code", "app")
	write(filepath.Join(project, "extensions", "data", "fixture.json"))
	if _, err := n.readFile(filepath.Join(project, "extensions", "data", "fixture.json"), "", 0, false); err != nil {
		t.Errorf("a project's extensions/data: %v", err)
	}
	if err := os.Symlink(filepath.Join(elsewhere, "data"), filepath.Join(project, "data")); err != nil {
		t.Fatal(err)
	}
	if _, err := n.readFile(filepath.Join(project, "data", "notes", "token.json"), "", 0, false); err != nil {
		t.Errorf("a project's link named data: %v", err)
	}
}

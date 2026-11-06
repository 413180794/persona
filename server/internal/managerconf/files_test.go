package managerconf

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func filesMarker(t *testing.T, home string) FilesMarker {
	t.Helper()
	o, err := ParseObject([]byte(readFile(t, DefaultFile(home))))
	if err != nil {
		t.Fatal(err)
	}
	var m FilesMarker
	raw, ok := o.Get(FilesMarkerKey)
	if !ok || json.Unmarshal(raw, &m) != nil {
		t.Fatalf("no marker in manager.json: %s", readFile(t, DefaultFile(home)))
	}
	return m
}

// The built-in manager's keys are copied from the data directory into its own, once: the data directory keeps them
// (an older build switched back reads them there), a copy already there is never replaced, and manager.json keeps what
// was copied (its other keys untouched).
func TestMoveFilesCopiesTheKeysOnce(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, SecretKey), "k1")
	writeFile(t, filepath.Join(home, TailcatKey), `{"key":"t1"}`)
	writeFile(t, DefaultFile(home), `{"lark": {"account": 2}}`)

	copied, err := MoveFiles(home)
	if err != nil || !slices.Equal(copied, []string{SecretKey, TailcatKey}) {
		t.Fatalf("copied %v %v", copied, err)
	}
	for name, want := range map[string]string{SecretKey: "k1", TailcatKey: `{"key":"t1"}`} {
		if got := readFile(t, filepath.Join(DefaultDir(home), name)); got != want {
			t.Fatalf("%s: %q", name, got)
		}
		if got := readFile(t, filepath.Join(home, name)); got != want {
			t.Fatalf("the data directory's %s changed: %q", name, got)
		}
		st, err := os.Stat(filepath.Join(DefaultDir(home), name))
		if err != nil || st.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode %v %v", name, st.Mode(), err)
		}
	}
	if m := filesMarker(t, home); !slices.Equal(m.Files, []string{SecretKey, TailcatKey}) || m.At.IsZero() {
		t.Fatalf("marker %+v", m)
	}
	if o, _ := ParseObject([]byte(readFile(t, DefaultFile(home)))); o != nil {
		if _, ok := o.Get("lark"); !ok {
			t.Fatal("manager.json lost its sections")
		}
	}

	// The manager's copies are its own from now on; a key that appears in the data directory later is copied then.
	writeFile(t, filepath.Join(DefaultDir(home), SecretKey), "rotated")
	writeFile(t, filepath.Join(home, HandlesKey), "h1")
	copied, err = MoveFiles(home)
	if err != nil || !slices.Equal(copied, []string{HandlesKey}) {
		t.Fatalf("again: %v %v", copied, err)
	}
	if got := readFile(t, filepath.Join(DefaultDir(home), SecretKey)); got != "rotated" {
		t.Fatalf("the manager's own key replaced: %q", got)
	}
	if m := filesMarker(t, home); !slices.Equal(m.Files, []string{SecretKey, TailcatKey, HandlesKey}) {
		t.Fatalf("marker %+v", m)
	}
	if copied, err := MoveFiles(home); err != nil || copied != nil {
		t.Fatalf("nothing left: %v %v", copied, err)
	}
}

// A fresh install has nothing to copy: manager.json is not written, and the keys are made in the manager's directory.
func TestMoveFilesOnAFreshInstall(t *testing.T) {
	home := t.TempDir()
	if copied, err := MoveFiles(home); err != nil || copied != nil {
		t.Fatalf("%v %v", copied, err)
	}
	if _, err := os.Stat(DefaultFile(home)); !os.IsNotExist(err) {
		t.Fatalf("manager.json written: %v", err)
	}
	for _, name := range OwnFiles {
		if got, want := OwnFile(home, name), filepath.Join(DefaultDir(home), name); got != want {
			t.Fatalf("%s: %s, want %s", name, got, want)
		}
	}
}

// A key the move could not copy is read where it was: a new one beside it would not open what it sealed.
func TestOwnFileFallsBackToTheDataDirectory(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, SecretKey), "k1")
	if got := OwnFile(home, SecretKey); got != filepath.Join(home, SecretKey) {
		t.Fatalf("before the copy: %s", got)
	}
	if _, err := MoveFiles(home); err != nil {
		t.Fatal(err)
	}
	if got := OwnFile(home, SecretKey); got != filepath.Join(DefaultDir(home), SecretKey) {
		t.Fatalf("after the copy: %s", got)
	}
}

// The knowledge base stays in the data directory's data/ unless manager.json says where.
func TestKBDir(t *testing.T) {
	home := t.TempDir()
	if got := KBDir(home); got != filepath.Join(home, "data") {
		t.Fatalf("default %s", got)
	}
	writeFile(t, DefaultFile(home), `{"kb_dir": "/srv/kb"}`)
	if got := KBDir(home); got != "/srv/kb" {
		t.Fatalf("absolute %s", got)
	}
	writeFile(t, DefaultFile(home), `{"kb_dir": "kb"}`)
	if got := KBDir(home); got != filepath.Join(DefaultDir(home), "kb") {
		t.Fatalf("relative %s", got)
	}
	writeFile(t, DefaultFile(home), `{"kb_dir": ""}`)
	if got := KBDir(home); got != filepath.Join(home, "data") {
		t.Fatalf("empty %s", got)
	}
	writeFile(t, DefaultFile(home), `not json`)
	if got := KBDir(home); got != filepath.Join(home, "data") {
		t.Fatalf("unreadable %s", got)
	}
}

// One key that cannot be copied does not stop the others, and what was copied is recorded; the manager's directory is
// there for the keys it makes itself even when nothing is copied.
func TestMoveFilesGoesOnPastAFailure(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, SecretKey), "k1")
	if err := os.Mkdir(filepath.Join(home, TailcatKey), 0o700); err != nil { // not a file: it does not read
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(home, HandlesKey), "h1")
	copied, err := MoveFiles(home)
	if err == nil || !slices.Equal(copied, []string{SecretKey, HandlesKey}) {
		t.Fatalf("copied %v, err %v", copied, err)
	}
	if m := filesMarker(t, home); !slices.Equal(m.Files, []string{SecretKey, HandlesKey}) {
		t.Fatalf("marker %+v", m)
	}

	fresh := t.TempDir()
	if _, err := MoveFiles(fresh); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(DefaultDir(fresh)); err != nil || !st.IsDir() || st.Mode().Perm() != 0o700 {
		t.Fatalf("the manager's directory on a fresh install: %v %v", st, err)
	}
}

// A file put in place meanwhile (the manager making its own key) is not replaced by the copy.
func TestPlaceNewKeepsWhatIsThere(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, SecretKey)
	if placed, err := placeNew(path, []byte("first")); err != nil || !placed {
		t.Fatalf("%v %v", placed, err)
	}
	if placed, err := placeNew(path, []byte("second")); err != nil || placed {
		t.Fatalf("placed over it: %v %v", placed, err)
	}
	if got := readFile(t, path); got != "first" {
		t.Fatalf("%q", got)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "*.tmp-*")); len(left) != 0 {
		t.Fatalf("temporary files left: %v", left)
	}
}

// A temporary copy a crash left (it holds a whole key) is removed at the next start; one that may be another process's
// on its way is not.
func TestMoveFilesRemovesStaleCopies(t *testing.T) {
	home := t.TempDir()
	stale := filepath.Join(DefaultDir(home), SecretKey+".tmp-123")
	fresh := filepath.Join(DefaultDir(home), TailcatKey+".tmp-456")
	writeFile(t, stale, "k")
	writeFile(t, fresh, "t")
	old := time.Now().Add(-2 * staleCopyAge)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	if _, err := MoveFiles(home); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale copy kept: %v", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("a copy on its way removed: %v", err)
	}
}

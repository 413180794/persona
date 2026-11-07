package testhome

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

// quiet is Main without the message about a write to ~/.persona, which these tests provoke on purpose.
func quiet(run func() int) int { return runIn(run, io.Discard) }

// The tests run with an empty HOME and no PERSONA_HOME, and both come back afterwards.
func TestMainGivesAnEmptyHomeAndRestoresTheEnvironment(t *testing.T) {
	t.Setenv("HOME", "/real/home")
	t.Setenv("PERSONA_HOME", "/real/state")
	t.Setenv(isolated, "")
	_ = os.Unsetenv(isolated)
	var home string
	code := quiet(func() int {
		home = os.Getenv("HOME")
		if home == "/real/home" || os.Getenv("PERSONA_HOME") != "" {
			t.Errorf("HOME=%q PERSONA_HOME=%q", home, os.Getenv("PERSONA_HOME"))
		}
		if entries, err := os.ReadDir(home); err != nil || len(entries) != 0 {
			t.Errorf("not an empty directory: %v %v", entries, err)
		}
		return 0
	})
	if code != 0 {
		t.Fatalf("exit code %d", code)
	}
	if os.Getenv("HOME") != "/real/home" || os.Getenv("PERSONA_HOME") != "/real/state" {
		t.Fatalf("not restored: HOME=%q PERSONA_HOME=%q", os.Getenv("HOME"), os.Getenv("PERSONA_HOME"))
	}
	if _, ok := os.LookupEnv(isolated); ok {
		t.Fatal("the marker is still set")
	}
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Fatalf("the throw-away home is still there: %v", err)
	}
}

// Anything written under <HOME>/.persona fails the run, even when the tests passed; state under PERSONA_HOME does not.
func TestMainFailsWhenTheDefaultStateDirectoryIsWritten(t *testing.T) {
	t.Setenv("HOME", "/real/home")
	t.Setenv("PERSONA_HOME", "")
	t.Setenv(isolated, "")
	_ = os.Unsetenv(isolated)
	write := func(dir string) {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "manager.json"), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if code := quiet(func() int { write(filepath.Join(os.Getenv("HOME"), ".persona", "managers")); return 0 }); code != 1 {
		t.Fatalf("a write to the default directory: exit code %d", code)
	}
	if code := quiet(func() int { _ = os.MkdirAll(filepath.Join(os.Getenv("HOME"), ".persona"), 0o700); return 0 }); code != 1 {
		t.Fatalf("an empty default directory: exit code %d", code)
	}
	if code := quiet(func() int { return 3 }); code != 3 {
		t.Fatalf("a failing run keeps its code: %d", code)
	}
	if code := quiet(func() int {
		own := filepath.Join(t.TempDir(), "state")
		t.Setenv("PERSONA_HOME", own)
		write(own)
		return 0
	}); code != 0 {
		t.Fatalf("state under PERSONA_HOME: exit code %d", code)
	}
}

// A test binary a test starts again keeps the HOME and PERSONA_HOME it was given.
func TestMainLeavesAChildAlone(t *testing.T) {
	t.Setenv(isolated, "/parent/home")
	t.Setenv("HOME", "/parent/home")
	t.Setenv("PERSONA_HOME", "/parent/state")
	code := quiet(func() int {
		if os.Getenv("HOME") != "/parent/home" || os.Getenv("PERSONA_HOME") != "/parent/state" {
			t.Errorf("replaced: HOME=%q PERSONA_HOME=%q", os.Getenv("HOME"), os.Getenv("PERSONA_HOME"))
		}
		return 0
	})
	if code != 0 {
		t.Fatalf("exit code %d", code)
	}
}

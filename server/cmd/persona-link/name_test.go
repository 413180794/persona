package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCommandName(t *testing.T) {
	for _, arg0 := range []string{"/home/me/.persona/bin/persona-link", "/Applications/Persona Link.app/Contents/Helpers/persona-link", "persona-link", "/tmp/persona-link-linux-amd64", "/tmp/custom-name"} {
		for _, override := range []string{"", "persona-link", "custom-name"} {
			if got := commandName(arg0, func(string) string { return override }); got != "persona-link" {
				t.Errorf("commandName(%q, %q) = %q", arg0, override, got)
			}
		}
	}
}

func TestInstalledCommandIsOneRealBinary(t *testing.T) {
	t.Setenv("PERSONA_HOME", "")
	f := newFakeEnv(t)
	path, copied, err := f.installBinary()
	if err != nil || !copied || path != f.binPath() {
		t.Fatalf("install: path=%q copied=%v err=%v", path, copied, err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 || entries[0].Name() != f.exeName("persona-link") {
		t.Fatalf("install created another command: %v %v", entries, err)
	}
	st, err := os.Lstat(path)
	if err != nil || !st.Mode().IsRegular() {
		t.Fatal("installed command is not a real binary", err)
	}
	f.exe = func() (string, error) { return path, nil }
	if got, copied, err := f.installBinary(); err != nil || copied || got != path {
		t.Fatalf("repeat install: path=%q copied=%v err=%v", got, copied, err)
	}
}

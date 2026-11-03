package identity

import (
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
)

func cleanRootEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"PERSONA_HOME", "JARVIS_HOME"} {
		value, exists := os.LookupEnv(key)
		os.Unsetenv(key)
		t.Cleanup(func() {
			if exists {
				os.Setenv(key, value)
			} else {
				os.Unsetenv(key)
			}
		})
	}
}

func TestEnvironmentCompatibility(t *testing.T) {
	cleanRootEnv(t)
	t.Setenv("JARVIS_HOME", "legacy")
	if Getenv("PERSONA_HOME") != "legacy" {
		t.Fatal("legacy environment was ignored")
	}
	t.Setenv("PERSONA_HOME", "current")
	if Getenv("JARVIS_HOME") != "current" {
		t.Fatal("current environment must take precedence")
	}
	t.Setenv("PERSONA_HOME", "")
	if value, ok := LookupEnv("JARVIS_HOME"); value != "" || !ok {
		t.Fatal("explicit empty current value must not expose a legacy secret")
	}
	base := []string{"PERSONA_SESSION_ID=wrong", "JARVIS_SESSION_ID=older", "PATH=first", "PATH=second"}
	want := []string{"PERSONA_SESSION_ID=right", "JARVIS_SESSION_ID=right", "PATH=second"}
	if got := Environment(base, "JARVIS_SESSION_ID=right"); !reflect.DeepEqual(got, want) {
		t.Fatalf("environment: %v", got)
	}
	for _, updates := range [][]string{{"PERSONA_SESSION_ID=right", "JARVIS_SESSION_ID=obsolete"}, {"JARVIS_SESSION_ID=obsolete", "PERSONA_SESSION_ID=right"}} {
		if got := Environment(base, updates...); !reflect.DeepEqual(got, want) {
			t.Fatalf("new/old environment precedence depends on order: %v", got)
		}
	}
	t.Setenv("JARVIS_PERSONA_ID", "legacy-avatar")
	t.Setenv("PERSONA_PERSONA_ID", "current-avatar")
	if Getenv("JARVIS_PERSONA_ID") != "current-avatar" {
		t.Fatal("brand prefixes in an environment suffix were removed twice")
	}
}

func TestStateDirectoryCompatibility(t *testing.T) {
	cleanRootEnv(t)
	for _, scenario := range []string{"fresh", "legacy", "current", "legacy-external", "current-external", "forward-dangling", "reverse-dangling", "both", "both-aliases", "dangling-external", "file", "cycle"} {
		t.Run(scenario, func(t *testing.T) {
			home := t.TempDir()
			current, legacy := filepath.Join(home, ".persona"), filepath.Join(home, ".jarvis")
			mkdir := func(path string) {
				t.Helper()
				if err := os.MkdirAll(path, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			link := func(target, path string) {
				t.Helper()
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			}
			want := current
			switch scenario {
			case "legacy":
				mkdir(legacy)
			case "current":
				mkdir(current)
			case "legacy-external":
				external := t.TempDir()
				link(external, legacy)
			case "current-external":
				external := t.TempDir()
				link(external, current)
			case "forward-dangling":
				link(".persona", legacy)
			case "reverse-dangling":
				link(".jarvis", current)
			case "both":
				mkdir(current)
				mkdir(legacy)
				want = legacy
			case "both-aliases":
				mkdir(legacy)
				link(".jarvis", current)
			case "dangling-external":
				link(filepath.Join(home, "missing-external"), legacy)
			case "file":
				if err := os.WriteFile(legacy, []byte("not a directory"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "cycle":
				link(".persona", legacy)
				link(".jarvis", current)
			}
			root, err := PrepareHome(home)
			if scenario == "dangling-external" || scenario == "file" || scenario == "cycle" {
				if err == nil {
					t.Fatal("invalid existing state must not start a new store")
				}
				return
			}
			if err != nil || root != want {
				t.Fatalf("root=%s want=%s err=%v", root, want, err)
			}
			if scenario == "both" {
				if a, _ := os.Stat(current); !a.IsDir() {
					t.Fatal("current directory was overwritten")
				}
				return
			}
			payload := []byte("unchanged token and state")
			if err := os.WriteFile(filepath.Join(legacy, "hub.json"), payload, 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(filepath.Join(current, "hub.json"))
			if err != nil || string(got) != string(payload) {
				t.Fatalf("old/new state diverged: %q %v", got, err)
			}
			if _, err := PrepareHome(home); err != nil {
				t.Fatal("repeat upgrade:", err)
			}
			if err := os.WriteFile(filepath.Join(current, "node.sock"), []byte("socket placeholder"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(legacy, "node.sock")); err != nil {
				t.Fatal("old node cannot find the new socket", err)
			}
		})
	}
}

func TestIndependentHomes(t *testing.T) {
	cleanRootEnv(t)
	home := t.TempDir()
	root := filepath.Join(t.TempDir(), "profile")
	t.Setenv("JARVIS_HOME", root)
	if got, err := PrepareHome(home); err != nil || got != root {
		t.Fatalf("%s %v", got, err)
	}
	if _, err := os.Lstat(filepath.Join(home, ".jarvis")); !os.IsNotExist(err) {
		t.Fatal("custom environment changed default state")
	}
	if !IsAssistantDir(filepath.Join(root, "assistant")) {
		t.Fatal("custom assistant identity lost")
	}
	for _, path := range []string{"/tmp/.jarvis/assistant", "/tmp/.persona/assistant"} {
		if !IsAssistantDir(path) {
			t.Fatal(path)
		}
	}
	if IsAssistantDir("/tmp/project/assistant") {
		t.Fatal("ordinary project became the assistant")
	}
}

func TestConcurrentFirstRun(t *testing.T) {
	cleanRootEnv(t)
	home := t.TempDir()
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := PrepareHome(home); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	a, err := os.Stat(filepath.Join(home, ".persona"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.Stat(filepath.Join(home, ".jarvis"))
	if err != nil || !os.SameFile(a, b) {
		t.Fatal("concurrent first run split state", err)
	}
}

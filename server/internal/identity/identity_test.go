package identity

import (
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/nwverse/persona/server/internal/compat"
)

func cleanRootEnv(t *testing.T) {
	t.Helper()
	current, legacy := compat.EnvNames("PERSONA_HOME")
	for _, key := range []string{current, legacy} {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
}

func TestEnvironment(t *testing.T) {
	cleanRootEnv(t)
	if value, ok := LookupEnv("PERSONA_HOME"); value != "" || ok {
		t.Fatal("unset variable must remain absent")
	}
	t.Setenv("OTHER_HOME", "unrelated")
	if value, ok := LookupEnv("PERSONA_HOME"); value != "" || ok {
		t.Fatal("unrelated variables must not supply a root")
	}
	t.Setenv("PERSONA_HOME", "profile")
	if Getenv("PERSONA_HOME") != "profile" {
		t.Fatal("configured environment was ignored")
	}
	t.Setenv("PERSONA_HOME", "")
	if value, ok := LookupEnv("PERSONA_HOME"); value != "" || !ok {
		t.Fatal("explicit empty values must remain present")
	}
	_, legacySession := compat.EnvNames("PERSONA_SESSION_ID")
	base := []string{"PERSONA_SESSION_ID=wrong", "PERSONA_SESSION_ID=older", "PATH=first", "PATH=second", "invalid"}
	for _, updates := range [][]string{{"PERSONA_SESSION_ID=right"}, {"PERSONA_SESSION_ID=obsolete", "PERSONA_SESSION_ID=right", "invalid"}} {
		want := []string{"PERSONA_SESSION_ID=right", "PATH=second", legacySession + "=right"}
		if got := Environment(base, updates...); !reflect.DeepEqual(got, want) {
			t.Fatalf("environment: %v", got)
		}
	}
	if got := Environment(base, "PERSONA_SESSION_ID=", "NEW=value=with=equals"); !reflect.DeepEqual(got, []string{"PERSONA_SESSION_ID=", "PATH=second", legacySession + "=", "NEW=value=with=equals"}) {
		t.Fatalf("empty values or new keys lost: %v", got)
	}
	t.Setenv("PERSONA_PERSONA_ID", "avatar")
	if Getenv("PERSONA_PERSONA_ID") != "avatar" {
		t.Fatal("environment suffix must not be rewritten")
	}
}

func TestStateDirectory(t *testing.T) {
	cleanRootEnv(t)
	for _, scenario := range []string{"fresh", "existing", "external", "relative-external", "dangling-external", "file", "symlink-file", "cycle"} {
		t.Run(scenario, func(t *testing.T) {
			home := t.TempDir()
			root := filepath.Join(home, ".persona")
			external := filepath.Join(home, "external")
			mkdir := func(path string) {
				t.Helper()
				if err := os.MkdirAll(path, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			link := func(target string) {
				t.Helper()
				if err := os.Symlink(target, root); err != nil {
					t.Fatal(err)
				}
			}
			switch scenario {
			case "existing":
				mkdir(root)
			case "external":
				mkdir(external)
				link(external)
			case "relative-external":
				mkdir(external)
				link("external")
			case "dangling-external":
				link(external)
			case "file":
				if err := os.WriteFile(root, []byte("not a directory"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "symlink-file":
				if err := os.WriteFile(external, []byte("not a directory"), 0o600); err != nil {
					t.Fatal(err)
				}
				link(external)
			case "cycle":
				link(".persona")
			}
			got, err := PrepareHome(home)
			if scenario == "dangling-external" || scenario == "file" || scenario == "symlink-file" || scenario == "cycle" {
				if err == nil {
					t.Fatal("invalid state directory was accepted")
				}
				if _, err := os.Lstat(root); err != nil {
					t.Fatal("invalid existing state was removed", err)
				}
				if scenario == "dangling-external" {
					if _, err := os.Lstat(external); !os.IsNotExist(err) {
						t.Fatal("dangling target was created", err)
					}
				}
				return
			}
			if err != nil || got != root {
				t.Fatalf("root=%s want=%s err=%v", got, root, err)
			}
			payload := []byte("unchanged token and state")
			if err := os.WriteFile(filepath.Join(root, "hub.json"), payload, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := PrepareHome(home); err != nil {
				t.Fatal("repeat initialization:", err)
			}
			data, err := os.ReadFile(filepath.Join(root, "hub.json"))
			if err != nil || string(data) != string(payload) {
				t.Fatal("state was overwritten", err)
			}
			entries, err := os.ReadDir(home)
			wantEntries := 2 // one store plus its compatibility symlink
			if scenario == "external" || scenario == "relative-external" {
				wantEntries = 3
			}
			if err != nil || len(entries) != wantEntries {
				t.Fatal("initialization created another state directory", err)
			}
			if scenario == "fresh" {
				st, err := os.Lstat(root)
				if err != nil || st.Mode()&os.ModeSymlink != 0 || st.Mode().Perm() != 0o700 {
					t.Fatal("fresh state must be a private real directory", err)
				}
			}
		})
	}
}

func TestIndependentHomes(t *testing.T) {
	cleanRootEnv(t)
	home := t.TempDir()
	root := filepath.Join(t.TempDir(), "profile")
	t.Setenv("PERSONA_HOME", root)
	if got, err := PrepareHome(home); err != nil || got != root {
		t.Fatalf("%s %v", got, err)
	}
	if _, err := os.Lstat(filepath.Join(home, ".persona")); !os.IsNotExist(err) {
		t.Fatal("custom environment changed default state")
	}
	if SocketDir(home) != root || !IsAssistantDir(filepath.Join(root, "assistant")) {
		t.Fatal("custom directory identity lost")
	}
	for _, path := range []string{"/tmp/.persona/assistant", "/tmp/.persona/health"} {
		if !IsAssistantDir(path) {
			t.Fatal(path)
		}
	}
	if IsAssistantDir("/tmp/project/assistant") {
		t.Fatal("ordinary project became the assistant")
	}
	for _, tc := range []struct{ value, want string }{{"~", home}, {"~/profile", filepath.Join(home, "profile")}, {"profile", filepath.Join(home, "profile")}, {"", filepath.Join(home, ".persona")}} {
		t.Setenv("PERSONA_HOME", tc.value)
		if got := DataDir(home); got != tc.want {
			t.Fatalf("%q: %s want %s", tc.value, got, tc.want)
		}
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
	entries, err := os.ReadDir(home)
	if err != nil || len(entries) != 2 {
		t.Fatal("concurrent initialization did not create one store and its alias", err)
	}
	a, aerr := os.Stat(filepath.Join(home, ".persona"))
	b, berr := os.Stat(filepath.Join(home, compat.StateDir))
	if aerr != nil || berr != nil || !os.SameFile(a, b) {
		t.Fatal("concurrent initialization split state", aerr, berr)
	}
}

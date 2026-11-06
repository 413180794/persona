package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// `personad manager join` and `manager run --join` without a database to use (no hub here, no --database-url) say
// in one line what to pass and exit 2, which a manager's service and a container's restart do not loop on, without
// trading the install code.
func TestManagerWithoutDatabaseExits2(t *testing.T) {
	if args := os.Getenv("PERSONAD_TEST_MANAGER_ARGS"); args != "" {
		os.Args = append([]string{"personad"}, strings.Split(args, "\x1f")...)
		main()
		os.Exit(0)
	}
	var trades atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		trades.Add(1)
		http.Error(w, "no", http.StatusUnauthorized)
	}))
	defer srv.Close()
	home := t.TempDir()
	for _, args := range [][]string{
		{"manager", "join", srv.URL, "mi_0123456789abcdef0123456789abcdef"},
		{"manager", "run", "--dir", filepath.Join(home, "m"), "--join", srv.URL, "mi_0123456789abcdef0123456789abcdef"},
	} {
		cmd := exec.Command(os.Args[0], "-test.run=^TestManagerWithoutDatabaseExits2$")
		env := []string{}
		for _, kv := range os.Environ() {
			if !strings.HasPrefix(kv, "PERSONA_") && !strings.HasPrefix(kv, "JARVIS_") {
				env = append(env, kv)
			}
		}
		cmd.Env = append(env, "PERSONAD_TEST_MANAGER_ARGS="+strings.Join(args, "\x1f"), "HOME="+home,
			"PERSONA_HOME="+filepath.Join(home, ".persona"))
		out, err := cmd.CombinedOutput()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		if code != 2 || len(lines) != 1 || !strings.Contains(lines[0], "--database-url 'postgres://USER:PASSWORD@HOST:5432/DBNAME'") {
			t.Errorf("%q: exit %d, output %q", args, code, out)
		}
	}
	if n := trades.Load(); n != 0 {
		t.Fatalf("the hub was asked to trade %d times", n)
	}
}

package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/bus"
)

// `personad manager join` and `manager run --join` without a database to use (no Docker to make one, no --database-url) say
// in one line what to pass, without trading the install code: join exits 2, which a manager's service does not restart
// on; run --join (a container, which --restart unless-stopped starts again on exit 2 too) waits instead.
func TestManagerWithoutDatabaseExits2(t *testing.T) {
	if args := os.Getenv("PERSONAD_TEST_MANAGER_ARGS"); args != "" {
		os.Args = append([]string{"personad"}, strings.Split(args, "\x1f")...)
		main()
		os.Exit(0)
	}
	// Only a trade counts: anything else (a stray connection from another test while run --join waits) is noted.
	var trades atomic.Int32
	var mu sync.Mutex
	var others []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == bus.JoinPath {
			trades.Add(1)
		} else {
			mu.Lock()
			others = append(others, r.Method+" "+r.URL.Path)
			mu.Unlock()
		}
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
			if !strings.HasPrefix(kv, "PERSONA_") && !strings.HasPrefix(kv, "PATH=") {
				env = append(env, kv)
			}
		}
		cmd.Env = append(env, "PERSONAD_TEST_MANAGER_ARGS="+strings.Join(args, "\x1f"), "HOME="+home, "PATH="+t.TempDir(), // no docker
			"PERSONA_HOME="+filepath.Join(home, ".persona"))
		var out lockedOutput
		cmd.Stdout, cmd.Stderr = &out, &out
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		exited := make(chan error, 1)
		go func() { exited <- cmd.Wait() }()
		code := -1 // still running
		select {
		case err := <-exited:
			code = 0
			if ee, ok := err.(*exec.ExitError); ok {
				code = ee.ExitCode()
			} else if err != nil {
				t.Fatal(err)
			}
		case <-time.After(3 * time.Second):
			_ = cmd.Process.Kill()
			<-exited
		}
		// manager join exits 2; manager run --join (a container, restarted on any exit) says so and waits.
		want := 2
		if args[1] == "run" {
			want = -1
		}
		lines := strings.Split(strings.TrimSpace(out.String()), "\n")
		if code != want || len(lines) != 1 || !strings.Contains(lines[0], "--database-url 'postgres://USER:PASSWORD@HOST:5432/DBNAME'") {
			t.Errorf("%q: exit %d (want %d), output %q", args, code, want, out.String())
		}
	}
	if n := trades.Load(); n != 0 {
		mu.Lock()
		defer mu.Unlock()
		t.Fatalf("the hub was asked to trade %d times (other requests: %q)", n, others)
	}
}

// lockedOutput collects a child's output, written from its pipes' goroutines and read by the test.
type lockedOutput struct {
	mu sync.Mutex
	b  strings.Builder
}

func (o *lockedOutput) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.b.Write(p)
}
func (o *lockedOutput) String() string { o.mu.Lock(); defer o.mu.Unlock(); return o.b.String() }

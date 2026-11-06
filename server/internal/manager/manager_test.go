package manager

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistant"
	"github.com/nwverse/persona/server/internal/bus"
	"github.com/nwverse/persona/server/internal/update"
)

// joinHub answers POST /api/v1/bus/join like the hub: code "mi_good" once, for manager m-0a1b2c.
type joinHub struct{ trades atomic.Int32 }

func (j *joinHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != bus.JoinPath {
		http.NotFound(w, r)
		return
	}
	var req struct{ Code string }
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Code != "mi_good" || j.trades.Add(1) > 1 {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":"bad_install_code"}`))
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"id": "m-0a1b2c", "name": "工作", "token": "jb_secret", "hub_id": "h1"})
}

func isolate(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PERSONA_HOME", filepath.Join(home, ".persona"))
	t.Setenv("JARVIS_HOME", filepath.Join(home, ".persona"))
	t.Setenv("PERSONA_DATABASE_URL", "")
	t.Setenv("JARVIS_DATABASE_URL", "")
	return home
}

// The code is traded once for manager.json in the default directory; without Docker for its database and no
// --database-url nothing is traded (the code works once); a code the hub does not take says so.
func TestJoin(t *testing.T) {
	home := isolate(t)
	j := &joinHub{}
	srv := httptest.NewServer(j)
	defer srv.Close()
	ctx := context.Background()
	noDocker := func(context.Context) error { return ConfigError{errNoDocker} }
	docker := func(context.Context) error { return nil }
	var ce ConfigError
	if _, _, err := Join(ctx, JoinOptions{Hub: srv.URL, Code: "mi_good", CheckDocker: noDocker}); !errors.As(err, &ce) ||
		!strings.Contains(err.Error(), "--database-url 'postgres://USER:PASSWORD@HOST:5432/DBNAME'") || strings.Contains(err.Error(), "\n") ||
		j.trades.Load() != 0 {
		t.Fatalf("no database: %v, %d trades", err, j.trades.Load())
	}
	dir, c, err := Join(ctx, JoinOptions{Hub: srv.URL + "/", Code: " mi_good ", CheckDocker: docker})
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".persona", "managers", "m-0a1b2c"); dir != want {
		t.Fatalf("dir %s, want %s", dir, want)
	}
	got, err := assistant.ReadManagerConfig(dir)
	if err != nil || *got != *c || got.Token != "jb_secret" || got.Hub != srv.URL || got.HubID != "h1" || got.DatabaseURL != "" {
		t.Fatalf("manager.json %+v %v", got, err)
	}
	if _, _, err := Join(ctx, JoinOptions{Hub: srv.URL, Code: "mi_good", CheckDocker: docker}); err == nil || !strings.Contains(err.Error(), "用过") {
		t.Fatalf("second trade: %v", err)
	}
	if _, _, err := Join(ctx, JoinOptions{Hub: "[fd00::20]:18660", Code: "mi_good", CheckDocker: docker}); err == nil {
		t.Fatal("hub without a scheme")
	}
	if _, _, err := Join(ctx, JoinOptions{Hub: srv.URL, Code: "mi_x", DatabaseURL: "mysql://x/y", CheckDocker: noDocker}); err == nil {
		t.Fatal("a database URL that is not postgres")
	}

	// Into a directory of its own with its own database; a directory that is another manager's is left alone.
	j.trades.Store(0)
	other := filepath.Join(t.TempDir(), "mgr")
	if err := assistant.WriteManagerConfig(other, assistant.ManagerConfig{ID: "m-ffffff", Hub: srv.URL, Token: "jb_other"}); err != nil {
		t.Fatal(err)
	}
	own := "postgres://me@[fd00::21]:5432/mine"
	dir2, _, err := Join(ctx, JoinOptions{Hub: srv.URL, Code: "mi_good", Dir: other, DatabaseURL: own, CheckDocker: noDocker})
	if err != nil {
		t.Fatal(err)
	}
	if dir2 == other {
		t.Fatal("overwrote another manager's directory")
	}
	if kept, _ := assistant.ReadManagerConfig(other); kept == nil || kept.ID != "m-ffffff" {
		t.Fatalf("other manager's config: %+v", kept)
	}
	if c2, _ := assistant.ReadManagerConfig(dir2); c2 == nil || c2.DatabaseURL != own {
		t.Fatalf("own database not kept: %+v", c2)
	}
}

// Without Docker (to make its own Postgres) and no --database-url, join and run --join stop with a configuration
// error (exit 2: not restarted) before the code is traded.
func TestNoDatabaseSpendsNoCode(t *testing.T) {
	isolate(t)
	t.Setenv("PATH", t.TempDir()) // no docker
	j := &joinHub{}
	srv := httptest.NewServer(j)
	defer srv.Close()
	dir := filepath.Join(t.TempDir(), "m")
	for _, args := range [][]string{{"join", srv.URL, "mi_good"}, {"run", "--dir", dir, "--join", srv.URL, "mi_good"}} {
		var ce ConfigError
		if err := Main(context.Background(), update.Build{}, args, io.Discard); !errors.As(err, &ce) || !strings.Contains(err.Error(), "--database-url") {
			t.Errorf("%q: %v", args, err)
		}
	}
	if j.trades.Load() != 0 {
		t.Fatalf("%d codes traded", j.trades.Load())
	}
	if _, err := os.Stat(filepath.Join(dir, assistant.ManagerFile)); !os.IsNotExist(err) {
		t.Fatal("manager.json written")
	}
}

// `manager join` prints where it is and what is next; run --join skips the join once manager.json is there.
func TestJoinCommandAndArgs(t *testing.T) {
	isolate(t)
	srv := httptest.NewServer(&joinHub{})
	defer srv.Close()
	dir := filepath.Join(t.TempDir(), "m")
	var out bytes.Buffer
	err := Main(context.Background(), update.Build{}, []string{"join", srv.URL, "mi_good", "--dir", dir,
		"--database-url", "postgres://me@[fd00::21]:5432/mine"}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "m-0a1b2c") || !strings.Contains(out.String(), "manager install --dir") || strings.Contains(out.String(), "jb_secret") {
		t.Fatalf("output: %s", out.String())
	}
	for _, args := range [][]string{{"join", srv.URL}, {"run", "extra"}, {"run", "--join", srv.URL}, {"nope"}, nil} {
		if err := Main(context.Background(), update.Build{}, args, &out); err != errUsage {
			t.Errorf("%q: %v", args, err)
		}
	}
}

// The service units run `manager run --dir` from the right binary, quoted; a configuration mistake (exit 2) is not
// restarted.
func TestServiceFiles(t *testing.T) {
	unit := serviceUnit("m-0a1b2c", "/opt/my bin/jarvisd", "/home/u/.persona/managers/m-0a1b2c")
	for _, want := range []string{`ExecStart="/opt/my bin/jarvisd" manager run --dir "/home/u/.persona/managers/m-0a1b2c"`,
		"Restart=always", "RestartPreventExitStatus=2", `Environment="PERSONA_ASSISTANT_DEPLOY=service"`, "WantedBy=default.target"} {
		if !strings.Contains(unit, want) {
			t.Errorf("unit lacks %q:\n%s", want, unit)
		}
	}
	plist := servicePlist("m-0a1b2c", "/opt/jarvisd", "/Users/u/a&b", "/Users/u")
	for _, want := range []string{"<string>com.jarvis.manager.m-0a1b2c</string>", "<string>manager</string><string>run</string><string>--dir</string><string>/Users/u/a&amp;b</string>",
		"<key>KeepAlive</key><true/>", "/Users/u/a&amp;b/manager.log"} {
		if !strings.Contains(plist, want) {
			t.Errorf("plist lacks %q:\n%s", want, plist)
		}
	}
}

// install writes the user unit and enables and restarts it; uninstall takes it away and keeps the directory.
func TestInstallSystemd(t *testing.T) {
	home := isolate(t)
	dir := filepath.Join(home, ".persona", "managers", "m-0a1b2c")
	if err := assistant.WriteManagerConfig(dir, assistant.ManagerConfig{ID: "m-0a1b2c", Name: "工作", Hub: "http://[fd00::20]:18660", Token: "t"}); err != nil {
		t.Fatal(err)
	}
	var calls []string
	var out bytes.Buffer
	d := &fakeDocker{}
	e := &env{stdout: &out, goos: "linux", home: home, exe: func() (string, error) { return "/opt/personad", nil },
		look:   func(name string) (string, error) { return "/usr/bin/" + name, nil },
		getenv: func(string) string { return "" },
		run: func(_ context.Context, name string, args ...string) (string, error) {
			calls = append(calls, name+" "+strings.Join(args, " "))
			return "active\n", nil
		}}
	d.wire(e)
	if err := cmdInstall(context.Background(), e, nil); err != nil { // the only manager: no --dir needed
		t.Fatal(err)
	}
	c, _ := assistant.ReadManagerConfig(dir)
	if c.DatabaseContainer != "persona-mgr-m-0a1b2c-pg" || !strings.HasPrefix(c.DatabaseURL, "postgres://jarvis:") ||
		!strings.Contains(c.DatabaseURL, "@127.0.0.1:25432/jarvis") || !d.called("docker run -d --name persona-mgr-m-0a1b2c-pg") ||
		!d.called("docker volume create persona-mgr-m-0a1b2c-data") || !strings.Contains(out.String(), "建好了它自己的 Postgres") {
		t.Fatalf("its database: %+v\n%s\n%s", c, strings.Join(d.calls, "\n"), out.String())
	}
	unit := filepath.Join(home, ".config", "systemd", "user", "jarvis-manager-m-0a1b2c.service")
	b, err := os.ReadFile(unit)
	if err != nil || !strings.Contains(string(b), `"/opt/personad" manager run --dir "`+dir+`"`) {
		t.Fatalf("unit %s %v", b, err)
	}
	if got := strings.Join(calls, "; "); !strings.Contains(got, "systemctl --user enable jarvis-manager-m-0a1b2c.service") ||
		!strings.Contains(got, "systemctl --user restart jarvis-manager-m-0a1b2c.service") {
		t.Fatalf("calls: %s", got)
	}
	out.Reset()
	if err := cmdStatus(context.Background(), e, nil); err != nil || !strings.Contains(out.String(), "jarvis-manager-m-0a1b2c.service active") ||
		!strings.Contains(out.String(), "persona-mgr-m-0a1b2c-pg") {
		t.Fatalf("status %v: %s", err, out.String())
	}
	out.Reset()
	if err := cmdUninstall(context.Background(), e, []string{"--dir", dir}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(unit); !os.IsNotExist(err) {
		t.Fatal("unit still there")
	}
	if c, err := assistant.ReadManagerConfig(dir); err != nil || c.DatabaseContainer == "" {
		t.Fatalf("uninstall took the directory or the database: %+v %v", c, err)
	}
	if !d.called("docker stop persona-mgr-m-0a1b2c-pg") || d.called("docker volume rm") || !strings.Contains(out.String(), "数据卷 persona-mgr-m-0a1b2c-data 和里面的数据都留着") {
		t.Fatalf("uninstall keeps the volume: %s\n%s", strings.Join(d.calls, "\n"), out.String())
	}

	// Installed again: the same container and password, started again; nothing made.
	before, _ := assistant.ReadManagerConfig(dir)
	runs := d.count("docker run")
	if err := cmdInstall(context.Background(), e, []string{"--dir", dir}); err != nil {
		t.Fatal(err)
	}
	if after, _ := assistant.ReadManagerConfig(dir); after.DatabaseURL != before.DatabaseURL || d.count("docker run") != runs {
		t.Fatalf("installed again: %+v", after)
	}

	out.Reset()
	if err := cmdUninstall(context.Background(), e, []string{"--dir", dir, "--purge"}); err != nil {
		t.Fatal(err)
	}
	if c, _ := assistant.ReadManagerConfig(dir); c.DatabaseURL != "" || c.DatabaseContainer != "" ||
		!d.called("docker rm -f persona-mgr-m-0a1b2c-pg") || !d.called("docker volume rm persona-mgr-m-0a1b2c-data") ||
		!strings.Contains(out.String(), "它的数据没了") {
		t.Fatalf("purge: %+v\n%s", c, out.String())
	}

	e.look = func(name string) (string, error) {
		if name == "systemctl" {
			return "", os.ErrNotExist
		}
		return "/usr/bin/" + name, nil
	}
	if err := cmdInstall(context.Background(), e, []string{"--dir", dir}); err == nil || !strings.Contains(err.Error(), "manager run --dir") {
		t.Fatalf("without systemd: %v", err)
	}
}

// A manager whose token was refused waits for a new one in manager.json, and tries the same token again after
// revokedRetry with none.
func TestWaitNewToken(t *testing.T) {
	defer func(p time.Duration) { revokedPoll = p }(revokedPoll)
	revokedPoll = 10 * time.Millisecond
	dir := t.TempDir()
	c := assistant.ManagerConfig{ID: "m-0a1b2c", Hub: "http://[fd00::20]:18660", Token: "jb_old"}
	if err := assistant.WriteManagerConfig(dir, c); err != nil {
		t.Fatal(err)
	}
	if renewed, err := waitNewToken(context.Background(), dir, &c, 50*time.Millisecond); renewed || err != nil {
		t.Fatalf("no new token: %v %v", renewed, err)
	}
	go func() {
		time.Sleep(30 * time.Millisecond)
		n := c
		n.Token = "jb_new"
		_ = assistant.WriteManagerConfig(dir, n)
	}()
	if renewed, err := waitNewToken(context.Background(), dir, &c, 5*time.Second); !renewed || err != nil {
		t.Fatalf("new token: %v %v", renewed, err)
	}
}

// After a refusal nothing of the manager starts again on the timer alone: every revokedRetry only the token is
// probed, and the manager comes back when the probe is no longer refused, or when manager.json gets a new token.
func TestAwaitRestartProbesTheTokenOnly(t *testing.T) {
	defer func(p, r time.Duration, f func(context.Context, *assistant.ManagerConfig) error) {
		revokedPoll, revokedRetry, probeToken = p, r, f
	}(revokedPoll, revokedRetry, probeToken)
	revokedPoll, revokedRetry = 5*time.Millisecond, 20*time.Millisecond
	dir := t.TempDir()
	c := assistant.ManagerConfig{ID: "m-0a1b2c", Hub: "http://[fd00::20]:18660", Token: "jb_old"}
	if err := assistant.WriteManagerConfig(dir, c); err != nil {
		t.Fatal(err)
	}
	var probes atomic.Int32
	refused := atomic.Bool{}
	refused.Store(true)
	probeToken = func(_ context.Context, got *assistant.ManagerConfig) error {
		probes.Add(1)
		if got.Token != "jb_old" {
			t.Errorf("probed %q", got.Token)
		}
		if refused.Load() {
			return bus.ErrRevoked
		}
		return nil
	}
	done := make(chan error, 1)
	var out bytes.Buffer
	go func() { done <- awaitRestart(context.Background(), dir, &c, &out) }()
	for deadline := time.Now().Add(10 * time.Second); probes.Load() < 3; {
		select {
		case err := <-done:
			t.Fatalf("came back while the token was still refused: %v", err)
		case <-time.After(5 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d probes in 10 s", probes.Load())
		}
	}
	refused.Store(false)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("did not come back once the hub took the token again")
	}

	// A new token comes back at once, probing nothing.
	probeToken = func(context.Context, *assistant.ManagerConfig) error { t.Error("probed"); return bus.ErrRevoked }
	revokedRetry = time.Hour
	go func() {
		time.Sleep(20 * time.Millisecond)
		n := c
		n.Token = "jb_new"
		_ = assistant.WriteManagerConfig(dir, n)
	}()
	if err := awaitRestart(context.Background(), dir, &c, &out); err != nil {
		t.Fatal(err)
	}
}

// A probe that gets no verdict (the hub unreachable, a proxy's 401 or 503) is never taken for yes: the manager keeps
// waiting and tries again sooner; only a probe the hub took brings it back.
func TestAwaitRestartHubDown(t *testing.T) {
	defer func(p, r, a time.Duration, f func(context.Context, *assistant.ManagerConfig) error) {
		revokedPoll, revokedRetry, probeAgain, probeToken = p, r, a, f
	}(revokedPoll, revokedRetry, probeAgain, probeToken)
	revokedPoll, revokedRetry, probeAgain = 5*time.Millisecond, 10*time.Millisecond, 10*time.Millisecond
	dir := t.TempDir()
	c := assistant.ManagerConfig{ID: "m-0a1b2c", Hub: "http://[fd00::20]:18660", Token: "jb_old"}
	if err := assistant.WriteManagerConfig(dir, c); err != nil {
		t.Fatal(err)
	}
	var probes atomic.Int32
	var up atomic.Bool
	probeToken = func(context.Context, *assistant.ManagerConfig) error {
		// From the second probe on the waits must be probeAgain's: revokedRetry is made an hour.
		if probes.Add(1) == 1 {
			revokedRetry = time.Hour
		}
		if up.Load() {
			return nil
		}
		return errors.New("dial tcp: connection refused")
	}
	done := make(chan error, 1)
	go func() { done <- awaitRestart(context.Background(), dir, &c, io.Discard) }()
	for deadline := time.Now().Add(10 * time.Second); probes.Load() < 5; {
		select {
		case err := <-done:
			t.Fatalf("came back while the hub was down: %v", err)
		case <-time.After(5 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d probes in 10 s", probes.Load())
		}
	}
	up.Store(true)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("did not come back once the hub took the token")
	}
}

// A database the user gave is used as it is: no docker, and uninstall leaves it alone.
func TestGivenDatabaseIsLeftAlone(t *testing.T) {
	home := isolate(t)
	dir := filepath.Join(home, "m")
	given := "postgres://me:pw@[fd00::21]:5432/mine"
	if err := assistant.WriteManagerConfig(dir, assistant.ManagerConfig{ID: "m-0a1b2c", Hub: "http://[fd00::20]:18660", Token: "t", DatabaseURL: given}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	d := &fakeDocker{}
	e := &env{stdout: &out, goos: "plan9", home: home, look: func(string) (string, error) { return "", os.ErrNotExist }}
	d.wire(e)
	c, _ := assistant.ReadManagerConfig(dir)
	if made, err := e.ownDatabase(context.Background(), dir, c); err != nil || made || len(d.calls) != 0 {
		t.Fatalf("given: %v %v %v", made, err, d.calls)
	}
	if err := cmdUninstall(context.Background(), e, []string{"--dir", dir, "--purge"}); err != nil || len(d.calls) != 0 ||
		!strings.Contains(out.String(), "--database-url 给的，没动它") {
		t.Fatalf("uninstall: %v %v %s", err, d.calls, out.String())
	}
	if c, _ := assistant.ReadManagerConfig(dir); c.DatabaseURL != given {
		t.Fatalf("its URL changed: %+v", c)
	}
}

// Without Docker, and none given: a configuration error (exit 2) saying what to do.
func TestOwnDatabaseNeedsDocker(t *testing.T) {
	home := isolate(t)
	dir := filepath.Join(home, "m")
	_ = assistant.WriteManagerConfig(dir, assistant.ManagerConfig{ID: "m-0a1b2c", Hub: "http://[fd00::20]:18660", Token: "t"})
	c, _ := assistant.ReadManagerConfig(dir)
	d := &fakeDocker{}
	e := &env{stdout: io.Discard, look: func(string) (string, error) { return "", os.ErrNotExist }}
	d.wire(e)
	var ce ConfigError
	if _, err := e.ownDatabase(context.Background(), dir, c); !errors.As(err, &ce) || !strings.Contains(err.Error(), "--database-url") {
		t.Fatalf("no docker: %v", err)
	}
	d.denied = true
	e.look = func(name string) (string, error) { return "/usr/bin/" + name, nil }
	if _, err := e.ownDatabase(context.Background(), dir, c); !errors.As(err, &ce) || !strings.Contains(err.Error(), "usermod") {
		t.Fatalf("denied: %v", err)
	}
}

// fakeDocker answers docker like a machine with no containers yet: run makes one, rm removes it.
type fakeDocker struct {
	calls    []string
	envs     []string
	exists   bool
	denied   bool
	down     bool // docker info fails: not running
	noVolume bool // docker volume inspect fails
}

func (d *fakeDocker) wire(e *env) {
	e.docker = d.run
	e.freePort = func() (int, error) { return 25432, nil }
	e.ping = func(context.Context, string) error { return nil }
	e.dbWait = time.Second
}

func (d *fakeDocker) run(_ context.Context, env []string, name string, args ...string) ([]byte, error) {
	call := strings.Join(append([]string{name}, args...), " ")
	d.calls = append(d.calls, call)
	d.envs = append(d.envs, env...)
	switch {
	case strings.HasPrefix(call, "docker info") && d.down:
		return []byte("Cannot connect to the Docker daemon"), errors.New("exit status 1")
	case strings.HasPrefix(call, "docker volume inspect") && d.noVolume:
		return []byte("no such volume"), errors.New("exit status 1")
	case strings.HasPrefix(call, "docker inspect -f {{.State.Status}}") && !d.exists:
		return []byte("No such object"), errors.New("exit status 1")
	case strings.HasPrefix(call, "docker info") && d.denied:
		return []byte("permission denied while trying to connect to the Docker daemon socket"), errors.New("exit status 1")
	case strings.HasPrefix(call, "docker ps -a") && d.exists:
		return []byte(args[len(args)-3][len("name=^/"):len(args[len(args)-3])-1] + "\n"), nil
	case strings.HasPrefix(call, "docker run"):
		d.exists = true
	case strings.HasPrefix(call, "docker rm"):
		d.exists = false
	case strings.HasPrefix(call, "docker port"):
		return []byte("127.0.0.1:25432\n"), nil
	case strings.HasPrefix(call, "docker inspect -f {{.State.Status}}"):
		return []byte("running\n"), nil
	}
	return nil, nil
}

func (d *fakeDocker) called(prefix string) bool { return d.count(prefix) > 0 }

func (d *fakeDocker) count(prefix string) int {
	n := 0
	for _, c := range d.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

// Joined again after a revoke, into its own directory: its database is there, so no Docker is needed and the code is
// traded; a container given its database through PERSONA_MANAGER_DATABASE_URL (`run --join`, never on a command line)
// keeps it; another database given into a directory whose container install made says the container is left unused.
func TestJoinAgainAndDatabaseFromTheEnvironment(t *testing.T) {
	home := isolate(t)
	t.Setenv("PATH", t.TempDir()) // no docker
	srv := httptest.NewServer(&joinHub{})
	defer srv.Close()
	dir := filepath.Join(home, ".persona", "managers", "m-0a1b2c")
	own := assistant.ManagerConfig{ID: "m-0a1b2c", Hub: srv.URL, Token: "jb_old",
		DatabaseURL: "postgres://jarvis:pw@127.0.0.1:25432/jarvis?sslmode=disable", DatabaseContainer: "persona-mgr-m-0a1b2c-pg"}
	if err := assistant.WriteManagerConfig(dir, own); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Main(context.Background(), update.Build{}, []string{"join", srv.URL, "mi_good", "--dir", dir}, &out); err != nil {
		t.Fatalf("join again without docker: %v", err)
	}
	if c, _ := assistant.ReadManagerConfig(dir); c.Token != "jb_secret" || c.DatabaseURL != own.DatabaseURL || c.DatabaseContainer != own.DatabaseContainer {
		t.Fatalf("joined again: %+v", c)
	}

	// The container's way: run --join with the URL in the environment (the hub is a fake; the run stops at the bus).
	srv2 := httptest.NewServer(&joinHub{})
	defer srv2.Close()
	dir2 := filepath.Join(t.TempDir(), "m")
	e := newEnv(io.Discard)
	e.getenv = func(k string) string {
		if k == databaseEnv {
			return "postgres://jarvis:secret@persona-mgr-m-0a1b2c-pg:5432/jarvis?sslmode=disable"
		}
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = cmdRun(ctx, e, update.Build{}, []string{"--dir", dir2, "--join", srv2.URL, "mi_good"})
	if c, err := assistant.ReadManagerConfig(dir2); err != nil || c.DatabaseURL != "postgres://jarvis:secret@persona-mgr-m-0a1b2c-pg:5432/jarvis?sslmode=disable" || c.DatabaseContainer != "" {
		t.Fatalf("from the environment: %+v %v", c, err)
	}

	// Another database given into the first directory: its container is not used any more, and it says so.
	srv3 := httptest.NewServer(&joinHub{})
	defer srv3.Close()
	out.Reset()
	if err := Main(context.Background(), update.Build{}, []string{"join", srv3.URL, "mi_good", "--dir", dir,
		"--database-url", "postgres://me@[fd00::21]:5432/mine"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "docker rm -f persona-mgr-m-0a1b2c-pg && docker volume rm persona-mgr-m-0a1b2c-data") {
		t.Fatalf("no word about the old container: %s", out.String())
	}
}

// When its container and volume are both gone, the run says the database it makes is a new, empty one; status tells
// docker not running apart from a container that is gone.
func TestLostDatabaseAndDockerDown(t *testing.T) {
	home := isolate(t)
	dir := filepath.Join(home, "m")
	c := assistant.ManagerConfig{ID: "m-0a1b2c", Hub: "http://[fd00::20]:18660", Token: "t",
		DatabaseURL: "postgres://jarvis:pw@127.0.0.1:25432/jarvis?sslmode=disable", DatabaseContainer: "persona-mgr-m-0a1b2c-pg"}
	_ = assistant.WriteManagerConfig(dir, c)
	var out bytes.Buffer
	d := &fakeDocker{}
	e := &env{stdout: &out, goos: "linux", home: home, look: func(n string) (string, error) { return "/usr/bin/" + n, nil },
		getenv: func(string) string { return "" }}
	d.wire(e)
	d.noVolume = true
	if made, err := e.ownDatabase(context.Background(), dir, &c); err != nil || !made || !strings.Contains(out.String(), "建了一个新的空库") {
		t.Fatalf("lost: %v %v %s", made, err, out.String())
	}
	d.down = true
	if got := e.containerState(context.Background(), c.DatabaseContainer); got != "Docker 没在运行，看不了" {
		t.Fatalf("docker down: %q", got)
	}
	d.down, d.exists = false, false
	if got := e.containerState(context.Background(), c.DatabaseContainer); got != "容器不在了" {
		t.Fatalf("gone: %q", got)
	}
}

// --dir with a manager.json that is a link to nothing (a volume not mounted yet): refused before the code is traded,
// which a write after it would have spent for nothing.
func TestJoinRefusesADanglingManagerJSONFirst(t *testing.T) {
	isolate(t)
	j := &joinHub{}
	srv := httptest.NewServer(j)
	defer srv.Close()
	dir := t.TempDir()
	if err := os.Symlink(filepath.Join(dir, "not-mounted", "manager.json"), filepath.Join(dir, "manager.json")); err != nil {
		t.Fatal(err)
	}
	var ce ConfigError
	_, _, err := Join(context.Background(), JoinOptions{Hub: srv.URL, Code: "mi_good", Dir: dir,
		CheckDocker: func(context.Context) error { return nil }})
	if !errors.As(err, &ce) || !strings.Contains(err.Error(), "link to nothing") || j.trades.Load() != 0 {
		t.Fatalf("%v, %d trades", err, j.trades.Load())
	}
}

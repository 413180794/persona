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

// The code is traded once for manager.json in the default directory; without a database it could use, nothing is
// traded (the code works once); a code the hub does not take says so.
func TestJoin(t *testing.T) {
	home := isolate(t)
	j := &joinHub{}
	srv := httptest.NewServer(j)
	defer srv.Close()
	ctx := context.Background()
	none := ""
	var ce ConfigError
	if _, _, err := Join(ctx, JoinOptions{Hub: srv.URL, Code: "mi_good", HubDatabaseURL: &none}); !errors.As(err, &ce) ||
		!strings.Contains(err.Error(), "--database-url 'postgres://USER:PASSWORD@HOST:5432/DBNAME'") || strings.Contains(err.Error(), "\n") ||
		j.trades.Load() != 0 {
		t.Fatalf("no database: %v, %d trades", err, j.trades.Load())
	}
	hubDB := "postgres://jarvis:pw@[fd00::20]:5432/jarvis"
	dir, c, err := Join(ctx, JoinOptions{Hub: srv.URL + "/", Code: " mi_good ", HubDatabaseURL: &hubDB})
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
	if _, _, err := Join(ctx, JoinOptions{Hub: srv.URL, Code: "mi_good", HubDatabaseURL: &hubDB}); err == nil || !strings.Contains(err.Error(), "用过") {
		t.Fatalf("second trade: %v", err)
	}
	if _, _, err := Join(ctx, JoinOptions{Hub: "[fd00::20]:18660", Code: "mi_good", HubDatabaseURL: &hubDB}); err == nil {
		t.Fatal("hub without a scheme")
	}
	if _, _, err := Join(ctx, JoinOptions{Hub: srv.URL, Code: "mi_x", DatabaseURL: "mysql://x/y", HubDatabaseURL: &none}); err == nil {
		t.Fatal("a database URL that is not postgres")
	}

	// Into a directory of its own with its own database; a directory that is another manager's is left alone.
	j.trades.Store(0)
	other := filepath.Join(t.TempDir(), "mgr")
	if err := assistant.WriteManagerConfig(other, assistant.ManagerConfig{ID: "m-ffffff", Hub: srv.URL, Token: "jb_other"}); err != nil {
		t.Fatal(err)
	}
	own := "postgres://me@[fd00::21]:5432/mine"
	dir2, _, err := Join(ctx, JoinOptions{Hub: srv.URL, Code: "mi_good", Dir: other, DatabaseURL: own, HubDatabaseURL: &none})
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

// In a container (or any computer without a hub) and no --database-url, join and run --join stop with a
// configuration error (exit 2: not restarted) before the code is traded.
func TestNoDatabaseSpendsNoCode(t *testing.T) {
	isolate(t) // no hub.json, no PERSONA_DATABASE_URL
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
	e := &env{stdout: &out, goos: "linux", home: home, exe: func() (string, error) { return "/opt/personad", nil },
		look:   func(string) (string, error) { return "/usr/bin/systemctl", nil },
		getenv: func(string) string { return "" },
		run: func(_ context.Context, name string, args ...string) (string, error) {
			calls = append(calls, name+" "+strings.Join(args, " "))
			return "active\n", nil
		}}
	if err := cmdInstall(context.Background(), e, nil); err != nil { // the only manager: no --dir needed
		t.Fatal(err)
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
	if err := cmdStatus(context.Background(), e, nil); err != nil || !strings.Contains(out.String(), "jarvis-manager-m-0a1b2c.service active") {
		t.Fatalf("status %v: %s", err, out.String())
	}
	if err := cmdUninstall(context.Background(), e, []string{"--dir", dir}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(unit); !os.IsNotExist(err) {
		t.Fatal("unit still there")
	}
	if _, err := assistant.ReadManagerConfig(dir); err != nil {
		t.Fatal("uninstall took the directory")
	}
	e.look = func(string) (string, error) { return "", os.ErrNotExist }
	if err := cmdInstall(context.Background(), e, []string{"--dir", dir}); err == nil || !strings.Contains(err.Error(), "manager run --dir") {
		t.Fatalf("without systemd: %v", err)
	}
}

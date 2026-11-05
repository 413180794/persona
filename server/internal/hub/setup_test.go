package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

var updateGolden = flag.Bool("update-hubjson-golden", false, "rewrite testdata/hubjson/*.golden.json")

func setupTestHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, k := range []string{"PERSONA_HOME", "JARVIS_HOME", "PERSONA_LOCAL_NODE", "JARVIS_LOCAL_NODE",
		"JARVIS_DATABASE_URL", "PERSONA_DATABASE_URL", "JARVIS_PUBLIC_URL", "PERSONA_PUBLIC_URL", "JARVIS_LISTEN", "PERSONA_LISTEN"} {
		t.Setenv(k, "")
	}
	return home
}

// normalizeHost takes this machine's host name and a generated token out of a config, for goldens.
func normalizeHost(s, token string) string {
	host, _ := os.Hostname()
	short := strings.Split(host, ".")[0]
	if token != "" {
		s = strings.ReplaceAll(s, token, "<generated token>")
	}
	s = strings.ReplaceAll(s, host, "<host>")
	if short != host {
		s = strings.ReplaceAll(s, `"`+short+`"`, `"<host>"`)
	}
	return s
}

// Old hub.json files load exactly as before: the new-install defaults (docs/onboarding.md 4.1) are not in
// defaultConfig(), so a file without "public" or "admin.first_setup" gets no TLS and no LAN setup code.
func TestLegacyHubJSONLoadsUnchanged(t *testing.T) {
	for _, name := range []string{"dbsh-only", "handwritten-no-public"} {
		t.Run(name, func(t *testing.T) {
			home := setupTestHome(t)
			in, err := os.ReadFile(filepath.Join("testdata", "hubjson", name+".json"))
			if err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(home, ".jarvis")
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "hub.json"), in, 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, err := LoadConfig()
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Public.TLSListen != "" || cfg.Admin.FirstSetup != "" {
				t.Fatalf("new-install defaults leaked into an old file: public %+v admin %+v", cfg.Public, cfg.Admin)
			}
			loaded, _ := json.MarshalIndent(cfg, "", "  ")
			file, _ := os.ReadFile(filepath.Join(dir, "hub.json"))
			generated := ""
			if !bytes.Contains(in, []byte(`"token"`)) {
				generated = cfg.Token
			}
			got := "// the config the hub runs with\n" + normalizeHost(string(loaded), generated) +
				"\n// hub.json after loading\n" + normalizeHost(string(file), generated) + "\n"
			golden := filepath.Join("testdata", "hubjson", name+".golden.json")
			if *updateGolden {
				if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("%v (run go test -run TestLegacyHubJSONLoadsUnchanged -update-hubjson-golden)", err)
			}
			if got != string(want) {
				t.Fatalf("an old hub.json loads differently now:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

func TestNewInstallDefaultsAreNotDefaults(t *testing.T) {
	setupTestHome(t)
	d := defaultConfig()
	if d.Public.TLSListen != "" || d.Admin.FirstSetup != "" {
		t.Fatalf("defaultConfig has new-install defaults: %+v %+v", d.Public, d.Admin)
	}
	lan := ip4(192, 168, 1, 20)
	n := newInstallConfig(installInputs{lanIP: lan, listen: ":18660", tls: ":18662", agents: true})
	if n.Public.TLSListen != lan+":18662" || n.PublicURL != "http://"+lan+":18660" || n.Admin.FirstSetup != "code" || len(n.Machines) != 1 {
		t.Fatalf("new install: %+v %s %+v %+v", n.Public, n.PublicURL, n.Admin, n.Machines)
	}
	if n := newInstallConfig(installInputs{lanIP: lan, listen: ":18660", tls: ":18662", outside: true}); n.Public.TLSListen != ":18662" || len(n.Machines) != 0 || n.Machines == nil {
		t.Fatalf("outside, no agents: %+v %#v", n.Public, n.Machines)
	}
	if n := newInstallConfig(installInputs{listen: ":18660", tls: ":18662"}); n.Public.TLSListen != "" || !strings.Contains(n.PublicURL, ":18660") {
		t.Fatalf("no LAN address: TLS stays off: %+v %s", n.Public, n.PublicURL)
	}
}

// fakeHub answers what setup asks the running hub: health, endpoints, a pair code and a setup code.
func fakeHub(t *testing.T, token *string, passwordSet bool) (port int) {
	t.Helper()
	mux := http.NewServeMux()
	auth := func(r *http.Request) bool { return r.Header.Get("Authorization") == "Bearer "+*token }
	mux.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, map[string]any{"ok": true}) })
	mux.HandleFunc("GET /api/v1/endpoints", func(w http.ResponseWriter, r *http.Request) {
		if !auth(r) {
			w.WriteHeader(401)
			return
		}
		writeJSON(w, map[string]any{"endpoints": []any{}})
	})
	mux.HandleFunc("POST /api/v1/auth/pair-codes", func(w http.ResponseWriter, r *http.Request) {
		if !auth(r) {
			w.WriteHeader(401)
			return
		}
		writeJSON(w, map[string]any{"token": "jp_test", "url": "http://hub.example.com:1", "expires_at": time.Now().Add(10 * time.Minute)})
	})
	mux.HandleFunc("POST /api/v1/admin/setup-code", func(w http.ResponseWriter, r *http.Request) {
		if passwordSet {
			writeErrCode(w, 409, "already_set", errors.New("set"))
			return
		}
		writeJSON(w, map[string]any{"code": "ABCD-EFGH", "expires_at": time.Now().Add(10 * time.Minute)})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	_, p, _ := net.SplitHostPort(srv.Listener.Addr().String())
	port, _ = strconv.Atoi(p)
	return port
}

type fakeCommands struct {
	calls   []string
	envs    []string
	replies map[string]fakeReply // by the call's prefix
}

type fakeReply struct {
	out string
	err bool
}

func (f *fakeCommands) run(_ context.Context, env []string, name string, args ...string) ([]byte, error) {
	call := strings.Join(append([]string{name}, args...), " ")
	f.calls = append(f.calls, call)
	f.envs = append(f.envs, env...)
	best := ""
	for prefix := range f.replies {
		if strings.HasPrefix(call, prefix) && len(prefix) > len(best) {
			best = prefix
		}
	}
	if best == "" {
		return nil, nil
	}
	r := f.replies[best]
	if r.err {
		return []byte(r.out), errors.New("exit status 1")
	}
	return []byte(r.out), nil
}

func (f *fakeCommands) called(prefix string) bool {
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}

func testSetupEnv(t *testing.T, home string, o setupOptions, cmds *fakeCommands, have ...string) (*setupEnv, *bytes.Buffer) {
	t.Helper()
	root := filepath.Join(home, ".persona")
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "bin", "jarvisd"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("PERSONA_HUB_SOCKET", "")
	var out bytes.Buffer
	e := &setupEnv{
		home: home, root: root, goos: "linux", uid: 1000, user: "someone", out: &out, o: o,
		run:    cmds.run,
		runTTY: func(context.Context, string, ...string) error { return errors.New("no terminal") },
		lookPath: func(name string) (string, error) {
			for _, h := range have {
				if h == name {
					return "/usr/bin/" + name, nil
				}
			}
			return "", errors.New("not found")
		},
		ask:        func(string) (bool, bool) { return false, false },
		lanIPv4:    func() string { return ip4(192, 168, 1, 20) },
		portFree:   func(int) bool { return true },
		exe:        func() (string, error) { return "/nonexistent", nil },
		pingDB:     func(context.Context, string) error { return nil },
		doctor:     func(context.Context, string, bool) []string { return nil },
		getenv:     func(string) string { return "" },
		sleep:      func(time.Duration) {},
		healthWait: time.Second, dbWait: time.Second,
		dbContainer: setupDBContainer, dbVolume: setupDBVolume, dbPort: setupDBPort,
	}
	return e, &out
}

// A new install: the whole file with the new defaults, the database container db.sh would make, the socket units,
// and at the end the pair link and the admin URL with the setup code (--json: one object a line).
func TestSetupNewInstall(t *testing.T) {
	home := setupTestHome(t)
	var token string
	port := fakeHub(t, &token, false)
	cmds := &fakeCommands{replies: map[string]fakeReply{
		"docker ps -a":               {out: ""},
		"docker port":                {out: "127.0.0.1:18661\n"},
		"systemctl --user is-active": {err: true},
		"loginctl show-user":         {out: "no\n"},
		"loginctl enable-linger":     {err: true, out: "Access denied"},
		"docker info":                {out: "27.0\n"},
	}}
	o := setupOptions{listen: ":" + strconv.Itoa(port), tls: ":18662", json: true}
	e, out := testSetupEnv(t, home, o, cmds, "docker", "systemctl", "loginctl", "herdr")
	// The hub "starts" when the units do: the token is what setup wrote.
	e.pingDB = func(context.Context, string) error {
		cfg, _ := readHubJSON(e.root)
		raw, _ := cfg.get("token")
		_ = json.Unmarshal(raw, &token)
		return nil
	}
	if err := e.setup(context.Background()); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	var cfg Config
	b, _ := os.ReadFile(filepath.Join(e.root, "hub.json"))
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	lan := ip4(192, 168, 1, 20)
	if cfg.Public.TLSListen != lan+":18662" || cfg.PublicURL != "http://"+lan+":"+strconv.Itoa(port) || cfg.Admin.FirstSetup != "code" ||
		cfg.Token == "" || len(cfg.Machines) != 1 || !strings.HasPrefix(cfg.DatabaseURL, "postgres://jarvis:") || !strings.Contains(cfg.DatabaseURL, "@127.0.0.1:18661/jarvis") {
		t.Fatalf("hub.json: %s", b)
	}
	if fi, _ := os.Stat(filepath.Join(e.root, "hub.json")); fi.Mode().Perm() != 0o600 {
		t.Fatalf("hub.json mode %v", fi.Mode())
	}
	// db.sh's container, the password through the environment only.
	run := ""
	for _, c := range cmds.calls {
		if strings.HasPrefix(c, "docker run") {
			run = c
		}
	}
	if !strings.Contains(run, "--name jarvis-postgres") || !strings.Contains(run, "-v jarvis-pgdata:/var/lib/postgresql") ||
		!strings.Contains(run, "-p 127.0.0.1:18661:5432") || !strings.HasSuffix(run, "postgres:18-alpine") || strings.Contains(run, "POSTGRES_PASSWORD=") {
		t.Fatalf("docker run: %q", run)
	}
	if len(cmds.envs) != 1 || !strings.HasPrefix(cmds.envs[0], "POSTGRES_PASSWORD=") {
		t.Fatalf("password env: %v", cmds.envs)
	}
	// hub-systemd.sh's units; the TLS address is bound, so FreeBind.
	unitDir := filepath.Join(home, ".config", "systemd", "user")
	socket, _ := os.ReadFile(filepath.Join(unitDir, "jarvisd.socket"))
	if !strings.Contains(string(socket), "ListenStream="+strconv.Itoa(port)+"\nListenStream="+lan+":18662\n") || !strings.Contains(string(socket), "FreeBind=true") {
		t.Fatalf("socket unit:\n%s", socket)
	}
	for _, c := range []string{"systemctl --user daemon-reload", "systemctl --user enable jarvisd", "systemctl --user enable jarvisd.socket", "systemctl --user start jarvisd"} {
		if !cmds.called(c) {
			t.Fatalf("no %q in %v", c, cmds.calls)
		}
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	var steps []string
	for _, l := range lines[:len(lines)-1] {
		var s struct {
			Step   string `json:"step"`
			OK     bool   `json:"ok"`
			Detail string `json:"detail"`
			Next   string `json:"next"`
		}
		if err := json.Unmarshal([]byte(l), &s); err != nil || s.Step == "" || !s.OK {
			t.Fatalf("step line %q", l)
		}
		steps = append(steps, s.Step)
		if s.Step == "service" && !strings.Contains(s.Next, "sudo loginctl enable-linger someone") {
			t.Fatalf("linger not offered: %q", l)
		}
	}
	if strings.Join(steps, ",") != "config,database,service,health,doctor" {
		t.Fatalf("steps %v", steps)
	}
	var done map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &done); err != nil || done["done"] != true ||
		!strings.HasPrefix(done["pair_url"].(string), "jarvis://pair?") ||
		done["admin_url"] != "http://"+lan+":"+strconv.Itoa(port)+"/admin#setup=ABCD-EFGH" || done["setup_code"] != "ABCD-EFGH" || done["expires_at"] == "" {
		t.Fatalf("done line %q", lines[len(lines)-1])
	}
	// Again: nothing changes, nothing restarts.
	before, _ := os.ReadFile(filepath.Join(e.root, "hub.json"))
	cmds.calls = nil
	cmds.replies["systemctl --user is-active"] = fakeReply{}
	cmds.replies["loginctl show-user"] = fakeReply{out: "yes\n"}
	out.Reset()
	if err := e.setup(context.Background()); err != nil {
		t.Fatalf("again: %v\n%s", err, out)
	}
	if after, _ := os.ReadFile(filepath.Join(e.root, "hub.json")); string(after) != string(before) {
		t.Fatalf("second run changed hub.json:\n%s\n%s", before, after)
	}
	if cmds.called("systemctl --user restart") || cmds.called("systemctl --user stop") || cmds.called("docker run") {
		t.Fatalf("second run restarted things: %v", cmds.calls)
	}
	// An upgrade (install.sh --hub replaced bin/jarvisd): the units are the same, the running hub is the old one.
	if err := os.WriteFile(filepath.Join(e.root, "bin", "jarvisd"), []byte("#!/bin/sh\n# new\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmds.calls = nil
	out.Reset()
	if err := e.setup(context.Background()); err != nil {
		t.Fatalf("upgrade: %v\n%s", err, out)
	}
	if !cmds.called("systemctl --user restart jarvisd") || cmds.called("systemctl --user stop") {
		t.Fatalf("upgrade did not restart the hub: %v", cmds.calls)
	}
}

// A hub.json a hub already ran with (a token, no "public") keeps every byte: no TLS, no LAN setup code.
func TestSetupKeepsAnExistingHubJSON(t *testing.T) {
	home := setupTestHome(t)
	token := "handwritten-token"
	port := fakeHub(t, &token, true)
	cmds := &fakeCommands{replies: map[string]fakeReply{"loginctl show-user": {out: "yes\n"}}}
	e, out := testSetupEnv(t, home, setupOptions{listen: ":18660", tls: ":18662", listenSet: true}, cmds, "systemctl", "loginctl")
	file := `{"token":"handwritten-token","listen":":` + strconv.Itoa(port) + `","public_url":"http://hub.example.com:1","database_url":"postgres://x"}`
	if err := os.WriteFile(filepath.Join(e.root, "hub.json"), []byte(file), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := e.setup(context.Background()); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if b, _ := os.ReadFile(filepath.Join(e.root, "hub.json")); string(b) != file {
		t.Fatalf("hub.json changed: %s", b)
	}
	socket, _ := os.ReadFile(filepath.Join(home, ".config", "systemd", "user", "jarvisd.socket"))
	if strings.Contains(string(socket), "18662") || strings.Contains(string(socket), "FreeBind") {
		t.Fatalf("socket unit:\n%s", socket)
	}
	text := out.String()
	for _, want := range []string{"已有的值不改", "--listen、--tls 只用于新装", "管理员密码已经设过了", "http://" + ip4(192, 168, 1, 20) + ":" + strconv.Itoa(port) + "/admin\n", "jarvis://pair?"} {
		if !strings.Contains(text, want) {
			t.Fatalf("no %q in:\n%s", want, text)
		}
	}
	if strings.Contains(text, "#setup=") {
		t.Fatalf("a setup code with the password set:\n%s", text)
	}
}

// db.sh wrote only database_url (no hub has run): the new-install defaults fill in around it.
func TestSetupFillsAFileNoHubHasRead(t *testing.T) {
	home := setupTestHome(t)
	var token string
	port := fakeHub(t, &token, false)
	cmds := &fakeCommands{replies: map[string]fakeReply{"loginctl show-user": {out: "yes\n"}}}
	e, out := testSetupEnv(t, home, setupOptions{listen: ":" + strconv.Itoa(port), tls: ":18662"}, cmds, "systemctl", "loginctl")
	if err := os.WriteFile(filepath.Join(e.root, "hub.json"), []byte(`{"database_url":"postgres://dbsh","admin":{"access":"loopback"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	e.pingDB = func(_ context.Context, u string) error {
		cfg, _ := readHubJSON(e.root)
		raw, _ := cfg.get("token")
		_ = json.Unmarshal(raw, &token)
		if u != "postgres://dbsh" {
			return errors.New("wrong database")
		}
		return nil
	}
	if err := e.setup(context.Background()); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	var cfg Config
	b, _ := os.ReadFile(filepath.Join(e.root, "hub.json"))
	_ = json.Unmarshal(b, &cfg)
	if cfg.DatabaseURL != "postgres://dbsh" || cfg.Admin.Access != "loopback" || cfg.Admin.FirstSetup != "code" || cfg.Public.TLSListen == "" || cfg.Token == "" {
		t.Fatalf("hub.json: %s", b)
	}
	if cmds.called("docker") {
		t.Fatalf("docker with a database_url: %v", cmds.calls)
	}
}

func TestSetupStopsWithoutADatabase(t *testing.T) {
	home := setupTestHome(t)
	for _, goos := range []string{"linux", "darwin"} {
		e, out := testSetupEnv(t, home, setupOptions{listen: ":18660", tls: ":18662"}, &fakeCommands{})
		e.goos = goos
		e.portFree = func(p int) bool { return p != 5432 || goos == "darwin" }
		_ = os.Remove(filepath.Join(e.root, "hub.json"))
		if err := e.setup(context.Background()); !errors.Is(err, ErrSetupStopped) {
			t.Fatalf("%s: %v", goos, err)
		}
		text := out.String()
		want := "装 Docker Desktop 或 OrbStack 后重跑这条命令"
		if goos == "linux" {
			want = "5432 端口上像是有 Postgres"
		}
		if !strings.Contains(text, want) || strings.Contains(text, "常驻") {
			t.Fatalf("%s:\n%s", goos, text)
		}
	}
}

func TestSetupDockerNotUsable(t *testing.T) {
	home := setupTestHome(t)
	cmds := &fakeCommands{replies: map[string]fakeReply{"docker info": {err: true, out: "permission denied while trying to connect to the Docker daemon socket"}}}
	e, out := testSetupEnv(t, home, setupOptions{listen: ":18660", tls: ":18662"}, cmds, "docker")
	if err := e.setup(context.Background()); !errors.Is(err, ErrSetupStopped) || !strings.Contains(out.String(), "usermod -aG docker someone") {
		t.Fatalf("%v\n%s", err, out)
	}
}

// The container is there (an earlier run, db.sh) but hub.json lost its address: the password comes from the container.
func TestSetupReusesTheContainer(t *testing.T) {
	home := setupTestHome(t)
	cmds := &fakeCommands{replies: map[string]fakeReply{
		"docker ps -a":   {out: "jarvis-postgres\n"},
		"docker inspect": {out: "PATH=/usr/bin\nPOSTGRES_PASSWORD=abc123\n"},
		"docker port":    {out: "127.0.0.1:18661\n"},
	}}
	e, out := testSetupEnv(t, home, setupOptions{listen: ":1", tls: ":18662", noService: true}, cmds, "docker")
	_ = e.setup(context.Background()) // stops at health: nothing runs
	b, _ := os.ReadFile(filepath.Join(e.root, "hub.json"))
	if !strings.Contains(string(b), `"database_url": "postgres://jarvis:abc123@127.0.0.1:18661/jarvis?sslmode=disable"`) || cmds.called("docker run") {
		t.Fatalf("%s\n%v\n%s", b, cmds.calls, out)
	}
	if !strings.Contains(out.String(), "personad hub run") {
		t.Fatalf("--no-service should say how to run it:\n%s", out)
	}
}

// Another program on 18660 / 18662: a new install moves along by 100.
func TestSetupMovesOffBusyPorts(t *testing.T) {
	home := setupTestHome(t)
	e, out := testSetupEnv(t, home, setupOptions{listen: ":18660", tls: ":18662"}, &fakeCommands{})
	e.portFree = func(p int) bool { return p != 18660 }
	if _, err := e.config(); err != nil {
		t.Fatal(err)
	}
	var cfg Config
	b, _ := os.ReadFile(filepath.Join(e.root, "hub.json"))
	_ = json.Unmarshal(b, &cfg)
	if cfg.Listen != ":18760" || !strings.HasSuffix(cfg.Public.TLSListen, ":18762") || !strings.HasSuffix(cfg.PublicURL, ":18760") || !strings.Contains(out.String(), "改用 18760 / 18762") {
		t.Fatalf("%s\n%s", b, out)
	}
}

func TestSetupOutsideOpensTLSOnAnExistingFile(t *testing.T) {
	home := setupTestHome(t)
	e, _ := testSetupEnv(t, home, setupOptions{listen: ":18660", tls: ":18662", outside: true}, &fakeCommands{})
	_ = os.WriteFile(filepath.Join(e.root, "hub.json"), []byte(`{"token":"t","public":{"tls_listen":"`+ip4(192, 168, 1, 20)+`:18672","advertise":["x"]}}`), 0o600)
	if _, err := e.config(); err != nil {
		t.Fatal(err)
	}
	var cfg Config
	b, _ := os.ReadFile(filepath.Join(e.root, "hub.json"))
	_ = json.Unmarshal(b, &cfg)
	if cfg.Public.TLSListen != ":18672" || len(cfg.Public.Advertise) != 1 || cfg.Token != "t" {
		t.Fatalf("%s", b)
	}
}

func TestSetupMacLaunchAgent(t *testing.T) {
	home := setupTestHome(t)
	var token string
	port := fakeHub(t, &token, false)
	cmds := &fakeCommands{replies: map[string]fakeReply{"launchctl print": {err: true}}}
	e, out := testSetupEnv(t, home, setupOptions{listen: ":" + strconv.Itoa(port), tls: ":18662"}, cmds)
	e.goos, e.uid = "darwin", 501
	e.run = func(ctx context.Context, env []string, name string, args ...string) ([]byte, error) {
		if name == "launchctl" && len(args) > 0 && args[0] == "bootstrap" { // the hub comes up with the agent
			cfg, _ := readHubJSON(e.root)
			raw, _ := cfg.get("token")
			_ = json.Unmarshal(raw, &token)
		}
		return cmds.run(ctx, env, name, args...)
	}
	_ = os.WriteFile(filepath.Join(e.root, "hub.json"), []byte(`{"database_url":"postgres://x"}`), 0o600)
	if err := e.setup(context.Background()); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	plist, _ := os.ReadFile(filepath.Join(home, "Library", "LaunchAgents", "com.jarvis.hub.plist"))
	bin := filepath.Join(e.root, "bin", "jarvisd")
	if !strings.Contains(string(plist), "<string>"+bin+"</string><string>hub</string>") || !strings.Contains(string(plist), "<key>PERSONA_HOME</key><string>"+e.root+"</string>") {
		t.Fatalf("plist:\n%s", plist)
	}
	if !cmds.called("launchctl bootstrap gui/501 " + filepath.Join(home, "Library", "LaunchAgents", "com.jarvis.hub.plist")) {
		t.Fatalf("calls %v", cmds.calls)
	}
	if !strings.Contains(out.String(), "▀") && !strings.Contains(out.String(), "█") {
		t.Fatalf("no QR code:\n%s", out)
	}
}

// The socket unit is hub-systemd.sh's (keep the two in step); FreeBind only with an address-bound listener.
func TestHubUnitsMatchHubSystemd(t *testing.T) {
	want := "[Unit]\nDescription=Persona hub's listening sockets (docs/hub-restart.md)\n\n[Socket]\nListenStream=18660\nListenStream=18662\n" +
		"Backlog=4096\nNoDelay=true\nService=jarvisd.service\n\n[Install]\nWantedBy=sockets.target\n"
	if got := hubSocketUnit([]string{":18660", ":18662"}); got != want {
		t.Fatalf("socket:\n%s", got)
	}
	svc := hubServiceUnit("/home/someone/.persona", true)
	for _, line := range []string{`ExecStart="/home/someone/.persona/bin/jarvisd" hub`, "Wants=jarvisd.socket", `Environment="PERSONA_HOME=/home/someone/.persona"`, "Restart=always"} {
		if !strings.Contains(svc, line+"\n") {
			t.Fatalf("no %q in:\n%s", line, svc)
		}
	}
}

// admin.first_setup "code" (new installs): a LAN source needs the setup code too; old files keep loopback only.
func TestAdminFirstSetupCodeFromTheLAN(t *testing.T) {
	h, _, mux := adminTestHub(t)
	lan := &adminClient{t: t, h: mux, remote: ip4(192, 168, 1, 20) + ":1"}
	if _, st, _ := lan.do("GET", "/api/v1/admin/state", ""); st["setup_code_required"] != false {
		t.Fatalf("old file: %v", st)
	}
	h.cfg.Admin.FirstSetup = "code"
	if _, st, _ := lan.do("GET", "/api/v1/admin/state", ""); st["setup_code_required"] != true || st["can_setup"] != true {
		t.Fatalf("first_setup code: %v", st)
	}
	if code, out, _ := lan.do("POST", "/api/v1/admin/setup", `{"password":"a good password"}`); code != 403 || out["code"] != "setup_code_required" {
		t.Fatalf("no code: %d %v", code, out)
	}
	setup, _ := h.newSetupCode()
	if code, st, _ := lan.do("POST", "/api/v1/admin/setup", `{"password":"a good password","setup_code":"`+setup+`"}`); code != 200 || st["logged_in"] != true {
		t.Fatalf("with the code: %d %v", code, st)
	}
}

func TestFillMissing(t *testing.T) {
	dst, _ := parseJSONObject([]byte(`{"a":1,"admin":{"access":"lan"},"machines":[{"id":"x"}],"n":null}`))
	src, _ := parseJSONObject([]byte(`{"a":2,"admin":{"access":"any","first_setup":"code"},"machines":[],"b":3,"n":{"x":1}}`))
	if err := fillMissing(dst, src); err != nil {
		t.Fatal(err)
	}
	got, _ := dst.indented()
	var v map[string]any
	_ = json.Unmarshal(got, &v)
	admin := v["admin"].(map[string]any)
	if v["a"] != float64(1) || v["b"] != float64(3) || admin["access"] != "lan" || admin["first_setup"] != "code" || len(v["machines"].([]any)) != 1 || v["n"] != nil {
		t.Fatalf("%s", got)
	}
}

// A TLS address this machine does not have (the LAN address moved): the hub starts without TLS instead of not at all.
func TestTLSAddressGone(t *testing.T) {
	addr := net.JoinHostPort(ip4(192, 0, 2, 77), "0") // TEST-NET-1: never this machine's
	_, err := net.Listen("tcp", addr)
	if err == nil {
		t.Skip("this machine has the address")
	}
	if !tlsAddressGone(addr, err) {
		t.Fatalf("%v not taken for a vanished address", err)
	}
	if _, err := net.Listen("tcp", "bad:addr:x"); tlsAddressGone(":18662", err) {
		t.Fatal("no host: not a vanished address")
	}
}

// An agent made before names the binary through ~/.jarvis, the same directory as ~/.persona: setup finds it (as
// hub-systemd.sh does), restarts it once for the binary it has not noted, and installs no second agent.
func TestSetupMacFindsAnAgentThroughASymlink(t *testing.T) {
	home := setupTestHome(t)
	token := "t"
	port := fakeHub(t, &token, true)
	cmds := &fakeCommands{replies: map[string]fakeReply{}}
	e, out := testSetupEnv(t, home, setupOptions{listen: ":" + strconv.Itoa(port), tls: ":18662"}, cmds)
	e.goos, e.uid = "darwin", 501
	if err := os.Symlink(".persona", filepath.Join(home, ".jarvis")); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(e.root, "hub.json"), []byte(`{"token":"t","listen":":`+strconv.Itoa(port)+`","database_url":"postgres://x"}`), 0o600)
	agents := filepath.Join(home, "Library", "LaunchAgents")
	_ = os.MkdirAll(agents, 0o755)
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict>
  <key>Label</key><string>com.example.myhub</string>
  <key>ProgramArguments</key><array><string>~/.jarvis/bin/jarvisd</string><string>hub</string></array>
  <key>KeepAlive</key><true/>
</dict></plist>
`
	if err := os.WriteFile(filepath.Join(agents, "myhub.plist"), []byte(plist), 0o644); err != nil {
		t.Fatal(err)
	}
	e.portFree = func(int) bool { return false } // the old hub holds the port
	if err := e.setup(context.Background()); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(agents, "com.jarvis.hub.plist")); err == nil || cmds.called("launchctl bootstrap") {
		t.Fatalf("a second agent: %v", cmds.calls)
	}
	if !cmds.called("launchctl kickstart -k gui/501/com.example.myhub") {
		t.Fatalf("not restarted for the new binary: %v", cmds.calls)
	}
	cmds.calls = nil
	if err := e.setup(context.Background()); err != nil || cmds.called("launchctl kickstart") {
		t.Fatalf("again: %v %v", err, cmds.calls)
	}
}

// A hub running some other way (a terminal) on a Mac: no agent that could not get the port and restart forever.
func TestSetupMacHubRunningElsewhere(t *testing.T) {
	home := setupTestHome(t)
	token := "t"
	port := fakeHub(t, &token, true)
	cmds := &fakeCommands{replies: map[string]fakeReply{"launchctl print": {err: true}}}
	e, out := testSetupEnv(t, home, setupOptions{listen: ":" + strconv.Itoa(port), tls: ":18662"}, cmds)
	e.goos, e.uid = "darwin", 501
	_ = os.WriteFile(filepath.Join(e.root, "hub.json"), []byte(`{"token":"t","listen":":`+strconv.Itoa(port)+`","database_url":"postgres://x"}`), 0o600)
	if err := e.setup(context.Background()); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if cmds.called("launchctl bootstrap") || !strings.Contains(out.String(), "不是 launchd 服务起的") {
		t.Fatalf("calls %v\n%s", cmds.calls, out)
	}
}

func TestLaunchdJob(t *testing.T) {
	label, args, ok := launchdJob([]byte(`<plist version="1.0"><dict><key>Label</key><string>a.b</string><key>Program</key><string>/x/jarvisd</string><key>RunAtLoad</key><true/></dict></plist>`))
	if !ok || label != "a.b" || len(args) != 1 || args[0] != "/x/jarvisd" {
		t.Fatalf("%q %q %v", label, args, ok)
	}
	if _, _, ok := launchdJob([]byte("bplist00")); ok {
		t.Fatal("binary plist parsed")
	}
}

// A container on the host's network listens on database_url's port inside, not 5432 (db.sh's PGPORT).
func TestStartContainerHostNetwork(t *testing.T) {
	home := setupTestHome(t)
	cmds := &fakeCommands{replies: map[string]fakeReply{"docker inspect -f {{.HostConfig.NetworkMode}}": {out: "host\n"}}}
	e, _ := testSetupEnv(t, home, setupOptions{}, cmds, "docker")
	if err := e.startContainer(context.Background(), "postgres://jarvis:p@127.0.0.1:5544/jarvis?sslmode=disable"); err != nil {
		t.Fatal(err)
	}
	if !cmds.called("docker exec jarvis-postgres pg_isready -h 127.0.0.1 -p 5544 ") {
		t.Fatalf("%v", cmds.calls)
	}
	cmds.replies["docker inspect -f {{.HostConfig.NetworkMode}}"] = fakeReply{out: "bridge\n"}
	cmds.calls = nil
	_ = e.startContainer(context.Background(), "postgres://jarvis:p@127.0.0.1:5544/jarvis")
	if !cmds.called("docker exec jarvis-postgres pg_isready -h 127.0.0.1 -p 5432 ") {
		t.Fatalf("%v", cmds.calls)
	}
}

// jarvisd.socket binds a moved LAN address anyway (FreeBind): the hub says so instead of TLS going quiet.
func TestAddressNotLocal(t *testing.T) {
	lan := ip4(192, 168, 1, 20)
	addrs := func() ([]net.Addr, error) {
		return []net.Addr{&net.IPNet{IP: net.ParseIP(lan), Mask: net.CIDRMask(24, 32)}}, nil
	}
	if _, gone := addressNotLocal(lan+":18662", addrs); gone {
		t.Fatal("own address taken for gone")
	}
	if host, gone := addressNotLocal(ip4(192, 168, 1, 21)+":18662", addrs); !gone || host != ip4(192, 168, 1, 21) {
		t.Fatal("moved address not noticed")
	}
	if _, gone := addressNotLocal(":18662", addrs); gone {
		t.Fatal("every address taken for gone")
	}
}

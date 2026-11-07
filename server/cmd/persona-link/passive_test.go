package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/link"
)

func TestPairListenPrintsOnlyTheLinkOnStdout(t *testing.T) {
	f := newFakeEnv(t)
	var stdout, stderr bytes.Buffer
	f.stdout, f.stderr = &stdout, &stderr
	if err := pairCmd(f.env, []string{"--listen", "--addr", "hub-reachable.example.com", "--addr", "[2001:db8::7]:9"}); err != nil {
		t.Fatal(err)
	}
	out := strings.TrimSpace(stdout.String())
	addrs, fp, code, err := link.ParseConnectorLink(out)
	if err != nil || strings.Contains(out, "\n") || strings.Join(addrs, " ") != "hub-reachable.example.com:18663 [2001:db8::7]:9" || fp == "" || code == "" {
		t.Fatalf("stdout %q: %v", out, err)
	}
	for _, want := range []string{"让 hub 来连", "监听 :18663", "电脑 › 添加电脑 › hub 来连", "10 分钟内有效", "只对 hub 放行 TCP 18663",
		"persona-link install --listen"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr lacks %q:\n%s", want, stderr.String())
		}
	}
	c, err := link.Read(f.configPath())
	if err != nil || c.CurrentMode() != link.ModePassive || c.Passive.Code == nil || strings.Contains(mustRead(t, f.configPath()), code) {
		t.Fatalf("link.json %+v %v", c, err)
	}
	// --listen takes an address; the link and --machine belong to active mode.
	stdout.Reset()
	if err := pairCmd(f.env, []string{"--listen=127.0.0.1:18700"}); err != nil || !strings.Contains(stdout.String(), "addr=127.0.0.1%3A18700") {
		t.Fatalf("%v %s", err, stdout.String())
	}
	if err := pairCmd(f.env, []string{"--listen", "--machine", "pi"}); err == nil {
		t.Fatal("--listen with --machine")
	}
}

func mustRead(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestModeCommand(t *testing.T) {
	f := newFakeEnv(t)
	if err := modeCmd(f.env, nil); err != nil || !strings.Contains(f.out.String(), "模式：我去连 hub（主动）") {
		t.Fatalf("%v %s", err, f.out)
	}
	f.out.Reset()
	if err := modeCmd(f.env, []string{"passive", "--listen", "18700"}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"改好了：让 hub 来连（被动），监听 :18700", "persona-link pair --listen", "只对 hub 放行 TCP 18700"} {
		if !strings.Contains(f.out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, f.out)
		}
	}
	f.pairHere(t) // active mode paired too (pairHere replaces the whole file: passive again below)
	if _, err := link.SetMode(f.configPath(), link.ModePassive, ""); err != nil {
		t.Fatal(err)
	}
	f.out.Reset()
	if err := modeCmd(f.env, []string{"active"}); err != nil || !strings.Contains(f.out.String(), "改好了：我去连 hub（主动）。") ||
		strings.Contains(f.out.String(), "还没配对") {
		t.Fatalf("%v %s", err, f.out)
	}
	if c, _ := link.Load(f.configPath()); c == nil || c.CurrentMode() != link.ModeActive || c.Passive == nil {
		t.Fatalf("%+v", c)
	}
	if err := modeCmd(f.env, []string{"sideways"}); err != errUsage {
		t.Fatalf("%v", err)
	}
}

func TestPassiveStatus(t *testing.T) {
	f := newFakeEnv(t)
	f.onCommand = func(call string) (string, error) {
		switch {
		case strings.Contains(call, "is-active"):
			return "active", nil
		case strings.Contains(call, "is-enabled"):
			return "enabled", nil
		}
		return "", nil
	}
	paired := f.now().Add(-26 * time.Hour)
	if err := link.Update(f.configPath(), func(c *link.Config) error {
		c.Mode, c.Passive = link.ModePassive, &link.Passive{Listen: ":18663", HubID: "0f1e2d3c-0000-4000-8000-000000000000", HubName: "家里",
			Machine: "example-workstation", TokenSHA256: strings.Repeat("ab", 32), PairedAt: &paired}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := link.Identity(link.TLSDir(f.configPath())); err != nil {
		t.Fatal(err)
	}
	release, err := link.LockRun(f.configPath())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	seen := f.now().Add(-3 * time.Minute)
	f.saveStatus(t, link.Status{State: "listening", Mode: link.ModePassive, Listen: ":18663", Machine: "example-workstation", HubName: "家里",
		LastSeen: &seen, From: "192.0.2.10", Node: "online", Addrs: []string{"[fd00::10]:18663"}}, 77)
	if err := status(f.env, nil); err != nil {
		t.Fatalf("listening is fine in passive mode: %v\n%s", err, f.out)
	}
	out := f.out.String()
	for _, want := range []string{"模式：让 hub 来连（被动），监听 :18663\n", "链路：在监听，等 hub 连进来\n", "hub「家里」（1 天 2 小时前配对）\n",
		"上一次 hub 连进来：3 分钟前，来自 192.0.2.10\n", "这台电脑：hub 上的「example-workstation」\n", "连接器指纹：", "  · [fd00::10]:18663\n",
		"后台服务：没装（persona-link install）", "进程：pid 77"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "主动模式也配对过") || strings.Contains(out, "还没有 hub 来配对") {
		t.Errorf("output:\n%s", out)
	}
	since := f.now().Add(-time.Minute)
	f.saveStatus(t, link.Status{State: "connected", Mode: link.ModePassive, Listen: ":18663", Address: "192.0.2.10", Since: &since, Node: "online"}, 77)
	f.out.Reset()
	if err := status(f.env, []string{"--json"}); err != nil {
		t.Fatal(err)
	}
	var r report
	if err := json.Unmarshal(f.out.Bytes(), &r); err != nil || r.Mode != link.ModePassive || r.Passive == nil || !r.Passive.Paired ||
		r.Passive.FP == "" || r.Passive.Machine != "example-workstation" || r.Link.State != "connected" || strings.Contains(f.out.String(), strings.Repeat("ab", 32)) {
		t.Fatalf("%v %s", err, f.out)
	}
	f.out.Reset()
	_ = status(f.env, nil)
	if !strings.Contains(f.out.String(), "链路：已连接 · hub 从 192.0.2.10 连进来 · 已连 1 分钟\n") || !strings.Contains(f.out.String(), "本机 node：在线") {
		t.Fatalf("%s", f.out)
	}
	// Cannot listen: not fine, and why.
	retry := f.now().Add(4 * time.Second)
	f.saveStatus(t, link.Status{State: "waiting", Mode: link.ModePassive, Listen: ":18663", Retry: &retry, Error: ":18663：端口被占用了"}, 77)
	f.out.Reset()
	if codeOf(status(f.env, nil)) != 1 || !strings.Contains(f.out.String(), "链路：监听不了 · 4 秒后重试\n  · :18663：端口被占用了\n") {
		t.Fatalf("%s", f.out)
	}
}

func TestInstallPassive(t *testing.T) {
	f := newFakeEnv(t)
	f.linger = true
	f.onCommand = func(call string) (string, error) {
		if call == "systemctl --user restart persona-link.service" {
			f.saveStatus(t, link.Status{State: "unpaired", Mode: link.ModePassive, Listen: ":18663"}, 60)
		}
		return "", nil
	}
	if err := install(f.env, []string{"--listen", "--addr", "workstation.example.com"}); err != nil {
		t.Fatalf("%v\n%s", err, f.out)
	}
	out := f.out.String()
	for _, want := range []string{"等它开始监听…", "在监听 :18663，等 hub 来配对。", "电脑 › 添加电脑 › hub 来连",
		"persona://connector?addr=workstation.example.com%3A18663&fp=", "只对 hub 放行 TCP 18663"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	unit, _ := os.ReadFile(f.unitPath())
	if !strings.Contains(string(unit), " run --log ") {
		t.Fatalf("unit %s", unit)
	}
	// Paired already: installing again keeps the pairing, prints no link.
	if err := link.Update(f.configPath(), func(c *link.Config) error {
		c.Passive.TokenSHA256, c.Passive.HubName, c.Passive.Code = strings.Repeat("cd", 32), "家里", nil
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	f.out.Reset()
	f.onCommand = func(call string) (string, error) {
		if call == "systemctl --user restart persona-link.service" {
			f.saveStatus(t, link.Status{State: "listening", Mode: link.ModePassive, Listen: ":18663", HubName: "家里"}, 61)
		}
		return "", nil
	}
	if err := install(f.env, []string{"--listen"}); err != nil {
		t.Fatal(err)
	}
	if out := f.out.String(); strings.Contains(out, "persona://connector") || !strings.Contains(out, "已经和 hub「家里」配对过") ||
		!strings.Contains(out, "在监听 :18663，等 hub「家里」连进来。") {
		t.Fatalf("output:\n%s", out)
	}
	if err := install(f.env, []string{"--listen", "--pair", "x"}); err == nil {
		t.Fatal("both modes at once")
	}
}

func TestNodeServiceLinux(t *testing.T) {
	linuxPaths(t)
	f := newFakeEnv(t)
	personad := filepath.Join(t.TempDir(), "personad")
	_ = os.WriteFile(personad, []byte("#!/bin/sh\n# personad\n"), 0o755)
	if err := nodeCmd(f.env, []string{"install"}); err == nil || !strings.Contains(err.Error(), "--personad") {
		t.Fatalf("without personad: %v", err)
	}
	if err := nodeCmd(f.env, []string{"install", "--personad", personad}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(f.nodeBin()); !strings.Contains(string(b), "# personad") {
		t.Fatal("personad not copied")
	}
	unit := mustRead(t, f.nodeUnitPath())
	if !strings.Contains(unit, `ExecStart="`+f.nodeBin()+`" node --attach`) || !strings.Contains(unit, "StandardError=append:"+f.nodeLog()) ||
		strings.Contains(unit, "network-online") {
		t.Fatalf("unit:\n%s", unit)
	}
	if strings.Join(f.calls, "\n") != "systemctl --user show-environment\nsystemctl --user daemon-reload\nsystemctl --user enable --now personad-node.service" {
		t.Fatalf("calls %q", f.calls)
	}
	// There already (installed by hand, by scripts/deploy/node-launchd.sh): left as it is.
	_ = os.WriteFile(f.nodeUnitPath(), []byte("mine"), 0o644)
	f.calls = nil
	f.out.Reset()
	if err := nodeInstall(f.env, personad); err != nil || mustRead(t, f.nodeUnitPath()) != "mine" || len(f.calls) != 0 ||
		!strings.Contains(f.out.String(), "保持原样") {
		t.Fatalf("%v %q %s", err, f.calls, f.out)
	}
	if err := nodeCmd(f.env, []string{"uninstall"}); err != nil || fileExists(f.nodeUnitPath()) || !fileExists(f.nodeBin()) {
		t.Fatalf("uninstall: %v", err)
	}
}

func TestInstallSetsUpTheNodeToo(t *testing.T) {
	f := newFakeEnv(t)
	f.pairHere(t)
	f.linger = true
	personad := filepath.Join(t.TempDir(), "personad")
	_ = os.WriteFile(personad, []byte("x"), 0o755)
	f.onCommand = func(call string) (string, error) {
		if call == "systemctl --user restart persona-link.service" {
			f.saveStatus(t, link.Status{State: "connected", Mode: link.ModeActive, Kind: "lan", Address: "https://192.0.2.10:18662", Node: "online"}, 9)
		}
		return "", nil
	}
	if err := install(f.env, []string{"--personad", personad}); err != nil {
		t.Fatalf("%v\n%s", err, f.out)
	}
	calls := strings.Join(f.calls, "\n")
	if !strings.Contains(calls, "enable --now personad-node.service") || !strings.Contains(calls, "restart persona-link.service") ||
		!strings.Contains(f.out.String(), "装好了常驻 node 的服务") || !strings.Contains(f.out.String(), "本机 node 在线") {
		t.Fatalf("calls %q\n%s", f.calls, f.out)
	}
	f.calls = nil
	if err := install(f.env, []string{"--no-node"}); err != nil || strings.Contains(strings.Join(f.calls, "\n"), "personad-node") {
		t.Fatalf("--no-node: %v %q", err, f.calls)
	}
}

func TestNodeServiceMac(t *testing.T) {
	f := newFakeEnv(t)
	f.goos, f.uid = "darwin", 501
	personad := filepath.Join(t.TempDir(), "personad")
	_ = os.WriteFile(personad, []byte("x"), 0o755)
	f.onCommand = func(call string) (string, error) {
		if strings.HasPrefix(call, "launchctl print") {
			return "", exec.ErrNotFound
		}
		return "", nil
	}
	if st := nodeStatus(f.env); st.Installed || st.HasBin {
		t.Fatalf("%+v", st)
	}
	if err := nodeInstall(f.env, personad); err != nil {
		t.Fatal(err)
	}
	plist := f.nodePlistPath()
	if plist != filepath.Join(f.home, "Library", "LaunchAgents", "com.persona.node.plist") {
		t.Fatal(plist)
	}
	if strings.Join(f.calls, "\n") != "launchctl bootout gui/501/com.persona.node\nlaunchctl bootstrap gui/501 "+plist {
		t.Fatalf("calls %q", f.calls)
	}
	body := mustRead(t, plist)
	if !strings.Contains(body, "<string>"+f.nodeBin()+"</string><string>node</string><string>--attach</string>") {
		t.Fatalf("plist:\n%s", body)
	}
	// Not throttled like a background job (#5944): `personad node doctor` reports a plist without it.
	if !strings.Contains(body, "\n  <key>ProcessType</key><string>Interactive</string>\n") {
		t.Fatalf("no ProcessType Interactive:\n%s", body)
	}
	if runtime.GOOS == "darwin" {
		if out, err := exec.Command("plutil", "-lint", plist).CombinedOutput(); err != nil {
			t.Fatalf("plutil: %s", out)
		}
	}
	if st := nodeStatus(f.env); !st.Installed || !st.HasBin || st.Running != "not loaded" {
		t.Fatalf("%+v", st)
	}
	// A home with characters XML cares about still makes a valid plist.
	if p := nodePlist("/Users/a&b/<x>/personad", "/tmp/l"); !strings.Contains(p, "/Users/a&amp;b/&lt;x&gt;/personad") {
		t.Fatal(p)
	}
}

func TestDiscover(t *testing.T) {
	f := newFakeEnv(t)
	old := browseHubs
	defer func() { browseHubs = old }()
	browseHubs = func(ctx context.Context, d time.Duration) ([]foundHub, error) {
		if d != 1500*time.Millisecond {
			t.Errorf("wait %s", d)
		}
		return []foundHub{{HubID: "h1", Name: "家里", Addresses: []string{"https://[fd00::10]:18662"}}}, nil
	}
	if err := discover(f.env, []string{"--wait", "1.5"}); err != nil || f.out.String() != "hub「家里」 · https://[fd00::10]:18662\n" {
		t.Fatalf("%v %q", err, f.out)
	}
	browseHubs = func(context.Context, time.Duration) ([]foundHub, error) { return nil, nil }
	f.out.Reset()
	if err := discover(f.env, []string{"--json"}); codeOf(err) != 1 || strings.TrimSpace(f.out.String()) != "[]" {
		t.Fatalf("%v %q", err, f.out)
	}
	if addressRank("https://[fd00::1]:1") != 0 || addressRank("https://[2400:cb00::1]:1") != 1 || addressRank("https://hub.example.com:1") != 2 {
		t.Fatal("rank")
	}
}

func TestUninstallPurgeTakesTheIdentityToo(t *testing.T) {
	f := newFakeEnv(t)
	if _, err := link.PairListen(f.configPath(), "", []string{"workstation.example.com"}, f.now()); err != nil {
		t.Fatal(err)
	}
	if !fileExists(link.TLSDir(f.configPath())) {
		t.Fatal("no identity")
	}
	if err := uninstall(f.env, []string{"--purge"}); err != nil || fileExists(link.TLSDir(f.configPath())) || fileExists(f.configPath()) {
		t.Fatalf("%v %s", err, f.out)
	}
}

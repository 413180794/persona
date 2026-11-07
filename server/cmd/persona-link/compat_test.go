package main

import (
	"bytes"
	"context"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/link"
	"github.com/nwverse/persona/server/internal/mdns"
)

func compatSeed(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCompatDaemonFlags(t *testing.T) {
	for _, args := range [][]string{{"--jarvisd", "file"}, {"--jarvisd=file"}, {"--personad", "file"}, {"--personad=file"}} {
		v, _, ok, err := flagValue(args, 0, "--personad")
		if err != nil || !ok || v != "file" {
			t.Fatalf("%v: %s %v %v", args, v, ok, err)
		}
	}
	if _, _, _, err := flagValue([]string{"--jarvisd"}, 0, "--personad"); err == nil {
		t.Fatal("missing value accepted")
	}
	for _, command := range []string{"install", "node"} {
		t.Run(command, func(t *testing.T) {
			f := newFakeEnv(t)
			from := filepath.Join(t.TempDir(), "daemon")
			compatSeed(t, from, "binary")
			var err error
			if command == "install" {
				err = install(f.env, []string{"--jarvisd=" + from})
			} else {
				err = nodeCmd(f.env, []string{"install", "--jarvisd", from})
			}
			if err != nil {
				t.Fatal(err)
			}
			if !fileExists(f.nodeBin()) {
				t.Fatal("alias did not install the node")
			}
		})
	}
}

func TestCompatExistingLinuxInstallation(t *testing.T) {
	f := newFakeEnv(t)
	dir := filepath.Dir(f.unitPath())
	oldUnit := filepath.Join(dir, legacyLinkUnit)
	oldNode := filepath.Join(dir, legacyNodeUnit)
	compatSeed(t, oldUnit, "old connector")
	compatSeed(t, oldNode, "old node")
	compatSeed(t, filepath.Join(f.personaDir(), "bin", legacyLinkBinary), "old binary")
	compatSeed(t, filepath.Join(f.personaDir(), "bin", legacyNodeBinary), "old daemon")
	if err := install(f.env, []string{}); err != nil {
		t.Fatal(err)
	}
	if fileExists(filepath.Join(dir, unitName)) || fileExists(filepath.Join(dir, nodeUnitName)) {
		t.Fatal("created a second service identity")
	}
	b, _ := os.ReadFile(oldNode)
	if string(b) != "old node" {
		t.Fatal("existing node modified")
	}
	b, _ = os.ReadFile(oldUnit)
	if !strings.Contains(string(b), legacyLinkBinary) {
		t.Fatal("existing executable path abandoned")
	}
	st := gather(f.env)
	if st.Service.Unit != legacyLinkUnit || nodeStatus(f.env).File != oldNode {
		t.Fatalf("wrong service report: %+v", st.Service)
	}
	if err := uninstall(f.env, nil); err != nil {
		t.Fatal(err)
	}
	if err := nodeCmd(f.env, []string{"uninstall"}); err != nil {
		t.Fatal(err)
	}
	calls := strings.Join(f.calls, "\n")
	for _, want := range []string{"restart " + legacyLinkUnit, "is-active " + legacyLinkUnit, "disable --now " + legacyLinkUnit, "disable --now " + legacyNodeUnit} {
		if !strings.Contains(calls, want) {
			t.Errorf("missing %s in %s", want, calls)
		}
	}
	if strings.Contains(calls, "restart "+unitName) {
		t.Fatal("canonical service restarted")
	}
}

func TestCompatConflictingInstallationsHaveNoSideEffects(t *testing.T) {
	f := newFakeEnv(t)
	dir := filepath.Dir(f.unitPath())
	compatSeed(t, filepath.Join(dir, unitName), "new")
	compatSeed(t, filepath.Join(dir, legacyLinkUnit), "old")
	if err := install(f.env, nil); err == nil {
		t.Fatal("ambiguous installations accepted")
	}
	if len(f.calls) != 0 || fileExists(f.binPath()) || fileExists(f.configPath()) {
		t.Fatal("conflict check mutated the installation")
	}
}

func TestCompatExistingMacNode(t *testing.T) {
	f := newFakeEnv(t)
	f.goos = "darwin"
	old := filepath.Join(f.home, "Library", "LaunchAgents", legacyNodeLabel+".plist")
	compatSeed(t, old, "old plist")
	if err := nodeInstall(f.env, ""); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 0 || fileExists(filepath.Join(filepath.Dir(old), nodeLabel+".plist")) {
		t.Fatal("old node provisioned again")
	}
	if nodeStatus(f.env).File != old {
		t.Fatal("old node not reported")
	}
	if err := nodeCmd(f.env, []string{"uninstall"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(f.calls, "\n"), "/"+legacyNodeLabel) {
		t.Fatal("wrong launchd label")
	}
}

func TestCompatWindowsInstallationPaths(t *testing.T) {
	f, registered := newWindowsEnv(t)
	*registered = true
	oldFile := filepath.Join(f.personaDir(), legacyTaskFile)
	oldExe := filepath.Join(f.personaDir(), "bin", legacyServiceExe)
	compatSeed(t, oldExe, "old exe")
	if err := writeUTF16(oldFile, taskXML("demo", oldExe, "supervise", f.now())); err != nil {
		t.Fatal(err)
	}
	if f.taskFile() != oldFile || f.serviceExePath() != oldExe || !f.taskRunsNode() {
		t.Fatal("old scheduled task paths not retained")
	}
	if st := f.taskState(); st.Unit != "Persona Link" || st.File != oldFile {
		t.Fatalf("wrong task %+v", st)
	}
	if err := install(f.env, []string{"--no-node"}); err != nil {
		t.Fatal(err)
	}
	if fileExists(filepath.Join(f.personaDir(), "persona-link-task.xml")) || fileExists(filepath.Join(f.personaDir(), "bin", serviceExe)) {
		t.Fatal("second task artifacts created")
	}
	for _, call := range f.calls {
		if strings.HasPrefix(call, "schtasks ") && !strings.Contains(call, "/TN Persona Link") {
			t.Fatalf("task identity changed: %s", call)
		}
	}
}

func TestCompatDiscoveryBothNamesDeduplicateAndGoodbye(t *testing.T) {
	old := runMDNS
	t.Cleanup(func() { runMDNS = old })
	var mu sync.Mutex
	seen := map[string]bool{}
	runMDNS = func(ctx context.Context, o mdns.Options) error {
		mu.Lock()
		seen[o.Browse] = true
		mu.Unlock()
		e := mdns.Entry{Port: 80, TTL: time.Minute, TXT: map[string]string{"hub_id": "same", "name": "demo", "tls": "443"}, Addrs: []netip.Addr{netip.MustParseAddr("192.0.2.1")}}
		o.Found(e)
		if o.Browse == legacyHubService {
			e.TTL = 0
			o.Found(e)
		}
		return nil
	}
	hubs, err := browseCompatibleHubs(context.Background(), time.Second)
	if err != nil || len(hubs) != 1 || len(hubs[0].Addresses) != 1 || len(seen) != 2 {
		t.Fatalf("%+v %v %v", hubs, err, seen)
	}
}

func TestCompatDiscoveryLegacyOnlyAndOneBrowserFails(t *testing.T) {
	old := runMDNS
	t.Cleanup(func() { runMDNS = old })
	runMDNS = func(ctx context.Context, o mdns.Options) error {
		if o.Browse == hubService {
			return errors.New("unavailable")
		}
		o.Found(mdns.Entry{Port: 443, TTL: time.Minute, TXT: map[string]string{"hub_id": "old"}, Addrs: []netip.Addr{netip.MustParseAddr("192.0.2.2")}})
		return nil
	}
	hubs, err := browseCompatibleHubs(context.Background(), time.Second)
	if err != nil || len(hubs) != 1 {
		t.Fatalf("%+v %v", hubs, err)
	}
}

func TestCompatInviteOutputUsesOneCode(t *testing.T) {
	f := newFakeEnv(t)
	inv, err := link.PairListen(f.configPath(), "", []string{"example.com:18663"}, f.now())
	if err != nil {
		t.Fatal(err)
	}
	printInvite(f.env, inv)
	if !strings.Contains(f.out.String(), inv.Link+"\n"+inv.LegacyLink+"\n") {
		t.Fatal("both URLs not printed")
	}
}

func TestCompatPairListenPipeKeepsSingleCanonicalURL(t *testing.T) {
	f := newFakeEnv(t)
	stderr := &bytes.Buffer{}
	f.stderr = stderr
	if err := pairListen(f.env, "", []string{"example.com:18663"}); err != nil {
		t.Fatal(err)
	}
	canonical := strings.TrimSpace(f.out.String())
	if strings.Contains(canonical, "\n") || !strings.HasPrefix(canonical, "persona://connector?") {
		t.Fatal("pipe output changed")
	}
	if !strings.Contains(stderr.String(), link.LegacyPairLink(canonical)+"\n") {
		t.Fatal("baseline URL not exposed")
	}
}

func TestCompatServiceAliasIsNotIndependentInstallation(t *testing.T) {
	f := newFakeEnv(t)
	canonical := f.unitPath()
	compatSeed(t, canonical, "shared unit")
	if err := os.Symlink(canonical, filepath.Join(filepath.Dir(canonical), legacyLinkUnit)); err != nil {
		t.Fatal(err)
	}
	if err := f.installationConflict(false); err != nil {
		t.Fatalf("alias treated as independent: %v", err)
	}
}

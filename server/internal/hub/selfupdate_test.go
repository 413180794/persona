package hub

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/update"
)

// fakePublisher serves a release whose binary is a shell script answering `version -v` as the real one does.
type fakePublisher struct {
	srv     *httptest.Server
	corrupt bool // serve another file than the one in the manifest
	script  []byte
	extra   map[string][]byte // more downloads by path (a program's)
}

func newPublisher(t *testing.T, version string, build int64) *fakePublisher {
	return newPublisherWith(t, version, build, func(*update.Manifest) {})
}

// newPublisherWith is newPublisher with the manifest changed first (its commit and ancestors).
func newPublisherWith(t *testing.T, version string, build int64, edit func(*update.Manifest)) *fakePublisher {
	t.Helper()
	p := &fakePublisher{script: []byte(fmt.Sprintf("#!/bin/sh\necho %s\necho 'channel release, build %d'\n", version, build))}
	gz := gz(p.script)
	outer, inner := sha256.Sum256(gz), sha256.Sum256(p.script)
	name := "jarvisd-" + runtime.GOOS + "-" + runtime.GOARCH + ".gz"
	m := update.Manifest{Version: version, Build: build, Protocol: proto.Protocol, Released: time.Now().UTC().Truncate(time.Second),
		Files: map[string]update.File{runtime.GOOS + "-" + runtime.GOARCH: {Name: name, Size: int64(len(gz)),
			SHA256: hex.EncodeToString(outer[:]), BinarySHA256: hex.EncodeToString(inner[:])}}}
	edit(&m)
	data, _ := json.Marshal(m)
	p.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/dl/latest.json":
			w.Write(data)
		case "/dl/" + name:
			if p.corrupt {
				w.Write(bytes.Repeat([]byte{0}, len(gz)))
				return
			}
			w.Write(gz)
		default:
			if b, ok := p.extra[r.URL.Path]; ok {
				w.Write(b)
				return
			}
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(p.srv.Close)
	return p
}

func gz(b []byte) []byte {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	zw.Write(b)
	zw.Close()
	return buf.Bytes()
}

// updatingHub is a hub of build 202611021500 that updates from p, running target (its "binary"), with one machine.
func updatingHub(t *testing.T, p *fakePublisher, channel string) (*Hub, *Machine, string, *[]string) {
	t.Helper()
	h := newTestHub(t)
	h.build = update.Make("v1-202611021500", channel, "202611021500", p.srv.URL+"/dl")
	h.version = h.build.Version
	u := h.upd
	u.client = p.srv.Client()
	u.idleEvery, u.confirmIn, u.manualIdle = 20*time.Millisecond, 300*time.Millisecond, 2*time.Second
	target := filepath.Join(t.TempDir(), "jarvisd")
	if err := os.WriteFile(target, []byte("#!/bin/sh\necho old\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	u.target = func() (string, error) { return target, nil }
	var mu sync.Mutex
	var execd []string
	u.exec = func(path string) error { mu.Lock(); execd = append(execd, path); mu.Unlock(); return nil }
	m := newMachine(h, MachineConfig{ID: "box", Name: "Box", Kind: "ssh"})
	m.online = true
	h.machines, h.byID["box"] = append(h.machines, m), m
	return h, m, target, &execd
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

func setSessions(m *Machine, ss ...proto.Session) {
	m.mu.Lock()
	m.sessions = ss
	m.mu.Unlock()
}

// Nothing restarts while a conversation is working or waiting for the user; once all is idle the new binary is in
// place, the old one kept, the marker written, and the hub runs the new one.
func TestUpdateWaitsForIdle(t *testing.T) {
	p := newPublisher(t, "v2-202611021700", 202611021700)
	h, m, target, execd := updatingHub(t, p, "release")
	setSessions(m, proto.Session{SessionID: "s1", Title: "migrate the db", Status: proto.StatusWorking})
	ctx := context.Background()
	if _, err := h.upd.check(ctx); err != nil {
		t.Fatal(err)
	}
	if err := h.upd.begin(ctx, false); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "waiting for idle", func() bool { v := h.upd.view(); return v.State == "waiting_idle" && len(v.Busy) == 1 })
	if b, _ := os.ReadFile(target); string(b) != "#!/bin/sh\necho old\n" {
		t.Fatal("replaced while an agent was working")
	}
	if v := h.upd.view(); v.Busy[0].Title == "" || v.Busy[0].MachineID != "box" {
		t.Fatalf("busy %+v", v.Busy)
	}
	setSessions(m, proto.Session{SessionID: "s1", Status: proto.StatusIdle})
	waitFor(t, "the restart", func() bool { return len(*execd) == 1 })
	if b, _ := os.ReadFile(target); !bytes.Equal(b, p.script) {
		t.Fatal("the new binary is not in place")
	}
	if b, _ := os.ReadFile(target + ".prev"); string(b) != "#!/bin/sh\necho old\n" {
		t.Fatal("the old binary was not kept")
	}
	var pend update.Pending
	if ok, _ := update.Load(update.PendingPath(h.upd.dir), &pend); !ok || pend.ToBuild != 202611021700 || !slices.Equal(pend.Online, []string{"box"}) {
		t.Fatalf("pending %+v", pend)
	}
}

// 现在就更新 (the second confirmation) does not wait.
func TestUpdateNowSkipsTheWait(t *testing.T) {
	p := newPublisher(t, "v2-202611021700", 202611021700)
	h, m, _, execd := updatingHub(t, p, "release")
	setSessions(m, proto.Session{SessionID: "s1", Status: proto.StatusWorking})
	if _, err := h.upd.check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := h.upd.begin(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the restart", func() bool { return len(*execd) == 1 })
}

// A download that does not match the manifest leaves the binary as it is, and the history says why.
func TestUpdateBadDownloadChangesNothing(t *testing.T) {
	p := newPublisher(t, "v2-202611021700", 202611021700)
	p.corrupt = true
	h, _, target, execd := updatingHub(t, p, "release")
	h.upd.check(context.Background())
	if err := h.upd.begin(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the failure", func() bool { return h.upd.view().State == "failed" })
	if b, _ := os.ReadFile(target); string(b) != "#!/bin/sh\necho old\n" || len(*execd) != 0 {
		t.Fatal("a bad download replaced the binary")
	}
	if hs := h.upd.settings.History; len(hs) != 1 || hs[0].Result != "failed" {
		t.Fatalf("history %+v", hs)
	}
}

// The new version confirms once the machines are back; when one does not come back it goes back to the old binary,
// which records it, and that build is not installed automatically again.
func TestUpdateConfirmOrRollBack(t *testing.T) {
	p := newPublisher(t, "v2-202611021700", 202611021700)
	// Running as the new version (build 202611021700) after the restart.
	h, m, target, execd := updatingHub(t, p, "release")
	h.build = update.Make("v2-202611021700", "release", "202611021700", p.srv.URL+"/dl")
	h.version = h.build.Version
	os.WriteFile(target+".prev", []byte("#!/bin/sh\necho old\n"), 0o755)
	os.WriteFile(target, p.script, 0o755)
	pending := update.Pending{FromVersion: "v1-202611021500", FromBuild: 202611021500, ToVersion: "v2-202611021700", ToBuild: 202611021700,
		Target: target, Prev: target + ".prev", Online: []string{"box"}, Started: time.Now(), Auto: true}
	update.Save(update.PendingPath(h.upd.dir), pending)
	var told []string
	h.upd.notify = func(title, body string) { told = append(told, title) }
	h.upd.boot()
	if h.upd.view().Confirmed {
		t.Fatal("confirmed before checking")
	}
	h.upd.confirm(context.Background(), pending)
	if !h.upd.view().Confirmed || h.upd.settings.History[0].Result != "updated" {
		t.Fatalf("a good update was not confirmed: %+v", h.upd.settings.History)
	}
	if len(told) != 1 || !strings.Contains(told[0], "v2-202611021700") {
		t.Fatalf("an automatic update did not tell the phones: %v", told)
	}
	if _, err := os.Stat(update.PendingPath(h.upd.dir)); !os.IsNotExist(err) {
		t.Fatal("the pending marker stayed")
	}

	// Again, but the machine does not come back.
	m.mu.Lock()
	m.online = false
	m.mu.Unlock()
	update.Save(update.PendingPath(h.upd.dir), pending)
	h.upd.confirm(context.Background(), pending)
	if b, _ := os.ReadFile(target); string(b) != "#!/bin/sh\necho old\n" || len(*execd) != 1 || (*execd)[0] != target {
		t.Fatal("did not go back to the old binary")
	}
	// The old version starts and records it.
	old := newTestHub(t)
	old.build, old.version = update.Make("v1-202611021500", "release", "202611021500", p.srv.URL+"/dl"), "v1-202611021500"
	old.upd.dir, old.upd.client = h.upd.dir, p.srv.Client()
	var oldTold []string
	old.upd.notify = func(title, body string) { oldTold = append(oldTold, body) }
	old.upd.start(context.Background())
	waitFor(t, "the record", func() bool { return len(old.upd.view().Error) > 0 })
	if len(oldTold) != 1 || !strings.Contains(oldTold[0], "box") {
		t.Fatalf("going back did not tell the phones why: %v", oldTold)
	}
	hs := old.upd.settings.History
	if len(hs) != 1 || hs[0].Result != "rolled_back" || !strings.Contains(hs[0].Reason, "box") || !slices.Contains(old.upd.settings.Bad, int64(202611021700)) {
		t.Fatalf("history %+v, bad %v", hs, old.upd.settings.Bad)
	}
	old.upd.check(context.Background())
	if old.upd.wantsAuto() {
		t.Fatal("the build that went back would be installed automatically again")
	}
	if !old.upd.view().Latest.Bad {
		t.Fatal("the app is not told this build went back")
	}
}

// A build deployed straight from master during an update's checks clears the marker without calling it a failure.
func TestUpdateDirectDeployIsNotARollback(t *testing.T) {
	p := newPublisher(t, "v2-202611021700", 202611021700)
	h, _, target, execd := updatingHub(t, p, "release")
	h.build = update.Make("deployed-202611021800", "release", "202611021800", p.srv.URL+"/dl")
	update.Save(update.PendingPath(h.upd.dir), update.Pending{FromBuild: 202611021500, ToBuild: 202611021700, Target: target, Prev: target + ".prev"})
	h.upd.boot()
	if _, err := os.Stat(update.RolledBackPath(h.upd.dir)); !os.IsNotExist(err) {
		t.Fatal("a direct deploy was recorded as a rollback (its release would be marked bad)")
	}
	if _, err := os.Stat(update.PendingPath(h.upd.dir)); !os.IsNotExist(err) || len(*execd) != 0 {
		t.Fatal("the marker stayed, or it restarted")
	}
}

// Each start is counted once: the third failed start of a new version still runs it, the fourth goes back.
func TestUpdateStartsCountedOnce(t *testing.T) {
	p := newPublisher(t, "v2-202611021700", 202611021700)
	h, _, target, execd := updatingHub(t, p, "release")
	h.build = update.Make("v2-202611021700", "release", "202611021700", p.srv.URL+"/dl")
	os.WriteFile(target+".prev", []byte("old"), 0o755)
	update.Save(update.PendingPath(h.upd.dir), update.Pending{ToBuild: 202611021700, Target: target, Prev: target + ".prev"})
	for i := 1; i <= updateMaxStarts; i++ {
		if !h.upd.boot() || len(*execd) != 0 {
			t.Fatalf("start %d went back already", i)
		}
	}
	h.upd.boot()
	if len(*execd) != 1 {
		t.Fatal("start 4 did not go back")
	}
}

// A new version that keeps dying in a service manager's restart loop goes back on its next start.
func TestUpdateCrashLoopGoesBack(t *testing.T) {
	p := newPublisher(t, "v2-202611021700", 202611021700)
	h, _, target, execd := updatingHub(t, p, "release")
	h.build = update.Make("v2-202611021700", "release", "202611021700", p.srv.URL+"/dl")
	os.WriteFile(target+".prev", []byte("old"), 0o755)
	update.Save(update.PendingPath(h.upd.dir), update.Pending{ToBuild: 202611021700, Target: target, Prev: target + ".prev", Starts: updateMaxStarts})
	h.upd.boot()
	if b, _ := os.ReadFile(target); string(b) != "old" || len(*execd) != 1 {
		t.Fatal("a crash loop did not go back")
	}
}

// A start count that cannot be saved (full or read-only disk, wrong owner) would leave a crash loop uncounted
// forever: the new version goes back on that start instead, and says why in the log.
func TestUpdateUnsavedStartCountGoesBack(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a directory the test cannot write to")
	}
	p := newPublisher(t, "v2-202611021700", 202611021700)
	h, _, target, execd := updatingHub(t, p, "release")
	var logs bytes.Buffer
	h.logger = log.New(&logs, "", 0)
	h.build = update.Make("v2-202611021700", "release", "202611021700", p.srv.URL+"/dl")
	os.WriteFile(target+".prev", []byte("old"), 0o755)
	if err := update.Save(update.PendingPath(h.upd.dir), update.Pending{ToBuild: 202611021700, Target: target, Prev: target + ".prev", Starts: 1}); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(h.upd.dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(h.upd.dir, 0o700) })
	h.upd.boot()
	if b, _ := os.ReadFile(target); string(b) != "old" || len(*execd) != 1 {
		t.Fatalf("an unsaved start count kept the new version (execd %v)", *execd)
	}
	for _, want := range []string{"save start count", "save rollback marker", "remove pending marker"} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("log misses %q:\n%s", want, logs.String())
		}
	}
}

// Only a release build updates itself: a dev build and the Docker image never replace their binary, whatever is out.
func TestUpdateNeverOnDevOrDocker(t *testing.T) {
	p := newPublisher(t, "v2-202611021700", 202611021700)
	for _, ch := range []string{"dev", "docker"} {
		h, _, _, _ := updatingHub(t, p, ch)
		srv := httptest.NewServer(h.routes())
		req, _ := http.NewRequest("POST", srv.URL+"/api/v1/hub/update", strings.NewReader(`{"now":true}`))
		req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var body struct {
			Error string `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()
		srv.Close()
		if resp.StatusCode != http.StatusConflict {
			t.Fatalf("%s: %d", ch, resp.StatusCode)
		}
		if ch == "docker" && !strings.Contains(body.Error, "docker compose") {
			t.Fatalf("docker: %q", body.Error)
		}
		if v := h.upd.view(); v.CanUpdate || v.Auto || v.AutoNotice {
			t.Fatalf("%s: %+v", ch, v)
		}
	}
}

// A build deployed from master (its build number its build time) is never replaced by an older or equal release.
func TestUpdateNoDowngrade(t *testing.T) {
	p := newPublisher(t, "v2-202611021700", 202611021700)
	h, _, _, _ := updatingHub(t, p, "release")
	h.build = update.Make("deployed-202611021800", "release", "202611021800", p.srv.URL+"/dl")
	h.upd.check(context.Background())
	if v := h.upd.view(); v.State != "idle" || v.Latest.Newer {
		t.Fatalf("an older release looks newer: %+v %+v", v.State, v.Latest)
	}
	if err := h.upd.begin(context.Background(), false); !errors.Is(err, errNoUpdate) {
		t.Fatalf("downgrade: %v", err)
	}
	if h.upd.wantsAuto() {
		t.Fatal("would downgrade automatically")
	}
}

// Commits for the anti-rollback tests: the release is published from b (a before it); a hub deployed straight
// from master runs d, which came after b.
const (
	gitA = "a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1"
	gitB = "b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2"
	gitD = "d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4"
)

func publishedAtB(t *testing.T) *fakePublisher {
	return newPublisherWith(t, "v2-202611021700", 202611021700, func(m *update.Manifest) {
		m.Commit = gitB
		m.Ancestors, _ = update.EncodeAncestors([]string{gitB, gitA})
	})
}

// A release whose code does not contain the commit the hub runs is newer by its number only: it is offered, with
// the reason, but not installed by itself, and the history says so once; the user can still install it.
func TestUpdateHeldWhenTheReleaseIsBehind(t *testing.T) {
	p := publishedAtB(t)
	h, _, _, execd := updatingHub(t, p, "release")
	h.build.Commit = gitD
	ctx := context.Background()
	if _, err := h.upd.check(ctx); err != nil {
		t.Fatal(err)
	}
	v := h.upd.view()
	if v.State != "available" || !v.Latest.Newer || !strings.Contains(v.Latest.Held, "不包含") {
		t.Fatalf("view %+v %+v", v.State, v.Latest)
	}
	if h.upd.wantsAuto() {
		t.Fatal("would install a release behind this hub's code by itself")
	}
	h.upd.noteHeld(ctx)
	h.upd.noteHeld(ctx) // the next check, 6 hours on: written once
	var held []updateRecord
	for _, r := range h.upd.settings.History {
		if r.Result == "held" {
			held = append(held, r)
		}
	}
	if len(held) != 1 || held[0].ToBuild != 202611021700 || !held[0].Auto || !strings.Contains(held[0].Reason, gitB[:8]) {
		t.Fatalf("history %+v", h.upd.settings.History)
	}
	if err := h.upd.begin(ctx, true); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the update the user asked for", func() bool { return len(*execd) == 1 })
}

// A release that contains the hub's commit installs by itself as before, and nothing is held.
func TestUpdateAutoWhenTheReleaseContainsIt(t *testing.T) {
	p := publishedAtB(t)
	h, _, _, _ := updatingHub(t, p, "release")
	h.build.Commit = gitA
	ctx := context.Background()
	if _, err := h.upd.check(ctx); err != nil {
		t.Fatal(err)
	}
	if v := h.upd.view(); v.Latest.Held != "" || !h.upd.wantsAuto() {
		t.Fatalf("held %q, auto %v", v.Latest.Held, h.upd.wantsAuto())
	}
	h.upd.noteHeld(ctx)
	if len(h.upd.settings.History) != 0 {
		t.Fatalf("history %+v", h.upd.settings.History)
	}
}

// With 自动更新 off nothing would install by itself anyway: no held record either.
func TestUpdateHeldNotNotedWithAutoOff(t *testing.T) {
	p := publishedAtB(t)
	h, _, _, _ := updatingHub(t, p, "release")
	h.build.Commit = gitD
	off := false
	h.upd.settings.Auto = &off
	h.upd.check(context.Background())
	h.upd.noteHeld(context.Background())
	if len(h.upd.settings.History) != 0 {
		t.Fatalf("history %+v", h.upd.settings.History)
	}
}

// Auto-update is on by default and the app is told once; turning it off sticks and counts as told.
func TestUpdateSettings(t *testing.T) {
	p := newPublisher(t, "v2-202611021700", 202611021700)
	h, _, _, _ := updatingHub(t, p, "release")
	srv := httptest.NewServer(h.routes())
	defer srv.Close()
	call := func(method, path, body string) updateView {
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var v updateView
		json.NewDecoder(resp.Body).Decode(&v)
		return v
	}
	if v := call("GET", "/api/v1/hub/update", ""); !v.Auto || !v.AutoNotice || !v.CanUpdate || v.Channel != "release" {
		t.Fatalf("defaults %+v", v)
	}
	if v := call("PUT", "/api/v1/hub/update/settings", `{"auto":false}`); v.Auto || v.AutoNotice {
		t.Fatalf("turned off %+v", v)
	}
	h.upd.check(context.Background())
	if h.upd.wantsAuto() {
		t.Fatal("updates automatically with auto off")
	}
}

// Once confirmed, the release's node binaries go where 重新部署 takes them (deploy.go), and only machines whose
// node is another build, reached through the hub, are redeployed: never the hub's own machine.
func TestUpdateNodesBinariesInPlace(t *testing.T) {
	nodes := t.TempDir()
	t.Setenv("JARVIS_NODE_BINARIES", nodes)
	staged := t.TempDir()
	os.WriteFile(filepath.Join(staged, "jarvisd-darwin-arm64"), []byte("node for macs"), 0o755)
	os.WriteFile(filepath.Join(staged, "jarvisd-darwin-arm64.gz"), []byte("the download"), 0o644)
	os.WriteFile(filepath.Join(nodes, "jarvisd-darwin-arm64"), []byte("old node"), 0o755)
	h := newTestHub(t)
	h.version = "v2"
	local := newMachine(h, MachineConfig{ID: "hub", Name: "Hub", Kind: "local"})
	local.online, local.info.Version = true, "v1" // the hub's own: runs the hub's binary, never deployed to
	h.machines, h.byID["hub"] = []*Machine{local}, local
	h.upd.updateNodes(context.Background(), staged)
	if b, _ := os.ReadFile(filepath.Join(nodes, "jarvisd-darwin-arm64")); string(b) != "node for macs" {
		t.Fatalf("node binary %q", b)
	}
	if _, err := os.Stat(filepath.Join(nodes, "jarvisd-darwin-arm64.gz")); !os.IsNotExist(err) {
		t.Fatal("the download itself went in with the binaries")
	}
	_ = local // the manager decides what to redeploy (internal/devices TestUpdateNodesSkipsTheHubsOwn)
}

// A Docker hub never replaces itself, but it looks up its image's tags and says when a newer release is out, and how
// to get it (a pinned tag is changed in .env); a private image without a token says what to set.
func TestUpdateDockerSaysNewImage(t *testing.T) {
	private := false
	reg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if private {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path != "/v2/owner/jarvis/tags/list" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `{"name":"owner/jarvis","tags":["0.3.0","0.4.0","0.5.0-rc1","edge","latest"]}`)
	}))
	defer reg.Close()
	host := strings.TrimPrefix(reg.URL, "http://")
	h := newTestHub(t)
	h.build, h.version = update.Make("0.3.0", "docker", "", ""), "0.3.0"
	h.upd.client = reg.Client()
	ctx := context.Background()

	t.Setenv("JARVIS_IMAGE", host+"/owner/jarvis:latest")
	if _, err := h.upd.check(ctx); err != nil {
		t.Fatal(err)
	}
	v := h.upd.view()
	if v.State != "available" || v.Latest == nil || v.Latest.Version != "0.4.0" || !v.Latest.Newer || v.CanUpdate {
		t.Fatalf("a newer image is not offered: %+v %+v", v, v.Latest)
	}
	if v.Image == nil || v.Image.Name != host+"/owner/jarvis" || v.Image.Tag != "latest" || v.Image.Pinned {
		t.Fatalf("image %+v", v.Image)
	}
	t.Setenv("JARVIS_IMAGE", host+"/owner/jarvis:0.3.0")
	if v := h.upd.view(); v.Image == nil || !v.Image.Pinned {
		t.Fatalf("a pinned tag: %+v", v.Image)
	}

	h.version = "0.4.0"
	h.upd.check(ctx)
	if v := h.upd.view(); v.State != "idle" || v.Latest.Newer {
		t.Fatalf("the newest image offered again: %+v", v)
	}

	private = true
	h.upd.check(ctx)
	if v := h.upd.view(); !strings.Contains(v.Error, "JARVIS_REGISTRY_TOKEN") {
		t.Fatalf("a private image does not say what to set: %q", v.Error)
	}

	t.Setenv("JARVIS_IMAGE", "")
	if _, err := h.upd.check(ctx); err == nil || !strings.Contains(h.upd.view().Error, "JARVIS_IMAGE") {
		t.Fatalf("no image to look at: %v, %q", err, h.upd.view().Error)
	}
	h.build.Image = host + "/owner/jarvis"
	private = false
	if _, err := h.upd.check(ctx); err != nil || h.upd.view().Image.Tag != "latest" {
		t.Fatalf("the image it was built as: %v %+v", err, h.upd.view().Image)
	}
}

// A release with a program the hub runs beside it (jarvisd-wechat): the program is checked and put next to the
// hub's binary with it, and going back takes it away again (it was new here).
func TestUpdateSwapsPrograms(t *testing.T) {
	plat := runtime.GOOS + "-" + runtime.GOARCH
	prog := []byte("#!/bin/sh\necho v2-202611021700\necho 'channel release, build 202611021700'\n")
	pgz := gz(prog)
	outer, inner := sha256.Sum256(pgz), sha256.Sum256(prog)
	pf := update.File{Name: "jarvisd-wechat-" + plat + ".gz", Size: int64(len(pgz)), SHA256: hex.EncodeToString(outer[:]), BinarySHA256: hex.EncodeToString(inner[:])}
	p := newPublisherWith(t, "v2-202611021700", 202611021700, func(m *update.Manifest) {
		m.Programs = map[string]map[string]update.File{"jarvisd-wechat": {plat: pf}}
	})
	p.extra = map[string][]byte{"/dl/" + pf.Name: pgz}
	h, _, target, execd := updatingHub(t, p, "release")
	ctx := context.Background()
	if _, err := h.upd.check(ctx); err != nil {
		t.Fatal(err)
	}
	if err := h.upd.begin(ctx, true); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the restart", func() bool { return len(*execd) == 1 })
	placed := filepath.Join(filepath.Dir(target), "jarvisd-wechat")
	if b, _ := os.ReadFile(placed); !bytes.Equal(b, prog) {
		t.Fatal("the program is not next to the hub's binary")
	}
	var pend update.Pending
	if ok, _ := update.Load(update.PendingPath(h.upd.dir), &pend); !ok || len(pend.Programs) != 1 || pend.Programs[0].Target != placed || pend.Programs[0].Prev != "" {
		t.Fatalf("pending %+v", pend)
	}
	h.upd.rollback(pend, "test")
	if _, err := os.Stat(placed); !os.IsNotExist(err) {
		t.Fatal("going back left the new program")
	}
	if b, _ := os.ReadFile(target); string(b) != "#!/bin/sh\necho old\n" {
		t.Fatal("going back did not restore the hub's binary")
	}
}

// A program's download that is not the release's build stops the update before anything is swapped.
func TestUpdateRefusesAWrongProgram(t *testing.T) {
	plat := runtime.GOOS + "-" + runtime.GOARCH
	prog := []byte("#!/bin/sh\necho v1-old\necho 'channel release, build 1'\n") // says another build
	pgz := gz(prog)
	outer, inner := sha256.Sum256(pgz), sha256.Sum256(prog)
	pf := update.File{Name: "jarvisd-wechat-" + plat + ".gz", Size: int64(len(pgz)), SHA256: hex.EncodeToString(outer[:]), BinarySHA256: hex.EncodeToString(inner[:])}
	p := newPublisherWith(t, "v2-202611021700", 202611021700, func(m *update.Manifest) {
		m.Programs = map[string]map[string]update.File{"jarvisd-wechat": {plat: pf}}
	})
	p.extra = map[string][]byte{"/dl/" + pf.Name: pgz}
	h, _, target, execd := updatingHub(t, p, "release")
	ctx := context.Background()
	h.upd.check(ctx)
	if err := h.upd.begin(ctx, true); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the failure", func() bool { return h.upd.view().State == "failed" })
	if len(*execd) != 0 {
		t.Fatal("restarted")
	}
	if b, _ := os.ReadFile(target); string(b) != "#!/bin/sh\necho old\n" {
		t.Fatal("the hub's binary was swapped")
	}
	if !strings.Contains(h.upd.view().Error, "jarvisd-wechat") {
		t.Fatalf("error %q", h.upd.view().Error)
	}
}

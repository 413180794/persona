package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/nwverse/persona/server/internal/identity"
	"github.com/nwverse/persona/server/internal/osproc"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/update"
)

// The hub updating itself to the publisher's release (docs/hub-update.md「更新」): check the manifest every 6 hours, download and verify, wait until the assistant and every agent are idle, swap the binary (keeping the old
// one), restart into the new one, and count it a success only once the machines that were connected are back;
// otherwise go back to the old binary, by the new process itself or by the old one's watchdog. A build that went
// back is not tried again automatically. Then the nodes follow, through deploy.go.

const (
	updateCheckEvery   = 6 * time.Hour
	updateFirstCheck   = time.Minute
	updateConfirmIn    = 2 * time.Minute
	updateWatchFor     = 3 * time.Minute
	updateMaxStarts    = 3
	updateIdleEvery    = 30 * time.Second
	updateManualIdle   = 2 * time.Hour
	updateHistoryKeep  = 50
	updateSettingsKey  = "hub_update"
	updateRequestLimit = 30 * time.Second
)

var (
	errNoUpdate    = errors.New("没有更新的版本")
	errUpdating    = errors.New("正在更新")
	errNotRelease  = errors.New("这个 hub 不能自己更新")
	errUpdateBusy  = errors.New("助手和 Agent 一直在忙，这次没更新，稍后再试")
	errUpdateAgain = errors.New("自动更新已关")
)

// updateRecord is one update in the history (设置 › hub › 更新记录).
type updateRecord struct {
	FromVersion string    `json:"from_version"`
	ToVersion   string    `json:"to_version"`
	ToBuild     int64     `json:"to_build"`
	Started     time.Time `json:"started"`
	Ended       time.Time `json:"ended"`
	Result      string    `json:"result"` // updated | rolled_back | failed | held (newer, not installed by itself: Reason)
	Reason      string    `json:"reason,omitempty"`
	Auto        bool      `json:"auto"`
}

// updateSettings are kept in Postgres (settings hub_update).
type updateSettings struct {
	Auto           *bool          `json:"auto,omitempty"` // nil: on (the default)
	AutoNoticeSeen bool           `json:"auto_notice_seen"`
	Bad            []int64        `json:"bad,omitempty"` // builds that went back: not installed automatically again
	History        []updateRecord `json:"history,omitempty"`
}

func (s updateSettings) auto() bool { return s.Auto == nil || *s.Auto }

// busyItem is something the restart would interrupt.
type busyItem struct {
	MachineID string `json:"machine_id"`
	Machine   string `json:"machine"`
	SessionID string `json:"session_id,omitempty"`
	Title     string `json:"title"`
}

type updater struct {
	h      *Hub
	dir    string
	client *http.Client
	// Hooks the tests replace: run another binary in this process, start the watchdog, find the running binary,
	// alert the phones.
	exec       func(path string) error
	startWatch func(prev, pendingPath string) error
	target     func() (string, error)
	notify     func(title, body string)
	confirmIn  time.Duration
	idleEvery  time.Duration
	manualIdle time.Duration

	mu          sync.Mutex
	settings    updateSettings
	loaded      bool
	state       string // idle | checking | available | downloading | waiting_idle | restarting | confirming | updated | rolled_back | failed
	progress    string
	errText     string
	release     *update.Published
	newestImage string // a Docker hub: the newest release tag of its image
	lastCheck   time.Time
	busy        []busyItem
	running     bool
	now         bool // the user confirmed 现在就更新: stop waiting for idle
	confirmed   bool // no update to confirm, or this process confirmed it
}

func newUpdater(h *Hub) *updater {
	return &updater{h: h, dir: update.Dir(), client: &http.Client{Timeout: 10 * time.Minute},
		exec: func(path string) error {
			return syscall.Exec(path, append([]string{path}, os.Args[1:]...), os.Environ())
		},
		startWatch: startWatchdog, target: runningBinary, confirmIn: updateConfirmIn, idleEvery: updateIdleEvery,
		manualIdle: updateManualIdle, state: "idle", confirmed: true,
		notify: func(title, body string) {
			h.push.send(map[string]any{"aps": map[string]any{
				"alert": map[string]any{"title": title, "body": body}, "sound": "default", "thread-id": "hub_update",
			}}, "hub_update", nil)
		}}
}

func runningBinary() (string, error) {
	p, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(p)
}

// startWatchdog runs `<old binary> update-watch <pending>` in its own session, its output in update/watch.log.
func startWatchdog(prev, pendingPath string) error {
	log, err := os.OpenFile(filepath.Join(filepath.Dir(pendingPath), "watch.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer log.Close()
	cmd := exec.Command(prev, "update-watch", pendingPath)
	cmd.Stdout, cmd.Stderr = log, log
	cmd.SysProcAttr = osproc.DetachedAttr()
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// boot runs once, first thing when the hub starts, before the config or the database: a new version that keeps
// dying (in a service manager's restart loop) goes back after updateMaxStarts starts; an old version that finds a
// marker for an update it is not (the watchdog or a person put it back) clears it for recording. It says whether
// this process has an update of its own to confirm.
func (u *updater) boot() (pending bool) {
	var p update.Pending
	ok, err := update.Load(update.PendingPath(u.dir), &p)
	if err != nil || !ok {
		return false
	}
	if p.ToBuild != u.h.build.Number {
		if u.h.build.Number == p.FromBuild {
			u.markRolledBack(p, "回到了旧版本")
		} else {
			_ = os.Remove(update.PendingPath(u.dir)) // replaced by a build deployed straight from master: not a failure
		}
		return false
	}
	u.confirmed = false
	p.Starts++
	_ = update.Save(update.PendingPath(u.dir), p)
	if p.Starts > updateMaxStarts {
		u.rollback(p, fmt.Sprintf("新版本启动了 %d 次都没起来", p.Starts-1))
	}
	return true
}

// start loads the settings, records an update that went back, confirms one under way, and checks for releases.
func (u *updater) start(ctx context.Context) {
	u.load(ctx)
	u.recordRolledBack(ctx)
	var p update.Pending
	if ok, _ := update.Load(update.PendingPath(u.dir), &p); ok && p.ToBuild == u.h.build.Number {
		go u.confirm(ctx, p)
	}
	go u.loop(ctx)
}

func (u *updater) load(ctx context.Context) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.loaded {
		return
	}
	u.loaded = true
	if u.h.store != nil {
		if _, err := u.h.store.Setting(ctx, updateSettingsKey, &u.settings); err != nil {
			u.h.logf("update: settings: %v", err)
		}
	}
}

// save writes the settings (called with u.mu held).
func (u *updater) saveLocked(ctx context.Context) {
	if len(u.settings.History) > updateHistoryKeep {
		u.settings.History = u.settings.History[len(u.settings.History)-updateHistoryKeep:]
	}
	if u.h.store != nil {
		if err := u.h.store.PutSetting(ctx, updateSettingsKey, u.settings); err != nil {
			u.h.logf("update: save settings: %v", err)
		}
	}
}

func (u *updater) record(ctx context.Context, r updateRecord) {
	u.mu.Lock()
	u.settings.History = append(u.settings.History, r)
	if r.Result == "rolled_back" && !slices.Contains(u.settings.Bad, r.ToBuild) {
		u.settings.Bad = append(u.settings.Bad, r.ToBuild)
	}
	u.saveLocked(ctx)
	u.mu.Unlock()
}

func (u *updater) markRolledBack(p update.Pending, reason string) {
	path := update.RolledBackPath(u.dir)
	if _, err := os.Stat(path); err != nil {
		_ = update.Save(path, update.RolledBack{FromVersion: p.FromVersion, ToVersion: p.ToVersion, ToBuild: p.ToBuild,
			Started: p.Started, At: time.Now(), Reason: reason, Auto: p.Auto})
	}
	_ = os.Remove(update.PendingPath(u.dir))
}

// recordRolledBack puts an update that went back into the history and the bad builds, once the database is there.
func (u *updater) recordRolledBack(ctx context.Context) {
	var rb update.RolledBack
	if ok, err := update.Load(update.RolledBackPath(u.dir), &rb); err != nil || !ok {
		return
	}
	u.record(ctx, updateRecord{FromVersion: rb.FromVersion, ToVersion: rb.ToVersion, ToBuild: rb.ToBuild, Started: rb.Started,
		Ended: rb.At, Result: "rolled_back", Reason: rb.Reason, Auto: rb.Auto})
	why := fmt.Sprintf("更新到 %s 没成功，已回到 %s：%s", rb.ToVersion, u.h.version, rb.Reason)
	u.set("rolled_back", "", why)
	_ = os.Remove(update.RolledBackPath(u.dir))
	u.notify("hub 更新没成功", why+"。这一版不会再自动装。")
	u.h.logf("update: %s went back to %s: %s", rb.ToVersion, u.h.version, rb.Reason)
}

// confirm decides whether the update this process is the result of worked: the database answers and every machine
// connected before the restart is back within confirmIn. Then the nodes follow; otherwise back to the old binary.
func (u *updater) confirm(ctx context.Context, p update.Pending) {
	u.set("confirming", "", "")
	deadline := time.Now().Add(u.confirmIn)
	missing := p.Online
	for {
		missing = u.offline(p.Online)
		dbOK := u.h.store == nil
		if !dbOK {
			var x updateSettings
			_, err := u.h.store.Setting(ctx, updateSettingsKey, &x)
			dbOK = err == nil
		}
		if dbOK && len(missing) == 0 {
			break
		}
		if !time.Now().Before(deadline) {
			why := "连不上数据库"
			if dbOK {
				why = "这些机器 2 分钟内没连回来：" + strings.Join(missing, "、")
			}
			u.rollback(p, why)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
	_ = os.Remove(update.PendingPath(u.dir))
	u.mu.Lock()
	u.confirmed = true
	u.mu.Unlock()
	u.record(ctx, updateRecord{FromVersion: p.FromVersion, ToVersion: p.ToVersion, ToBuild: p.ToBuild, Started: p.Started,
		Ended: time.Now(), Result: "updated", Auto: p.Auto})
	u.set("updated", "", "")
	u.h.logf("update: %s -> %s confirmed", p.FromVersion, p.ToVersion)
	if p.Auto { // one the user started is watched in the app
		u.notify("hub 已更新到 "+p.ToVersion, "助手和 Agent 都空闲时自动装上的；各台电脑上的 jarvisd 也会换成这一版。")
	}
	u.updateNodes(ctx, p.Nodes)
}

// offline lists which of ids are not connected.
func (u *updater) offline(ids []string) []string {
	var out []string
	for _, id := range ids {
		m := u.h.localMachine(id)
		if m == nil {
			continue // gone from the config: nothing to wait for
		}
		m.mu.Lock()
		on := m.online
		m.mu.Unlock()
		if !on {
			out = append(out, id)
		}
	}
	return out
}

// rollback writes why, puts the old binary back and runs it. It returns only when that fails.
func (u *updater) rollback(p update.Pending, reason string) {
	u.h.logf("update: going back to %s: %s", p.FromVersion, reason)
	u.markRolledBack(p, reason)
	if err := update.RestoreAll(p); err != nil {
		u.h.logf("update: restore %s: %v", p.Prev, err)
		return
	}
	if err := u.exec(p.Target); err != nil {
		u.h.logf("update: run %s: %v", p.Target, err)
	}
}

// updateNodes puts the release's node binaries where 重新部署 takes them (deploy.go) and redeploys each connected
// machine whose node is not this build, one at a time.
func (u *updater) updateNodes(ctx context.Context, staged string) {
	if staged != "" {
		files, _ := filepath.Glob(filepath.Join(staged, "jarvisd-*"))
		for _, f := range files {
			if strings.HasSuffix(f, ".gz") || strings.HasSuffix(f, ".part") {
				continue
			}
			if err := os.MkdirAll(nodeBinaryDir(), 0o755); err == nil {
				_, err = update.Swap(filepath.Join(nodeBinaryDir(), filepath.Base(f)), f)
				if err != nil && errors.Is(err, os.ErrNotExist) {
					err = copyInto(f, filepath.Join(nodeBinaryDir(), filepath.Base(f)))
				}
				if err != nil {
					u.h.logf("update: node binary %s: %v", filepath.Base(f), err)
				}
			}
		}
	}
	for _, m := range u.h.machinesNow() {
		m.mu.Lock()
		need := m.cfg.Kind != "local" && m.online && nodeMismatch(u.h.version, m.info.Version)
		offlineLinked := !m.online && m.rpcDeploymentLocked()
		m.mu.Unlock()
		if offlineLinked {
			if err := m.queueOfflineNodeUpdate(ctx); err != nil {
				u.h.logf("update: [%s] queue node update: %v", m.cfg.ID, err)
			}
			continue
		}
		if !need {
			continue
		}
		if err := m.startDeploy(); err != nil {
			u.h.logf("update: [%s] %v", m.cfg.ID, err)
			continue
		}
		for i := 0; i < 500; i++ { // deploy.go gives up after 15 minutes
			m.mu.Lock()
			busy := m.deploying
			m.mu.Unlock()
			if !busy {
				break
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(2 * time.Second):
			}
		}
	}
}

func copyInto(from, to string) error {
	b, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	return os.WriteFile(to, b, 0o755)
}

// loop checks for releases (a minute after starting, then every 6 hours) and installs one when updating itself is
// on and the release is newer and not a build that went back.
func (u *updater) loop(ctx context.Context) {
	if !u.h.build.SelfUpdates() && u.h.build.Channel != update.Docker {
		return
	}
	wait := updateFirstCheck
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = updateCheckEvery
		if _, err := u.check(ctx); err != nil {
			continue
		}
		if u.wantsAuto() {
			go u.install(ctx, true)
		} else {
			u.noteHeld(ctx)
		}
	}
}

// wantsAuto: on, newer, not a bad build, its code contains this one's (Build.Hold), not already running.
func (u *updater) wantsAuto() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	r := u.release
	return u.settings.auto() && !u.running && r != nil && u.h.build.Newer(r.Manifest.Build) &&
		!slices.Contains(u.settings.Bad, r.Manifest.Build) && u.h.build.Hold(r.Manifest) == ""
}

// noteHeld writes into the history, once per release, that a newer release was not installed by itself because its
// code may not contain this hub's (Build.Hold): the app shows it as available, with the reason, and the user can
// still install it.
func (u *updater) noteHeld(ctx context.Context) {
	u.mu.Lock()
	r := u.release
	if !u.settings.auto() || u.running || r == nil || !u.h.build.Newer(r.Manifest.Build) || slices.Contains(u.settings.Bad, r.Manifest.Build) {
		u.mu.Unlock()
		return
	}
	why := u.h.build.Hold(r.Manifest)
	for _, h := range u.settings.History {
		if h.Result == "held" && h.ToBuild == r.Manifest.Build {
			why = ""
		}
	}
	u.mu.Unlock()
	if why == "" {
		return
	}
	u.h.logf("update: %s is newer but not installed by itself: %s", r.Manifest.Version, why)
	now := time.Now()
	u.record(ctx, updateRecord{FromVersion: u.h.version, ToVersion: r.Manifest.Version, ToBuild: r.Manifest.Build,
		Started: now, Ended: now, Result: "held", Reason: why, Auto: true})
}

// check reads the release now (a Docker hub: its image's tags).
func (u *updater) check(ctx context.Context) (*update.Published, error) {
	if u.h.build.Channel == update.Docker {
		return nil, u.checkImage(ctx)
	}
	if u.h.build.UpdateURL == "" {
		return nil, errNotRelease
	}
	u.setState("checking")
	cctx, cancel := context.WithTimeout(ctx, updateRequestLimit)
	defer cancel()
	r, err := update.Check(cctx, u.client, u.h.build.UpdateURL)
	u.mu.Lock()
	u.lastCheck = time.Now()
	if err == nil {
		u.release = r
	}
	u.mu.Unlock()
	if err != nil {
		u.set("idle", "", "检查更新失败："+err.Error())
		return nil, err
	}
	if u.h.build.Newer(r.Manifest.Build) {
		u.set("available", "", "")
	} else {
		u.set("idle", "", "")
	}
	return r, nil
}

// begin starts an update the user asked for; now skips waiting for idle (二级确认的「现在就更新」).
func (u *updater) begin(ctx context.Context, now bool) error {
	if !u.h.build.SelfUpdates() {
		return u.whyNot()
	}
	u.mu.Lock()
	if u.running {
		if now {
			u.now = true
		}
		u.mu.Unlock()
		return nil
	}
	r := u.release
	u.mu.Unlock()
	if r == nil || !u.h.build.Newer(r.Manifest.Build) {
		return errNoUpdate
	}
	u.mu.Lock()
	u.now = now
	u.mu.Unlock()
	go u.install(ctx, false)
	return nil
}

func (u *updater) whyNot() error {
	switch u.h.build.Channel {
	case update.Docker:
		return fmt.Errorf("%w：Docker 里的 hub 由新镜像更新（docker compose pull && docker compose up -d）", errNotRelease)
	case update.Release:
		return fmt.Errorf("%w：不知道去哪里找新版本", errNotRelease)
	}
	return fmt.Errorf("%w：开发版（%s）", errNotRelease, u.h.version)
}

// install is one update: download, verify, wait for idle, swap, restart. It returns only when it did not restart.
func (u *updater) install(ctx context.Context, auto bool) {
	u.mu.Lock()
	if u.running {
		u.mu.Unlock()
		return
	}
	u.running = true
	r := u.release
	u.mu.Unlock()
	started := time.Now()
	fail := func(err error) {
		u.mu.Lock()
		u.running, u.now = false, false
		u.busy = nil
		u.mu.Unlock()
		u.set("failed", "", err.Error())
		u.record(ctx, updateRecord{FromVersion: u.h.version, ToVersion: r.Manifest.Version, ToBuild: r.Manifest.Build,
			Started: started, Ended: time.Now(), Result: "failed", Reason: err.Error(), Auto: auto})
		u.h.logf("update to %s: %v", r.Manifest.Version, err)
	}
	m := r.Manifest
	file, ok := m.Files[runtime.GOOS+"-"+runtime.GOARCH]
	if !ok {
		fail(fmt.Errorf("这个版本没有 %s/%s 的二进制", runtime.GOOS, runtime.GOARCH))
		return
	}
	staged := filepath.Join(u.dir, fmt.Sprint(m.Build))
	u.set("downloading", "hub", "")
	bin, err := update.Download(ctx, u.client, u.h.build.UpdateURL, file, staged)
	if err != nil {
		fail(err)
		return
	}
	nodes := filepath.Join(staged, "nodes")
	for _, plat := range u.nodePlatforms() {
		f, ok := m.Files[plat]
		if !ok {
			continue
		}
		u.set("downloading", plat, "")
		if _, err := update.Download(ctx, u.client, u.h.build.UpdateURL, f, nodes); err != nil {
			fail(err)
			return
		}
	}
	if err := update.CheckBinary(ctx, bin, m); err != nil {
		fail(err)
		return
	}
	// The programs the hub runs beside it (jarvisd-wechat): this platform's build of each, checked like jarvisd.
	programs := map[string]string{} // name -> the downloaded binary
	for _, name := range update.ProgramNames {
		f, ok := m.Programs[name][runtime.GOOS+"-"+runtime.GOARCH]
		if !ok {
			continue
		}
		u.set("downloading", name, "")
		pb, err := update.Download(ctx, u.client, u.h.build.UpdateURL, f, filepath.Join(staged, "programs"))
		if err != nil {
			fail(err)
			return
		}
		if err := update.CheckBinary(ctx, pb, m); err != nil {
			fail(fmt.Errorf("%s: %w", name, err))
			return
		}
		programs[name] = pb
	}
	// Wait until nothing the restart would interrupt is going on (or the user said 现在就更新).
	waitStarted := time.Now()
	for {
		busy := u.h.updateBusy()
		u.mu.Lock()
		u.busy = busy
		now, autoOn := u.now, u.settings.auto()
		u.mu.Unlock()
		if len(busy) == 0 || now {
			break
		}
		if auto && !autoOn {
			fail(errUpdateAgain)
			return
		}
		if !auto && time.Since(waitStarted) > u.manualIdle {
			fail(errUpdateBusy)
			return
		}
		u.set("waiting_idle", "", "")
		select {
		case <-ctx.Done():
			return
		case <-time.After(u.idleEvery):
		}
	}
	target, err := u.target()
	if err != nil {
		fail(err)
		return
	}
	p := update.Pending{FromVersion: u.h.version, FromBuild: u.h.build.Number, ToVersion: m.Version, ToBuild: m.Build,
		Target: target, Started: started, Online: u.onlineIDs(), Auto: auto, PID: os.Getpid(), Args: os.Args,
		HealthURL: u.healthURL(), Nodes: nodes}
	prev, err := update.Swap(target, bin)
	if err != nil {
		fail(err)
		return
	}
	p.Prev = prev
	for _, name := range update.ProgramNames {
		if pb, ok := programs[name]; ok {
			sw, err := update.SwapProgram(filepath.Join(filepath.Dir(target), name), pb)
			p.Programs = append(p.Programs, sw)
			if err != nil {
				_ = update.RestoreAll(p)
				fail(fmt.Errorf("%s: %w", name, err))
				return
			}
		}
	}
	if err := update.Save(update.PendingPath(u.dir), p); err != nil {
		_ = update.RestoreAll(p)
		fail(err)
		return
	}
	if err := u.startWatch(prev, update.PendingPath(u.dir)); err != nil {
		u.h.logf("update: watchdog: %v (the new version still checks itself)", err)
	}
	u.set("restarting", "", "")
	u.h.logf("update: restarting into %s", m.Version)
	time.Sleep(500 * time.Millisecond) // the apps get the restarting event before the socket goes
	if err := u.exec(target); err != nil {
		_ = update.RestoreAll(p)
		_ = os.Remove(update.PendingPath(u.dir))
		fail(err)
	}
}

// nodePlatforms are the os-arch of the machines the hub reaches (not its own: that one runs the hub's binary).
func (u *updater) nodePlatforms() []string {
	var out []string
	for _, m := range u.h.machinesNow() {
		m.mu.Lock()
		plat := m.info.OS + "-" + m.info.Arch
		local := m.cfg.Kind == "local"
		known := m.info.OS != "" && m.info.Arch != ""
		m.mu.Unlock()
		if !local && known && !slices.Contains(out, plat) {
			out = append(out, plat)
		}
	}
	return out
}

func (u *updater) onlineIDs() []string {
	var out []string
	for _, m := range u.h.machinesNow() {
		m.mu.Lock()
		if m.online {
			out = append(out, m.cfg.ID)
		}
		m.mu.Unlock()
	}
	return out
}

func (u *updater) healthURL() string {
	host, port, err := net.SplitHostPort(u.h.cfg.Listen)
	if err != nil || port == "" {
		port = "18660"
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/api/v1/health"
}

// updateBusy is what a restart would interrupt: any conversation working or waiting for the user (the assistant's
// and every agent's), and a machine being redeployed.
func (h *Hub) updateBusy() []busyItem {
	var out []busyItem
	for _, m := range h.machinesNow() {
		m.mu.Lock()
		deploying := m.deploying
		m.mu.Unlock()
		if deploying {
			out = append(out, busyItem{MachineID: m.cfg.ID, Machine: m.cfg.Name, Title: "正在重新部署"})
		}
		for _, s := range h.decorated(m) {
			if s.State == proto.StateWorking || s.State == proto.StateNeedsYou || s.Status == proto.StatusWorking {
				out = append(out, busyItem{MachineID: m.cfg.ID, Machine: m.cfg.Name, SessionID: s.SessionID, Title: agentTitle(s)})
			}
		}
	}
	return out
}

func (u *updater) setState(state string) { u.set(state, "", "") }

func (u *updater) isConfirmed() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.confirmed
}

// set changes the state the apps see and tells them.
func (u *updater) set(state, progress, errText string) {
	u.mu.Lock()
	u.state, u.progress = state, progress
	if errText != "" || state != "rolled_back" {
		u.errText = errText
	}
	u.mu.Unlock()
	u.h.broadcast(map[string]any{"type": "hub_update", "update": u.view()})
}

// updateView is GET /api/v1/hub/update.
type updateView struct {
	Version    string      `json:"version"`
	Build      int64       `json:"build"`
	Channel    string      `json:"channel"`
	CanUpdate  bool        `json:"can_update"`
	Why        string      `json:"why_not,omitempty"`
	Latest     *latestView `json:"latest,omitempty"`
	State      string      `json:"state"`
	Progress   string      `json:"progress,omitempty"`
	Busy       []busyItem  `json:"busy"`
	Error      string      `json:"error,omitempty"`
	Auto       bool        `json:"auto"`
	AutoNotice bool        `json:"auto_notice"`
	LastCheck  *time.Time  `json:"last_check,omitempty"`
	Confirmed  bool        `json:"confirmed"`
	Image      *imageView  `json:"image,omitempty"` // a Docker hub's image
}

type latestView struct {
	Version  string     `json:"version"`
	Build    int64      `json:"build"` // 0 for an image (tags have no build numbers)
	Released *time.Time `json:"released,omitempty"`
	Notes    string     `json:"notes,omitempty"`
	Newer    bool       `json:"newer"`
	Bad      bool       `json:"bad"` // went back before: not installed automatically
	// Held: why a newer release is not installed automatically even with 自动更新 on (Build.Hold), for people.
	Held string `json:"held,omitempty"`
}

func (u *updater) view() updateView {
	u.mu.Lock()
	defer u.mu.Unlock()
	v := updateView{Version: u.h.version, Build: u.h.build.Number, Channel: u.h.build.Channel, CanUpdate: u.h.build.SelfUpdates(),
		State: u.state, Progress: u.progress, Busy: u.busy, Error: u.errText, Auto: u.settings.auto() && u.h.build.SelfUpdates(),
		AutoNotice: u.h.build.SelfUpdates() && !u.settings.AutoNoticeSeen, Confirmed: u.confirmed}
	if v.Busy == nil {
		v.Busy = []busyItem{}
	}
	if !v.CanUpdate {
		v.Why = strings.TrimPrefix(u.whyNot().Error(), errNotRelease.Error()+"：")
	}
	if !u.lastCheck.IsZero() {
		t := u.lastCheck
		v.LastCheck = &t
	}
	if r := u.release; r != nil {
		released := r.Manifest.Released
		v.Latest = &latestView{Version: r.Manifest.Version, Build: r.Manifest.Build, Released: &released, Notes: r.Manifest.Notes,
			Newer: u.h.build.Newer(r.Manifest.Build), Bad: slices.Contains(u.settings.Bad, r.Manifest.Build)}
		if v.Latest.Newer && u.h.build.SelfUpdates() {
			v.Latest.Held = u.h.build.Hold(r.Manifest)
		}
	}
	if u.h.build.Channel == update.Docker {
		if im, err := u.dockerImage(); err == nil {
			v.Image = &imageView{Name: im.Name(), Tag: im.Tag, Pinned: im.Pinned()}
		}
		if u.newestImage != "" {
			v.Latest = &latestView{Version: u.newestImage, Newer: update.NewerVersion(u.newestImage, u.h.version)}
		}
	}
	return v
}

// imageView is the image a Docker hub runs, for the app's instructions: a pinned tag has to be changed in .env.
type imageView struct {
	Name   string `json:"name"`
	Tag    string `json:"tag"`
	Pinned bool   `json:"pinned"`
}

// dockerImage is the image this Docker hub runs: the compose file's JARVIS_IMAGE, else the one it was built as.
func (u *updater) dockerImage() (update.Image, error) {
	s := identity.Getenv("JARVIS_IMAGE")
	if s == "" {
		s = u.h.build.Image
	}
	if s == "" {
		return update.Image{}, errors.New("不知道这个 hub 是哪个镜像：compose 没把 JARVIS_IMAGE 传进容器")
	}
	return update.ParseImage(s)
}

// checkImage asks the registry for the newest release of the image this Docker hub runs (docs/hub-update.md「Docker」).
// A private image needs JARVIS_REGISTRY_TOKEN (a read token) in the container's environment.
func (u *updater) checkImage(ctx context.Context) error {
	im, err := u.dockerImage()
	if err != nil {
		u.set("idle", "", err.Error())
		return err
	}
	u.setState("checking")
	cctx, cancel := context.WithTimeout(ctx, updateRequestLimit)
	defer cancel()
	newest, err := update.LatestVersion(cctx, u.client, im, identity.Getenv("JARVIS_REGISTRY_TOKEN"))
	u.mu.Lock()
	u.lastCheck = time.Now()
	if err == nil {
		u.newestImage = newest
	}
	u.mu.Unlock()
	switch {
	case errors.Is(err, update.ErrPrivate):
		u.set("idle", "", "查不了新镜像："+err.Error()+"。在 .env 里设 JARVIS_REGISTRY_TOKEN（能读这个镜像的 token），或者把镜像改成公开")
	case err != nil:
		u.set("idle", "", "查不了新镜像："+err.Error())
	case update.NewerVersion(newest, u.h.version):
		u.set("available", "", "")
	default:
		u.set("idle", "", "")
	}
	return err
}

func (u *updater) routes(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/hub/update", func(w http.ResponseWriter, r *http.Request) {
		u.load(r.Context())
		writeJSON(w, u.view())
	})
	api.HandleFunc("POST /api/v1/hub/update/check", func(w http.ResponseWriter, r *http.Request) {
		u.load(r.Context())
		if _, err := u.check(r.Context()); err != nil && !errors.Is(err, errNotRelease) {
			writeErr(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, u.view())
	})
	api.HandleFunc("POST /api/v1/hub/update", func(w http.ResponseWriter, r *http.Request) {
		u.load(r.Context())
		var body struct {
			Now bool `json:"now"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body) // no body: an ordinary update
		// The update outlives this request: it runs on the hub's own context.
		if err := u.begin(context.WithoutCancel(r.Context()), body.Now); err != nil {
			writeErr(w, http.StatusConflict, err)
			return
		}
		writeJSON(w, u.view())
	})
	api.HandleFunc("PUT /api/v1/hub/update/settings", func(w http.ResponseWriter, r *http.Request) {
		u.load(r.Context())
		var body struct {
			Auto           *bool `json:"auto"`
			AutoNoticeSeen *bool `json:"auto_notice_seen"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		u.mu.Lock()
		if body.Auto != nil {
			a := *body.Auto
			u.settings.Auto = &a
			u.settings.AutoNoticeSeen = true // they chose: no need to tell them again
		}
		if body.AutoNoticeSeen != nil {
			u.settings.AutoNoticeSeen = *body.AutoNoticeSeen
		}
		u.saveLocked(r.Context())
		u.mu.Unlock()
		writeJSON(w, u.view())
	})
	api.HandleFunc("GET /api/v1/hub/update/history", func(w http.ResponseWriter, r *http.Request) {
		u.load(r.Context())
		u.mu.Lock()
		out := slices.Clone(u.settings.History)
		u.mu.Unlock()
		slices.Reverse(out)
		if out == nil {
			out = []updateRecord{}
		}
		writeJSON(w, map[string]any{"history": out})
	})
}

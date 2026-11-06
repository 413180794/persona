package devices

import (
	"compress/gzip"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/identity"
)

// Redeploying a node from the hub (POST /api/v1/machines/{id}/deploy, the button where a machine is not the hub's
// build): deploy.sh leaves every node binary of the build it deployed on the hub machine
// (~/.persona/bin/nodes/personad-<os>-<arch>), so the hub sends a machine its own build over ssh, kills the resident node
// if there is one (launchd starts it again from the new binary; `launchctl kickstart` from an ssh session did not always
// take, which is how two Macs kept an old node through deploys), and connects again.

func nodeBinaryDir() string { return identity.NodeBinaryDir() }

// nodeBinary is the hub's build for a machine's OS and architecture.
func nodeBinary(goos, arch string) (string, error) {
	if goos == "" || arch == "" {
		return "", i18n.Err("devices-deploy.os_unknown")
	}
	p := filepath.Join(nodeBinaryDir(), "personad-"+goos+"-"+arch)
	if _, err := os.Stat(p); err != nil {
		return "", i18n.Err("devices-deploy.no_binary", i18n.Args{"os": goos, "arch": arch})
	}
	return p, nil
}

// startDeploy checks a deploy can start and runs it in the background; the machine list shows its progress.
func (m *Machine) startDeploy() error {
	m.mu.Lock()
	if m.deploying {
		m.mu.Unlock()
		return nil
	}
	blocked := m.canDeployLocked()
	goos, arch, linked := m.info.OS, m.info.Arch, m.rpcDeploymentLocked()
	m.mu.Unlock()
	if blocked != nil {
		return blocked
	}
	bin, err := nodeBinary(goos, arch)
	if err != nil {
		return err
	}
	if linked {
		return m.startRPCDeployment(bin, nil)
	}
	m.mu.Lock()
	if m.deploying {
		m.mu.Unlock()
		return nil
	}
	m.deploying, m.deployErr, m.deployPhase = true, "", "transferring"
	m.mu.Unlock()
	m.d.changed()
	target := m.d.hubVersion()
	go func() {
		ctx := m.d.deployContext()
		err := m.deploy(ctx, bin)
		m.mu.Lock()
		m.deploying, m.deployPhase = false, ""
		if err != nil {
			m.deployErr = err.Error()
		}
		m.mu.Unlock()
		if err != nil {
			m.d.logf("[%s] deploy: %v", m.cfg.ID, err)
		} else {
			m.d.logf("[%s] deployed the hub's build %s", m.cfg.ID, target)
		}
		m.saveDeployOutcome(ctx, target, err)
		m.d.changed()
	}()
	return nil
}

// deployScript is the remote command that installs the gzipped binary on stdin as bin and stops a resident node so
// launchd restarts it. The pkill pattern is written so it does not match this ssh session's own command line.
func deployScript(bin string) string {
	target := shellQuote(bin)
	if rest, ok := strings.CutPrefix(bin, "~/"); ok {
		target = `"$HOME"/` + shellQuote(rest)
	}
	return posixCommand("set -e; b=" + target + `; mkdir -p "$(dirname "$b")"; gzip -dc > "$b.new"; chmod 755 "$b.new"; mv "$b.new" "$b"; ` +
		`pkill -f '[j]arvisd node --attach' || true`)
}

// shellQuote quotes s for a POSIX shell only (the script inside posixCommand); a remote command goes through
// anyShellQuote.
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// posixCommand is a remote command that runs a POSIX sh script on any machine. sshd hands the remote command to the
// user's login shell, which is not always a POSIX shell: on a machine whose login shell is fish the script failed at
// its first assignment and nothing was installed (#1770). `sh -c <script>` reads the same in sh, bash, zsh, fish and
// csh once the script is quoted by anyShellQuote; sh then runs the script itself.
func posixCommand(script string) string { return "sh -c " + anyShellQuote(script) }

// anyShellQuote quotes s as one word that POSIX shells, fish and csh all read back as s. Inside single quotes fish
// still takes \\ and \' as escapes and csh still expands !history (POSIX shells take all three literally), so a quote,
// a backslash or a ! is put outside the quotes, backslash-escaped, which every one of them reads alike. s must be one
// line: csh rejects a newline inside quotes.
func anyShellQuote(s string) string {
	return "'" + strings.NewReplacer(`'`, `'\''`, `\`, `'\\'`, `!`, `'\!'`).Replace(s) + "'"
}

func (m *Machine) deploy(ctx context.Context, bin string) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute) // a slow link: about 8 MB gzipped
	defer cancel()
	m.mu.Lock()
	via := m.via
	m.mu.Unlock()
	if via == "tailcat" { // no ssh to it now: the node takes the binary over tailcat itself
		if err := m.deployTailcat(ctx, bin); err != nil {
			return explain(i18n.Err("devices-deploy.install_failed", i18n.Args{"err": err.Error()}), err)
		}
		// The node exits about a second after answering: reconnecting before that would land on the old one.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
		return m.waitDeployed(ctx)
	}
	remote := m.cfg.RemoteBin
	if remote == "" {
		remote = "~/.persona/bin/personad"
	}
	host := m.cfg.SSHHost
	if host == "" {
		host = m.cfg.ID
	}
	f, err := os.Open(bin)
	if err != nil {
		return err
	}
	defer f.Close()
	cmd := exec.CommandContext(ctx, "ssh", "-T", "-o", "BatchMode=yes", "-o", "ConnectTimeout=15", host, deployScript(remote))
	pr, pw := io.Pipe()
	// exec copies a non-*os.File Stdin in its own goroutine and never closes it: once ssh exits early (auth failure,
	// connect timeout, the remote script failing, ctx done) nothing reads pr, and the gzip writer below would block on
	// pw for good (#2377). Closing pr makes its Write fail with io.ErrClosedPipe so it returns.
	defer pr.Close()
	cmd.Stdin = pr
	go func() {
		zw, _ := gzip.NewWriterLevel(pw, gzip.BestSpeed)
		_, err := io.Copy(zw, f)
		if err == nil {
			err = zw.Close()
		}
		pw.CloseWithError(err)
	}()
	if out, err := cmd.CombinedOutput(); err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return i18n.Err("devices-deploy.install_failed", i18n.Args{"err": msg})
	}
	return m.waitDeployed(ctx)
}

// deployWait bounds how long waitDeployed waits for the machine to come back (a variable for tests): the new node
// answers within a few seconds, a resident one waits for launchd's restart (ThrottleInterval 10 s), and over tailcat
// the new node first brings its endpoint up again.
var deployWait, deployPoll = 2 * time.Minute, 2 * time.Second

// waitText is how long a wait was, in the owner's words.
func waitText(l i18n.Lang, d time.Duration) string {
	if d >= time.Minute && d%time.Minute == 0 {
		return i18n.T(l, "devices-deploy.minutes", i18n.Args{"count": int(d / time.Minute)})
	}
	return i18n.T(l, "devices-deploy.seconds", i18n.Args{"count": int(d.Round(time.Second) / time.Second)})
}

// waitDeployed reconnects and waits for the machine to come back on the hub's build.
func (m *Machine) waitDeployed(ctx context.Context) error {
	start := time.Now()
	m.reconnect()
	deadline := start.Add(deployWait)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(deployPoll):
		}
		m.mu.Lock()
		online, version := m.online, m.info.Version
		m.mu.Unlock()
		if online && !buildDiffers(m.d.hubVersion(), version) {
			return nil
		}
	}
	m.mu.Lock()
	online, version, why := m.online, m.info.Version, m.err
	if m.dialErrAt.Before(start) {
		why = "" // how the old connection ended (our own reconnect), not why the new node is not reached
	}
	m.mu.Unlock()
	if online && !buildDiffers(m.d.hubVersion(), version) {
		return nil // came back just now
	}
	if !online {
		// Not connected: what it runs is not known (m.info is still the node from before the deploy), so saying it
		// "still runs" that build sent the owner to restart a node that may not start at all (#3393).
		l := i18n.Default // Keep stored errors canonical; the hub translates at response time.
		if why != "" {
			return i18n.Err("devices-deploy.not_back_why", i18n.Args{"wait": waitText(l, deployWait), "why": why})
		}
		return i18n.Err("devices-deploy.not_back", i18n.Args{"wait": waitText(l, deployWait)})
	}
	return i18n.Err("devices-deploy.still_old", i18n.Args{"version": version})
}

// updateNodes redeploys each connected machine whose node is not the hub's build, one at a time, and queues the
// update of an offline one reached over its link or connector (devices.update_nodes: the hub updated itself and put
// the release's node binaries where "Redeploy" takes them).
func (d *Devices) updateNodes(ctx context.Context) {
	version := d.hubVersion()
	for _, m := range d.machinesNow() {
		m.mu.Lock()
		need := m.cfg.Kind != "local" && m.online && buildDiffers(version, m.info.Version)
		offlineLinked := !m.online && m.rpcDeploymentLocked()
		m.mu.Unlock()
		if offlineLinked {
			if err := m.queueOfflineNodeUpdate(ctx); err != nil {
				d.logf("update: [%s] queue node update: %v", m.cfg.ID, err)
			}
			continue
		}
		if !need {
			continue
		}
		if err := m.startDeploy(); err != nil {
			d.logf("update: [%s] %v", m.cfg.ID, err)
			continue
		}
		for i := 0; i < 500; i++ { // deploy gives up after 15 minutes
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

// updateOnConnect gives a machine that comes back on another node build the hub's own (conn.go serve has just
// connected it). Only a hub that updates itself asks the manager to redeploy (selfupdate.go confirm ->
// devices.update_nodes); scripts/deploy/deploy.sh writes the hub's binaries where Redeploy takes them and restarts
// the hub, which is a plain start, so a machine that was asleep then, or that it could not reach, kept the old
// node: serve only logged the mismatch and the machine showed the version mismatch until someone pressed Redeploy.
//
// One attempt per machine and build: its outcome is kept (node.deploy.<id>, so it is known again after a hub
// restart) and a build that failed (the machine unreachable, a node that cannot run it, one that rolled back after
// a failed start) is not sent again on every reconnect. Redeploy still tries. When that record cannot be read it
// does not deploy either: it could not record the outcome, so a flapping machine would get a transfer per reconnect.
func (m *Machine) updateOnConnect(ctx context.Context) {
	version := m.d.hubVersion()
	m.mu.Lock()
	online, deploying, info := m.online, m.deploying, m.info
	m.mu.Unlock()
	if m.cfg.Kind == "local" || version == "" || !online || deploying || !nodeMismatch(version, info) {
		return
	}
	if m.d.opts.Store != nil {
		var record nodeDeployRecord
		found, err := m.d.opts.Store.Setting(ctx, m.deployRecordKey(), &record)
		switch {
		case err != nil:
			m.d.logf("[%s] read the last node update: %v", m.cfg.ID, err)
			return
		case found && record.Target == version:
			return // this build was tried already: a queued one continues in nodeUpdateConnected, a failed one waits for Redeploy
		}
	}
	m.d.logf("[%s] node %s is not the hub's build %s: send it the hub's", m.cfg.ID, info.Version, version)
	if err := m.startDeploy(); err != nil {
		m.d.logf("[%s] update: %v", m.cfg.ID, err)
		// the reason stays on the machine's card, as a failed Redeploy's does
		m.mu.Lock()
		m.deployErr = err.Error()
		m.mu.Unlock()
		m.d.changed()
		m.saveDeployOutcome(ctx, version, err)
	}
}

// saveDeployOutcome keeps what came of a deploy over ssh or tailcat (the update protocol saves its own,
// deploy_rpc.go), so updateOnConnect does not send a machine the same build again on every reconnect.
func (m *Machine) saveDeployOutcome(ctx context.Context, target string, err error) {
	if target == "" {
		return
	}
	record := nodeDeployRecord{Target: target, State: "succeeded", At: time.Now().UTC()}
	if err != nil {
		record.State, record.Error = "failed", err.Error()
	}
	if saveErr := m.saveDeployRecord(ctx, record); saveErr != nil {
		m.d.logf("[%s] save node deployment: %v", m.cfg.ID, saveErr)
	}
}

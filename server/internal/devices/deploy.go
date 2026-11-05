package devices

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"github.com/nwverse/persona/server/internal/devicewire"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Redeploying a node from the hub (POST /api/v1/machines/{id}/deploy, the button where a machine is not the hub's
// build): deploy.sh leaves every node binary of the build it deployed on the hub machine
// (~/.jarvis/bin/nodes/jarvisd-<os>-<arch>), so the hub sends a machine its own build over ssh, kills the resident node
// if there is one (launchd starts it again from the new binary; `launchctl kickstart` from an ssh session did not always
// take, which is how two Macs kept an old node through deploys), and connects again.

func nodeBinaryDir() string { return devicewire.NodeBinaryDir() }

// nodeBinary is the hub's build for a machine's OS and architecture.
func nodeBinary(goos, arch string) (string, error) {
	if goos == "" || arch == "" {
		return "", errors.New("还不知道这台电脑的系统，连上以后再试")
	}
	p := filepath.Join(nodeBinaryDir(), "jarvisd-"+goos+"-"+arch)
	if _, err := os.Stat(p); err != nil {
		return "", fmt.Errorf("hub 上没有 %s/%s 的 jarvisd：在开发机上跑一次 scripts/deploy/deploy.sh（Docker 部署的 hub 用镜像里带的）", goos, arch)
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
	allowed, reason := m.canDeployLocked()
	goos, arch, linked := m.info.OS, m.info.Arch, m.rpcDeploymentLocked()
	m.mu.Unlock()
	if !allowed {
		return errors.New(reason)
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
			m.d.logf("[%s] deployed the hub's build %s", m.cfg.ID, m.d.hubVersion())
		}
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
			return fmt.Errorf("装不上：%v", err)
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
		remote = "~/.jarvis/bin/jarvisd"
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
		return fmt.Errorf("装不上：%s", msg)
	}
	return m.waitDeployed(ctx)
}

// deployWait bounds how long waitDeployed waits for the machine to come back (a variable for tests): the new node
// answers within a few seconds, a resident one waits for launchd's restart (ThrottleInterval 10 s), and over tailcat
// the new node first brings its endpoint up again.
var deployWait, deployPoll = 2 * time.Minute, 2 * time.Second

// waitText is how long a wait was, in the owner's words.
func waitText(d time.Duration) string {
	if d >= time.Minute && d%time.Minute == 0 {
		return fmt.Sprintf("%d 分钟", int(d/time.Minute))
	}
	return fmt.Sprintf("%d 秒", int(d.Round(time.Second)/time.Second))
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
		if online && !nodeMismatch(m.d.hubVersion(), version) {
			return nil
		}
	}
	m.mu.Lock()
	online, version, why := m.online, m.info.Version, m.err
	if m.dialErrAt.Before(start) {
		why = "" // how the old connection ended (our own reconnect), not why the new node is not reached
	}
	m.mu.Unlock()
	if online && !nodeMismatch(m.d.hubVersion(), version) {
		return nil // came back just now
	}
	if !online {
		// Not connected: what it runs is not known (m.info is still the node from before the deploy), so saying it
		// "still runs" that build sent the owner to restart a node that may not start at all (#3393).
		msg := fmt.Sprintf("装好了，但这台电脑 %s内没有重新连上 hub", waitText(deployWait))
		if why != "" {
			msg += "：" + why
		}
		return errors.New(msg)
	}
	return fmt.Errorf("装好了，但这台电脑上跑的还是 %s：在那台电脑上重启一下 jarvisd", version)
}

// updateNodes redeploys each connected machine whose node is not the hub's build, one at a time, and queues the
// update of an offline one reached over its link or connector (devices.update_nodes: the hub updated itself and put
// the release's node binaries where 重新部署 takes them).
func (d *Devices) updateNodes(ctx context.Context) {
	version := d.hubVersion()
	for _, m := range d.machinesNow() {
		m.mu.Lock()
		need := m.cfg.Kind != "local" && m.online && nodeMismatch(version, m.info.Version)
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

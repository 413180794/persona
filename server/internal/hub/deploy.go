package hub

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"github.com/nwverse/persona/server/internal/identity"
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

// nodeBinaryDir is where deploy.sh puts the node binaries next to the hub's own; JARVIS_NODE_BINARIES points
// elsewhere (the Docker image carries them in /opt/jarvis/nodes: ~/.jarvis is the data volume there).
func nodeBinaryDir() string {
	if d := identity.Getenv("JARVIS_NODE_BINARIES"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(identity.DataDir(home), "bin", "nodes")
}

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
	m.hub.broadcastMachines()
	go func() {
		ctx := m.hub.deployContext()
		err := m.deploy(ctx, bin)
		m.mu.Lock()
		m.deploying, m.deployPhase = false, ""
		if err != nil {
			m.deployErr = err.Error()
		}
		m.mu.Unlock()
		if err != nil {
			m.hub.logf("[%s] deploy: %v", m.cfg.ID, err)
		} else {
			m.hub.logf("[%s] deployed the hub's build %s", m.cfg.ID, m.hub.version)
		}
		m.hub.broadcastMachines()
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

// waitDeployed reconnects and waits for the machine to come back on the hub's build.
func (m *Machine) waitDeployed(ctx context.Context) error {
	m.reconnect()
	// The new node answers within a few seconds; a resident one waits for launchd's restart (ThrottleInterval 10 s).
	deadline := time.Now().Add(2 * time.Minute) // over tailcat the new node first brings its endpoint up again
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
		m.mu.Lock()
		online, version := m.online, m.info.Version
		m.mu.Unlock()
		if online && !nodeMismatch(m.hub.version, version) {
			return nil
		}
	}
	m.mu.Lock()
	version := m.info.Version
	m.mu.Unlock()
	return fmt.Errorf("装好了，但这台电脑上跑的还是 %s：在那台电脑上重启一下 jarvisd", version)
}

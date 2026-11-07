package devices

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime/pprof"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/i18n"
)

// loginShells are the shells on this machine an ssh target's login shell could be: sshd hands the remote command to
// it as `<shell> -c <command>`, so a remote command must mean the same in each (fish and csh are not POSIX shells).
func loginShells(t *testing.T) []string {
	t.Helper()
	var found []string
	for _, sh := range []string{"sh", "bash", "zsh", "dash", "fish", "csh", "tcsh"} {
		if p, err := exec.LookPath(sh); err == nil {
			found = append(found, p)
		}
	}
	if len(found) == 0 {
		t.Skip("no shell")
	}
	return found
}

// The command the hub runs over ssh installs the gzipped binary from stdin at the node's path (~ expanded, a path
// with a quote, a backslash or a ! kept whole), leaves no .new file behind, and restarts the machine's resident node,
// whatever the machine's login shell is (#1770: on a machine whose login shell is fish it failed at `b=`).
func TestDeployScriptInstallsTheBinary(t *testing.T) {
	for _, shell := range loginShells(t) {
		for _, bin := range []string{"~/.persona/bin/personad", "~/odd 'dir'/personad", `~/back\\slash \'q/personad`, "~/bang!x/personad"} {
			t.Run(filepath.Base(shell)+"/"+bin, func(t *testing.T) { testDeployScript(t, shell, bin) })
		}
	}
}

func testDeployScript(t *testing.T, shell, bin string) {
	home := t.TempDir()
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	_, _ = zw.Write([]byte("#!/bin/sh\necho new\n"))
	_ = zw.Close()
	pkilled := fakePkill(t)
	cmd := exec.Command(shell, "-c", deployScript(bin))
	cmd.Env = append(os.Environ(), "HOME="+home, "PATH="+filepath.Dir(pkilled)+string(os.PathListSeparator)+os.Getenv("PATH"))
	cmd.Stdin = &gz
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	got, _ := os.ReadFile(pkilled)
	pattern, ok := strings.CutPrefix(strings.TrimSuffix(string(got), "\n"), "-f ")
	if !ok {
		t.Fatalf("the resident node is not restarted: pkill got %q", got)
	}
	// The pattern finds the resident node persona-link's service runs (cmd/persona-link nodesvc.go), and the one of a
	// computer not cut over to the Persona names yet; it leaves the other Persona processes and the remote command that
	// carries it alone.
	re := regexp.MustCompile(pattern)
	personad := filepath.Join(home, ".persona", "bin", "personad")
	for _, node := range []string{personad + " node --attach", filepath.Join(home, ".jarvis", "bin", "jarvisd") + " node --attach"} {
		if !re.MatchString(node) {
			t.Errorf("pkill -f %q does not find the resident node %q", pattern, node)
		}
	}
	for _, other := range []string{personad + " node --stdio", personad + " hub run", personad + " hook", personad + "-wechat run",
		personad + " manager run --dir /x", deployScript(bin)} {
		if re.MatchString(other) {
			t.Errorf("pkill -f %q matches %q", pattern, other)
		}
	}
	path := filepath.Join(home, strings.TrimPrefix(bin, "~/"))
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "#!/bin/sh\necho new\n" {
		t.Fatalf("%q %v", got, err)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o755 {
		t.Errorf("mode %v", st.Mode())
	}
	if _, err := os.Stat(path + ".new"); err == nil {
		t.Error(".new left behind")
	}
}

// No binary for the machine's platform on the hub: say what to do, don't start.
func TestNodeBinaryMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir on Windows
	if _, err := nodeBinary("plan9", "mips"); err == nil || !strings.Contains(err.Error(), "deploy.sh") {
		t.Fatalf("err = %v", err)
	}
	if _, err := nodeBinary("", ""); err == nil {
		t.Fatal("no platform yet: want an error")
	}
	dir := nodeBinaryDir()
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "personad-darwin-arm64"), []byte("x"), 0o755)
	if p, err := nodeBinary("darwin", "arm64"); err != nil || filepath.Base(p) != "personad-darwin-arm64" {
		t.Fatalf("%s %v", p, err)
	}
}

// fakePkill puts a pkill first on PATH that only records its arguments (the returned file) and kills nothing. The
// deploy script ends with `pkill -f '[p]ersonad node --attach'`; run for real on a developer's machine it stops the
// resident node serving that machine (it did, every time the hub tests ran on a Mac with one: the machine went offline
// for a minute and a half).
func fakePkill(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	calls := filepath.Join(dir, "pkill.calls")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + shellQuote(calls) + "\n"
	if err := os.WriteFile(filepath.Join(dir, "pkill"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return calls
}

// A word anyShellQuote quotes reads back unchanged in every login shell, quotes, backslashes and the characters each
// shell treats specially included.
func TestAnyShellQuoteReadsTheSameInEveryShell(t *testing.T) {
	words := []string{"", "plain", "it's", `back\slash`, `\\`, `\'`, `'\''`, `a"b`, "a!b", "!!", "!$", "$HOME ~ * ? [x] {a,b} `id` $(id) ! ; & | < > # %"}
	for _, shell := range loginShells(t) {
		for _, w := range words {
			out, err := exec.Command(shell, "-c", "printf '%s|' "+anyShellQuote(w)).CombinedOutput()
			if err != nil || string(out) != w+"|" {
				t.Errorf("%s: %q read back as %q (%v)", filepath.Base(shell), w, out, err)
			}
		}
	}
}

// The remote command that starts a node over ssh is a plain command with arguments, which every login shell runs
// alike (#1770): it starts the node at ~/.persona/bin/personad with `node --stdio`.
func TestNodeCommandRunsInEveryShell(t *testing.T) {
	m := newMachine(newTestDevices(t), MachineConfig{ID: "box", Name: "Box", Kind: "ssh"})
	args := m.command(context.Background()).Args
	remote := args[len(args)-1]
	for _, shell := range loginShells(t) {
		home := t.TempDir()
		calls := filepath.Join(home, "calls")
		bin := filepath.Join(home, ".persona", "bin", "personad")
		_ = os.MkdirAll(filepath.Dir(bin), 0o755)
		_ = os.WriteFile(bin, []byte("#!/bin/sh\nprintf '%s|' \"$@\" > "+shellQuote(calls)+"\n"), 0o755)
		cmd := exec.Command(shell, "-c", remote)
		cmd.Env = append(os.Environ(), "HOME="+home)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s: %q: %v: %s", filepath.Base(shell), remote, err, out)
		}
		if got, _ := os.ReadFile(calls); string(got) != "node|--stdio|" {
			t.Errorf("%s: %q ran the node with %q", filepath.Base(shell), remote, got)
		}
	}
}

// An ssh that fails before reading the binary (auth failure, connect timeout) must not leave the goroutine that gzips
// the binary into it blocked on the pipe for good (#2377).
func TestDeployOverFailedSSHDoesNotLeakTheWriter(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte("#!/bin/sh\necho 'Permission denied (publickey).' >&2\nexit 255\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	// Incompressible and far larger than any pipe buffer, so the writer is still writing when ssh exits.
	data := make([]byte, 8<<20)
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "personad")
	if err := os.WriteFile(bin, data, 0o755); err != nil {
		t.Fatal(err)
	}
	m := &Machine{cfg: MachineConfig{ID: "test-node"}}
	err := m.deploy(context.Background(), bin)
	if err == nil || !strings.Contains(err.Error(), "Permission denied") {
		t.Fatalf("deploy: %v", err)
	}
	for deadline := time.Now().Add(5 * time.Second); ; {
		var stacks bytes.Buffer
		_ = pprof.Lookup("goroutine").WriteTo(&stacks, 2)
		if !strings.Contains(stacks.String(), "(*Machine).deploy.func") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the gzip writer is still running after deploy returned:\n%s", stacks.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// A deploy whose machine does not come back must not say what it "still runs": the hub is not connected to it, and
// the version it knows is the node from before the deploy (#3393). The last connection error says what went wrong.
func TestDeployThatDoesNotComeBackSaysSo(t *testing.T) {
	wait, poll := deployWait, deployPoll
	deployWait, deployPoll = 50*time.Millisecond, 10*time.Millisecond
	t.Cleanup(func() { deployWait, deployPoll = wait, poll })
	d := newTestDevices(t)

	m := newMachine(d, MachineConfig{ID: "away", Name: "away", Kind: "ssh"})
	m.info.Version = "old-build"
	m.err = "reconnect requested" // how the old connection ended: no reason why the new one is not there
	go func() {
		time.Sleep(10 * time.Millisecond)
		m.markOffline(errors.New("ssh: connect to host away port 22: Connection refused"))
	}()
	err := m.waitDeployed(context.Background())
	if err == nil {
		t.Fatal("waitDeployed: no error for a machine that never came back")
	}
	if strings.Contains(err.Error(), "old-build") || strings.Contains(err.Error(), "跑的还是") {
		t.Errorf("says what an unconnected machine runs: %v", err)
	}
	if !strings.Contains(err.Error(), "没有重新连上") || !strings.Contains(err.Error(), "Connection refused") {
		t.Errorf("does not say it did not come back, and why: %v", err)
	}

	// Back, but on another build: that is what it still runs.
	m2 := newMachine(d, MachineConfig{ID: "back", Name: "back", Kind: "ssh"})
	m2.online, m2.info.Version = true, "old-build"
	if err := m2.waitDeployed(context.Background()); err == nil || !strings.Contains(err.Error(), "跑的还是 old-build") {
		t.Errorf("online on another build: %v", err)
	}
}

// Our own reconnect's "reconnect requested" is not why a redeployed machine is not back.
func TestDeployThatDoesNotComeBackHidesItsOwnReconnect(t *testing.T) {
	wait, poll := deployWait, deployPoll
	deployWait, deployPoll = 30*time.Millisecond, 10*time.Millisecond
	t.Cleanup(func() { deployWait, deployPoll = wait, poll })
	d := newTestDevices(t)
	m := newMachine(d, MachineConfig{ID: "away", Name: "away", Kind: "ssh"})
	m.info.Version, m.err = "old-build", "reconnect requested"
	err := m.waitDeployed(context.Background())
	if err == nil || strings.Contains(err.Error(), "reconnect requested") || !strings.Contains(err.Error(), "没有重新连上") {
		t.Errorf("waitDeployed: %v", err)
	}
}

func TestWaitText(t *testing.T) {
	for d, want := range map[time.Duration]string{2 * time.Minute: "2 分钟", 90 * time.Second: "90 秒", 50 * time.Millisecond: "0 秒"} {
		if got := waitText(i18n.ZhHans, d); got != want {
			t.Errorf("waitText(%v) = %q, want %q", d, got, want)
		}
	}
}

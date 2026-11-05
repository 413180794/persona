package hub

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/pprof"
	"strings"
	"testing"
	"time"
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
		for _, bin := range []string{"~/.jarvis/bin/jarvisd", "~/odd 'dir'/jarvisd", `~/back\\slash \'q/jarvisd`, "~/bang!x/jarvisd"} {
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
	if got, _ := os.ReadFile(pkilled); string(got) != "-f [j]arvisd node --attach\n" {
		t.Errorf("the resident node is not restarted: pkill got %q", got)
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
	_ = os.WriteFile(filepath.Join(dir, "jarvisd-darwin-arm64"), []byte("x"), 0o755)
	if p, err := nodeBinary("darwin", "arm64"); err != nil || filepath.Base(p) != "jarvisd-darwin-arm64" {
		t.Fatalf("%s %v", p, err)
	}
}

// fakePkill puts a pkill first on PATH that only records its arguments (the returned file) and kills nothing. The
// deploy script ends with `pkill -f '[j]arvisd node --attach'`; run for real on a developer's machine it stops the
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
// alike (#1770): it starts the node at ~/.jarvis/bin/jarvisd with `node --stdio`.
func TestNodeCommandRunsInEveryShell(t *testing.T) {
	m := newMachine(newTestHub(t), MachineConfig{ID: "box", Name: "Box", Kind: "ssh"})
	args := m.command(context.Background()).Args
	remote := args[len(args)-1]
	for _, shell := range loginShells(t) {
		home := t.TempDir()
		calls := filepath.Join(home, "calls")
		bin := filepath.Join(home, ".jarvis", "bin", "jarvisd")
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
	bin := filepath.Join(dir, "jarvisd")
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

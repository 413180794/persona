package hub

import (
	"bytes"
	"compress/gzip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The script the hub runs over ssh installs the gzipped binary from stdin at the node's path (~ expanded, a path
// with a quote kept whole), leaves no .new file behind, and restarts the machine's resident node.
func TestDeployScriptInstallsTheBinary(t *testing.T) {
	for _, bin := range []string{"~/.jarvis/bin/jarvisd", "~/odd 'dir'/jarvisd"} {
		home := t.TempDir()
		var gz bytes.Buffer
		zw := gzip.NewWriter(&gz)
		_, _ = zw.Write([]byte("#!/bin/sh\necho new\n"))
		_ = zw.Close()
		pkilled := fakePkill(t)
		cmd := exec.Command("bash", "-c", deployScript(bin))
		cmd.Env = append(os.Environ(), "HOME="+home, "PATH="+filepath.Dir(pkilled)+string(os.PathListSeparator)+os.Getenv("PATH"))
		cmd.Stdin = &gz
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s: %v: %s", bin, err, out)
		}
		if got, _ := os.ReadFile(pkilled); string(got) != "-f [j]arvisd node --attach\n" {
			t.Errorf("%s: the resident node is not restarted: pkill got %q", bin, got)
		}
		path := filepath.Join(home, strings.TrimPrefix(bin, "~/"))
		got, err := os.ReadFile(path)
		if err != nil || string(got) != "#!/bin/sh\necho new\n" {
			t.Fatalf("%s: %q %v", bin, got, err)
		}
		if st, _ := os.Stat(path); st.Mode().Perm() != 0o755 {
			t.Errorf("%s: mode %v", bin, st.Mode())
		}
		if _, err := os.Stat(path + ".new"); err == nil {
			t.Errorf("%s: .new left behind", bin)
		}
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

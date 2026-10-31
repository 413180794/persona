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
// with a quote kept whole) and leaves no .new file behind.
func TestDeployScriptInstallsTheBinary(t *testing.T) {
	for _, bin := range []string{"~/.jarvis/bin/jarvisd", "~/odd 'dir'/jarvisd"} {
		home := t.TempDir()
		var gz bytes.Buffer
		zw := gzip.NewWriter(&gz)
		_, _ = zw.Write([]byte("#!/bin/sh\necho new\n"))
		_ = zw.Close()
		cmd := exec.Command("bash", "-c", deployScript(bin))
		cmd.Env = append(os.Environ(), "HOME="+home)
		cmd.Stdin = &gz
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s: %v: %s", bin, err, out)
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
	t.Setenv("HOME", t.TempDir())
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

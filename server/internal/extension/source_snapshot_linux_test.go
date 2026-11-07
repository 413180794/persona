//go:build linux

package extension

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/extension/sandbox"
)

func TestSealedSourceSnapshotPinsMountedBytes(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "bundle.mjs")
	b := []byte("export default 1")
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(b)
	s := &SourceRuntime{Bundle: "bundle.mjs", SHA256: hex.EncodeToString(h[:])}
	f, err := snapshotSourceBundle(root, s)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteAt([]byte("x"), 0); err == nil {
		t.Fatal("snapshot was writable")
	}
	if err := f.Truncate(0); err == nil {
		t.Fatal("snapshot was resizable")
	}
	// The package pathname changes after checking: mounting the snapshot still
	// yields only the signed bytes, even when a symlink now replaces the artifact.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(root, "other.mjs")
	if err := os.WriteFile(other, []byte("export default 2"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, path); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(f.Name())
	if err != nil || string(got) != string(b) {
		t.Fatal(string(got), err)
	}
	// bwrap on PATH is not enough: a container may refuse the /proc mount the
	// namespace needs. Probe tries those mounts; when they work, a failure
	// below is a real one.
	sup := sandbox.Probe()
	if sup.Bwrap == "" {
		t.Skip("namespace mount test unavailable: " + sup.Why)
	}
	bwrap := sup.Bwrap
	self, _ := os.Executable()
	ns, err := sandbox.ValidationNamespace(self, "snapshot-test-read", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ns = append([]string{"--ro-bind-data", "3", "/extension/bundle.mjs"}, ns...)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bwrap, ns...)
	cmd.Env = []string{"PATH=/usr/bin:/bin"}
	cmd.ExtraFiles = []*os.File{f}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("sealed snapshot mount: %v %s", err, out)
	}
	// Scope descriptor plumbing is separate from real resource-controller
	// acceptance. This trusted-fixture scope establishes no memory/CPU claim.
	if os.Getenv("PERSONA_TEST_SOURCE_VALIDATOR_PROCESS") == "1" {
		if _, err := f.Seek(0, 0); err != nil {
			t.Fatal(err)
		}
		unit := fmt.Sprintf("persona-ext-snapshot-test-%d.scope", time.Now().UnixNano())
		scope := exec.CommandContext(ctx, "systemd-run", append([]string{"--user", "--scope", "--quiet", "--collect", "--unit", unit, "--", bwrap}, ns...)...)
		scope.ExtraFiles = []*os.File{f}
		if out, err := scope.CombinedOutput(); err != nil {
			t.Fatalf("scope descriptor mount (not resource acceptance): %v %s", err, out)
		}
	}
}

//go:build persona_dev_resources && linux

package sandbox

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestDevelopmentMissingNamespaceStillRefuses(t *testing.T) {
	if os.Getenv("TEST_RESOURCE_MISSING_BWRAP") == "1" {
		self, _ := os.Executable()
		dev, err := SourceDevelopmentResources(self)
		if dev || err == nil || !strings.Contains(err.Error(), "sandbox unavailable") {
			t.Fatalf("missing namespace accepted: %v %v", dev, err)
		}
		return
	}
	self, _ := os.Executable()
	for _, failure := range []string{"missing", "namespace-rejected"} {
		t.Run(failure, func(t *testing.T) {
			dir := t.TempDir()
			if failure == "namespace-rejected" {
				if err := os.WriteFile(filepath.Join(dir, "bwrap"), []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, self, "-test.run=^TestDevelopmentMissingNamespaceStillRefuses$", "-test.v")
			cmd.Env = append(os.Environ(), "PATH="+dir, "TEST_RESOURCE_MISSING_BWRAP=1", DevelopmentResourceEnv+"=1")
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("bwrap rejection test: %s %v", out, err)
			}
		})
	}
}

func TestDevelopmentChildRlimits(t *testing.T) {
	self, _ := os.Executable()
	before := map[int]syscall.Rlimit{}
	for _, kind := range []int{syscall.RLIMIT_CPU, syscall.RLIMIT_FSIZE, syscall.RLIMIT_NOFILE, syscall.RLIMIT_CORE} {
		var r syscall.Rlimit
		if err := syscall.Getrlimit(kind, &r); err != nil {
			t.Fatal(err)
		}
		before[kind] = r
	}
	run := func(args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		return exec.CommandContext(ctx, self, append([]string{RunCommand, "--development-exec", "3", "--"}, args...)...).CombinedOutput()
	}
	out, err := run("/bin/sh", "-c", "ulimit -n; ulimit -f; ulimit -c; ulimit -t")
	if err != nil || !strings.Contains(string(out), "128\n16384\n0\n3\n") {
		t.Fatalf("child limits: %s %v", out, err)
	}
	t.Logf("actual child limits:\n%s", out)
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal(err)
	}
	out, err = run(python, "-I", "-c", "import os\nfds=[]\ntry:\n while True: fds.append(os.open('/dev/null', os.O_RDONLY))\nexcept OSError as e:\n print('opened='+str(len(fds))+' errno='+str(e.errno))\n if e.errno != 24: raise\n")
	if err != nil || !strings.Contains(string(out), "opened=125 errno=24") {
		t.Fatalf("fd bound did not enforce: %s %v", out, err)
	}
	t.Logf("fd bound rejection: %s", out)
	file := filepath.Join(t.TempDir(), "bounded")
	out, err = run("/usr/bin/dd", "if=/dev/zero", "of="+file, "bs=1048576", "count=9", "status=none")
	st, statErr := os.Stat(file)
	if err == nil || statErr != nil || st.Size() != 8<<20 {
		t.Fatalf("file size limit: %s %v %v", out, err, statErr)
	}
	t.Logf("file growth terminated at %d bytes: %v", st.Size(), err)
	for kind, want := range before {
		var r syscall.Rlimit
		syscall.Getrlimit(kind, &r)
		if r != want {
			t.Fatal("manager/test-process limits changed", kind, r, want)
		}
	}
	t.Log("manager limits unchanged")
}

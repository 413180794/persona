package node

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Linux's /proc and /sys (#4172): every process's own folder and the kernel's secrets are refused, by any spelling
// that leads there (self, thread-self, a pid, root and cwd back into the file system); the machine's own files are
// not. The rules are about paths, so they are checked on every OS.
func TestPathGuardProcAndSys(t *testing.T) {
	n := &Node{home: t.TempDir()}
	for _, p := range []string{
		"/proc/self/environ", "/proc/thread-self/environ", "/proc/1/environ", "/proc/4242/cmdline", "/proc/4242/mem",
		"/proc/4242/maps", "/proc/4242/fd/3", "/proc/4242/root/etc/shadow", "/proc/4242/cwd/.ssh/id_ed25519",
		"/proc/4242/task/4243/environ", "/proc/4242", "/proc/self", "/proc/SELF/environ",
		"/proc/kcore", "/proc/kmsg", "/proc/kallsyms", "/proc/keys", "/proc/key-users", "/proc/kpageflags",
		"/sys/firmware/efi/efivars/Boot0000-8be4df61", "/sys/kernel/debug/x", "/sys/kernel/security/lsm",
		"/sys/kernel/tracing/trace", "/sys/firmware",
	} {
		if err := n.offLimits(p); err != errOffLimits {
			t.Errorf("%s: %v, want refused", p, err)
		}
	}
	for _, p := range []string{
		"/proc", "/proc/cpuinfo", "/proc/meminfo", "/proc/loadavg", "/proc/version", "/proc/net/dev",
		"/proc/sys/kernel/hostname", "/sys/class/net/eth0/address", "/sys/kernel/mm/transparent_hugepage/enabled",
		"/sys/kernel", "/home/someone/proc/4242/environ", "/tmp/sys/firmware",
	} {
		if err := n.offLimits(p); err != nil {
			t.Errorf("%s: %v, want readable", p, err)
		}
	}
}

// A link elsewhere that points into a process's folder is refused as it is followed, before anything is read. Where
// there is no /proc (macOS) the walk stops at it first: nothing is read either way.
func TestPathGuardLinkIntoProc(t *testing.T) {
	home := t.TempDir()
	n := &Node{home: home}
	if err := os.Symlink("/proc/self/environ", filepath.Join(home, "env")); err != nil {
		t.Skip("symlinks:", err)
	}
	if err := os.Symlink("/proc/self", filepath.Join(home, "me")); err != nil {
		t.Skip("symlinks:", err)
	}
	for _, p := range []string{"~/env", "~/me/environ", "~/me/root/etc/hostname"} {
		_, err := n.readFile(p, "", 0, false)
		if err == nil || runtime.GOOS == "linux" && !strings.Contains(err.Error(), "secrets") {
			t.Errorf("%s: %v, want refused", p, err)
		}
	}
}

// Only a regular file of size 0 is read to its end: a device of size 0 is not read at all (/dev/zero never ends).
func TestReadFileDeviceIsNotRead(t *testing.T) {
	n := &Node{home: t.TempDir()}
	done := make(chan error, 1)
	var fc *FileContent
	go func() {
		var err error
		fc, err = n.readFile("/dev/zero", "", 0, false)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
		if fc.Text != "" || fc.Data != "" || fc.Truncated {
			t.Fatalf("read a device: %+v", fc)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("reading /dev/zero did not end")
	}
}

// On Linux, a /proc file about the machine (size 0 in stat) reads with its content, not as an empty file; a process's
// folder stays refused.
func TestReadFileProcOnLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("/proc is Linux's")
	}
	n := &Node{home: t.TempDir()}
	fc, err := n.readFile("/proc/version", "", 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(fc.Text, "Linux version") || fc.Size != int64(len(fc.Text)) || fc.Truncated {
		t.Fatalf("/proc/version: size %d, truncated %v, %q", fc.Size, fc.Truncated, fc.Text)
	}
	if _, err := n.readFile("/proc/self/environ", "", 0, false); err == nil {
		t.Fatal("/proc/self/environ was read")
	}
}

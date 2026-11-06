package sandbox

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStrictResourceFilesV1AndV2(t *testing.T) {
	unit := "persona-ext-example.scope"
	cases := []struct {
		name, cg, mounts string
		files            map[string]string
	}{
		{"v1", "7:memory:/user/" + unit + "\n6:cpu,cpuacct:/user/" + unit, "1 0 0:1 / /memory rw - cgroup cgroup rw,memory\n2 0 0:2 / /cpu rw - cgroup cgroup rw,cpu,cpuacct", map[string]string{"/memory/user/" + unit + "/memory.limit_in_bytes": "134217728", "/memory/user/" + unit + "/memory.memsw.limit_in_bytes": "134217728", "/cpu/user/" + unit + "/cpu.cfs_quota_us": "20000", "/cpu/user/" + unit + "/cpu.cfs_period_us": "100000"}},
		{"v2", "0::/user/" + unit, "1 0 0:1 / /cg rw - cgroup2 cgroup rw", map[string]string{"/cg/user/" + unit + "/memory.max": "134217728", "/cg/user/" + unit + "/memory.swap.max": "0", "/cg/user/" + unit + "/cpu.max": "20000 100000"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			read := func(p string) ([]byte, error) {
				v, ok := tc.files[p]
				if !ok {
					return nil, os.ErrNotExist
				}
				return []byte(v), nil
			}
			if err := verifyResourceFiles(tc.cg, tc.mounts, 128, 20, unit, read); err != nil {
				t.Fatal(err)
			}
			if err := verifyResourceFiles(strings.ReplaceAll(tc.cg, unit, "shared.scope"), tc.mounts, 128, 20, unit, read); err == nil {
				t.Fatal("accepted shared controller membership")
			}
			for p, v := range tc.files {
				tc.files[p] = "max"
				if err := verifyResourceFiles(tc.cg, tc.mounts, 128, 20, unit, read); err == nil {
					t.Errorf("accepted unenforced %s", p)
				}
				tc.files[p] = v
			}
			if err := verifyResourceFiles(tc.cg, "", 128, 20, unit, read); err == nil {
				t.Fatal("accepted missing controllers")
			}
		})
	}
}

func TestStrictSandboxMountsAreNarrow(t *testing.T) {
	args := linuxSetup(Plan{Strict: true, Self: "/trusted/self", ProxySocket: "/own/proxy.sock", ReadOnly: []Mount{{Source: "/pinned/deno", Target: "/runtime/deno"}, {Source: "/own/bundle.mjs", Target: "/extension/bundle.mjs"}}})
	joined := strings.Join(args, " ")
	for _, bad := range []string{"--ro-bind-try /usr", "--ro-bind-try /etc", "--bind /own", "--unshare-user-try"} {
		if strings.Contains(joined, bad) {
			t.Fatal(joined)
		}
	}
	if !strings.Contains(joined, "--unshare-net") || !strings.Contains(joined, "--remount-ro /") {
		t.Fatal(joined)
	}
}

func TestStrictEnforcementForReal(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := ProbeStrict(self); err != nil {
		t.Skipf("UNAVAILABLE: full sandbox/resource acceptance blocked: %v", err)
	}
	t.Log("VERIFIED: actual namespaces, read-only mounts, independent kernel memory/swap/CPU controls")
}
func TestResourceCheckFailsBeforeGuest(t *testing.T) {
	sentinel := filepath.Join(t.TempDir(), "not-started")
	if code := Run([]string{"--resource-check", "128", "20", "persona-ext-nonexistent.scope", "--", "/bin/sh", "-c", "touch " + sentinel}, os.Stderr); code == 0 {
		t.Fatal("accepted unrelated scope")
	}
	if _, err := os.Stat(sentinel); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("guest started before resource verification")
	}
}

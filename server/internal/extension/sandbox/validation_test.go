package sandbox

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestValidatorResourceDirectoriesAreExactOwnScopes(t *testing.T) {
	unit := "persona-ext-validator-test.scope"
	groups := "0::/user.slice/" + unit
	mounts := "31 20 0:28 / /sys/fs/cgroup rw - cgroup2 cgroup rw"
	got, err := validationResourceDirs(groups, mounts, unit)
	want := []Mount{{Source: "/sys/fs/cgroup/user.slice/" + unit, Target: "/sys/fs/cgroup/user.slice/" + unit}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal(got, err)
	}
	groups = "8:memory:/user.slice/" + unit + "\n7:cpu,cpuacct:/user.slice/" + unit
	mounts = "31 20 0:28 / /sys/fs/cgroup/memory rw - cgroup cgroup rw,memory\n32 20 0:29 / /sys/fs/cgroup/cpu rw - cgroup cgroup rw,cpu,cpuacct"
	got, err = validationResourceDirs(groups, mounts, unit)
	if err != nil || len(got) != 2 {
		t.Fatal(got, err)
	}
	for _, tc := range []struct{ groups, mounts string }{
		{"0::/user.slice/shared", mounts},
		{groups, ""},
		{"8:memory:/user.slice/" + unit + "\n7:cpu,cpuacct:/user.slice/shared", mounts},
		{"0::/user.slice/" + unit, "31 20 0:28 /outside /sys/fs/cgroup rw - cgroup2 cgroup rw"},
	} {
		if _, err := validationResourceDirs(tc.groups, tc.mounts, unit); err == nil {
			t.Fatal("accepted unavailable/shared validator controller")
		}
	}
}

func TestValidatorNamespaceHasNoBrokerOrAmbientMounts(t *testing.T) {
	self, _ := os.Executable()
	args, err := ValidationNamespace(self, "validator-test", nil, nil)
	if err != nil {
		t.Skip("ELF namespace test requires Linux executable")
	}
	joined := strings.Join(args, " ")
	for _, token := range []string{"--unshare-user", "--unshare-net", "--unshare-pid", "--clearenv", "--cap-drop ALL", "--remount-ro /"} {
		if !strings.Contains(joined, token) {
			t.Fatal("missing validator isolation", token)
		}
	}
	for _, token := range []string{"proxy.sock", "HTTP_PROXY", "--bind ", "--ro-bind /etc", "--ro-bind /usr /usr", "--ro-bind /home", "--share-net"} {
		if strings.Contains(joined, token) {
			t.Fatal("ambient validator authority", token)
		}
	}
}

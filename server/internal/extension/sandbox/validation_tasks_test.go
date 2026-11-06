package sandbox

import (
	"os"
	"strings"
	"testing"
)

func TestValidatorTaskControllerFailsClosed(t *testing.T) {
	unit := "persona-ext-validator-test.scope"
	for _, tc := range []struct{ groups, mounts, path string }{
		{"0::/user/" + unit, "1 0 0:1 / /cg rw - cgroup2 cgroup rw", "/cg/user/" + unit + "/pids.max"},
		{"7:pids:/user/" + unit, "1 0 0:1 / /cg rw - cgroup cgroup rw,pids", "/cg/user/" + unit + "/pids.max"},
	} {
		for _, limit := range []string{"32", "16", "max", "33", "0", "-1", "missing"} {
			read := func(path string) ([]byte, error) {
				if path != tc.path || limit == "missing" {
					return nil, os.ErrNotExist
				}
				return []byte(limit), nil
			}
			err := verifyValidationTasks(tc.groups, tc.mounts, unit, read)
			if (err == nil) != (limit == "32" || limit == "16") {
				t.Fatal(limit, err)
			}
			if err := verifyValidationTasks(strings.ReplaceAll(tc.groups, unit, "shared.scope"), tc.mounts, unit, read); err == nil {
				t.Fatal("accepted shared task controller")
			}
		}
	}
	// A valid v1 memory/CPU scope must not hide a shared task controller.
	groups := "8:memory:/user/" + unit + "\n7:cpu:/user/" + unit + "\n6:pids:/user/shared.scope"
	mounts := "1 0 0:1 / /cg rw - cgroup cgroup rw,pids"
	if err := verifyValidationTasks(groups, mounts, unit, func(string) ([]byte, error) { return []byte("32"), nil }); err == nil {
		t.Fatal("accepted partial delegation")
	}
	if _, err := validationResourceDirs(groups, mounts, unit); err == nil {
		t.Fatal("mounted shared task controller")
	}
}

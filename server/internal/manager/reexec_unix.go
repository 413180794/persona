//go:build unix

package manager

import (
	"os"
	"syscall"
)

// reexec starts this program again in this process, with env: a manager that waited for a new token comes back as
// a fresh one (nothing of the run that ended stays behind), its service none the wiser.
func reexec(env []string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return syscall.Exec(exe, os.Args, env)
}

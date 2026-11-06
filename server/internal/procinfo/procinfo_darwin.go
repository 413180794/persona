//go:build darwin

package procinfo

import "golang.org/x/sys/unix"

// Parent is pid's parent process (kern.proc.pid).
func Parent(pid int) (int, error) {
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return 0, err
	}
	return int(kp.Eproc.Ppid), nil
}

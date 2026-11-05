//go:build linux

package node

import (
	"os"
	"strconv"
	"strings"
	"syscall"
)

// readProcArgv walks a procfs root and returns pid -> argv for the processes owned by uid. Other users' processes
// are skipped before their cmdline is read (one lstat each); this matches `ps -U <uid>` on macOS, and their environ
// could not be read anyway. Processes with an empty cmdline (zombies, kernel threads) are left out.
func readProcArgv(root string, uid uint32) map[int][]string {
	out := map[int][]string{}
	entries, err := os.ReadDir(root)
	if err != nil {
		return out
	}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue // exited since the directory was read
		}
		if st, ok := info.Sys().(*syscall.Stat_t); !ok || st.Uid != uid {
			continue
		}
		b, err := os.ReadFile(root + "/" + e.Name() + "/cmdline")
		if err != nil || len(b) == 0 {
			continue
		}
		out[pid] = strings.Split(strings.TrimRight(string(b), "\x00"), "\x00")
	}
	return out
}

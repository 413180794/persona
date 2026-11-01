package node

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Only file names of the selected app-server processes are retained, never contents or their other descriptors.
func openCodexRollouts(processes []codexProcess) map[int][]string {
	out := map[int][]string{}
	var pids []string
	for _, p := range processes {
		if p.pid > 0 {
			pids = append(pids, strconv.Itoa(p.pid))
		}
	}
	if len(pids) == 0 {
		return out
	}
	if runtime.GOOS == "linux" {
		for _, pid := range pids {
			entries, _ := os.ReadDir("/proc/" + pid + "/fd")
			for _, e := range entries {
				path, err := os.Readlink("/proc/" + pid + "/fd/" + e.Name())
				if err == nil && rolloutIDRe.MatchString(path) {
					id, _ := strconv.Atoi(pid)
					out[id] = append(out[id], path)
				}
			}
		}
		return out
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	b, err := exec.CommandContext(ctx, "lsof", "-a", "-p", strings.Join(pids, ","), "-Fn").Output()
	if err != nil && len(b) == 0 {
		return out
	}
	pid := 0
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "p") {
			pid, _ = strconv.Atoi(line[1:])
		}
		if pid > 0 && strings.HasPrefix(line, "n") {
			path := line[1:]
			if filepath.IsAbs(path) && rolloutIDRe.MatchString(path) {
				out[pid] = append(out[pid], path)
			}
		}
	}
	return out
}

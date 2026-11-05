package node

import (
	"os"
	"sync"
	"time"
)

// procSnapshotTTL is how long one walk of the process table serves later callers. The Claude scan
// (refreshClaudeDirs) and the Codex scan (discoverCodex) each run every 30 s; runCodex starts its scan on the first
// tick after a newer snapshot appears, so in steady state both use one walk per 30 s.
const procSnapshotTTL = 10 * time.Second

// procSnapshot is the last walk of this user's processes (Linux only; see userProcesses).
var procSnapshot struct {
	sync.Mutex
	at   time.Time
	argv map[int][]string
}

// userProcesses maps pid -> argv for every process of the node's own user. The map is shared between callers:
// read it, do not modify it. Only Linux walks /proc for this; elsewhere it is empty (macOS and Windows ask ps or the
// process table per scan).
func userProcesses() map[int][]string {
	procSnapshot.Lock()
	defer procSnapshot.Unlock()
	if procSnapshot.argv != nil && time.Since(procSnapshot.at) < procSnapshotTTL {
		return procSnapshot.argv
	}
	procSnapshot.argv = readProcArgv("/proc", uint32(os.Getuid()))
	procSnapshot.at = time.Now()
	return procSnapshot.argv
}

// procSnapshotAfter reports whether a still-fresh snapshot was taken after t (always false off Linux).
func procSnapshotAfter(t time.Time) bool {
	procSnapshot.Lock()
	defer procSnapshot.Unlock()
	return procSnapshot.argv != nil && procSnapshot.at.After(t) && time.Since(procSnapshot.at) < procSnapshotTTL
}

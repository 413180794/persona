package node

import "github.com/nwverse/persona/server/internal/procinfo"

// parentPID is pid's parent process.
func parentPID(pid int) (int, error) { return procinfo.Parent(pid) }

// procStartMark is when pid started ("" where the system does not tell, or the process is gone): procinfo.StartMark.
func procStartMark(pid int) string { return procinfo.StartMark(pid) }

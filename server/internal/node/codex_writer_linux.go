package node

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// Read the kernel's lock table by device and inode. The file can survive an owner, and probing with flock
// could briefly take ownership ourselves. If visibility is incomplete, thread/resume remains the authority.
func codexWriterHeld(home, id string) bool {
	if home == "" || !rolloutUUID(id) {
		return false
	}
	var st unix.Stat_t
	if err := unix.Stat(filepath.Join(home, "thread-writer-locks", id+".lock"), &st); err != nil || st.Mode&unix.S_IFMT != unix.S_IFREG {
		return false
	}
	raw, err := os.ReadFile("/proc/locks")
	if err != nil {
		return false
	}
	for line := range strings.SplitSeq(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 6 || fields[1] != "FLOCK" || fields[3] != "WRITE" {
			continue // A waiting lock ('->') is not the holder.
		}
		parts := strings.Split(fields[5], ":")
		if len(parts) != 3 {
			continue
		}
		major, majorErr := strconv.ParseUint(parts[0], 16, 32)
		minor, minorErr := strconv.ParseUint(parts[1], 16, 32)
		inode, inodeErr := strconv.ParseUint(parts[2], 10, 64)
		if majorErr == nil && minorErr == nil && inodeErr == nil &&
			major == uint64(unix.Major(st.Dev)) && minor == uint64(unix.Minor(st.Dev)) && inode == st.Ino {
			return true
		}
	}
	return false
}

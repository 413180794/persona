package node

import (
	"encoding/binary"
	"net"
	"time"
)

// Windows' process and TCP tables as scanListeners reads them (ports_windows.go fetches them from the system). The
// decoding has no system calls in it, so it is tested on every platform.

// winEntry is one process of a Toolhelp snapshot, with its creation time (zero when this user may not open it).
type winEntry struct {
	pid, ppid int
	name      string
	start     time.Time
}

// winProcTable is scanListeners' process table from a snapshot. Windows keeps a process's parent pid after the parent
// has exited, and pids are reused: a parent link is kept only when the parent is known to have started no later than
// the child, so a process that got an exited parent's pid does not adopt its children. Windows has no controlling
// terminal: tty stays empty and what a pane owns follows the process tree alone.
func winProcTable(entries []winEntry) map[int]proc {
	start := make(map[int]time.Time, len(entries))
	for _, e := range entries {
		start[e.pid] = e.start
	}
	out := make(map[int]proc, len(entries))
	for _, e := range entries {
		ppid := e.ppid
		if ps, ok := start[ppid]; ppid == e.pid || !ok || ps.IsZero() || e.start.IsZero() || ps.After(e.start) {
			ppid = 0
		}
		out[e.pid] = proc{ppid: ppid, cmd: e.name}
	}
	return out
}

// The rows of GetExtendedTcpTable's TCP_TABLE_OWNER_PID_* tables: MIB_TCPROW_OWNER_PID (six DWORDs) and
// MIB_TCP6ROW_OWNER_PID (two 16-byte addresses, six DWORDs). Both tables are a DWORD count followed by the rows.
const (
	winTCPRow4Size = 24
	winTCPRow6Size = 56
	winTCPListen   = 2 // MIB_TCP_STATE_LISTEN
)

// parseWinTCP4 reads a MIB_TCPTABLE_OWNER_PID: each row is state, local address, local port, remote address, remote
// port, owning pid. The address is in network order as it lies in memory, and so are the port's low two bytes.
func parseWinTCP4(b []byte) []listener {
	return parseWinTCP(b, winTCPRow4Size, func(r []byte) (state uint32, l listener) {
		l = listener{pid: int(binary.LittleEndian.Uint32(r[20:])), port: int(r[8])<<8 | int(r[9]), addr: net.IP(r[4:8]).String()}
		return binary.LittleEndian.Uint32(r[0:]), l
	})
}

// parseWinTCP6 reads a MIB_TCP6TABLE_OWNER_PID: each row is local address (16 bytes), local scope id, local port,
// remote address, remote scope id, remote port, state, owning pid. The scope id is dropped: a link-local listener is
// dialed on loopback like any other (dialTargets).
func parseWinTCP6(b []byte) []listener {
	return parseWinTCP(b, winTCPRow6Size, func(r []byte) (state uint32, l listener) {
		l = listener{pid: int(binary.LittleEndian.Uint32(r[52:])), port: int(r[20])<<8 | int(r[21]), addr: net.IP(r[0:16]).String()}
		return binary.LittleEndian.Uint32(r[48:]), l
	})
}

// parseWinTCP reads a table's rows, keeping the listening ones; a count larger than the bytes given stops at the
// last whole row.
func parseWinTCP(b []byte, size int, row func([]byte) (uint32, listener)) []listener {
	if len(b) < 4 {
		return nil
	}
	n := int(binary.LittleEndian.Uint32(b))
	var ls []listener
	for i := 0; i < n; i++ {
		off := 4 + i*size
		if off+size > len(b) {
			break
		}
		state, l := row(b[off : off+size])
		if state != winTCPListen || l.port <= 0 {
			continue
		}
		ls = append(ls, l)
	}
	return ls
}

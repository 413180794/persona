package node

import (
	"encoding/binary"
	"errors"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// winProc is one process as Windows reports it: what /proc gives on Linux. Read from the process's PEB (its
// RTL_USER_PROCESS_PARAMETERS), which a process of the same user may read.
type winProc struct {
	pid, ppid int
	name      string // image name, lower case, without ".exe"
	argv      []string
	cwd       string
	start     time.Time
	// env holds only the variables asked for (upper-case names): the rest of it (tokens among them) is never kept.
	env map[string]string
}

// maxProcEnv caps the environment block read from one process.
const maxProcEnv = 1 << 20

// listWinProcs lists the processes whose image name (lower case, without ".exe") want accepts, with their command
// line, working directory, start time and the environment variables named in keys, and the parent of every process
// (by pid). Processes this user may not read (another user's, protected ones) are left out of procs.
func listWinProcs(want func(name string) bool, keys ...string) (procs []winProc, parents map[int]int, err error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, nil, err
	}
	defer windows.CloseHandle(snap)
	parents = map[int]int{}
	e := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		parents[int(e.ProcessID)] = int(e.ParentProcessID)
		name := imageName(windows.UTF16ToString(e.ExeFile[:]))
		if !want(name) {
			continue
		}
		p, err := readWinProc(e.ProcessID, keys)
		if err != nil {
			continue
		}
		p.pid, p.ppid, p.name = int(e.ProcessID), int(e.ParentProcessID), name
		procs = append(procs, p)
	}
	if !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return nil, nil, err
	}
	return procs, parents, nil
}

// progName is a command line's program as the scans compare it (isClaudeCmd, agentOfCmd): base name, lower case,
// without ".exe".
func progName(argv0 string) string { return imageName(argv0) }

// imageName is an executable's name as the scans compare it: base name, lower case, without ".exe".
func imageName(path string) string {
	return strings.TrimSuffix(strings.ToLower(filepath.Base(path)), ".exe")
}

// readWinProc reads one process's command line, working directory, start time and the variables in keys.
func readWinProc(pid uint32, keys []string) (winProc, error) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_VM_READ, false, pid)
	if err != nil {
		return winProc{}, err
	}
	defer windows.CloseHandle(h)
	var p winProc
	var created, exited, kernel, user windows.Filetime
	if windows.GetProcessTimes(h, &created, &exited, &kernel, &user) == nil {
		p.start = time.Unix(0, created.Nanoseconds())
	}
	var pbi windows.PROCESS_BASIC_INFORMATION
	if err := windows.NtQueryInformationProcess(h, windows.ProcessBasicInformation, unsafe.Pointer(&pbi),
		uint32(unsafe.Sizeof(pbi)), nil); err != nil {
		return winProc{}, err
	}
	// The PEB and the parameters hold addresses in the other process: they are read as bytes and the fields taken at
	// x/sys's offsets, never as Go structs (a Go pointer to another process's memory would confuse the collector).
	var peb windows.PEB
	pebBuf, err := readMem(h, uintptr(unsafe.Pointer(pbi.PebBaseAddress)), unsafe.Sizeof(peb))
	if err != nil {
		return winProc{}, err
	}
	paramsAt := word(pebBuf, unsafe.Offsetof(peb.ProcessParameters))
	var rp windows.RTL_USER_PROCESS_PARAMETERS
	params, err := readMem(h, paramsAt, unsafe.Sizeof(rp))
	if err != nil {
		return winProc{}, err
	}
	cmdline, err := readUnicodeString(h, params, unsafe.Offsetof(rp.CommandLine))
	if err != nil {
		return winProc{}, err
	}
	if p.argv, err = windows.DecomposeCommandLine(cmdline); err != nil {
		p.argv = strings.Fields(cmdline)
	}
	if cwd, err := readUnicodeString(h, params, unsafe.Offsetof(rp.CurrentDirectory)+unsafe.Offsetof(rp.CurrentDirectory.DosPath)); err == nil {
		p.cwd = trimDirSep(cwd)
	}
	if len(keys) > 0 {
		size := word(params, unsafe.Offsetof(rp.EnvironmentSize))
		if size > maxProcEnv {
			size = maxProcEnv
		}
		if block, err := readMem(h, word(params, unsafe.Offsetof(rp.Environment)), size); err == nil {
			p.env = pickEnv(utf16Bytes(block), keys)
		}
	}
	return p, nil
}

// readMem reads n bytes at addr in the process h.
func readMem(h windows.Handle, addr, n uintptr) ([]byte, error) {
	if addr == 0 || n == 0 {
		return nil, errors.New("nothing to read")
	}
	buf := make([]byte, n)
	var got uintptr
	if err := windows.ReadProcessMemory(h, addr, &buf[0], n, &got); err != nil && got == 0 {
		return nil, err
	}
	return buf[:got], nil
}

// word is the pointer-sized value at off in b (0 when b is short).
func word(b []byte, off uintptr) uintptr {
	size := unsafe.Sizeof(uintptr(0))
	if uintptr(len(b)) < off+size {
		return 0
	}
	if size == 8 {
		return uintptr(binary.LittleEndian.Uint64(b[off:]))
	}
	return uintptr(binary.LittleEndian.Uint32(b[off:]))
}

// readUnicodeString reads the UNICODE_STRING at off in b (bytes of a struct read from h): its text, from h.
func readUnicodeString(h windows.Handle, b []byte, off uintptr) (string, error) {
	var us windows.NTUnicodeString
	if uintptr(len(b)) < off+unsafe.Sizeof(us) {
		return "", errors.New("short read")
	}
	n := uintptr(binary.LittleEndian.Uint16(b[off:]))
	if n == 0 {
		return "", nil
	}
	text, err := readMem(h, word(b, off+unsafe.Offsetof(us.Buffer)), n)
	if err != nil {
		return "", err
	}
	return utf16Bytes(text), nil
}

// utf16Bytes decodes little-endian UTF-16 bytes.
func utf16Bytes(b []byte) string {
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = binary.LittleEndian.Uint16(b[2*i:])
	}
	return string(utf16.Decode(u))
}

// pickEnv takes the variables named in keys (compared without case, as Windows does) out of an environment block
// (NAME=value strings separated by NULs).
func pickEnv(block string, keys []string) map[string]string {
	out := map[string]string{}
	for kv := range strings.SplitSeq(block, "\x00") {
		name, value, ok := strings.Cut(kv, "=")
		if !ok || name == "" { // "=C:=C:\…": the per-drive directories start with "="
			continue
		}
		for _, k := range keys {
			if strings.EqualFold(name, k) {
				out[k] = value
			}
		}
	}
	return out
}

// trimDirSep drops the separator Windows keeps at the end of a working directory ("C:\work\" → "C:\work"), not a
// root's ("C:\").
func trimDirSep(dir string) string {
	if len(dir) > 3 && strings.HasSuffix(dir, `\`) {
		return dir[:len(dir)-1]
	}
	return dir
}

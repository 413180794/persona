package node

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// Files sent from the phone to an agent (docs/protocol.md「发送文件」): the hub streams the upload in chunks
// (file.upload.begin / chunk / end; one JSON line per chunk, never the whole file), the node writes it to a hidden
// part file in the temp directory and, once every byte is there, renames it to the original name plus a timestamp.
// The app puts the returned absolute path in the input box.

// uploadIdle is how long an upload may go without a chunk before its part file is removed (the hub died mid-way).
const uploadIdle = 2 * time.Minute

type uploads struct {
	mu   sync.Mutex
	open map[string]*upload
}

type upload struct {
	name    string // as the phone sent it
	size    int64
	written int64
	f       *os.File
	idle    *time.Timer
	random  bool // the final name starts with a random prefix instead of ending in a timestamp (terminal pastes)
}

// begin opens an upload into the temp directory (the chat's 「发送文件」).
func (u *uploads) begin(name string, size int64) (string, error) {
	return u.beginIn(os.TempDir(), name, size, proto.MaxUploadBytes, false)
}

// beginIn opens an upload whose part file and final file live in dir (one directory, so the rename never crosses
// file systems). random: the final name is <random>-<clean name>, which nobody can guess from the file name.
func (u *uploads) beginIn(dir, name string, size, limit int64, random bool) (string, error) {
	if size < 0 {
		return "", fmt.Errorf("size is required")
	}
	if size > limit {
		return "", &rpc.Error{Code: proto.CodeTooLarge, Message: fmt.Sprintf("file is larger than %d MB", limit>>20)}
	}
	f, err := os.CreateTemp(dir, ".jarvis-upload-*") // 0600
	if err != nil {
		return "", err
	}
	id := randHex(8)
	up := &upload{name: name, size: size, f: f, random: random}
	up.idle = time.AfterFunc(uploadIdle, func() { u.abort(id) })
	u.mu.Lock()
	if u.open == nil {
		u.open = map[string]*upload{}
	}
	u.open[id] = up
	u.mu.Unlock()
	return id, nil
}

// take removes an upload from the open set while a call works on it: a chunk racing an abort finds nothing.
func (u *uploads) take(id string) (*upload, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	up := u.open[id]
	if up == nil {
		return nil, fmt.Errorf("no such upload (it timed out or was cancelled)")
	}
	delete(u.open, id)
	up.idle.Stop()
	return up, nil
}

func (u *uploads) put(id string, up *upload) {
	u.mu.Lock()
	u.open[id] = up
	u.mu.Unlock()
	up.idle.Reset(uploadIdle)
}

func (u *uploads) chunk(id string, offset int64, data []byte) (int64, error) {
	up, err := u.take(id)
	if err != nil {
		return 0, err
	}
	fail := func(err error) (int64, error) { up.discard(); return 0, err }
	if offset != up.written {
		return fail(fmt.Errorf("chunk at %d, expected %d", offset, up.written))
	}
	if up.written+int64(len(data)) > up.size {
		return fail(fmt.Errorf("more data than the %d bytes announced", up.size))
	}
	if _, err := up.f.Write(data); err != nil {
		return fail(err)
	}
	up.written += int64(len(data))
	u.put(id, up)
	return up.written, nil
}

// end checks the size and gives the file its name; it returns the absolute path.
func (u *uploads) end(id string, now time.Time) (string, int64, error) {
	up, err := u.take(id)
	if err != nil {
		return "", 0, err
	}
	if err := up.f.Close(); err != nil {
		up.discard()
		return "", 0, err
	}
	part := up.f.Name()
	if st, err := os.Stat(part); err != nil || st.Size() != up.size || up.written != up.size {
		up.discard()
		return "", 0, fmt.Errorf("got %d of %d bytes", up.written, up.size)
	}
	reserve := reserveName
	if up.random {
		reserve = reserveRandomName
	}
	path, err := reserve(filepath.Dir(part), up.name, now)
	if err != nil {
		up.discard()
		return "", 0, err
	}
	if err := os.Rename(part, path); err != nil { // replaces the empty reservation
		_ = os.Remove(path)
		up.discard()
		return "", 0, err
	}
	return path, up.size, nil
}

func (u *uploads) abort(id string) {
	if up, err := u.take(id); err == nil {
		up.discard()
	}
}

func (up *upload) discard() {
	_ = up.f.Close()
	_ = os.Remove(up.f.Name())
}

// reserveName creates an empty 0600 file for name-<time>.ext in dir and returns its path; on a clash within the
// same second it tries again with a short random suffix.
func reserveName(dir, name string, now time.Time) (string, error) {
	stem, ext := splitName(cleanName(name))
	base := stem + "-" + now.Format("20060102-150405")
	for i := 0; i < 8; i++ {
		n := base + ext
		if i > 0 {
			n = base + "-" + randHex(2) + ext
		}
		p := filepath.Join(dir, n)
		f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			_ = f.Close()
			return p, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return "", err
		}
	}
	return "", fmt.Errorf("no free name for %s", name)
}

// reserveRandomName creates an empty 0600 file named <16 hex>-<clean name> in dir and returns its path. The name is
// cut to what a file system takes, and never has a separator in it (cleanName), so it cannot leave dir.
func reserveRandomName(dir, name string, _ time.Time) (string, error) {
	stem, ext := splitName(cleanName(name))
	for i := 0; i < 8; i++ {
		p := filepath.Join(dir, randHex(8)+"-"+stem+ext)
		f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			_ = f.Close()
			return p, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return "", err
		}
	}
	return "", fmt.Errorf("no free name for %s", name)
}

// cleanName keeps the phone's file name but nothing that would leave the directory or break a terminal.
func cleanName(name string) string {
	name = strings.ToValidUTF8(name, "_")
	name = strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r < 0x20 || r == 0x7f {
			return '_'
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if name == "" || strings.Trim(name, ".") == "" {
		return "file"
	}
	return name
}

var plainExt = regexp.MustCompile(`^\.[\p{L}\p{N}]{1,12}$`)

// compound extensions stay whole: report.tar.gz -> report-<time>.tar.gz.
var compoundExts = []string{".tar.gz", ".tar.bz2", ".tar.xz", ".tar.zst"}

// splitName splits off the extension and shortens the stem so the full name stays well under 255 bytes.
func splitName(name string) (stem, ext string) {
	lower := strings.ToLower(name)
	for _, c := range compoundExts {
		if strings.HasSuffix(lower, c) && len(name) > len(c) {
			stem, ext = name[:len(name)-len(c)], name[len(name)-len(c):]
		}
	}
	if ext == "" {
		ext = filepath.Ext(name)
		stem = strings.TrimSuffix(name, ext)
		if stem == "" || !plainExt.MatchString(ext) { // ".env", "notes. final", "a.._b"
			stem, ext = name, ""
		}
	}
	const maxStem = 180
	for len(stem) > maxStem {
		_, size := utf8.DecodeLastRuneInString(stem)
		stem = stem[:len(stem)-size]
	}
	return stem, ext
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// uploadCall serves the file.upload.* methods.
func (n *Node) uploadCall(method string, params json.RawMessage) (any, error) {
	var p struct {
		UploadID string `json:"upload_id"`
		Name     string `json:"name"`
		Dest     string `json:"dest"` // "terminal": a paste into a terminal (uploadDir); "" the temp directory
		Size     int64  `json:"size"`
		Offset   int64  `json:"offset"`
		Data     []byte `json:"data"` // base64 in JSON
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("bad params: %w", err)
	}
	switch method {
	case "file.upload.begin":
		var id string
		var err error
		switch p.Dest {
		case "":
			id, err = n.ups.begin(p.Name, p.Size)
		case proto.UploadDestTerminal:
			dir := terminalUploadDir(n.dir, time.Now())
			if err = os.MkdirAll(dir, 0o700); err == nil {
				id, err = n.ups.beginIn(dir, p.Name, p.Size, proto.MaxTerminalUploadBytes, true)
			}
		default:
			err = fmt.Errorf("unknown upload destination %q", p.Dest)
		}
		if err != nil {
			return nil, err
		}
		return map[string]any{"upload_id": id, "chunk_bytes": proto.UploadChunkBytes}, nil
	case "file.upload.chunk":
		got, err := n.ups.chunk(p.UploadID, p.Offset, p.Data)
		if err != nil {
			return nil, err
		}
		return map[string]any{"received": got}, nil
	case "file.upload.end":
		path, size, err := n.ups.end(p.UploadID, time.Now())
		if err != nil {
			return nil, err
		}
		n.logger.Printf("file from the phone: %s (%d bytes)", path, size)
		return proto.UploadedFile{Path: path, Name: filepath.Base(path), Size: size}, nil
	case "file.upload.abort":
		n.ups.abort(p.UploadID)
		return map[string]any{"ok": true}, nil
	}
	return nil, fmt.Errorf("unknown method %s", method)
}

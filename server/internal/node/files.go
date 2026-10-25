package node

import (
	"encoding/base64"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// maxFileBytes caps what the phone can pull in one request.
const maxFileBytes = 2 << 20

// FileContent is a file read for the phone's file viewer (paths mentioned in a conversation).
type FileContent struct {
	Path      string   `json:"path"`
	Name      string   `json:"name"`
	Size      int64    `json:"size"`
	Mime      string   `json:"mime"`
	Text      string   `json:"text,omitempty"` // UTF-8 text files
	Data      string   `json:"data,omitempty"` // base64 for binary files (images, PDFs)
	Truncated bool     `json:"truncated"`
	IsDir     bool     `json:"is_dir"`
	Entries   []string `json:"entries,omitempty"` // directory listing (names, dirs end with /)
}

func (n *Node) readFile(path, cwd string) (*FileContent, error) {
	path = strings.TrimSpace(path)
	switch {
	case path == "":
		return nil, fmt.Errorf("empty path")
	case path == "~" || strings.HasPrefix(path, "~/"):
		path = filepath.Join(n.home, strings.TrimPrefix(path, "~"))
	case !filepath.IsAbs(path) && cwd != "":
		path = filepath.Join(cwd, path)
	}
	path = filepath.Clean(path)
	// iCloud Drive hangs on a locked Mac; never touch it.
	if strings.Contains(path, "/Library/Mobile Documents") {
		return nil, fmt.Errorf("iCloud Drive files are not readable remotely")
	}
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	fc := &FileContent{Path: path, Name: filepath.Base(path), Size: st.Size()}
	if st.IsDir() {
		fc.IsDir = true
		ents, err := os.ReadDir(path)
		if err != nil {
			return nil, err
		}
		for i, e := range ents {
			if i >= 500 {
				fc.Truncated = true
				break
			}
			name := e.Name()
			if e.IsDir() {
				name += "/"
			}
			fc.Entries = append(fc.Entries, name)
		}
		return fc, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, min(st.Size(), maxFileBytes))
	nr, _ := f.Read(buf)
	buf = buf[:nr]
	fc.Truncated = st.Size() > int64(nr)
	fc.Mime = mime.TypeByExtension(strings.ToLower(filepath.Ext(path)))
	if fc.Mime == "" {
		fc.Mime = http.DetectContentType(buf)
	}
	if utf8.Valid(buf) || (fc.Truncated && utf8.Valid(trimPartialRune(buf))) {
		fc.Text = string(trimPartialRune(buf))
		if !strings.HasPrefix(fc.Mime, "text/") && !strings.Contains(fc.Mime, "json") {
			fc.Mime = "text/plain; charset=utf-8"
		}
		return fc, nil
	}
	fc.Data = base64.StdEncoding.EncodeToString(buf)
	return fc, nil
}

// trimPartialRune drops a UTF-8 sequence cut in half at the end of a truncated read.
func trimPartialRune(b []byte) []byte {
	for i := 0; i < 4 && len(b) > 0; i++ {
		if utf8.Valid(b) {
			return b
		}
		b = b[:len(b)-1]
	}
	return b
}

// DirEntry is one row of the phone's file tree.
type DirEntry struct {
	Name    string    `json:"name"`
	Dir     bool      `json:"dir"`
	Symlink bool      `json:"symlink,omitempty"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mtime"`
}

const maxDirEntries = 2000

// listDir lists a directory for the file tree: directories first, then files, each by name (case-insensitive).
func (n *Node) listDir(path, cwd string) (map[string]any, error) {
	path = strings.TrimSpace(path)
	switch {
	case path == "":
		path = cwd
	case path == "~" || strings.HasPrefix(path, "~/"):
		path = filepath.Join(n.home, strings.TrimPrefix(path, "~"))
	case !filepath.IsAbs(path) && cwd != "":
		path = filepath.Join(cwd, path)
	}
	if path == "" {
		return nil, fmt.Errorf("empty path")
	}
	path = filepath.Clean(path)
	if strings.Contains(path, "/Library/Mobile Documents") {
		return nil, fmt.Errorf("iCloud Drive files are not readable remotely")
	}
	ents, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	list := make([]DirEntry, 0, min(len(ents), maxDirEntries))
	for _, e := range ents {
		if len(list) >= maxDirEntries {
			break
		}
		d := DirEntry{Name: e.Name(), Dir: e.IsDir(), Symlink: e.Type()&os.ModeSymlink != 0}
		if info, err := e.Info(); err == nil {
			d.Size, d.ModTime = info.Size(), info.ModTime()
		}
		if d.Symlink { // follow links so linked directories open like directories
			if st, err := os.Stat(filepath.Join(path, d.Name)); err == nil {
				d.Dir = st.IsDir()
			}
		}
		list = append(list, d)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Dir != list[j].Dir {
			return list[i].Dir
		}
		return strings.ToLower(list[i].Name) < strings.ToLower(list[j].Name)
	})
	return map[string]any{"path": path, "entries": list, "truncated": len(ents) > maxDirEntries}, nil
}

package managerconf

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// The built-in manager's own files (docs/assistant/managers.md "The built-in manager's own files"). It used to keep its keys in the data
// directory beside the hub's; they are its own now, in DefaultDir, copied there once from the data directory
// (MoveFiles). Copied, not moved: the same bytes open what is already sealed with them, and an older build switched back
// still finds them where it looks. The hub reads none of them (its own secret.key stays its own).
const (
	// SecretKey seals the manager's secrets in its database (model providers' keys, extensions', connectors' and
	// bots' secrets, mail passwords; internal/secretkey).
	SecretKey = "secret.key"
	// TailcatKey is the identity the computers reached over tailcat trust (internal/tunnel): the manager dials them.
	TailcatKey = "tailcat-hub.json"
	// HandlesKey keys the knowledge base's identity hints of senders (internal/kb handles).
	HandlesKey = "handles.key"
)

// OwnFiles are the files MoveFiles copies.
var OwnFiles = []string{SecretKey, TailcatKey, HandlesKey}

// FilesMarkerKey is manager.json's record of MoveFiles: which files it copied from the data directory, and when.
const FilesMarkerKey = "moved_from_data_dir"

// FilesMarker is FilesMarkerKey's value.
type FilesMarker struct {
	Files []string  `json:"files"`
	At    time.Time `json:"at"`
}

// KBDirKey is manager.json's knowledge base directory for the built-in manager; absent, it is the data directory's
// data/ (~/.persona/data), where it has always been: the docs, the prompts and the agents that search it go by that.
const KBDirKey = "kb_dir"

// MoveFiles copies each of OwnFiles that the data directory home has and the built-in manager's directory does not
// into that directory (0600, the directory 0700, made here so the manager can make the keys there that it does not
// copy), and records what it copied in manager.json (FilesMarkerKey). A file already in the manager's directory is
// never replaced, also not one made there meanwhile: from the first copy on it is the manager's (a later key rotation
// there is not undone). The data directory's files are left as they are. A file that fails does not stop the others;
// the errors are joined.
func MoveFiles(home string) (copied []string, err error) {
	dir := DefaultDir(home)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	removeStaleCopies(dir)
	var errs []error
	for _, name := range OwnFiles {
		src, dst := filepath.Join(home, name), filepath.Join(dir, name)
		if _, err := os.Lstat(dst); err == nil || !errors.Is(err, os.ErrNotExist) {
			continue // there (or not readable: left alone, OwnFile says which is read)
		}
		b, err := os.ReadFile(src)
		if errors.Is(err, os.ErrNotExist) {
			continue // nothing to copy: the manager makes its own there when it first needs it
		}
		if err == nil {
			var placed bool
			if placed, err = placeNew(dst, b); err == nil && placed {
				copied = append(copied, name)
			}
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
		}
	}
	if len(copied) > 0 {
		now := time.Now().UTC()
		if err := Edit(filepath.Join(dir, File), func(o *Object) error {
			var m FilesMarker
			if raw, ok := o.Get(FilesMarkerKey); ok {
				_ = json.Unmarshal(raw, &m) // a marker that does not read is written anew
			}
			for _, f := range copied {
				if !slices.Contains(m.Files, f) {
					m.Files = append(m.Files, f)
				}
			}
			m.At = now
			raw, err := MarshalNoEscape(m)
			if err != nil {
				return err
			}
			o.SetRaw(FilesMarkerKey, raw)
			return nil
		}); err != nil {
			errs = append(errs, fmt.Errorf("%s: recording the copy: %w", FilesMarkerKey, err))
		}
	}
	return copied, errors.Join(errs...)
}

// staleCopyAge is how old a temporary copy of a key (placeNew, tunnel.LoadOrCreateHubKey) must be to be taken for one
// left by a crash: younger ones may be another process's on their way.
const staleCopyAge = time.Minute

// removeStaleCopies deletes the temporary copies of OwnFiles a crash left in dir: each holds a whole key.
func removeStaleCopies(dir string) {
	for _, name := range OwnFiles {
		left, _ := filepath.Glob(filepath.Join(dir, name+".tmp-*"))
		for _, p := range left {
			if st, err := os.Lstat(p); err == nil && st.Mode().IsRegular() && time.Since(st.ModTime()) > staleCopyAge {
				_ = os.Remove(p)
			}
		}
	}
}

// placeNew writes b to path, 0600, only if path is not there: a temporary file in the same directory, fsync, then a
// hard link into place, which fails when another process put one there first (false then; that one stays).
func placeNew(path string, b []byte) (bool, error) {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return false, err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return false, err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return false, err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return false, err
	}
	if err := f.Close(); err != nil {
		return false, err
	}
	if err := os.Link(tmp, path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return false, nil
		}
		// A file system without hard links (some network or FUSE mounts): renamed into place, still only if nothing
		// is there (a key made between this look and the rename would be replaced: a window of one system call).
		if _, lerr := os.Lstat(path); lerr == nil || !errors.Is(lerr, os.ErrNotExist) {
			return false, nil
		}
		if err := os.Rename(tmp, path); err != nil {
			return false, err
		}
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		d.Close()
	}
	return true, nil
}

// OwnFile is where the built-in manager reads name, one of OwnFiles: its own copy; the data directory's while that is
// the only one (MoveFiles could not copy it: making a new one beside it would no longer open what the old one
// sealed); its own place when neither is there (it makes it there).
func OwnFile(home, name string) string {
	own := filepath.Join(DefaultDir(home), name)
	if _, err := os.Lstat(own); err == nil {
		return own
	}
	if old := filepath.Join(home, name); fileExists(old) {
		return old
	}
	return own
}

func fileExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// KBDir is the built-in manager's knowledge base directory: manager.json's kb_dir (a leading ~/ is the user's home),
// else home's data/.
func KBDir(home string) string {
	def := filepath.Join(home, "data")
	b, err := os.ReadFile(DefaultFile(home))
	if err != nil {
		return def
	}
	o, err := ParseObject(b)
	if err != nil {
		return def
	}
	raw, ok := o.Get(KBDirKey)
	if !ok {
		return def
	}
	var dir string
	if json.Unmarshal(raw, &dir) != nil || strings.TrimSpace(dir) == "" {
		return def
	}
	if rest, ok := strings.CutPrefix(dir, "~/"); ok {
		if h, err := os.UserHomeDir(); err == nil {
			dir = filepath.Join(h, rest)
		}
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(DefaultDir(home), dir)
	}
	return filepath.Clean(dir)
}

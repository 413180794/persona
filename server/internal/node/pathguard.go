package node

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// What a client may look at by a path a conversation names (fs.stat, file.read, fs.list, downloads): anything the
// user can read, except where looking hangs the machine (iCloud Drive on a locked Mac) or hands out a secret (ssh
// keys, the hub's token). A path is checked one component at a time as its links are followed, before each is
// looked at, so a link elsewhere that points in is refused without touching what it points to.

var (
	errICloud    = errors.New("iCloud Drive files are not readable remotely")
	errOffLimits = errors.New("this path holds secrets and is not readable remotely")
)

// maxLinks bounds the links one path may go through (a loop of links ends here).
const maxLinks = 40

// secretDirs are folders under the home that hold credentials, refused whole.
var secretDirs = []string{
	".ssh", ".gnupg", ".aws", ".azure", ".kube", ".docker", ".password-store",
	"Library/Keychains", ".local/share/keyrings",
	".config/gcloud", ".config/gh",
}

// secretFiles are files under the home that are credentials.
var secretFiles = []string{
	".netrc", ".git-credentials", ".npmrc", ".pypirc", ".pgpass",
	".claude/.credentials.json", ".codex/auth.json",
}

// keyNames are private keys by their usual names, wherever they are.
var keyNames = map[string]bool{"id_rsa": true, "id_dsa": true, "id_ecdsa": true, "id_ed25519": true, "id_ecdsa_sk": true, "id_ed25519_sk": true}

// hubKeyExt are the key files in the hub's folder.
var hubKeyExt = map[string]bool{".key": true, ".p8": true, ".pem": true, ".crt": true}

// fold compares paths as macOS's default file system does: case-insensitively.
func fold(p string) string { return strings.ToLower(filepath.ToSlash(p)) }

// inICloud: iCloud Drive hangs on a locked Mac; never touch it.
func inICloud(path string) bool { return strings.Contains(fold(path), "/library/mobile documents") }

// offLimits says why a path (absolute, clean, links already followed up to it) is not to be looked at, or nil.
func (n *Node) offLimits(path string) error {
	if inICloud(path) {
		return errICloud
	}
	p := fold(path)
	if keyNames[filepath.Base(p)] {
		return errOffLimits
	}
	// The hub's and the connector's own folders: hub.json (the token, the database password) and its backups, the
	// keys beside it (secret.key, handles.key, hub.key, the APNs .p8).
	for _, part := range strings.Split(p, "/") {
		if part == ".persona" || part == ".jarvis" {
			base := filepath.Base(p)
			if strings.HasPrefix(base, "hub.json") || strings.HasPrefix(base, "apns") || hubKeyExt[filepath.Ext(base)] {
				return errOffLimits
			}
			break
		}
	}
	if n.home == "" {
		return nil
	}
	rel, ok := homeRel(p, n.home)
	if !ok {
		return nil
	}
	for _, d := range secretDirs {
		if d = strings.ToLower(d); rel == d || strings.HasPrefix(rel, d+"/") {
			return errOffLimits
		}
	}
	for _, f := range secretFiles {
		if rel == strings.ToLower(f) {
			return errOffLimits
		}
	}
	return nil
}

// homeRel is the folded path p relative to home, home as written or with its links followed: realPath hands over
// the physical path, so a home behind a link (/var -> /private/var) is matched both ways.
func homeRel(p, home string) (string, bool) {
	home = filepath.Clean(home)
	if rel, ok := strings.CutPrefix(p, fold(home)+"/"); ok {
		return rel, true
	}
	if real, err := filepath.EvalSymlinks(home); err == nil && real != home {
		return strings.CutPrefix(p, fold(real)+"/")
	}
	return "", false
}

// realPath follows the links in an absolute, clean path one component at a time, checking each step with offLimits
// before looking at it. A component that is not there ends the walk: the rest is joined on as written, and the error
// is the one looking at it gave (fs.ErrNotExist, fs.ErrPermission).
func (n *Node) realPath(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("path is not absolute: %s", path)
	}
	if err := n.offLimits(path); err != nil {
		return "", err
	}
	vol := filepath.VolumeName(path)
	done := vol + string(filepath.Separator)
	pending := splitPath(path[len(vol):])
	links := 0
	for len(pending) > 0 {
		c := pending[0]
		pending = pending[1:]
		switch c {
		case "", ".":
			continue
		case "..":
			done = filepath.Dir(done)
			continue
		}
		next := filepath.Join(done, c)
		if err := n.offLimits(next); err != nil {
			return "", err
		}
		st, err := os.Lstat(next)
		if err != nil {
			return filepath.Join(append([]string{next}, pending...)...), err
		}
		if st.Mode()&fs.ModeSymlink == 0 {
			done = next
			continue
		}
		if links++; links > maxLinks {
			return "", fmt.Errorf("too many links: %s", path)
		}
		target, err := os.Readlink(next)
		if err != nil {
			return "", err
		}
		if filepath.IsAbs(target) {
			v := filepath.VolumeName(target)
			done, target = v+string(filepath.Separator), target[len(v):]
		}
		pending = append(splitPath(target), pending...)
	}
	return done, nil
}

func splitPath(p string) []string {
	return strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == filepath.Separator })
}

// absPath makes a path absolute against the node's own working directory (a relative path asked with no cwd).
func absPath(p string) string {
	if a, err := filepath.Abs(p); err == nil {
		return a
	}
	return p
}

// statSafe is os.Stat through realPath; path is how it was asked (resolved, before links).
func (n *Node) statSafe(path string) (string, os.FileInfo, error) {
	real, err := n.realPath(absPath(path))
	if err != nil {
		return "", nil, err
	}
	st, err := os.Stat(real)
	return real, st, err
}

package node

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/nwverse/persona/server/internal/identity"
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
	".claude/.credentials.json", ".codex/auth.json", ".claude.json", ".claude-custom-oauth.json",
}

// keyNames are private keys by their usual names, wherever they are.
var keyNames = map[string]bool{"id_rsa": true, "id_dsa": true, "id_ecdsa": true, "id_ed25519": true, "id_ecdsa_sk": true, "id_ed25519_sk": true}

// hubKeyExt matches key files in the hub's folder, including backup suffixes.
var hubKeyExt = regexp.MustCompile(`\.(key|p8|pem|crt)([.~-][^/]*)?$`)

// dataSecrets are the credential files in the data folder (and a manager's folder under it) by the start of their
// name, so a backup beside one is refused too: hub.json (the token, the database password), Persona Link's credential,
// the tunnel identities (a private key), the daemons' tokens, a manager's bus token, the APNs key.
var dataSecrets = []string{"hub.json", "link.json", "tailcat.json", "tailcat-hub.json", "daemons.json", "manager.json", "apns",
	"relay.json", "relay.secret", "devices.json", "mail_accounts.json"}

// dataSecretDirs are folders in the data folder (or a manager's folder) refused whole: the launchers' merged settings
// hold their tokens, tls/ the hub's TLS key (hub/public.go), link-tls/ Persona Link's (link/tlsid.go).
var dataSecretDirs = []string{"launcher-settings", "tls", "link-tls"}

// procSecrets are what Linux's /proc holds besides the processes' own folders that hands out a secret: the kernel's
// memory, its log, its symbols' addresses, the keyrings' entries and the page tables (#4172).
var procSecrets = []string{"kcore", "kmem", "kmsg", "kallsyms", "keys", "key-users", "kpagecount", "kpageflags", "kpagecgroup"}

// sysSecretDirs are folders in Linux's /sys refused whole: firmware variables, kernel debugging, tracing and security
// policy.
var sysSecretDirs = []string{"firmware", "kernel/debug", "kernel/security", "kernel/tracing"}

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
	if keyNames[filepath.Base(p)] || pseudoFSSecret(p) {
		return errOffLimits
	}
	// The hub's, the node's and the managers' own folders: the credentials in them and the keys beside them (secret.key,
	// handles.key, hub.key, the APNs .p8), wherever PERSONA_HOME puts the folder.
	if slices.ContainsFunc(n.inDataDirs(p), dataSecret) {
		return errOffLimits
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

// dataSecret says whether rel, a folded path in a data folder, is one of its credentials. They are matched only where
// they are written: directly in the folder or in a manager's (managers/<id>/), or anywhere in a folder that holds
// nothing else. Not at any depth: with PERSONA_HOME=~ the data folder is the home, and a project's link.json or
// certs/dev.pem below it is the project's own.
func dataSecret(rel string) bool {
	parts := strings.Split(rel, "/")
	if len(parts) > 2 && parts[0] == "managers" {
		parts = parts[2:]
	}
	if len(parts) > 1 {
		return slices.Contains(dataSecretDirs, parts[0])
	}
	base := parts[0]
	if hubKeyExt.MatchString(base) {
		return true
	}
	for _, name := range dataSecrets {
		if strings.HasPrefix(base, name) {
			return true
		}
	}
	return false
}

// pseudoFSSecret says whether the folded path p is a secret Linux's /proc or /sys makes up as it is read (#4172).
// Every process's own folder is refused whole (/proc/<pid>, self, thread-self): its environment (API keys and tokens,
// the hub's and the agents' own among them), command line, memory, maps and open files, and root and cwd, which lead
// back into the whole file system as that process sees it. Files about the machine (cpuinfo, meminfo, loadavg, net/…)
// stay readable.
func pseudoFSSecret(p string) bool {
	if rest, ok := strings.CutPrefix(p, "/proc/"); ok {
		first, _, _ := strings.Cut(rest, "/")
		return first == "self" || first == "thread-self" || isPID(first) || slices.Contains(procSecrets, first)
	}
	if rest, ok := strings.CutPrefix(p, "/sys/"); ok {
		for _, d := range sysSecretDirs {
			if rest == d || strings.HasPrefix(rest, d+"/") {
				return true
			}
		}
	}
	return false
}

// isPID: a /proc entry named by a process id.
func isPID(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// inDataDirs are where the folded path p is in each data folder it is in: one named .persona or .jarvis anywhere
// (this machine's, another user's, a copy), and the one PERSONA_HOME names for this node (identity.DataDir), as
// written or with its links followed. Every one is kept, not the first: a data folder PERSONA_HOME puts below a
// .persona folder (~/.persona/inst2) has its credentials where they are written in it, not one level below .persona.
func (n *Node) inDataDirs(p string) []string {
	var rels []string
	parts := strings.Split(p, "/")
	for i, part := range parts {
		if part == ".persona" || part == ".jarvis" {
			rels = append(rels, strings.Join(parts[i+1:], "/"))
		}
	}
	for _, dir := range n.dataDirs() {
		if rel, ok := strings.CutPrefix(p, dir+"/"); ok {
			rels = append(rels, rel)
		}
	}
	return rels
}

// dataDirs are this node's data folder (folded), as written and with its links followed: looked up once, not on
// every component of every path a client asks about. None without a home or PERSONA_HOME to find it by.
func (n *Node) dataDirs() []string {
	n.dataDirOnce.Do(func() {
		dir := identity.DataDir(n.home)
		if !filepath.IsAbs(dir) {
			return
		}
		dir = filepath.Clean(dir)
		n.dataDirList = []string{fold(dir)}
		if real, err := filepath.EvalSymlinks(dir); err == nil && real != dir {
			n.dataDirList = append(n.dataDirList, fold(real))
		}
	})
	return n.dataDirList
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

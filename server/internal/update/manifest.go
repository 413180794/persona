package update

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Manifest is latest.json: the release the publisher put up (docs/hub-update.md "Release manifest and signature").
type Manifest struct {
	Version      string          `json:"version"`
	Build        int64           `json:"build"`
	Commit       string          `json:"commit"`
	Released     time.Time       `json:"released"`
	MinAppBuild  int             `json:"min_app_build,omitempty"`
	MinNodeBuild int64           `json:"min_node_build,omitempty"`
	Protocol     int             `json:"protocol"`
	Notes        string          `json:"notes,omitempty"`
	Files        map[string]File `json:"files"` // "linux-amd64" -> its download
	// Ancestors is every commit the release's code contains (EncodeAncestors of git rev-list <commit>): a hub installs
	// it by itself only when its own commit is one of them (Build.Hold).
	Ancestors string `json:"ancestors,omitempty"`
	// Programs are the programs released with jarvisd that the hub runs beside it (jarvisd-wechat): name ->
	// "linux-amd64" -> its download. The hub swaps them with jarvisd, and goes back with it. Older hubs ignore them.
	Programs map[string]map[string]File `json:"programs,omitempty"`
}

// ProgramNames are the programs released with jarvisd, each built from server/cmd/<name>.
var ProgramNames = []string{"jarvisd-wechat"}

// program names the program a download belongs to ("jarvisd-wechat-linux-amd64.gz" -> "jarvisd-wechat"), "" for
// jarvisd's own.
func program(gz string) string {
	for _, p := range ProgramNames {
		if strings.HasPrefix(gz, p+"-") {
			return p
		}
	}
	return ""
}

// ancestorBytes is how much of each commit id Ancestors keeps: 6 bytes, 12 hex digits. A few KB for a thousand
// commits; a commit that is not there matches one by chance about once in 10^10 lookups at ten thousand.
const ancestorBytes = 6

var errNoAncestors = errors.New("the manifest lists no commits")

// EncodeAncestors packs commit ids (hex, as git rev-list prints them) for Manifest.Ancestors: each one's first 6
// bytes, sorted, base64.
func EncodeAncestors(commits []string) (string, error) {
	seen := map[string]bool{}
	var keys []string
	for _, c := range commits {
		if c = strings.TrimSpace(c); c == "" {
			continue
		}
		k, err := commitKey(c)
		if err != nil {
			return "", err
		}
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		return "", errNoAncestors
	}
	sort.Strings(keys)
	return base64.StdEncoding.EncodeToString([]byte(strings.Join(keys, ""))), nil
}

// commitKey is the first 6 bytes of a hex commit id.
func commitKey(c string) (string, error) {
	if len(c) < 2*ancestorBytes {
		return "", fmt.Errorf("commit %q is too short to look up", c)
	}
	b, err := hex.DecodeString(c[:2*ancestorBytes])
	if err != nil {
		return "", fmt.Errorf("commit %q is not hex", c)
	}
	return string(b), nil
}

// Contains says whether the release's code includes commit: Ancestors lists it.
func (m *Manifest) Contains(commit string) (bool, error) {
	if m.Ancestors == "" {
		return false, errNoAncestors
	}
	raw, err := base64.StdEncoding.DecodeString(m.Ancestors)
	if err != nil || len(raw)%ancestorBytes != 0 {
		return false, errors.New("the manifest's list of commits does not read")
	}
	k, err := commitKey(commit)
	if err != nil {
		return false, err
	}
	at := func(i int) string { return string(raw[i*ancestorBytes : (i+1)*ancestorBytes]) }
	n := len(raw) / ancestorBytes
	i := sort.Search(n, func(i int) bool { return at(i) >= k })
	return i < n && at(i) == k, nil
}

// File is one platform's download.
type File struct {
	Name         string `json:"name"`   // jarvisd-linux-amd64.gz, next to latest.json
	Size         int64  `json:"size"`   // of the .gz
	SHA256       string `json:"sha256"` // of the .gz
	BinarySHA256 string `json:"binary_sha256"`
}

// ParseManifest reads latest.json.
func ParseManifest(b []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("latest.json: %w", err)
	}
	if m.Build <= 0 || m.Version == "" || len(m.Files) == 0 {
		return nil, fmt.Errorf("latest.json is missing its version, build or files")
	}
	return &m, nil
}

// BuildManifest describes the downloads in dir: every jarvisd-<os>-<arch>.gz there, and every program's
// <program>-<os>-<arch>.gz, with its hashes and the hash of what it unpacks to.
func BuildManifest(dir string, m Manifest) (*Manifest, error) {
	gzs, err := filepath.Glob(filepath.Join(dir, "jarvisd-*-*.gz"))
	if err != nil {
		return nil, err
	}
	m.Files = map[string]File{}
	for _, p := range gzs {
		f, err := describe(p)
		if err != nil {
			return nil, err
		}
		base := filepath.Base(p)
		if prog := program(base); prog != "" {
			if m.Programs == nil {
				m.Programs = map[string]map[string]File{}
			}
			if m.Programs[prog] == nil {
				m.Programs[prog] = map[string]File{}
			}
			m.Programs[prog][strings.TrimSuffix(strings.TrimPrefix(base, prog+"-"), ".gz")] = f
			continue
		}
		m.Files[strings.TrimSuffix(strings.TrimPrefix(base, "jarvisd-"), ".gz")] = f
	}
	if len(m.Files) == 0 {
		return nil, fmt.Errorf("no jarvisd-*.gz in %s", dir)
	}
	return &m, nil
}

func describe(gz string) (File, error) {
	f, err := os.Open(gz)
	if err != nil {
		return File{}, err
	}
	defer f.Close()
	outer := sha256.New()
	n, err := io.Copy(outer, f)
	if err != nil {
		return File{}, err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return File{}, err
	}
	zr, err := gzip.NewReader(f)
	if err != nil {
		return File{}, fmt.Errorf("%s: %w", gz, err)
	}
	inner := sha256.New()
	if _, err := io.Copy(inner, zr); err != nil {
		return File{}, fmt.Errorf("%s: %w", gz, err)
	}
	return File{Name: filepath.Base(gz), Size: n, SHA256: hex.EncodeToString(outer.Sum(nil)), BinarySHA256: hex.EncodeToString(inner.Sum(nil))}, nil
}

// CheckNext is publish.sh's check before a release: its build number must be larger than the one served, and the
// served release's commit an ancestor of this one (isAncestor runs git), or a release from an older branch would
// read as newer. Nothing served yet (served nil) passes.
func CheckNext(served *Manifest, build int64, commit string, isAncestor func(older, newer string) (bool, error)) error {
	if served == nil {
		return nil
	}
	if build <= served.Build {
		return fmt.Errorf("build %d is not newer than the one served (%d): is this machine's clock right?", build, served.Build)
	}
	if served.Commit == "" || served.Commit == commit {
		return nil
	}
	ok, err := isAncestor(served.Commit, commit)
	if err != nil {
		return fmt.Errorf("cannot tell whether the served release's commit %.12s comes before this one (git fetch?): %w", served.Commit, err)
	}
	if !ok {
		return fmt.Errorf("the served release's commit %.12s is not an ancestor of %.12s: this would publish an older branch as newer", served.Commit, commit)
	}
	return nil
}

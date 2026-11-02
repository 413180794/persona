package update

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Manifest is latest.json: the release the publisher put up (docs/hub-update.md「版本清单和签名」).
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
}

// File is one platform's download.
type File struct {
	Name         string `json:"name"`   // jarvisd-linux-amd64.gz, next to latest.json
	Size         int64  `json:"size"`   // of the .gz
	SHA256       string `json:"sha256"` // of the .gz
	BinarySHA256 string `json:"binary_sha256"`
}

// ParseManifest reads latest.json (verify its signature first: Verify).
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

// BuildManifest describes the downloads in dir: every jarvisd-<os>-<arch>.gz there, with its hashes and the hash of
// what it unpacks to.
func BuildManifest(dir string, m Manifest) (*Manifest, error) {
	gzs, err := filepath.Glob(filepath.Join(dir, "jarvisd-*-*.gz"))
	if err != nil {
		return nil, err
	}
	if len(gzs) == 0 {
		return nil, fmt.Errorf("no jarvisd-*.gz in %s", dir)
	}
	m.Files = map[string]File{}
	for _, p := range gzs {
		f, err := describe(p)
		if err != nil {
			return nil, err
		}
		m.Files[strings.TrimSuffix(strings.TrimPrefix(filepath.Base(p), "jarvisd-"), ".gz")] = f
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

// Package denohost prepares a pinned shared engine and the trusted stdio adapter.
// The manager must put every prepared process in a verified independent OS sandbox.
package denohost

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/extension/sandbox"
)

const Version = "2.9.6"
const EngineMost = 512 << 20
const EnginePath = "/runtime/deno"
const AdapterPath = "/runtime/adapter.mjs"
const BundlePath = "/extension/bundle.mjs"

//go:embed adapter.mjs
var adapter []byte

// Config is manager-owned, never read from an extension or discovered on PATH.
// Installation/download is explicit offline administration, not activation work.
type Config struct {
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
	Version string `json:"version"`
}

func (c Config) Verify(ctx context.Context) error {
	if runtime.GOOS != "linux" || c.Version != Version || !extension.ValidSHA256(c.SHA256) || !filepath.IsAbs(c.Path) {
		return errors.New("a pinned Linux Deno path, version and SHA-256 are required")
	}
	resolved, err := filepath.EvalSymlinks(c.Path)
	if err != nil || resolved != c.Path {
		return errors.New("pinned Deno must not traverse symlinks")
	}
	f, err := os.Open(c.Path)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Mode()&0o111 == 0 || st.Mode()&0o022 != 0 || st.Size() <= 0 || st.Size() > EngineMost {
		return errors.New("pinned Deno must be a bounded executable, not group/world writable")
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, EngineMost+1))
	if err != nil || n > EngineMost {
		return errors.New("pinned engine exceeds size limit")
	}
	if hex.EncodeToString(h.Sum(nil)) != c.SHA256 {
		return errors.New("pinned Deno integrity mismatch")
	}
	cmd := exec.CommandContext(ctx, c.Path, "--version")
	cmd.Env = []string{"PATH=/usr/bin:/bin", "DENO_NO_UPDATE_CHECK=1", "DENO_NO_PROMPT=1"}
	out, err := cmd.Output()
	if err != nil || !strings.HasPrefix(string(out), "deno "+Version+" ") {
		return errors.New("pinned Deno version mismatch")
	}
	return nil
}

// InstallAdapter writes only manager-embedded bytes into a separate content-addressed directory.
// No extension source, configuration or dependency script can choose or overwrite the adapter.
func InstallAdapter(root string) (string, error) {
	h := sha256.Sum256(adapter)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", err
	}
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, hex.EncodeToString(h[:]))
	if err := os.Mkdir(dir, 0o700); err != nil && !os.IsExist(err) {
		return "", err
	}
	dirInfo, err := os.Lstat(dir)
	if err != nil || !dirInfo.IsDir() || dirInfo.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("trusted adapter directory must not be a symlink")
	}
	p := filepath.Join(dir, "adapter.mjs")
	if st, err := os.Lstat(p); err == nil {
		if !st.Mode().IsRegular() || st.Size() != int64(len(adapter)) {
			return "", errors.New("trusted adapter is not a bounded regular file")
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return "", err
		}
		if sha256.Sum256(b) != h {
			return "", errors.New("trusted adapter integrity mismatch")
		}
		return p, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o400)
	if err != nil {
		return "", err
	}
	_, err = f.Write(adapter)
	closeErr := f.Close()
	if err != nil {
		return "", err
	}
	return p, closeErr
}

func Args(bundle, adapter, proxy string) []string {
	return []string{"run", "--no-config", "--no-lock", "--no-npm", "--no-remote", "--cached-only", "--no-check", "--no-prompt",
		"--deny-import", "--deny-env", "--allow-read=" + bundle, "--deny-write", "--deny-run", "--deny-ffi", "--deny-sys",
		"--allow-net=" + strings.TrimPrefix(proxy, "http://"), "--v8-flags=--disallow-code-generation-from-strings", adapter, bundle, proxy}
}

// DevelopmentArgs adds a bounded old-space heap without disabling V8's sandbox
// or altering native permissions. Eligibility is checked by the manager first.
func DevelopmentArgs(args []string) []string {
	out := append([]string(nil), args...)
	for i, a := range out {
		if strings.HasPrefix(a, "--v8-flags=") {
			out[i] += ",--max-old-space-size=64"
		}
	}
	return out
}

// Mounts includes only the engine, trusted adapter, bundle and exact ELF dependencies
// needed to load the runtime/launcher. It never mounts a package or manager directory.
func Mounts(c Config, adapter, bundle, self string) ([]sandbox.Mount, error) {
	out := []sandbox.Mount{{Source: c.Path, Target: EnginePath}, {Source: adapter, Target: AdapterPath}, {Source: bundle, Target: BundlePath}}
	seen := map[string]bool{}
	for _, p := range []string{c.Path, self} {
		deps, err := elfDependencies(p, seen)
		if err != nil {
			return nil, err
		}
		for _, p := range deps {
			out = append(out, sandbox.Mount{Source: p, Target: p})
		}
	}
	return out, nil
}

func elfDependencies(p string, seen map[string]bool) ([]string, error) {
	return sandbox.ELFDependencies(p, seen)
}

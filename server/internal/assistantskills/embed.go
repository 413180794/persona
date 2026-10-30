// Package assistantskills holds the skills the assistant loads, copied from their upstreams (SOURCE.md). The node
// installs them in the assistant's directory (.claude/skills), so they ship with jarvisd.
package assistantskills

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

//go:embed all:lark-connector
var files embed.FS

// Version identifies the embedded skills (a hash of every file).
func Version() string {
	h := sha256.New()
	_ = fs.WalkDir(files, ".", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			b, _ := files.ReadFile(p)
			h.Write([]byte(p))
			h.Write(b)
		}
		return err
	})
	return hex.EncodeToString(h.Sum(nil))[:12]
}

// Install writes the skills under dir (<assistant dir>/.claude/skills), each one replacing what an older jarvisd left
// (a stamp file skips identical installs). Scripts are made executable.
func Install(dir string) error {
	stamp := filepath.Join(dir, ".jarvis-skills")
	v := Version()
	if b, err := os.ReadFile(stamp); err == nil && strings.TrimSpace(string(b)) == v {
		return nil
	}
	tops, err := fs.ReadDir(files, ".")
	if err != nil {
		return err
	}
	for _, t := range tops {
		tmp := filepath.Join(dir, "."+t.Name()+".new")
		_ = os.RemoveAll(tmp)
		err := fs.WalkDir(files, t.Name(), func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			out := filepath.Join(tmp, strings.TrimPrefix(p, t.Name()))
			if d.IsDir() {
				return os.MkdirAll(out, 0o755)
			}
			b, err := files.ReadFile(p)
			if err != nil {
				return err
			}
			mode := os.FileMode(0o644)
			if strings.HasSuffix(p, ".sh") {
				mode = 0o755
			}
			return os.WriteFile(out, b, mode)
		})
		if err != nil {
			return err
		}
		final := filepath.Join(dir, t.Name())
		_ = os.RemoveAll(final)
		if err := os.Rename(tmp, final); err != nil {
			return err
		}
	}
	return os.WriteFile(stamp, []byte(v+"\n"), 0o644)
}

// OlScript is the lark-connector helper's path once installed under dir.
func OlScript(dir string) string { return filepath.Join(dir, "lark-connector", "scripts", "ol.sh") }

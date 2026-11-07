package assistant

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// handleKey is the knowledge base's key for the senders' identity hints (kb/handles.go): handles.key in dir, the
// manager's own directory (the built-in manager's a copy of the data directory's, so the hashes stay the same; docs/assistant/managers.md
// "The built-in manager's own files"), 32 random bytes made on first use, 0600, never in the database or a log. Losing it
// only means later hints hash differently.
func handleKey(dir string) ([]byte, error) {
	p := filepath.Join(dir, "handles.key")
	key, err := os.ReadFile(p)
	if err == nil {
		if len(key) < 32 {
			return nil, fmt.Errorf("%s is too short", p)
		}
		return key, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	key = make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, fs.ErrExist) { // made meanwhile
		return handleKey(dir)
	}
	if err != nil {
		return nil, err
	}
	if _, err := f.Write(key); err != nil {
		f.Close()
		return nil, err
	}
	return key, f.Close()
}

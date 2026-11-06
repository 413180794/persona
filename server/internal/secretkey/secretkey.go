// Package secretkey seals secrets kept in Postgres with a part's own secret.key (32 bytes, AES-256-GCM, base64 of
// nonce + ciphertext). The hub seals its own with the one in its data directory (peers' tokens, the computers'
// connector credentials); a manager with the one in its own directory (model providers' keys, extensions', connectors'
// and bots' secrets, mail passwords; the built-in manager's is a copy of the data directory's, docs/managers.md "The
// built-in manager's own files").
package secretkey

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// Box seals and opens with the key in one file, read (or made) on first use.
type Box struct {
	path string
	mu   sync.Mutex
	key  []byte
}

// New is a Box on the key file at path; "" is a key made for this process only (tests).
func New(path string) *Box { return &Box{path: path} }

// Key is the AES key: the file's, made when there is none (only one process gets to write it: the hub and the manager
// may make it at the same moment, and both then read the winner's).
func (b *Box) Key() ([]byte, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.key != nil {
		return b.key, nil
	}
	fresh := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, fresh); err != nil {
		return nil, err
	}
	if b.path == "" {
		b.key = fresh
		return fresh, nil
	}
	key, err := os.ReadFile(b.path)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(b.path), 0o700); err != nil { // a manager's own directory, made at first use
			return nil, err
		}
		f, err := os.OpenFile(b.path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		switch {
		case err == nil:
			_, err = f.Write(fresh)
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return nil, err
			}
			key = fresh
		case errors.Is(err, os.ErrExist):
			if key, err = os.ReadFile(b.path); err != nil {
				return nil, err
			}
		default:
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("%s must hold 32 bytes", b.path)
	}
	b.key = key
	return key, nil
}

// Seal is plain sealed.
func (b *Box) Seal(plain string) (string, error) {
	gcm, err := b.gcm()
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(plain), nil)), nil
}

// Open is what Seal sealed; "" opens to "".
func (b *Box) Open(sealed string) (string, error) {
	if sealed == "" {
		return "", nil
	}
	gcm, err := b.gcm()
	if err != nil {
		return "", err
	}
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("sealed key too short")
	}
	plain, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
	return string(plain), err
}

func (b *Box) gcm() (cipher.AEAD, error) {
	key, err := b.Key()
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

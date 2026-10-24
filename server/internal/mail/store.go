package mail

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// store keeps accounts on disk with their secrets encrypted.
type store struct {
	path string
	key  []byte // 32-byte AES-256 key
	mu   sync.Mutex
}

// accountFile is the on-disk JSON shape. Secrets are base64 AES-GCM blobs.
type accountFile struct {
	Accounts []accountRecord `json:"accounts"`
}

type accountRecord struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Email        string `json:"email"`
	Provider     string `json:"provider"`
	Auth         string `json:"auth"`
	Username     string `json:"username"`
	IMAPHost     string `json:"imap_host"`
	IMAPPort     int    `json:"imap_port"`
	IMAPSecurity string `json:"imap_security,omitempty"`
	SMTPHost     string `json:"smtp_host"`
	SMTPPort     int    `json:"smtp_port"`
	SMTPSecurity string `json:"smtp_security"`

	// PasswordEnc holds the IMAP/SMTP password, encrypted.
	PasswordEnc string `json:"password_enc,omitempty"`
	// OAuthEnc holds the JSON OAuth credentials, encrypted.
	OAuthEnc string `json:"oauth_enc,omitempty"`
}

// openStore loads (or creates) dataDir/secret.key and dataDir/mail_accounts.json.
func openStore(dataDir string) (*store, error) {
	if dataDir == "" {
		return nil, errors.New("mail: data dir is empty")
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("mail: create data dir: %w", err)
	}
	keyPath := filepath.Join(dataDir, "secret.key")
	key, err := loadOrCreateKey(keyPath)
	if err != nil {
		return nil, err
	}
	return &store{path: filepath.Join(dataDir, "mail_accounts.json"), key: key}, nil
}

func loadOrCreateKey(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err == nil {
		if len(b) != 32 {
			return nil, fmt.Errorf("mail: %s must contain exactly 32 bytes, got %d", path, len(b))
		}
		return b, nil
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("mail: read secret key: %w", err)
	}
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("mail: generate secret key: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		// Another process won the race; read what it wrote.
		if os.IsExist(err) {
			return loadOrCreateKey(path)
		}
		return nil, fmt.Errorf("mail: create secret key: %w", err)
	}
	if _, err := f.Write(key); err != nil {
		f.Close()
		return nil, fmt.Errorf("mail: write secret key: %w", err)
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("mail: close secret key: %w", err)
	}
	return key, nil
}

func (s *store) encrypt(plain []byte) (string, error) {
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, plain, nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

func (s *store) decrypt(enc string) ([]byte, error) {
	if enc == "" {
		return nil, nil
	}
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return nil, fmt.Errorf("mail: decode secret: %w", err)
	}
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(raw) < gcm.NonceSize() {
		return nil, errors.New("mail: encrypted secret is truncated")
	}
	nonce, ciphertext := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	return gcm.Open(nil, nonce, ciphertext, nil)
}

// load reads every account. A missing file is not an error.
func (s *store) load() ([]*MailAccount, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked()
}

func (s *store) loadLocked() ([]*MailAccount, error) {
	b, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("mail: read accounts: %w", err)
	}
	var f accountFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("mail: parse accounts: %w", err)
	}
	accounts := make([]*MailAccount, 0, len(f.Accounts))
	for _, r := range f.Accounts {
		a := r.account()
		if r.PasswordEnc != "" {
			plain, err := s.decrypt(r.PasswordEnc)
			if err != nil {
				return nil, fmt.Errorf("mail: decrypt password for %s: %w", r.Email, err)
			}
			a.Password = string(plain)
		}
		if r.OAuthEnc != "" {
			plain, err := s.decrypt(r.OAuthEnc)
			if err != nil {
				return nil, fmt.Errorf("mail: decrypt oauth for %s: %w", r.Email, err)
			}
			var oc OAuthCreds
			if err := json.Unmarshal(plain, &oc); err != nil {
				return nil, fmt.Errorf("mail: parse oauth for %s: %w", r.Email, err)
			}
			a.OAuth = &oc
		}
		accounts = append(accounts, a)
	}
	return accounts, nil
}

func (r accountRecord) account() *MailAccount {
	return &MailAccount{
		ID:           r.ID,
		Name:         r.Name,
		Email:        r.Email,
		Provider:     r.Provider,
		Auth:         r.Auth,
		Username:     r.Username,
		IMAPHost:     r.IMAPHost,
		IMAPPort:     r.IMAPPort,
		IMAPSecurity: r.IMAPSecurity,
		SMTPHost:     r.SMTPHost,
		SMTPPort:     r.SMTPPort,
		SMTPSecurity: r.SMTPSecurity,
	}
}

// save writes every account atomically with 0600 permissions, encrypting the
// stored secrets again with a fresh nonce.
func (s *store) save(accounts []*MailAccount) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	f := accountFile{Accounts: make([]accountRecord, 0, len(accounts))}
	for _, a := range accounts {
		r := accountRecord{
			ID:           a.ID,
			Name:         a.Name,
			Email:        a.Email,
			Provider:     a.Provider,
			Auth:         a.Auth,
			Username:     a.Username,
			IMAPHost:     a.IMAPHost,
			IMAPPort:     a.IMAPPort,
			IMAPSecurity: a.IMAPSecurity,
			SMTPHost:     a.SMTPHost,
			SMTPPort:     a.SMTPPort,
			SMTPSecurity: a.SMTPSecurity,
		}
		if a.Password != "" {
			enc, err := s.encrypt([]byte(a.Password))
			if err != nil {
				return fmt.Errorf("mail: encrypt password: %w", err)
			}
			r.PasswordEnc = enc
		}
		if a.OAuth != nil {
			plain, err := json.Marshal(a.OAuth)
			if err != nil {
				return err
			}
			enc, err := s.encrypt(plain)
			if err != nil {
				return fmt.Errorf("mail: encrypt oauth: %w", err)
			}
			r.OAuthEnc = enc
		}
		f.Accounts = append(f.Accounts, r)
	}

	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(s.path, b, 0o600)
}

// writeFileAtomic writes b to path via a temp file in the same directory, so a
// crash never leaves a half-written account file.
func writeFileAtomic(path string, b []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// normalizeEmail lowercases the address and trims spaces.
func normalizeEmail(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

package mail

import (
	"context"
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

	"github.com/nwverse/persona/server/internal/db"
)

// Records is where accounts are persisted: Postgres (internal/db) in production, memory in tests.
// The mail package encrypts secrets before they reach it, with a key that never leaves dataDir.
type Records interface {
	MailAccounts(ctx context.Context) ([]db.MailAccountRow, error)
	ReplaceMailAccounts(ctx context.Context, rows []db.MailAccountRow) error
}

// store keeps accounts in Records with their secrets encrypted.
type store struct {
	recs Records
	key  []byte // 32-byte AES-256 key
	mu   sync.Mutex
}

// accountFile is the legacy on-disk JSON shape (mail_accounts.json), imported once into Records.
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
	Status       string `json:"status,omitempty"`

	// PasswordEnc holds the IMAP/SMTP password, encrypted.
	PasswordEnc string `json:"password_enc,omitempty"`
	// OAuthEnc holds the OAuthCreds JSON of a Sign in with Google account, encrypted.
	OAuthEnc string `json:"oauth_enc,omitempty"`
}

// openStore loads (or creates) dataDir/secret.key and imports a legacy dataDir/mail_accounts.json into recs.
func openStore(dataDir string, recs Records) (*store, error) {
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
	st := &store{recs: recs, key: key}
	if err := st.importLegacy(filepath.Join(dataDir, "mail_accounts.json")); err != nil {
		return nil, err
	}
	return st, nil
}

// importLegacy moves accounts from the old JSON file into Records (only when Records is still empty),
// then renames the file so it is not imported twice.
func (s *store) importLegacy(path string) error {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("mail: read legacy accounts: %w", err)
	}
	existing, err := s.recs.MailAccounts(context.Background())
	if err != nil {
		return err
	}
	if len(existing) == 0 {
		var f accountFile
		if err := json.Unmarshal(b, &f); err != nil {
			return fmt.Errorf("mail: parse legacy accounts: %w", err)
		}
		rows := make([]db.MailAccountRow, 0, len(f.Accounts))
		for _, r := range f.Accounts {
			row, err := r.row()
			if err != nil {
				return err
			}
			rows = append(rows, row)
		}
		if err := s.recs.ReplaceMailAccounts(context.Background(), rows); err != nil {
			return fmt.Errorf("mail: import legacy accounts: %w", err)
		}
	}
	return os.Rename(path, path+".migrated")
}

// row converts a record (secrets already encrypted) to its database shape.
func (r accountRecord) row() (db.MailAccountRow, error) {
	pw, oauth := r.PasswordEnc, r.OAuthEnc
	r.PasswordEnc, r.OAuthEnc = "", ""
	data, err := json.Marshal(r)
	if err != nil {
		return db.MailAccountRow{}, err
	}
	return db.MailAccountRow{ID: r.ID, Email: r.Email, Data: data, PasswordEnc: pw, OAuthEnc: oauth}, nil
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
	rows, err := s.recs.MailAccounts(context.Background())
	if err != nil {
		return nil, fmt.Errorf("mail: load accounts: %w", err)
	}
	var f accountFile
	for _, row := range rows {
		var r accountRecord
		if err := json.Unmarshal(row.Data, &r); err != nil {
			return nil, fmt.Errorf("mail: parse account %s: %w", row.ID, err)
		}
		r.ID, r.PasswordEnc, r.OAuthEnc = row.ID, row.PasswordEnc, row.OAuthEnc
		f.Accounts = append(f.Accounts, r)
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
		if r.OAuthEnc != "" && a.Auth == AuthOAuth2 {
			plain, err := s.decrypt(r.OAuthEnc)
			if err != nil {
				return nil, fmt.Errorf("mail: decrypt oauth for %s: %w", r.Email, err)
			}
			a.OAuth = &OAuthCreds{}
			if err := json.Unmarshal(plain, a.OAuth); err != nil {
				return nil, fmt.Errorf("mail: parse oauth for %s: %w", r.Email, err)
			}
		}
		accounts = append(accounts, a)
	}
	return accounts, nil
}

func (r accountRecord) account() *MailAccount {
	a := &MailAccount{
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
		Status:       r.Status,
	}
	if a.Status == "" {
		a.Status = "ok"
	}
	return a
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
			Status:       a.Status,
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

	rows := make([]db.MailAccountRow, 0, len(f.Accounts))
	for _, r := range f.Accounts {
		row, err := r.row()
		if err != nil {
			return err
		}
		rows = append(rows, row)
	}
	if err := s.recs.ReplaceMailAccounts(context.Background(), rows); err != nil {
		return fmt.Errorf("mail: save accounts: %w", err)
	}
	return nil
}

// normalizeEmail lowercases the address and trims spaces.
func normalizeEmail(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

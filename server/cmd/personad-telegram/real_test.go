package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/connector/proc"
)

type memSecrets struct {
	mu   sync.Mutex
	data []byte
}

func (m *memSecrets) Load(context.Context) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.data, nil
}
func (m *memSecrets) Save(_ context.Context, d []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data = d
	return nil
}
func (m *memSecrets) Delete(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data = nil
	return nil
}

// TestRealTelegramRefusesMadeUpCredentials runs the whole chain against Telegram itself, without any account: the
// hub's side (connector/proc) starts this program, and the sign-in's first step sends made-up API credentials, which
// Telegram must refuse (API_ID_INVALID) and the step must say so. Opt-in: JARVIS_TELEGRAM_REAL=1 (it reaches the
// network).
func TestRealTelegramRefusesMadeUpCredentials(t *testing.T) {
	if os.Getenv("JARVIS_TELEGRAM_REAL") != "1" {
		t.Skip("set JARVIS_TELEGRAM_REAL=1 to reach Telegram")
	}
	bin := filepath.Join(t.TempDir(), "jarvisd-telegram")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	sec := &memSecrets{}
	c := proc.New(proc.Config{ID: "telegram", Name: "Telegram", Path: bin, Version: "dev\nchannel dev, build 0", Secrets: sec, Logf: t.Logf})
	go c.Run(ctx)
	for c.Status(ctx).State != connector.StateNotSignedIn {
		if ctx.Err() != nil {
			t.Fatalf("status %+v", c.Status(context.Background()))
		}
		time.Sleep(50 * time.Millisecond)
	}
	p, err := c.LoginStep(ctx, "", nil)
	if err != nil || p.Step != "credentials" {
		t.Fatalf("start %+v %v", p, err)
	}
	p, err = c.LoginStep(ctx, "credentials", map[string]string{"api_id": "1", "api_hash": strings.Repeat("0", 32)})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Telegram's answer as the step shows it: %q", p.Error)
	if p.Step != "credentials" || !strings.Contains(p.Error, "不认") {
		t.Fatalf("made-up credentials were not refused: %+v", p)
	}
	if sec.data != nil {
		t.Fatal("refused credentials were kept")
	}
}

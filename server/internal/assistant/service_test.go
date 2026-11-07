package assistant

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/nwverse/persona/server/internal/bus"
	"github.com/nwverse/persona/server/internal/managerconf"
	"github.com/nwverse/persona/server/internal/update"
)

// The built-in manager as a service of its own reads its token from its binding, never from stdin: not bound, it
// does not start (before anything else, the database included).
func TestServiceModeNeedsABinding(t *testing.T) {
	home := t.TempDir()
	data := filepath.Join(home, ".persona")
	t.Setenv("HOME", home)
	t.Setenv("PERSONA_HOME", data)
	t.Setenv("PERSONA_DATABASE_URL", "")
	t.Setenv("PERSONA_ASSISTANT_TOKEN", "from-the-environment")
	t.Setenv("PERSONA_ASSISTANT_DEPLOY", DeployService)
	if err := os.MkdirAll(data, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "hub.json"), []byte(`{"database_url":"postgres://u@127.0.0.1:1/x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	stdin := &readCounter{}
	err := Main(context.Background(), update.Build{}, stdin)
	if !errors.Is(err, managerconf.ErrNotBound) || stdin.reads > 0 {
		t.Fatalf("err %v, stdin read %d times", err, stdin.reads)
	}
}

type readCounter struct{ reads int }

func (r *readCounter) Read(p []byte) (int, error) {
	r.reads++
	return 0, errors.New("no stdin for a service")
}

// The built-in manager's own service checks its token before anything of it runs: any refusal is a revoke (a hub
// from before this mode answers a plain 401), no answer is not (the hub away: it runs as usual).
func TestTokenTaken(t *testing.T) {
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid token"}`))
	}))
	defer plain.Close()
	logf := func(string, ...any) {}
	if err := tokenTaken(context.Background(), plain.URL, "jb_x", nil, logf); !errors.Is(err, bus.ErrRevoked) {
		t.Fatalf("plain 401: %v", err)
	}
	revoked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":"` + bus.CodeTokenRevoked + `"}`))
	}))
	defer revoked.Close()
	if err := tokenTaken(context.Background(), revoked.URL, "jb_x", nil, logf); !errors.Is(err, bus.ErrRevoked) {
		t.Fatalf("revoked: %v", err)
	}
	away := plain.URL
	plain.Close()
	if err := tokenTaken(context.Background(), away, "jb_x", nil, logf); err != nil {
		t.Fatalf("hub away: %v", err)
	}
}

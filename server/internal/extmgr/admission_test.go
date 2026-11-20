//go:build linux || darwin

package extmgr

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/nwverse/persona/server/internal/admission"
)

func TestAdmissionRefusesFeishuAndExtensionBeforeDispatch(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "gate")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	g, err := admission.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.Control("init", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Control("close", "extension-attempt"); err != nil {
		t.Fatal(err)
	}
	h := &Manager{opts: Options{Admission: g}}
	event := botMessageEvent("om_unadmitted", "ou_me", "text", textContent("fixture"), nil)
	// A nil bot world would fail or hang if either path ever reached dispatch.
	if _, err := h.botEvent(context.Background(), event); !errors.Is(err, admission.ErrClosed) {
		t.Fatalf("Feishu delivery was not refused: %v", err)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/extension-fixture", nil))
	if w.Code != http.StatusServiceUnavailable || w.Header().Get("Retry-After") == "" {
		t.Fatalf("extension request: %d %v", w.Code, w.Header())
	}
}

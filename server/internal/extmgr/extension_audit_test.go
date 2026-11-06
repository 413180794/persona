package extmgr

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/db"
)

// auditReadStore is keepStore with an audit to read back, newest first.
type auditReadStore struct {
	*keepStore
	rows  []db.ExtensionAudit // oldest first, as written
	limit int                 // the last limit asked for
}

func (s *auditReadStore) ExtensionAudits(_ context.Context, plugin string, limit int) ([]db.ExtensionAudit, error) {
	s.limit = limit
	var out []db.ExtensionAudit
	for i := len(s.rows) - 1; i >= 0 && len(out) < limit; i-- {
		if plugin == "" || s.rows[i].Plugin == plugin {
			out = append(out, s.rows[i])
		}
	}
	return out, nil
}

func newAuditReadStore() *auditReadStore {
	at := time.Date(2026, 11, 6, 8, 0, 0, 0, time.UTC)
	return &auditReadStore{keepStore: newKeepStore(), rows: []db.ExtensionAudit{
		{Plugin: "example", Action: "install", Actor: "personad extension", Detail: json.RawMessage(`{"version":"1.0.0"}`), At: at},
		{Plugin: "feishu", Action: "bot_paired", Actor: "feishu:ou_example", Detail: json.RawMessage(`{"open_id":"ou_example"}`), At: at.Add(time.Minute)},
		{Plugin: "example", Action: "connector_logout", Actor: "device:dev_phone", Detail: json.RawMessage(`{}`), At: at.Add(2 * time.Minute)},
	}}
}

// The audit reads back newest first, by id or everyone's, at most 1000 rows a read; only a device with full rights or
// the owner reads it, never an answer-only device or an extension's token (#5356).
func TestAuditRoute(t *testing.T) {
	h := newTestManager(t)
	st := newAuditReadStore()
	h.store = st
	mux := h.routes()
	read := func(path string) (int, []db.ExtensionAudit) {
		t.Helper()
		code, out := call(t, mux, "GET", path, h.cfg.Token, "")
		raw, _ := json.Marshal(out["audit"])
		var rows []db.ExtensionAudit
		_ = json.Unmarshal(raw, &rows)
		return code, rows
	}
	code, rows := read("/api/v1/extensions/-/audit")
	if code != 200 || len(rows) != 3 || rows[0].Action != "connector_logout" || rows[2].Action != "install" || st.limit != auditReadDefault {
		t.Fatalf("everyone's: %d %+v (limit %d)", code, rows, st.limit)
	}
	if code, rows = read("/api/v1/extensions/-/audit?plugin=example&limit=1"); code != 200 || len(rows) != 1 || rows[0].Action != "connector_logout" {
		t.Fatalf("one extension, one row: %d %+v", code, rows)
	}
	if code, _ = read("/api/v1/extensions/-/audit?limit=5000"); code != 200 || st.limit != auditReadMost {
		t.Fatalf("too many: %d, asked the database for %d", code, st.limit)
	}
	for _, bad := range []string{"0", "-1", "ten"} {
		if code, _ := read("/api/v1/extensions/-/audit?limit=" + bad); code != 400 {
			t.Fatalf("limit %s: %d", bad, code)
		}
	}
	if code, rows = read("/api/v1/extensions/-/audit?plugin=nobody"); code != 200 || rows == nil || len(rows) != 0 {
		t.Fatalf("nobody's: %d %v", code, rows)
	}

	as := func(p principal) int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/v1/extensions/-/audit", nil)
		h.apiMux.ServeHTTP(rec, req.WithContext(withPrincipal(req.Context(), p)))
		return rec.Code
	}
	if code := as(principal{ID: "dev_phone", Role: roleFull}); code != 200 {
		t.Fatalf("a full device: %d", code)
	}
	if code := as(principal{ID: "dev_watch", Role: roleAnswer}); code != 403 {
		t.Fatalf("an answer-only device: %d", code)
	}
	if code := as(principal{Role: roleFull, Extension: "example"}); code != 403 {
		t.Fatalf("an extension's token: %d", code)
	}

	h.store = newKeepStore() // no database
	if code, _ := read("/api/v1/extensions/-/audit"); code != 503 {
		t.Fatalf("without a database: %d", code)
	}
}

// `personad extension audit` prints one line per row, newest first, in UTC, without empty details.
func TestAuditCommand(t *testing.T) {
	h := newTestManager(t)
	h.store = newAuditReadStore()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	hs := &http.Server{Handler: h.routes()}
	go hs.Serve(ln)
	t.Cleanup(func() { hs.Close() })
	home := t.TempDir()
	t.Setenv("PERSONA_HOME", home)
	t.Setenv("PERSONA_LISTEN", "")
	b, _ := json.Marshal(map[string]any{"listen": ln.Addr().String(), "token": h.cfg.Token})
	if err := os.WriteFile(filepath.Join(home, "hub.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) (string, error) {
		var out bytes.Buffer
		err := ExtensionCommand(context.Background(), append([]string{"audit"}, args...), strings.NewReader(""), &out, &out)
		return out.String(), err
	}
	out, err := run()
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if err != nil || len(lines) != 3 || lines[0] != "2026-11-06T08:02:00Z  example  connector_logout  device:dev_phone" ||
		lines[2] != `2026-11-06T08:00:00Z  example  install  personad extension  {"version":"1.0.0"}` {
		t.Fatalf("everyone's: %v\n%s", err, out)
	}
	if out, err = run("feishu", "--limit=5"); err != nil || strings.TrimSpace(out) !=
		`2026-11-06T08:01:00Z  feishu  bot_paired  feishu:ou_example  {"open_id":"ou_example"}` {
		t.Fatalf("the bot's: %v\n%s", err, out)
	}
	if _, err = run("--limit=0"); err == nil {
		t.Fatal("a bad limit went through")
	}
	if _, err = run("a", "b"); err == nil {
		t.Fatal("two ids went through")
	}
	if _, err = run("--since=1h"); err == nil {
		t.Fatal("an unknown flag went through")
	}
}

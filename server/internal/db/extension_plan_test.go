package db

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/extension"
)

type planTestStore struct {
	*DB
	commits int
}

func (s *planTestStore) PutExtensionPlan(ctx context.Context, rows []extension.Record) error {
	s.commits++
	return s.DB.PutExtensionPlan(ctx, rows)
}

type planTestSealer struct{}

func (planTestSealer) Seal(s string) (string, error) { return "synthetic:" + s, nil }
func (planTestSealer) Open(s string) (string, error) { return strings.TrimPrefix(s, "synthetic:"), nil }

func planTestPackage(t *testing.T, id, version string, pack bool) string {
	t.Helper()
	m := map[string]any{"manifest_version": 1, "id": id, "name": id, "version": version}
	if pack {
		m["extension_pack"] = []map[string]string{{"id": "a-existing", "version": ">=2.0.0"}, {"id": "b-new"}}
	} else {
		m["contributes"] = map[string]any{"providers": []map[string]string{{"id": id + "-provider", "name": id, "base_url": "https://provider.example.com"}}}
		m["configuration"] = map[string]any{"type": "object", "properties": map[string]any{"saved": map[string]string{"type": "string"}}}
		m["secrets"] = []map[string]any{{"name": "api_key", "title": "Example key", "required": true}}
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err = os.WriteFile(filepath.Join(dir, extension.ManifestFile), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "README.md"), []byte(id+" "+version), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestExtensionPlanPostgresFailureRestoresPackagesAndRecords(t *testing.T) {
	// Without CREATE DATABASE, dbtest falls back to a shared database; fault triggers are never allowed in that mode.
	u, err := url.Parse(os.Getenv("PERSONA_TEST_DATABASE_URL"))
	if err != nil || !regexp.MustCompile(`^/persona_test_[0-9]{10}_[0-9a-f]{8}$`).MatchString(u.Path) {
		t.Skip("fault injection requires dbtest's per-run isolated database; shared fallback is not allowed")
	}
	for _, failure := range []string{"prevalidation", "second", "main", "commit"} {
		t.Run(failure, func(t *testing.T) {
			d, ctx := open(t), t.Context()
			cleanupRows := func() {
				t.Helper()
				cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if _, err := d.pool.Exec(cleanupCtx, `DELETE FROM plugin_audit WHERE plugin IN ('a-existing','b-new','z-pack'); DELETE FROM plugins WHERE id IN ('a-existing','b-new','z-pack')`); err != nil {
					t.Error(err)
				}
			}
			cleanupRows()
			t.Cleanup(cleanupRows)
			root := t.TempDir()
			store := &planTestStore{DB: d}
			reg := extension.NewRegistry(root, store, planTestSealer{}, 202611100000)
			preview := func(id, version, upgrade string, pack bool) *extension.Preview {
				t.Helper()
				pv, err := reg.Preview(ctx, planTestPackage(t, id, version, pack), upgrade)
				if err != nil {
					t.Fatal(err)
				}
				return pv
			}
			first := preview("a-existing", "0.9.0", "", false)
			if _, err := reg.Install(ctx, first.ID, first.Digest, "test"); err != nil {
				t.Fatal(err)
			}
			first = preview("a-existing", "1.0.0", "a-existing", false)
			if _, err := reg.Upgrade(ctx, "a-existing", first.ID, first.Digest, "test"); err != nil {
				t.Fatal(err)
			}
			if err := reg.SetConfig(ctx, "a-existing", json.RawMessage(`{"saved":"retained"}`), "test"); err != nil {
				t.Fatal(err)
			}
			if err := reg.SetSecret(ctx, "a-existing", "api_key", "synthetic-value", "test"); err != nil {
				t.Fatal(err)
			}
			if err := reg.SetContribution(ctx, "a-existing", "providers", "a-existing-provider", false, "test"); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(reg.DataDir("a-existing"), 0o700); err != nil {
				t.Fatal(err)
			}
			data := filepath.Join(reg.DataDir("a-existing"), "retained.txt")
			if err := os.WriteFile(data, []byte("synthetic user data"), 0o600); err != nil {
				t.Fatal(err)
			}
			beforeReg := reg.Snapshot("a-existing").Record
			beforeRows, err := d.Extensions(ctx)
			if err != nil {
				t.Fatal(err)
			}
			beforeSecrets, err := d.ExtensionSecrets(ctx, "a-existing")
			if err != nil {
				t.Fatal(err)
			}
			var beforeSecretTime time.Time
			if err = d.pool.QueryRow(ctx, `SELECT updated_at FROM plugin_secrets WHERE plugin='a-existing' AND name='api_key'`).Scan(&beforeSecretTime); err != nil {
				t.Fatal(err)
			}
			var beforeAudit int
			if err = d.pool.QueryRow(ctx, `SELECT count(*) FROM plugin_audit`).Scan(&beforeAudit); err != nil {
				t.Fatal(err)
			}
			first = preview("a-existing", "2.0.0", "a-existing", false)
			second := preview("b-new", "1.0.0", "", false)
			main := preview("z-pack", "1.0.0", "", true)
			main.Bundle = []*extension.Preview{first, second}
			refs := []extension.PreviewRef{{PreviewID: first.ID, Digest: first.Digest}, {PreviewID: second.ID, Digest: second.Digest}}
			id := "b-new"
			when := "BEFORE"
			constraint := ""
			deferred := ""
			if failure != "second" {
				id = "z-pack"
			}
			if failure == "commit" {
				when = "AFTER"
				constraint = "CONSTRAINT "
				deferred = "DEFERRABLE INITIALLY DEFERRED"
			}
			sql := fmt.Sprintf(`CREATE FUNCTION extension_plan_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.id = '%s' THEN RAISE EXCEPTION 'synthetic plan failure'; END IF; RETURN NEW; END $$; CREATE %sTRIGGER extension_plan_fail %s INSERT OR UPDATE ON plugins %s FOR EACH ROW EXECUTE FUNCTION extension_plan_fail()`, id, constraint, when, deferred)
			if failure == "prevalidation" {
				main.Unresolved = []string{"shared >=2.0.0"}
			} else if _, err = d.pool.Exec(ctx, sql); err != nil {
				t.Fatal(err)
			}
			cleanup := func() {
				t.Helper()
				if failure == "prevalidation" {
					return
				}
				cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if _, e := d.pool.Exec(cleanupCtx, `DROP TRIGGER IF EXISTS extension_plan_fail ON plugins; DROP FUNCTION IF EXISTS extension_plan_fail()`); e != nil {
					t.Error(e)
				}
			}
			t.Cleanup(cleanup)
			calls := 0
			beforeCommits := store.commits
			reg.OnChange = func(extension.Change) { calls++ }
			if _, err = reg.InstallPlan(ctx, main.ID, main.Digest, refs, "test"); err == nil {
				t.Fatal("injected plan failure was accepted")
			}
			if failure == "prevalidation" && store.commits != beforeCommits {
				t.Fatal("prevalidation reached the PostgreSQL transaction")
			}
			if !reflect.DeepEqual(beforeReg, reg.Snapshot("a-existing").Record) {
				t.Error("registry record changed")
			}
			rows, e := d.Extensions(ctx)
			if e != nil || !reflect.DeepEqual(beforeRows, rows) {
				t.Error("database records changed:", e)
			}
			secrets, e := d.ExtensionSecrets(ctx, "a-existing")
			if e != nil || !reflect.DeepEqual(beforeSecrets, secrets) {
				t.Error("sealed secrets changed:", e)
			}
			var secretTime time.Time
			if e = d.pool.QueryRow(ctx, `SELECT updated_at FROM plugin_secrets WHERE plugin='a-existing' AND name='api_key'`).Scan(&secretTime); e != nil || !secretTime.Equal(beforeSecretTime) {
				t.Error("secret was rewritten:", e)
			}
			var audit int
			if e = d.pool.QueryRow(ctx, `SELECT count(*) FROM plugin_audit`).Scan(&audit); e != nil || audit != beforeAudit || calls != 0 {
				t.Error("failure published, audited or activated:", e)
			}
			for _, version := range []string{"0.9.0", "1.0.0"} {
				b, e := os.ReadFile(filepath.Join(root, "installed", "a-existing", version, "README.md"))
				if e != nil || string(b) != "a-existing "+version {
					t.Error("old package changed:", version, e)
				}
			}
			for _, entry := range []struct{ id, version string }{{"a-existing", "2.0.0"}, {"b-new", "1.0.0"}, {"z-pack", "1.0.0"}} {
				if _, e = os.Stat(filepath.Join(root, "installed", entry.id, entry.version)); !os.IsNotExist(e) {
					t.Error("partial package remained:", entry, e)
				}
			}
			if b, e := os.ReadFile(data); e != nil || !bytes.Equal(b, []byte("synthetic user data")) {
				t.Error("user data changed:", e)
			}
			for _, pv := range []*extension.Preview{first, second, main} {
				if reg.PreviewOf(pv.ID) == nil {
					t.Error("failed preview consumed:", pv.Manifest.ID)
				}
			}
			cleanup()
			main.Unresolved = nil
			if _, err = reg.InstallPlan(ctx, main.ID, main.Digest, refs, "test"); err != nil {
				t.Fatal("retry failed:", err)
			}
			if reg.Snapshot("a-existing").Record.Version != "2.0.0" || calls != 3 {
				t.Fatal("retry did not publish complete plan")
			}
			secrets, e = d.ExtensionSecrets(ctx, "a-existing")
			if e != nil || !reflect.DeepEqual(beforeSecrets, secrets) {
				t.Error("successful retry changed sealed secrets:", e)
			}
		})
	}
}

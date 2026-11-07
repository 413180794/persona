package assistant

import (
	"os"
	"testing"

	"github.com/nwverse/persona/server/internal/db/dbtest"
	"github.com/nwverse/persona/server/internal/testhome"
)

// The tests that need Postgres run on a database of their own (dbtest, #583); without PERSONA_TEST_DATABASE_URL they
// skip. Run with a throw-away HOME (testhome): nothing in the package may write the default ~/.persona.
func TestMain(m *testing.M) { os.Exit(testhome.Main(func() int { return dbtest.Main(m) })) }

// The package's tests never see the developer's HOME or PERSONA_HOME (testhome); most of them need a database and skip
// in CI, this one keeps the TestMain wiring from being dropped there.
func TestTestsRunInAThrowAwayHome(t *testing.T) {
	if !testhome.Active() || os.Getenv("PERSONA_HOME") != "" {
		t.Fatalf("not isolated: active %v, PERSONA_HOME %q", testhome.Active(), os.Getenv("PERSONA_HOME"))
	}
}

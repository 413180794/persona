package assistant

import (
	"os"
	"testing"

	"github.com/nwverse/persona/server/internal/db/dbtest"
)

// The tests that need Postgres run on a database of their own (dbtest, #583); without PERSONA_TEST_DATABASE_URL they
// skip.
func TestMain(m *testing.M) { os.Exit(dbtest.Main(m)) }

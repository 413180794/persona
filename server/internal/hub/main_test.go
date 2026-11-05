package hub

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/nwverse/persona/server/internal/assistant"
	"github.com/nwverse/persona/server/internal/db/dbtest"
	"github.com/nwverse/persona/server/internal/update"
)

// TestMain runs the tests on a database of their own (dbtest, #583); re-run with JARVIS_TEST_ASSISTANT=1 the test
// binary is the assistant's process (TestAssistantProcessIsSupervised).
func TestMain(m *testing.M) {
	if os.Getenv("JARVIS_TEST_ASSISTANT") == "1" {
		if err := assistant.Main(context.Background(), update.Build{Version: "test"}, os.Stdin); err != nil {
			log.Fatal(err)
		}
		return
	}
	os.Exit(dbtest.Main(m))
}

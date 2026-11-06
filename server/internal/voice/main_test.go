package voice

import (
	"os"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	// The insertion gate's waits are measured for a person and a model (gate.go); a test's model is a script that
	// answers at once. The tests of the gate set them back.
	delegationQuiet, speechQuiet, replyAudioIn = 50*time.Millisecond, 50*time.Millisecond, 50*time.Millisecond
	os.Exit(m.Run())
}

package telegram

import (
	"testing"

	"github.com/nwverse/persona/server/internal/connector/conformance"
)

// Telegram is one implementation of the connector protocol: what it gives directly, it gives through the wire.
func TestConformance(t *testing.T) {
	c, _, _ := archiveAccount(t)
	conformance.Check(t, c)
}

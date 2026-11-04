package wechat

import (
	"testing"

	"github.com/nwverse/persona/server/internal/connector/conformance"
)

// The connector answers the same through the protocol as directly.
func TestConformance(t *testing.T) {
	conformance.Check(t, New(Options{}, &memSecrets{}))
}

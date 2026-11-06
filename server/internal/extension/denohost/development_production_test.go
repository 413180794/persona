//go:build !persona_dev_resources || !linux

package denohost

import (
	"github.com/nwverse/persona/server/internal/extension/sandbox"
	"os"
	"strings"
	"testing"
)

func TestProductionCannotEnableDevelopmentResources(t *testing.T) {
	t.Setenv(sandbox.DevelopmentResourceEnv, "1")
	self, _ := os.Executable()
	dev, err := sandbox.SourceDevelopmentResources(self)
	if dev || err == nil || !strings.Contains(err.Error(), "unavailable in this build") {
		t.Fatalf("production opted into fallback: %v %v", dev, err)
	}
}

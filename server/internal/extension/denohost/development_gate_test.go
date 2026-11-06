package denohost

import (
	"os"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/extension/sandbox"
)

func TestProductionResourceMode(t *testing.T) {
	self, _ := os.Executable()
	t.Setenv(sandbox.DevelopmentResourceEnv, "")
	_, strictErr := sandbox.SourceDevelopmentResources(self)
	if strictErr != nil {
		t.Logf("default refuses on this host: %v", strictErr)
	}
	t.Setenv(sandbox.DevelopmentResourceEnv, "1")
	dev, err := sandbox.SourceDevelopmentResources(self)
	if err != nil && strings.Contains(err.Error(), "unavailable in this build") {
		t.Log("production build rejected explicit fallback opt-in")
		if code := sandbox.Run([]string{"--development-exec", "1", "--", "/bin/true"}, os.Stderr); code != 2 {
			t.Fatal("production helper switch accepted")
		}
		return
	}
	// The corresponding tagged test verifies actual development acceptance.
	if strictErr != nil && (err != nil || !dev) {
		t.Fatalf("tagged fallback not accepted: %v", err)
	}
}

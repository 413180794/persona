package sandbox

import (
	"errors"
	"os"
	"strconv"
	"time"

	"github.com/nwverse/persona/server/internal/i18n"
)

// DevelopmentResourceEnv is an explicit isolated-instance opt-in, not hub config.
const DevelopmentResourceEnv = "PERSONA_DEV_SOURCE_RESOURCES"

// SourceDevelopmentResources never falls back for filesystem/network failures.
// The production build rejects the switch even if full kernel controls exist.
func SourceDevelopmentResources(self string) (bool, error) {
	enabled := os.Getenv(DevelopmentResourceEnv) == "1"
	if enabled && !developmentResourcesBuilt {
		return false, i18n.ErrEnglish("extension-source.development_unavailable")
	}
	err := ProbeStrict(self)
	if err == nil {
		return false, nil
	}
	var missing *ResourceUnavailable
	if !enabled || !errors.As(err, &missing) {
		return false, err
	}
	if err := ProbeNamespaces(); err != nil {
		return false, err
	}
	return true, nil
}

// DevelopmentLimits are non-production process controls, not hard total RSS/quota.
// CPUSeconds and Wall are lifetime budgets; processes restart when exhausted.
// RSSMB is a sampled termination threshold and can overshoot between samples.
type DevelopmentLimits struct {
	CPUSeconds, RSSMB int
	Wall              time.Duration
}

func DefaultDevelopmentLimits() *DevelopmentLimits {
	return &DevelopmentLimits{CPUSeconds: 30, RSSMB: 256, Wall: 5 * time.Minute}
}
func (d *DevelopmentLimits) valid() bool {
	return d != nil && d.CPUSeconds >= 1 && d.CPUSeconds <= 30 && d.RSSMB >= 96 && d.RSSMB <= 256 && d.Wall >= time.Second && d.Wall <= 5*time.Minute
}
func (d *DevelopmentLimits) args() []string {
	return []string{strconv.Itoa(d.CPUSeconds), strconv.Itoa(d.RSSMB), strconv.FormatInt(d.Wall.Milliseconds(), 10)}
}

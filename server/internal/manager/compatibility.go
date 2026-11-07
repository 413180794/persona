package manager

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/nwverse/persona/server/internal/compat"
	"github.com/nwverse/persona/server/internal/i18n"
)

func (e *env) checkCompatibleService(id string) error {
	if e.goos != "linux" && e.goos != "darwin" {
		return nil
	}
	base := e.compatConfigHome()
	if base == "" {
		base = filepath.Join(e.home, ".config")
	}
	pairs := [][2]string{
		{filepath.Join(base, "systemd", "user", unitName(id)), filepath.Join(base, "systemd", "user", compat.LegacyManagerUnit(id))},
		{filepath.Join(e.home, "Library", "LaunchAgents", launchdLabel(id)+".plist"), filepath.Join(e.home, "Library", "LaunchAgents", compat.LegacyManagerLabel(id)+".plist")},
	}
	for _, pair := range pairs {
		a, ae := os.Stat(pair[0])
		b, be := os.Stat(pair[1])
		if ae != nil && !os.IsNotExist(ae) {
			return ae
		}
		if be != nil && !os.IsNotExist(be) {
			return be
		}
		if ae == nil && be == nil && !os.SameFile(a, b) {
			return i18n.Err("manager-compatibility.service_conflict")
		}
	}
	return nil
}

func (e *env) compatConfigHome() string {
	if e.getenv == nil {
		return ""
	}
	return e.getenv("XDG_CONFIG_HOME")
}
func (e *env) compatUnit(id string) string {
	base := e.compatConfigHome()
	if base == "" {
		base = filepath.Join(e.home, ".config")
	}
	legacy := compat.LegacyManagerUnit(id)
	if _, err := os.Lstat(filepath.Join(base, "systemd", "user", legacy)); err == nil {
		return legacy
	}
	return unitName(id)
}
func (e *env) compatLabel(id string) string {
	legacy := compat.LegacyManagerLabel(id)
	if _, err := os.Lstat(filepath.Join(e.home, "Library", "LaunchAgents", legacy+".plist")); err == nil {
		return legacy
	}
	return launchdLabel(id)
}
func (e *env) compatUnitText(id, text string) string {
	return strings.ReplaceAll(text, unitName(id), e.compatUnit(id))
}
func (e *env) compatLabelText(id, text string) string {
	return strings.ReplaceAll(text, launchdLabel(id), e.compatLabel(id))
}

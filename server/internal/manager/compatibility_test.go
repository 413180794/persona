package manager

import (
	"github.com/nwverse/persona/server/internal/compat"
	"os"
	"path/filepath"
	"testing"
)

func TestCompatibleManagerServiceRefusesIndependentIdentities(t *testing.T) {
	for _, platform := range []string{"linux", "darwin"} {
		t.Run(platform, func(t *testing.T) {
			home := t.TempDir()
			e := &env{home: home, goos: platform, getenv: func(string) string { return "" }}
			current, old := filepath.Join(home, ".config", "systemd", "user", unitName("fixture")), filepath.Join(home, ".config", "systemd", "user", compat.LegacyManagerUnit("fixture"))
			if platform == "darwin" {
				current = filepath.Join(home, "Library", "LaunchAgents", launchdLabel("fixture")+".plist")
				old = filepath.Join(home, "Library", "LaunchAgents", compat.LegacyManagerLabel("fixture")+".plist")
			}
			os.MkdirAll(filepath.Dir(old), 0o755)
			os.WriteFile(old, []byte("original"), 0o644)
			if err := e.checkCompatibleService("fixture"); err != nil {
				t.Fatal(err)
			}
			os.Symlink(filepath.Base(old), current)
			if err := e.checkCompatibleService("fixture"); err != nil {
				t.Fatal("one alias installation rejected", err)
			}
			os.Remove(current)
			os.WriteFile(current, []byte("separate"), 0o644)
			if err := e.checkCompatibleService("fixture"); err == nil {
				t.Fatal("independent service identities accepted")
			}
			if b, _ := os.ReadFile(old); string(b) != "original" {
				t.Fatal("existing service was changed")
			}
		})
	}
}

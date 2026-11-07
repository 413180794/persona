package update

import (
	"path/filepath"
	"strings"
)

// TargetArgs are the arguments (without the program's name) to run target with in place of the hub running as argv:
// argv's own, except that a hub running as persona-hub takes its commands without "hub" (`persona-hub run`), which
// personad needs (`personad hub run`). The update's restart and rollback and the watchdog run the server's binary
// (personad) with them; without this, a hub run as persona-hub with no edge would come back as `personad run`, which
// is no command, and stay down.
func TargetArgs(argv []string, target string) []string {
	if len(argv) == 0 {
		return []string{"hub"}
	}
	args := append([]string{}, argv[1:]...)
	if len(args) == 0 { // `personad` alone is no command; "hub" alone runs the hub (for persona-hub too)
		return []string{"hub"}
	}
	if programName(argv[0]) == "persona-hub" && programName(target) != "persona-hub" && args[0] != "hub" {
		args = append([]string{"hub"}, args...)
	}
	return args
}

// programName is a binary's name without its directory and a Windows .exe.
func programName(p string) string {
	return strings.TrimSuffix(filepath.Base(p), ".exe")
}

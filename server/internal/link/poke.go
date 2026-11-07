package link

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
)

// Pokes are how another program on this computer tells a running persona-link to act now. On Linux and Mac they are
// signals (SIGUSR1 / SIGUSR2, cmd/persona-link/signals_unix.go); Windows has none, so there they are named events
// (poke_windows.go), which the Windows tray sets.
const (
	PokeCheck  = "recheck"   // the network changed: look at the link now
	PokeRedial = "reconnect" // drop the link and dial again
)

// PokeEventName is the name of the Windows event for kind, one per link.json (as the run lock is): two data
// directories on one computer never poke each other. Local\ keeps it inside this logon session.
func PokeEventName(path, kind string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	sum := sha256.Sum256([]byte(strings.ToLower(filepath.Clean(abs))))
	return `Local\PersonaLink.` + hex.EncodeToString(sum[:8]) + "." + kind
}

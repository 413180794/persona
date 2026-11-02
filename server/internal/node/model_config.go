package node

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
)

// modelExecution is non-secret per-turn execution metadata supplied by the hub.
type modelExecution struct {
	Model       string
	Effort      string
	UseEffort   bool
	Fingerprint string
}

func (m modelExecution) check() error {
	if len(m.Model) > 512 || strings.ContainsAny(m.Model, "\r\n\x00") {
		return errors.New("invalid model")
	}
	return validModes("", m.Effort)
}

// executionFingerprint also detects a managed provider changed before the next turn.
func (n *Node) executionFingerprint(launcher string, m modelExecution) string {
	raw, _ := json.Marshal(m)
	if l, ok := n.managedLauncher(launcher); ok {
		v, _ := json.Marshal(l)
		raw = append(raw, v...)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func executionOption(options []modelExecution) modelExecution {
	if len(options) > 0 {
		return options[0]
	}
	return modelExecution{}
}

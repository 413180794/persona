package node

import (
	"context"
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

// assistantModel is the launcher and model the hub runs conversation id's next message on (as a message from the phone
// would be), for a message the node sends it by itself (recoverAssistantTasks). Started on the model it last ran, the
// conversation's next message from the phone would need another process (#1554). Nothing when the hub can't say (an
// older hub): the conversation's own then.
func (n *Node) assistantModel(ctx context.Context, id string) (string, modelExecution) {
	var res struct {
		Launcher         string `json:"launcher"`
		Model            string `json:"model"`
		Effort           string `json:"effort"`
		ModelFingerprint string `json:"model_fingerprint"`
	}
	if n.peer == nil || n.peer.Call(ctx, "assistant.model", map[string]any{"session_id": id}, &res) != nil {
		return "", modelExecution{}
	}
	return res.Launcher, modelExecution{Model: res.Model, Effort: res.Effort, UseEffort: res.ModelFingerprint != "", Fingerprint: res.ModelFingerprint}
}

package admission

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var larkToken = regexp.MustCompile(`^work-lark-extension\.[a-f0-9]{64}\.[0-9]+$`)

// BeginLark is used by the separate, locally owned connector process. Only a
// verified completion may retire a token. Process death leaves it pending.
func (g *Gate) BeginLark(id string) (string, error) {
	if g == nil || !idPattern.MatchString(id) {
		return "", errors.New("lark admission needs a configured gate and operation ID")
	}
	path, err := g.begin("lark-extension", "", id, nil, true)
	if err != nil {
		return "", err
	}
	return filepath.Base(path), nil
}

func (g *Gate) EndLark(token string) error {
	if g == nil || !larkToken.MatchString(token) {
		return errors.New("invalid lark admission token")
	}
	return g.finish(filepath.Join(g.dir, token))
}

type PendingOperation struct {
	Kind          string `json:"kind"`
	Ticket        string `json:"ticket"`
	PayloadSHA256 string `json:"payload_sha256"`
	Bytes         int64  `json:"bytes"`
}

// Pending provides a redacted inventory for the owner of the closed attempt.
// It does not replay or settle any operation.
func (g *Gate) Pending(attempt string) ([]PendingOperation, error) {
	if g == nil {
		return nil, errors.New("admission not configured")
	}
	lock, err := g.lock()
	if err != nil {
		return nil, err
	}
	defer lock.close()
	state, err := g.state()
	if err != nil {
		return nil, err
	}
	if state.Phase == "open" || state.Attempt != attempt {
		return nil, errors.New("attempt does not own the closed admission")
	}
	files, err := os.ReadDir(g.dir)
	if err != nil {
		return nil, err
	}
	out := make([]PendingOperation, 0)
	for _, entry := range files {
		name := entry.Name()
		if !strings.HasPrefix(name, "work-") {
			continue
		}
		kind, _, ok := strings.Cut(strings.TrimPrefix(name, "work-"), ".")
		if !ok || !kindPattern.MatchString(kind) || !entry.Type().IsRegular() {
			return nil, errors.New("untrusted pending operation")
		}
		data, err := os.ReadFile(filepath.Join(g.dir, name))
		if err != nil {
			return nil, err
		}
		out = append(out, PendingOperation{kind, name, fmt.Sprintf("%x", sha256.Sum256(data)), int64(len(data))})
	}
	return out, nil
}

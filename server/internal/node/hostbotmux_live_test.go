//go:build botmuxlive

package node

// A live check against the user's running botmux (go test -tags botmuxlive -run BotmuxLive ./internal/node/ -v): it
// lists botmux's sessions the way a node does and prints counts by state and whether each session id has a Claude
// transcript — never titles, ids or tokens. Then it hands the first live session a message through botmux's delivery
// as a dry run: botmux checks the token and the target and renders the turn, and dispatches nothing.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/host"
	"github.com/nwverse/persona/server/internal/proto"
)

func TestBotmuxLive(t *testing.T) {
	bin, err := exec.LookPath("botmux")
	if err != nil {
		t.Skip("botmux not installed")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	h := newBotmuxHost(bin, t.Logf)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	start := time.Now()
	agents, err := h.Agents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("observe + map: %s, %d sessions listed", time.Since(start).Round(time.Millisecond), len(agents))
	byStatus, withTranscript := map[string]int{}, 0
	for _, a := range agents {
		byStatus[a.AgentStatus]++
		if m, _ := filepath.Glob(filepath.Join(home, ".claude", "projects", "*", a.SessionID()+".jsonl")); len(m) > 0 {
			withTranscript++
		}
	}
	t.Logf("by state: %v; with a transcript: %d", byStatus, withTranscript)
	h.dryRun = true
	for _, a := range agents {
		if a.AgentStatus == proto.StatusOffline {
			continue
		}
		if err := h.Prompt(ctx, host.ParsePane(a.PaneID), "Persona live test (dry run)"); err != nil {
			t.Fatal(err)
		}
		t.Log("dry-run delivery accepted")
		return
	}
	t.Log("no live session to dry-run a delivery to")
}

package node

import (
	"context"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/screen"
)

// Claude Code's suggested next message: after a turn it may draw one in grey in the empty input box (Tab takes it,
// Enter sends it). It is only on the screen, never in the transcript, and comes a few seconds after the turn ends,
// so when a Claude session goes from working to idle / done the node reads its screen a few times (suggestReads)
// and keeps what it finds on the session (proto.Session.Suggestion) until the agent works again or a message goes in.

// suggestReads: when the screen is read after a turn ends — the suggestion is generated after it, mostly within
// seconds. Four reads a turn, nothing in between.
var suggestReads = []time.Duration{0, 2 * time.Second, 5 * time.Second, 10 * time.Second}

type suggestState struct {
	text    string
	status  string             // the status last seen, to notice a turn ending
	scanned context.CancelFunc // the reads after the turn that ended last; nil when none run
}

// noteStatus follows a session's status from each list rebuild: a turn that ended starts the reads, one that starts
// drops the suggestion. Returns the suggestion to show.
func (n *Node) noteStatus(id, agent, pane, status string) string {
	if agent != "" && agent != "claude" {
		return ""
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.suggests == nil {
		n.suggests = map[string]*suggestState{}
	}
	st := n.suggests[id]
	if st == nil {
		st = &suggestState{status: status}
		n.suggests[id] = st
		return ""
	}
	prev := st.status
	st.status = status
	switch {
	case status == proto.StatusWorking:
		st.text = ""
		if st.scanned != nil {
			st.scanned()
			st.scanned = nil
		}
	case (prev == proto.StatusWorking || prev == proto.StatusBlocked) && (status == proto.StatusIdle || status == proto.StatusDone):
		if st.scanned != nil {
			st.scanned()
		}
		ctx, cancel := context.WithCancel(context.Background())
		st.scanned = cancel
		go n.readSuggestion(ctx, id, pane)
	}
	return st.text
}

// readSuggestion reads the screen at suggestReads after a turn ended until a suggestion shows, someone types, or the
// agent works again (which cancels ctx).
func (n *Node) readSuggestion(ctx context.Context, id, pane string) {
	start := time.Now()
	for _, at := range suggestReads {
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Until(start.Add(at))):
		}
		rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		text, err := n.readPaneANSI(rctx, pane, "visible", 60)
		cancel()
		if err != nil {
			continue
		}
		in := screen.ClaudeInput(text)
		if in.Draft {
			return // the user is typing on the computer
		}
		if in.Suggestion != "" {
			n.mu.Lock()
			if st := n.suggests[id]; st != nil && ctx.Err() == nil {
				st.text = in.Suggestion
			}
			n.mu.Unlock()
			if n.events != nil {
				n.events.poke(false) // a list rebuild carries it to the hub
			}
			return
		}
	}
}

// dropSuggestion forgets a session's suggestion: a message is going in.
func (n *Node) dropSuggestion(id string) {
	n.mu.Lock()
	if st := n.suggests[id]; st != nil {
		st.text = ""
		if st.scanned != nil {
			st.scanned()
			st.scanned = nil
		}
	}
	n.mu.Unlock()
}

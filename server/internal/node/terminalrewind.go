package node

import (
	"context"
	"hash/crc32"
	"os"
	"strconv"
	"time"

	"github.com/nwverse/persona/server/internal/screen"
)

// A rewind done in the terminal (/rewind or Esc Esc, then "Restore conversation") writes nothing to the transcript on
// Claude Code 2.1.318 and puts the prompt gone back over into the input box; with a draft there (or the pane focused)
// Claude writes no recap either, so transcript/branch.go, which sees a rewind by the next line written at the rewind
// point, saw nothing for as long as the draft stayed: the session kept the rewound turn, its API error and its preview,
// 18 minutes in a live test (#5396). No hook, setting or file says it; only the screen does.
//
// So the dialog read of a resting Claude's screen (dialogstatus.go, every paneDialogEvery) also looks for it: when the
// box holds the words of the last prompt in the transcript, that prompt was answered, its turn ended, and nothing was
// written since that did not grow from it (transcript.File.LastPrompt), the screen is read again with colors and with
// its history, and screen.TerminalRewound decides. A rewind it sees is told to the file by the prompt's line
// (File.RewoundAt), as session.rewind tells it its own (forgetRewound): the turn is left out of the transcript and the
// list at once. A wrong guess is taken back by itself when Claude goes on from that turn (transcript/branch.go). The
// node presses nothing.

// seeTerminalRewind is the look, made on the plain screen the dialog read just took. A plain read takes Claude's grey
// suggestion (on most resting screens) for text too: the file is not touched before a read with colors shows a draft.
func (n *Node) seeTerminalRewind(id, pane, plain string, p *paneDialog) {
	if id == "" || n.plainScreen(pane) {
		return // no colors, no telling a draft from Claude's suggestion
	}
	if in := screen.ClaudeInput(plain); !in.Box || in.Text == "" {
		return
	}
	// The same screen over the same transcript is looked at once, before it is read with colors: a resting pane showing
	// Claude's suggestion (or an old draft) is read again with colors only when its screen or transcript changes. A
	// rewind redraws the conversation above the box, so the screen it leaves is a new one.
	key := strconv.FormatUint(uint64(crc32.ChecksumIEEE([]byte(plain))), 16)
	if path := n.transcriptPath(id); path != "" {
		if st, err := os.Stat(path); err == nil {
			key += "/" + strconv.FormatInt(st.Size(), 10)
		}
	}
	n.dialogs.mu.Lock()
	seen := p.rewindLooked == key
	p.rewindLooked = key
	n.dialogs.mu.Unlock()
	if seen {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ansi, err := n.readPaneANSI(ctx, pane, "visible", screen.DialogLines)
	if err != nil {
		return
	}
	if in := screen.ClaudeInput(ansi); !in.Draft || in.Text == "" {
		return
	}
	f, err := n.file(id)
	if err != nil {
		return
	}
	n.refresh(id, f)
	last, ok := f.LastPrompt()
	if !ok || !screen.RewindDraftIs(screen.ClaudeInput(ansi).Text, last.Text) {
		return
	}
	recent, err := n.readPane(ctx, pane, "recent", 200)
	if err != nil || !screen.TerminalRewound(ansi, recent, last.Text, last.Before, last.First) {
		return
	}
	mu := n.pushLock(id)
	mu.Lock()
	span, ok := f.RewoundAt(last.UUID)
	if ok {
		n.applyRewound(id, f, span)
	}
	mu.Unlock()
	if ok {
		n.logf("session %s: rewound in its terminal to before its last prompt (seen on the screen)", id)
		if n.events != nil {
			n.events.poke(true)
		}
	}
}

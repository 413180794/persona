package node

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"time"

	"github.com/nwverse/persona/server/internal/transcript"
)

// recoverUltracodeLost fills a resume notice when a cold, large transcript's background scan was still pending
// at the process boundary. Keep the open file and its size: a later prompt or a rewritten path belongs to the
// new process. This only recovers the notice, never enables ultracode or changes the resumed process's state.
// The completion channel lets callers wait in tests without blocking a real SessionStart hook.
func (n *Node) recoverUltracodeLost(id string, previousSince, previousAt, boundary time.Time) <-chan struct{} {
	done := make(chan struct{})
	f, err := os.Open(n.transcriptPath(id))
	if err != nil {
		close(done)
		return done
	}
	st, err := f.Stat()
	if err != nil {
		_ = f.Close()
		close(done)
		return done
	}
	go func() {
		defer close(done)
		defer f.Close()
		bgSlot <- struct{}{}
		defer func() { <-bgSlot }()
		value, at := readUltracodeHistory(io.NewSectionReader(f, 0, st.Size()), previousSince, boundary)
		// The screen may have observed a newer off command than the transcript's last delayed attachment.
		if value == nil || !*value || !at.After(previousAt) {
			return
		}
		n.mu.Lock()
		u := n.ultracodeLocked(id)
		if !u.Since.Equal(boundary) || u.LostDismissed || u.LostAt != nil || (u.Value != nil && *u.Value) {
			n.mu.Unlock()
			return
		}
		lost := boundary
		u.LostAt = &lost
		n.mu.Unlock()
		n.persistUltracode(id)
		n.notifySessions()
	}()
	return done
}

// readUltracodeHistory streams only bounded marker lines from the previous process's time interval. It shares
// transcript's parser so sidechains, command output, status lines and full/sparse attachments mean the same thing.
func readUltracodeHistory(r io.Reader, since, until time.Time) (*bool, time.Time) {
	m := transcript.Meta{UltracodeSince: since}
	b := bufio.NewReaderSize(r, 256<<10)
	long := false
	for {
		line, err := b.ReadSlice('\n')
		if err == bufio.ErrBufferFull {
			long = true
			continue
		}
		if err != nil {
			return m.Ultracode, m.UltracodeAt // an incomplete final line is not evidence
		}
		if !long && len(line) <= 64<<10 && containsAny(line, transcript.UltracodeMarkers) {
			var stamp struct {
				Timestamp time.Time `json:"timestamp"`
			}
			if json.Unmarshal(line, &stamp) == nil && stamp.Timestamp.Before(until) {
				transcript.ParseLine(0, line, &m)
			}
		}
		long = false
	}
}

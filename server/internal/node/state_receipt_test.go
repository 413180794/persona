//go:build unix

package node

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/nodeproto"
)

type stateJournalPeer struct {
	committed map[string]pendingNodeState
	writes    int
	loseReply bool
	beforePut func() error
}

func (p *stateJournalPeer) Call(_ context.Context, method string, params, out any) error {
	ack := out.(*nodeproto.StateRevision)
	switch method {
	case nodeproto.MethodStateReceipt:
		ack.Revision = p.committed[params.(map[string]string)["session_id"]].Revision
	case nodeproto.MethodStatePutReceipt:
		if p.beforePut != nil {
			if err := p.beforePut(); err != nil {
				return err
			}
		}
		entry := params.(pendingNodeState)
		prior := p.committed[entry.SessionID]
		if prior.Revision != entry.Revision {
			if prior.Revision != entry.Previous {
				return errors.New("unexpected predecessor")
			}
			p.committed[entry.SessionID] = entry
			p.writes++
		}
		if p.loseReply {
			p.loseReply = false
			return errors.New("receipt lost")
		}
		ack.Revision = entry.Revision
	default:
		return errors.New("unexpected method")
	}
	return nil
}

func TestStateJournalFlushSerializesWithAnOlderAttachment(t *testing.T) {
	path := filepath.Join(t.TempDir(), stateJournalFile)
	a, b := &stateJournal{path: path}, &stateJournal{path: path}
	if err := a.append("session", sessionState{Pane: "first"}); err != nil {
		t.Fatal(err)
	}
	inFlight, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	peer := &stateJournalPeer{
		committed: map[string]pendingNodeState{},
		beforePut: func() error {
			if calls.Add(1) != 1 {
				return errors.New("concurrent receipt submission")
			}
			close(inFlight)
			<-release
			return nil
		},
	}
	defer func() { close(release) }()
	done := make(chan error, 1)
	go func() {
		_, err := a.send(t.Context(), peer)
		done <- err
	}()
	<-inFlight
	if err := b.append("session", sessionState{Pane: "newest"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Millisecond)
	defer cancel()
	if err := b.flush(ctx, peer); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("flush did not wait for the sender within its deadline: %v", err)
	}
	release <- struct{}{}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	peer.beforePut = nil
	if err := b.flush(t.Context(), peer); err != nil {
		t.Fatal(err)
	}
	if _, found, err := b.head(); err != nil || found || peer.writes != 2 || peer.committed["session"].State.Pane != "newest" {
		t.Fatalf("flush did not finish the shared queue in order: %v %v %+v", found, err, peer)
	}
}

func TestStateJournalSurvivesLostReceiptAndAttachmentRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), stateJournalFile)
	a := &stateJournal{path: path}
	for _, pane := range []string{"first", "newest"} {
		if err := a.append("session", sessionState{Pane: pane}); err != nil {
			t.Fatal(err)
		}
	}
	peer := &stateJournalPeer{committed: map[string]pendingNodeState{}, loseReply: true}
	if sent, err := a.send(t.Context(), peer); sent || err == nil {
		t.Fatalf("lost reply should remain queued: %v, %v", sent, err)
	}
	b := &stateJournal{path: path}
	first, ok, err := b.head()
	if err != nil || !ok || !first.Prepared || first.State.Pane != "first" {
		t.Fatalf("head before retry: %+v, %v, %v", first, ok, err)
	}
	for range 2 {
		if sent, err := b.send(t.Context(), peer); err != nil || !sent {
			t.Fatalf("replay did not settle: %v %v", sent, err)
		}
	}
	if _, found, err := b.head(); err != nil || found || peer.writes != 2 || peer.committed["session"].State.Pane != "newest" {
		t.Fatalf("final queue or write order wrong: %v %v %+v", found, err, peer)
	}
}

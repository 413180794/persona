package node

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/link"
	"github.com/nwverse/persona/server/internal/nodeproto"
)

const stateJournalFile = "node-state-pending.json"

// A resident carries the one journal across transport replacements. Access
// from an old and new attachment is serialized in the resident process.
var stateJournalMu sync.Mutex

type stateJournal struct {
	path string
	wake chan struct{}
}

type pendingNodeState struct {
	SessionID string       `json:"session_id"`
	State     sessionState `json:"state"`
	Revision  string       `json:"revision"`
	Previous  string       `json:"previous"`
	Prepared  bool         `json:"prepared"`
}

type stateReceiptPeer interface {
	Call(context.Context, string, any, any) error
}

func (j *stateJournal) read() ([]pendingNodeState, error) {
	data, err := os.ReadFile(j.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var pending []pendingNodeState
	err = json.Unmarshal(data, &pending)
	return pending, err
}

func (j *stateJournal) write(pending []pendingNodeState) error {
	data, err := json.Marshal(pending)
	if err != nil {
		return err
	}
	if err := link.WritePrivate(j.path, data, true); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(j.path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func (j *stateJournal) append(id string, value sessionState) error {
	revision, err := randomHex(16)
	if err != nil {
		return err
	}
	stateJournalMu.Lock()
	defer stateJournalMu.Unlock()
	pending, err := j.read()
	if err != nil {
		return err
	}
	pending = append(pending, pendingNodeState{SessionID: id, State: value, Revision: revision})
	if err := j.write(pending); err != nil {
		return err
	}
	select {
	case j.wake <- struct{}{}:
	default:
	}
	return nil
}

func (j *stateJournal) head() (pendingNodeState, bool, error) {
	stateJournalMu.Lock()
	defer stateJournalMu.Unlock()
	pending, err := j.read()
	if err != nil || len(pending) == 0 {
		return pendingNodeState{}, false, err
	}
	return pending[0], true, nil
}

// updateHead is conditional so two generations of an attachment can recover
// the same operation without overwriting the next queued snapshot.
func (j *stateJournal) updateHead(op pendingNodeState, acknowledged bool) (pendingNodeState, error) {
	stateJournalMu.Lock()
	defer stateJournalMu.Unlock()
	pending, err := j.read()
	if err != nil {
		return op, err
	}
	if len(pending) == 0 || pending[0].Revision != op.Revision {
		return op, errors.New("node state journal advanced concurrently")
	}
	if acknowledged {
		return op, j.write(pending[1:])
	}
	if pending[0].Prepared {
		return pending[0], nil
	}
	pending[0] = op
	return op, j.write(pending)
}

func (j *stateJournal) send(ctx context.Context, peer stateReceiptPeer) (bool, error) {
	op, found, err := j.head()
	if !found || err != nil {
		return false, err
	}
	if !op.Prepared {
		var current nodeproto.StateRevision
		if err := peer.Call(ctx, nodeproto.MethodStateReceipt, map[string]string{"session_id": op.SessionID}, &current); err != nil {
			return false, err
		}
		op.Previous, op.Prepared = current.Revision, true
		op, err = j.updateHead(op, false)
		if err != nil {
			return false, err
		}
	}
	var ack nodeproto.StateRevision
	if err := peer.Call(ctx, nodeproto.MethodStatePutReceipt, op, &ack); err != nil {
		return false, err
	}
	if ack.Revision != op.Revision {
		return false, errors.New("manager did not confirm the submitted node state")
	}
	_, err = j.updateHead(op, true)
	return err == nil, err
}

func (n *Node) stateJournalLoop(ctx context.Context, j *stateJournal) {
	for ctx.Err() == nil {
		call, cancel := context.WithTimeout(ctx, 15*time.Second)
		sent, err := j.send(call, n.peer)
		cancel()
		if sent {
			continue
		}
		if err != nil && ctx.Err() == nil {
			n.logf("node state receipt pending: %v", err)
		}
		timer := time.NewTimer(3 * time.Second)
		select {
		case <-ctx.Done():
		case <-j.wake:
		case <-timer.C:
		}
		timer.Stop()
	}
}

func (n *Node) configureStateJournal(ctx context.Context, supported bool) error {
	if n.keeper == nil || n.nodeStateJournal.Load() != nil {
		return nil
	}
	path := filepath.Join(n.dir, stateJournalFile)
	_, err := os.Stat(path)
	if !supported && errors.Is(err, os.ErrNotExist) {
		return nil // unchanged protocol for a resident that has never negotiated receipts
	}
	j := &stateJournal{path: path, wake: make(chan struct{}, 1)}
	stateJournalMu.Lock()
	pending, err := j.read()
	stateJournalMu.Unlock()
	if err != nil {
		return err
	}
	newest := make(map[string]sessionState)
	for _, op := range pending {
		newest[op.SessionID] = op.State
	}
	n.state.merge(newest) // local durable updates outrank the manager's older copy on reconnect
	n.nodeStateJournal.Store(j)
	if supported {
		go n.stateJournalLoop(ctx, j)
	} else {
		n.logf("node state receipts deferred: manager has no receipt protocol")
	}
	return nil
}

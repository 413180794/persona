package node

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/nwverse/persona/server/internal/db"
)

const stateCheckpointEnv = "PERSONA_NODE_STATE_CHECKPOINT"

// beginCheckpoint pauses new guarded work without stopping a process or
// disconnecting the active attach. Release it on every attempt outcome.
func (r *ResidentReload) beginCheckpoint(ctx context.Context) (func(), error) {
	r.mu.Lock()
	if r.paused || r.checkpoint != nil {
		r.mu.Unlock()
		return nil, errors.New("resident maintenance already active")
	}
	checkpoint := make(chan struct{})
	r.checkpoint = checkpoint
	idle := r.idle
	busy := r.active > 0
	r.mu.Unlock()
	release := func() {
		r.mu.Lock()
		r.checkpoint = nil
		close(checkpoint)
		r.mu.Unlock()
	}
	if busy {
		select {
		case <-idle:
		case <-ctx.Done():
			release()
			return nil, ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		release()
		return nil, err
	}
	return release, nil
}

type stateCheckpointReceipt struct {
	ObservedAt time.Time `json:"observed_at"`
	Sessions   int       `json:"sessions"`
	SHA256     string    `json:"sha256"`
}

func (n *Node) checkpointRequest(ctx context.Context) any {
	if runtime.GOOS != "linux" || os.Getenv(stateCheckpointEnv) != "1" {
		return map[string]string{"error": "Linux node state checkpoint is disabled"}
	}
	call, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	r, err := n.checkpointState(call)
	if err != nil {
		return map[string]string{"error": err.Error()}
	}
	return r
}

func (n *Node) checkpointState(ctx context.Context) (stateCheckpointReceipt, error) {
	var empty stateCheckpointReceipt
	if n.reload == nil || n.peer == nil || n.state.isLoading() {
		return empty, errors.New("no live resident state-bearing connection")
	}
	release, err := n.reload.beginCheckpoint(ctx)
	if err != nil {
		return empty, err
	}
	defer release()
	if AssistantsBusy() {
		return empty, errors.New("assistant work is active")
	}

	// This protects a state mutation and its corresponding persistence callback.
	// Refuse rather than waiting unboundedly for a producer blocked on the peer.
	if !n.state.persistMu.TryLock() {
		return empty, errors.New("state persistence is busy")
	}
	defer n.state.persistMu.Unlock()
	if n.state.isLoading() {
		return empty, errors.New("resident is loading state")
	}
	if journal := n.nodeStateJournal.Load(); journal != nil {
		if _, found, err := journal.head(); err != nil || found {
			return empty, errors.New("node state journal is not confirmed empty")
		}
	}

	n.state.mu.Lock()
	snapshot, err := json.Marshal(n.state.Sessions)
	n.state.mu.Unlock()
	if err != nil {
		return empty, errors.New("could not encode sender state")
	}
	var expected map[string]db.NodeSession
	if err := json.Unmarshal(snapshot, &expected); err != nil || expected == nil {
		return empty, errors.New("could not project sender state")
	}

	type reply struct {
		Sessions map[string]db.NodeSession `json:"sessions"`
		err      error
	}
	peer := n.peer
	n.reload.mu.Lock()
	if n.reload.checkpointReadBusy {
		n.reload.mu.Unlock()
		return empty, errors.New("a previous checkpoint read has not exited")
	}
	n.reload.checkpointReadBusy = true
	n.reload.mu.Unlock()
	read := make(chan reply, 1)
	go func() {
		defer func() {
			n.reload.mu.Lock()
			n.reload.checkpointReadBusy = false
			n.reload.mu.Unlock()
		}()
		var value reply
		// Use the existing ordered transport and the regular read request. The
		// reload barrier acknowledges pending requests but does not return state.
		value.err = peer.Call(ctx, "state.load", map[string]any{}, &value)
		read <- value
	}()
	var stored reply
	select {
	case stored = <-read:
	case <-ctx.Done():
		return empty, ctx.Err()
	}
	if stored.err != nil || stored.Sessions == nil {
		return empty, errors.New("ordinary state.load did not return a complete snapshot")
	}
	select {
	case <-peer.Done():
		return empty, errors.New("original connection closed")
	default:
	}
	left, err := canonicalCheckpoint(expected)
	if err != nil {
		return empty, err
	}
	right, err := canonicalCheckpoint(stored.Sessions)
	if err != nil {
		return empty, err
	}
	n.state.mu.Lock()
	after, err := json.Marshal(n.state.Sessions)
	n.state.mu.Unlock()
	if err != nil || !bytes.Equal(snapshot, after) {
		return empty, errors.New("sender state changed during readback")
	}
	if !bytes.Equal(left, right) {
		return empty, errors.New("sender and Manager state differ")
	}
	return stateCheckpointReceipt{ObservedAt: time.Now().UTC(), Sessions: len(expected),
		SHA256: fmt.Sprintf("%x", sha256.Sum256(left))}, nil
}

// Compare the Manager's persisted wire projection. Its timestamp column has
// microsecond precision and its JSONB columns can change member order.
func canonicalCheckpoint(sessions map[string]db.NodeSession) ([]byte, error) {
	for id, value := range sessions {
		value.StartedAt = value.StartedAt.UTC().Truncate(time.Microsecond)
		sessions[id] = value
	}
	data, err := json.Marshal(sessions)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var tree any
	if err := decoder.Decode(&tree); err != nil {
		return nil, err
	}
	return json.Marshal(tree)
}

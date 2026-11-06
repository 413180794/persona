package extmgr

import (
	"fmt"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/i18n"
)

// TestExtensionOpsPrune checks that a full operation list only gives up finished operations: one still running or
// waiting for the user's consent stays listed however old it is.
func TestExtensionOpsPrune(t *testing.T) {
	now := time.Now()
	for _, state := range []string{opRunning, opNeedsConsent} {
		t.Run(state, func(t *testing.T) {
			ops := &extensionOps{byID: map[string]*extOp{}}
			// The active one is the oldest by UpdatedAt.
			active := &extOp{v: ExtensionOperation{ID: "active", State: state, StartedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour)}}
			ops.byID[active.v.ID] = active
			for i := 1; i < opMost; i++ {
				at := now.Add(-time.Duration(opMost-i) * time.Second)
				ops.byID[fmt.Sprintf("done-%02d", i)] = &extOp{v: ExtensionOperation{ID: fmt.Sprintf("done-%02d", i), State: opSucceeded,
					StartedAt: at, UpdatedAt: at, EndedAt: &at}}
			}
			ops.prune(now)
			if ops.byID["active"] == nil {
				t.Fatalf("the %s operation was evicted", state)
			}
			if len(ops.byID) != opMost-1 {
				t.Fatalf("got %d operations after prune, want %d", len(ops.byID), opMost-1)
			}
			if ops.byID["done-01"] != nil {
				t.Fatal("the oldest finished operation was not the one evicted")
			}
		})
	}

	t.Run("all active", func(t *testing.T) {
		ops := &extensionOps{byID: map[string]*extOp{}}
		for i := range opMost + 2 {
			id := fmt.Sprintf("run-%02d", i)
			ops.byID[id] = &extOp{v: ExtensionOperation{ID: id, State: opRunning, StartedAt: now, UpdatedAt: now}}
		}
		ops.prune(now)
		if len(ops.byID) != opMost+2 {
			t.Fatalf("got %d operations after prune, want all %d active ones kept", len(ops.byID), opMost+2)
		}
	})

	t.Run("expired", func(t *testing.T) {
		old := now.Add(-opKeep - time.Minute)
		ops := &extensionOps{byID: map[string]*extOp{
			"old":  {v: ExtensionOperation{ID: "old", State: opSucceeded, UpdatedAt: old, EndedAt: &old}},
			"live": {v: ExtensionOperation{ID: "live", State: opRunning, UpdatedAt: old}},
		}}
		ops.prune(now)
		if ops.byID["old"] != nil || ops.byID["live"] == nil {
			t.Fatalf("got %v, want only the running operation kept", ops.byID)
		}
	})
}

// TestStartOpExpiresStalePreviews checks that the list stays bounded when nobody lists it: starting an operation
// cancels the consent-waiting ones whose preview is gone, so prune can evict them.
func TestStartOpExpiresStalePreviews(t *testing.T) {
	h, _, _ := marketHub(t)
	ps := h.extensions
	now := time.Now()
	ps.ops.mu.Lock()
	if ps.ops.byID == nil {
		ps.ops.byID = map[string]*extOp{}
	}
	for i := range opMost {
		id := fmt.Sprintf("stale-%02d", i)
		at := now.Add(-time.Duration(opMost-i) * time.Second)
		ps.ops.byID[id] = &extOp{v: ExtensionOperation{ID: id, Kind: opInstall, State: opNeedsConsent, PreviewID: "gone-" + id,
			Steps: []ExtensionOpStep{{ID: "consent", State: "running"}}, StartedAt: at, UpdatedAt: at}}
	}
	ps.ops.mu.Unlock()

	op := h.startOp(opInstall, "example", "", i18n.Default, false)

	ps.ops.mu.Lock()
	defer ps.ops.mu.Unlock()
	if len(ps.ops.byID) > opMost {
		t.Fatalf("got %d operations after startOp, want at most %d", len(ps.ops.byID), opMost)
	}
	if ps.ops.byID[op.v.ID] == nil {
		t.Fatal("the new operation is not listed")
	}
	for id, o := range ps.ops.byID {
		if id != op.v.ID && o.v.State != opCanceled {
			t.Fatalf("operation %s with an expired preview is %s, want %s", id, o.v.State, opCanceled)
		}
	}
}

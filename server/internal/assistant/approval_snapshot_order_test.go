package assistant

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	approvalops "github.com/nwverse/persona/server/internal/approval"
	"github.com/nwverse/persona/server/internal/proto"
)

func TestPreparedSnapshotOutOfOrderDoesNotCancelOriginal(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ft := newFleetTestNoStore(t)
		preparedTestConversation(ft)
		ctx := context.Background()
		older := ft.s.nextSnapshot(ctx)
		if older.Seq == 0 || len(older.Interactions) != 0 {
			t.Fatalf("older snapshot must have a sequence and no pending approval: %+v", older)
		}

		const id = "ix_out_of_order_snapshot"
		var applies atomic.Int32
		originalDone := startPreparedTest(t, ft, id, "", func(context.Context, proto.Resolution) (string, error) {
			applies.Add(1)
			return "setting applied", nil
		})

		ft.s.publish(ctx)
		// The older publisher reached the fleet after the current one had applied.
		ft.s.fleetTakeSnapshot(older)
		synctest.Wait()

		select {
		case got := <-originalDone:
			t.Fatalf("a stale snapshot ended the original prepared call before the owner answered: %+v, %v", got.out, got.err)
		default:
		}
		if _, ok := ft.s.pendingIx(id); !ok {
			t.Fatal("a stale snapshot removed the still-unanswered owner card")
		}
		if got := applies.Load(); got != 0 {
			t.Fatalf("a stale snapshot applied the operation %d times", got)
		}

		allow := true
		if err := ft.s.resolve(id, proto.Resolution{Allow: &allow}); err != nil {
			t.Fatalf("the owner's fresh Allow was rejected after a stale snapshot: %v", err)
		}
		got := awaitPreparedTest(t, originalDone)
		if got.err != nil || got.out.Clarifying || got.out.Text != "setting applied" {
			t.Fatalf("original prepared call after owner Allow = %+v, %v", got.out, got.err)
		}
		select {
		case duplicate := <-originalDone:
			t.Fatalf("owner Allow returned the original prepared call twice: %+v", duplicate)
		default:
		}
		synctest.Wait()
		if got := applies.Load(); got != 1 {
			t.Fatalf("owner Allow applied %d times, want exactly one", got)
		}
		ft.waitPending(0)
	})
}

func TestPreparedSnapshotConcurrentPublishersAllowOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ft := newFleetTestNoStore(t)
		preparedTestConversation(ft)
		const id = "ix_concurrent_snapshot_publishers"
		var applies atomic.Int32
		originalDone := startPreparedInteraction(t, ft, proto.Interaction{
			ID: id, MachineID: "mac", SessionID: "s1", Kind: proto.IxPermission,
			ToolName: "persona_settings", Summary: "Change the example setting",
			ExpiresAt: time.Now().Add(time.Hour),
		}, approvalops.Operation{Apply: func(context.Context, proto.Resolution) (string, error) {
			applies.Add(1)
			return "setting applied", nil
		}})

		const publisherCount = 16
		start := make(chan struct{})
		var publishers sync.WaitGroup
		publishers.Add(publisherCount)
		for range publisherCount {
			go func() {
				defer publishers.Done()
				<-start
				ft.s.publish(context.Background())
			}()
		}
		close(start)
		publishers.Wait()
		synctest.Wait()

		select {
		case got := <-originalDone:
			t.Fatalf("concurrent current snapshots ended the unanswered prepared call: %+v, %v", got.out, got.err)
		default:
		}
		allow := true
		if err := ft.s.resolve(id, proto.Resolution{Allow: &allow}); err != nil {
			t.Fatalf("owner Allow after concurrent publishers: %v", err)
		}
		got := awaitPreparedTest(t, originalDone)
		if got.err != nil || got.out.Clarifying || got.out.Text != "setting applied" {
			t.Fatalf("original prepared call = %+v, %v", got.out, got.err)
		}
		synctest.Wait()
		if got := applies.Load(); got != 1 {
			t.Fatalf("concurrent publishers made owner Allow apply %d times, want exactly one", got)
		}
		ft.waitPending(0)
	})
}

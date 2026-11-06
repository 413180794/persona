package assistant

import (
	"context"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/proto"
)

func TestOwnedQuestionReplaysDurableOutcomeWithoutAnotherCard(t *testing.T) {
	for _, tc := range []struct {
		name, status string
		resolution   proto.Resolution
	}{
		{"answered", "resolved", proto.Resolution{Answers: map[string]string{"Ready?": "Later"}}},
		{"closed", "cancelled", proto.Resolution{Cancel: true}},
		{"handed back", "dismissed", proto.Resolution{Dismiss: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ft := newFleetTest(t)
			ix := proto.Interaction{ID: "work_settled", Kind: proto.IxQuestion, CanCancel: true, ExpiresAt: time.Now().Add(time.Hour)}
			ft.s.record(ix)
			ft.s.recordEnd(ix.ID, tc.status, &tc.resolution)
			for i := 0; i < 2; i++ {
				r, err := ft.s.waitOwnedQuestion(context.Background(), ix)
				if err != nil || r.Cancel != tc.resolution.Cancel || r.Dismiss != tc.resolution.Dismiss || r.Answers["Ready?"] != tc.resolution.Answers["Ready?"] {
					t.Fatalf("replay: %+v %v", r, err)
				}
				if len(ft.s.pendingInteractions()) != 0 {
					t.Fatal("settled question was shown again")
				}
			}
			// Same words with a genuinely new ID must still reach the user.
			ix.ID = "work_new"
			result := make(chan proto.Resolution, 1)
			go func() {
				r, err := ft.s.waitOwnedQuestion(context.Background(), ix)
				if err != nil {
					t.Error(err)
				}
				result <- r
			}()
			ft.waitPending(1)
			if err := ft.s.resolve(ix.ID, proto.Resolution{Cancel: true}); err != nil {
				t.Fatal(err)
			}
			select {
			case r := <-result:
				if !r.Cancel {
					t.Fatal("new question did not resolve")
				}
			case <-time.After(time.Second):
				t.Fatal("new question remained blocked")
			}
		})
	}
}

func TestOwnedQuestionHistoryFailureDoesNotReask(t *testing.T) {
	shortHistoryTimeout(t, 10*time.Millisecond)
	ft := newFleetTestOn(t, stuckHistoryStore{newFleetMem()})
	ix := proto.Interaction{ID: "work_unknown", Kind: proto.IxQuestion, CanCancel: true}
	_, err := ft.s.waitOwnedQuestion(context.Background(), ix)
	if err == nil {
		t.Fatal("unreadable history must be retried rather than treated as a new question")
	}
	if len(ft.s.pendingInteractions()) != 0 {
		t.Fatal("unreadable outcome created another pending card")
	}
}

func TestOwnedQuestionReattachesPendingWithoutAnnouncement(t *testing.T) {
	ft := newFleetTest(t)
	ix := proto.Interaction{ID: "work_retry", Kind: proto.IxQuestion, ToolName: workCardTool,
		CanCancel: true, ExpiresAt: time.Now().Add(time.Hour)}
	p := &pending{ix: ix, done: make(chan proto.Resolution, 1)}
	putPending(ft.s, p)
	ft.s.record(ix)
	result := make(chan proto.Resolution, 1)
	go func() {
		r, err := ft.s.waitOwnedQuestion(context.Background(), ix)
		if err != nil {
			t.Error(err)
		}
		result <- r
	}()
	deadline := time.Now().Add(time.Second)
	for {
		ft.s.fleet.mu.Lock()
		attached := p.resolved != nil
		ft.s.fleet.mu.Unlock()
		if attached {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("owner did not reattach")
		}
		time.Sleep(time.Millisecond)
	}
	if err := ft.s.resolve(ix.ID, proto.Resolution{Cancel: true}); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-result:
		if !r.Cancel {
			t.Fatal("owner lost cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("owner remained blocked")
	}
	if len(ft.toldOf(assistantwire.EventFleetInteraction)) != 0 {
		t.Fatal("existing card was announced again")
	}
}

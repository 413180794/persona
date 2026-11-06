package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/proto"
)

// mutesFailing is the fleet's store whose reads of the mutes fail while fail is set.
type mutesFailing struct {
	*fleetMem
	fail atomic.Bool
}

func (m *mutesFailing) Setting(ctx context.Context, key string, out any) (bool, error) {
	if key == "mutes" && m.fail.Load() {
		return false, errors.New("database hiccup")
	}
	return m.fleetMem.Setting(ctx, key, out)
}

// The mutes changed (fleet.mutes) and reading them again fails for a moment: an agent the user muted stays muted
// meanwhile, it does not push its cards to every phone. What failed to read is read again next time.
func TestMutedStaysMutedWhenMutesReadFails(t *testing.T) {
	st := &mutesFailing{fleetMem: newFleetMem()}
	if err := st.PutSetting(context.Background(), "mutes", Pins{Spaces: []string{}, Sessions: []string{"mac/s1"}}); err != nil {
		t.Fatal(err)
	}
	ft := newFleetTestOn(t, st)
	if !ft.s.muted("mac", "s1") {
		t.Fatal("s1 is muted")
	}
	st.fail.Store(true)
	ft.s.FleetEvent(context.Background(), assistantwire.EventMutes, nil) // another change: read them again
	if !ft.s.muted("mac", "s1") {
		t.Fatal("mutes unreadable: s1 is no longer muted")
	}
	ft.s.tell(permission("ix_1"))
	if got := ft.pushesOf(assistantwire.NotifyInput, 1, 200*time.Millisecond); len(got) != 0 {
		t.Fatalf("a muted agent's card was pushed while the mutes could not be read: %+v", got)
	}
	ft.sessions("mac", proto.Session{SessionID: "s1", Title: "a"})
	if m := ft.lastMuted("mac"); len(m) != 1 || m[0] != "s1" {
		t.Fatalf("muted told the hub = %v, want [s1]", m)
	}

	// Read again once the store answers: a change made meanwhile is taken.
	if err := st.PutSetting(context.Background(), "mutes", Pins{Spaces: []string{}, Sessions: []string{}}); err != nil {
		t.Fatal(err)
	}
	st.fail.Store(false)
	if ft.s.muted("mac", "s1") {
		t.Fatal("after recovery: s1 is still muted, the change was not read")
	}
}

// Mutes never read (the store failing from the start) are none: there is nothing older to keep.
func TestMutesUnreadAtStartAreNone(t *testing.T) {
	st := &mutesFailing{fleetMem: newFleetMem()}
	_ = st.PutSetting(context.Background(), "mutes", Pins{Spaces: []string{}, Sessions: []string{"mac/s1"}})
	st.fail.Store(true)
	ft := newFleetTestOn(t, st)
	if ft.s.muted("mac", "s1") {
		t.Fatal("muted before any read went through")
	}
	st.fail.Store(false)
	if !ft.s.muted("mac", "s1") {
		t.Fatal("the mutes were not read again once the store answered")
	}
}

// lastMuted is the muted sessions of the last list the hub was told for machine.
func (ft *fleetTest) lastMuted(machine string) []string {
	ft.t.Helper()
	list := ft.toldOf(assistantwire.EventFleetSessions)
	for i := len(list) - 1; i >= 0; i-- {
		var p assistantwire.FleetSessions
		if json.Unmarshal(list[i], &p) == nil && p.MachineID == machine {
			return p.Muted
		}
	}
	return nil
}

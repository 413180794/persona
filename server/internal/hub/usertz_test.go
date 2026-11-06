package hub

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
)

func TestUserLocationUsesManagerProfile(t *testing.T) {
	h := newTestHub(t)
	ctx := context.Background()
	builtin := assistantwire.AssistantID
	other := "m-0a0b0c"
	report := func(id string, seq uint64, p *assistantwire.Profile) error {
		_, err := (assistantLink{h}).snapshotFrom(ctx, id, assistantwire.Snapshot{Epoch: "e", Seq: seq, Profile: p})
		return err
	}
	if err := report(builtin, 1, &assistantwire.Profile{Timezone: "Europe/Paris"}); err != nil {
		t.Fatal(err)
	}
	if err := report(other, 1, &assistantwire.Profile{Timezone: "Asia/Tokyo"}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ id, zone string }{
		{builtin, "Europe/Paris"},
		{other, "Asia/Tokyo"},
		{"m-ffffff", "Europe/Paris"}, // No profile yet: use the built-in's.
	} {
		if got := h.userLocationOf(tc.id).String(); got != tc.zone {
			t.Errorf("manager %s: zone = %s, want %s", tc.id, got, tc.zone)
		}
	}
	// A snapshot without a profile retains the last one, including while disconnected.
	if err := report(other, 2, nil); err != nil {
		t.Fatal(err)
	}
	if got := h.userLocationOf(other).String(); got != "Asia/Tokyo" {
		t.Fatalf("snapshot without profile: zone = %s", got)
	}
	if err := report(other, 1, &assistantwire.Profile{Timezone: "UTC"}); !errors.Is(err, assistantwire.ErrStaleSnapshot) {
		t.Fatalf("stale snapshot: %v", err)
	}
	if got := h.userLocationOf(other).String(); got != "Asia/Tokyo" {
		t.Fatalf("stale snapshot replaced profile: zone = %s", got)
	}
	for i, zone := range []string{"", "Invalid/Zone"} {
		if err := report(other, uint64(i+3), &assistantwire.Profile{Timezone: zone}); err != nil {
			t.Fatal(err)
		}
		if got := h.userLocationOf(other); got != time.Local {
			t.Errorf("reported zone %q: got %s, want the hub's zone", zone, got)
		}
	}
	if got := h.assistantProfile().Timezone; got != "Europe/Paris" {
		t.Fatalf("another manager changed the built-in profile: %s", got)
	}
	h.forgetManagerState(other)
	if got := h.userLocationOf(other).String(); got != "Europe/Paris" {
		t.Fatalf("removed manager retained profile: zone = %s", got)
	}
}

func TestUserLocationWithoutProfile(t *testing.T) {
	h := newTestHub(t)
	if got := h.userLocation(); got != time.Local {
		t.Fatalf("no profile: zone = %s, want the hub's zone", got)
	}
}

func TestUserLocationFollowsDefaultManager(t *testing.T) {
	const other = "m-0a0b0c"
	h, store := newProfileTestHub(t, other)
	ctx := context.Background()
	for id, zone := range map[string]string{
		assistantwire.AssistantID: "Europe/Paris",
		other:                     "Asia/Tokyo",
	} {
		if _, err := (assistantLink{h}).snapshotFrom(ctx, id, assistantwire.Snapshot{
			Epoch: "e", Seq: 1, Profile: &assistantwire.Profile{Timezone: zone},
		}); err != nil {
			t.Fatal(err)
		}
	}
	h.push.userLoc = h.userLocation
	h.push.now = func() time.Time { return time.Date(2026, 1, 10, 13, 30, 0, 0, time.UTC) }
	prefs := NotifyPrefs{Quiet: QuietHours{Enabled: true, Start: "22:00", End: "07:00"}}
	for _, tc := range []struct {
		id, zone string
		quiet    bool
	}{
		{assistantwire.AssistantID, "Europe/Paris", false},
		{other, "Asia/Tokyo", true},
		{assistantwire.AssistantID, "Europe/Paris", false},
	} {
		if err := store.PutSetting(ctx, managerDefaultKey, tc.id); err != nil {
			t.Fatal(err)
		}
		h.ownersChanged()
		if got := h.userLocation().String(); got != tc.zone {
			t.Errorf("default %s: zone = %s, want %s", tc.id, got, tc.zone)
		}
		if got := h.push.inQuiet(prefs); got != tc.quiet {
			t.Errorf("default %s: quiet = %t, want %t", tc.id, got, tc.quiet)
		}
	}
}

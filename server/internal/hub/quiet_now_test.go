package hub

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
)

// The Lark bot's nudges (#6084) wait out quiet hours, which are per device: the user is quiet while any device with
// quiet hours on is inside its window. The assistant asks the hub with notify.quiet.
func TestQuietNowIsAnyDevicesWindow(t *testing.T) {
	h := newTestHub(t)
	night := time.Date(2026, 11, 6, 2, 0, 0, 0, time.UTC)
	h.push.now = func() time.Time { return night }
	ask := func() bool {
		t.Helper()
		out, ok, err := h.assistantAppCall(context.Background(), assistantwire.AssistantID,
			assistantwire.AppCall{AppID: assistantwire.AppNotify, Method: assistantwire.NotifyQuietGet})
		if !ok || err != nil {
			t.Fatalf("notify.quiet: handled %v, %v", ok, err)
		}
		var res struct {
			Quiet bool `json:"quiet"`
		}
		if err := json.Unmarshal(out, &res); err != nil {
			t.Fatal(err)
		}
		if res.Quiet != h.push.quietNow() {
			t.Fatalf("the app call says %v, the pusher %v", res.Quiet, h.push.quietNow())
		}
		return res.Quiet
	}
	if ask() {
		t.Fatal("quiet with no device at all")
	}
	window := QuietHours{Enabled: true, Start: "23:00", End: "07:00", TZ: "UTC"}
	if err := h.push.setPrefs("tablet", NotifyPrefs{Input: true}); err != nil { // quiet hours never turned on
		t.Fatal(err)
	}
	if ask() {
		t.Fatal("quiet because of a device that has no quiet hours")
	}
	if err := h.push.setPrefs("phone", NotifyPrefs{Input: true, Quiet: window}); err != nil {
		t.Fatal(err)
	}
	if !ask() {
		t.Fatal("not quiet at 02:00 with the phone's window 23:00-07:00")
	}
	h.push.now = func() time.Time { return night.Add(10 * time.Hour) }
	if ask() {
		t.Fatal("quiet at 12:00")
	}
	// One more device whose window is on now: quiet again, for as long as it lasts.
	if err := h.push.setPrefs("watch", NotifyPrefs{Quiet: QuietHours{Enabled: true, Start: "11:00", End: "13:00", TZ: "UTC"}}); err != nil {
		t.Fatal(err)
	}
	if !ask() {
		t.Fatal("not quiet inside the watch's window")
	}
	// Turned off on the device: no longer counts.
	if err := h.push.setPrefs("watch", NotifyPrefs{Quiet: QuietHours{Enabled: false, Start: "11:00", End: "13:00", TZ: "UTC"}}); err != nil {
		t.Fatal(err)
	}
	if ask() {
		t.Fatal("quiet with the window turned off")
	}
}

// Each device's window is read in the device's own zone, else in the user's zone the hub knows (userLoc), else in UTC;
// and a window that starts before midnight holds at 23:30.
func TestQuietNowZones(t *testing.T) {
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Skip("no tzdata:", err)
	}
	at := func(hh, mm int) time.Time { return time.Date(2026, 11, 6, hh, mm, 0, 0, time.UTC) }
	for _, c := range []struct {
		name    string
		quiet   QuietHours
		userLoc *time.Location // the hub's user zone (nil: none known)
		now     time.Time
		want    bool
	}{
		// 15:30 UTC is 00:30 in Tokyo, 02:00 UTC is 11:00 there.
		{"the device's zone differs from the hub's: inside there", QuietHours{Enabled: true, Start: "23:00", End: "07:00", TZ: "Asia/Tokyo"}, time.UTC, at(15, 30), true},
		{"the device's zone differs from the hub's: outside there, though inside in the hub's", QuietHours{Enabled: true, Start: "23:00", End: "07:00", TZ: "Asia/Tokyo"}, time.UTC, at(2, 0), false},
		{"no zone on the device: the user's zone, inside", QuietHours{Enabled: true, Start: "23:00", End: "07:00"}, tokyo, at(15, 30), true},
		{"no zone on the device: the user's zone, outside though inside in UTC", QuietHours{Enabled: true, Start: "23:00", End: "07:00"}, tokyo, at(2, 0), false},
		{"no zone anywhere: UTC", QuietHours{Enabled: true, Start: "23:00", End: "07:00"}, nil, at(2, 0), true},
		{"half past 23, before midnight", QuietHours{Enabled: true, Start: "23:00", End: "07:00", TZ: "UTC"}, time.UTC, at(23, 30), true},
		{"half past 22, before the window", QuietHours{Enabled: true, Start: "23:00", End: "07:00", TZ: "UTC"}, time.UTC, at(22, 30), false},
	} {
		h := newTestHub(t)
		loc := c.userLoc
		h.push.userLoc = func() *time.Location { return loc }
		h.push.now = func() time.Time { return c.now }
		if err := h.push.setPrefs("phone", NotifyPrefs{Quiet: c.quiet}); err != nil {
			t.Fatal(err)
		}
		if got := h.push.quietNow(); got != c.want {
			t.Errorf("%s: quietNow = %v, want %v", c.name, got, c.want)
		}
	}
}

package hub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chyroc/jarvis/server/internal/proto"
)

func TestNotifyConfigDefaults(t *testing.T) {
	var c NotifyConfig
	if c.doneDelay() != time.Minute || c.longTurn() != time.Minute || c.backgroundHold() != 2*time.Hour {
		t.Fatalf("defaults: %v %v %v", c.doneDelay(), c.longTurn(), c.backgroundHold())
	}
	c = NotifyConfig{DoneDelaySeconds: 5, LongTurnSeconds: 300, BackgroundHoldMinutes: 10}
	if c.doneDelay() != 5*time.Second || c.longTurn() != 5*time.Minute || c.backgroundHold() != 10*time.Minute {
		t.Fatalf("configured: %v %v %v", c.doneDelay(), c.longTurn(), c.backgroundHold())
	}
}

func TestDevicePrefs(t *testing.T) {
	if p := devicePrefs(nil); p != (NotifyPrefs{Input: true, Done: DoneLong, LiveActivity: true, Island: IslandPrefs{Preset: "a"}}) {
		t.Fatalf("default %+v", p)
	}
	if p := devicePrefs(json.RawMessage(`{"input":false,"done":"always","live_activity":false,"lock_details":true}`)); p != (NotifyPrefs{Input: false, Done: DoneAlways, LockDetails: true, Island: IslandPrefs{Preset: "a"}}) {
		t.Fatalf("stored %+v", p)
	}
	// Only what was stored changes; nonsense falls back.
	if p := devicePrefs(json.RawMessage(`{"done":"sometimes"}`)); p != defaultPrefs() {
		t.Fatalf("unknown done %+v", p)
	}
	long := time.Minute
	for _, c := range []struct {
		done string
		took time.Duration
		want bool
	}{
		{DoneOff, time.Hour, false},
		{DoneLong, 59 * time.Second, false},
		{DoneLong, time.Minute, true},
		{DoneAlways, time.Second, true},
	} {
		if got := (NotifyPrefs{Done: c.done}).wantsDone(c.took, long); got != c.want {
			t.Errorf("%s %v: %v", c.done, c.took, got)
		}
	}
}

func TestTurnTracker(t *testing.T) {
	t0 := time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)
	at := func(sec int) time.Time { return t0.Add(time.Duration(sec) * time.Second) }
	done := proto.Session{Status: proto.StatusDone}
	hold := time.Hour

	t.Run("finished and left alone: push with the turn's length", func(t *testing.T) {
		tr := newTurnTracker()
		tr.working("k", at(0))
		gen := tr.ended("k", at(90))
		v, took := tr.settle("k", gen, done, false, false, false, at(150), hold)
		if v != finishPush || took != 90*time.Second {
			t.Fatalf("%v %v", v, took)
		}
		// Settled: a second check for the same end does nothing.
		if v, _ := tr.settle("k", gen, done, false, false, false, at(151), hold); v != finishStale {
			t.Fatalf("again: %v", v)
		}
	})

	t.Run("seen in herdr (idle) or in the app: no push", func(t *testing.T) {
		tr := newTurnTracker()
		tr.working("k", at(0))
		gen := tr.ended("k", at(90))
		if v, _ := tr.settle("k", gen, proto.Session{Status: proto.StatusIdle}, false, false, false, at(150), hold); v != finishDrop {
			t.Fatalf("idle: %v", v)
		}
		tr.working("k", at(200))
		gen = tr.ended("k", at(300))
		if v, _ := tr.settle("k", gen, done, true, false, false, at(360), hold); v != finishDrop {
			t.Fatalf("seen: %v", v)
		}
	})

	t.Run("resumed within the delay: same turn, counted from its first start", func(t *testing.T) {
		tr := newTurnTracker()
		tr.working("k", at(0))
		first := tr.ended("k", at(20))
		tr.working("k", at(30)) // status flickers between tool calls
		if v, _ := tr.settle("k", first, done, false, false, false, at(80), hold); v != finishStale {
			t.Fatalf("first end: %v", v)
		}
		gen := tr.ended("k", at(100))
		v, took := tr.settle("k", gen, done, false, false, false, at(160), hold)
		if v != finishPush || took != 100*time.Second {
			t.Fatalf("%v %v", v, took)
		}
	})

	t.Run("still working at check time: wait for the next end", func(t *testing.T) {
		tr := newTurnTracker()
		tr.working("k", at(0))
		gen := tr.ended("k", at(10))
		if v, _ := tr.settle("k", gen, proto.Session{Status: proto.StatusWorking}, false, false, false, at(70), hold); v != finishStale {
			t.Fatalf("%v", v)
		}
	})

	t.Run("background work: hold, then one push when the woken turn ends", func(t *testing.T) {
		tr := newTurnTracker()
		tr.working("k", at(0))
		gen := tr.ended("k", at(30))
		busy := proto.Session{Status: proto.StatusDone, Background: []proto.BackgroundTask{{ID: "b1", Kind: "shell"}}}
		if v, _ := tr.settle("k", gen, busy, false, false, false, at(90), hold); v != finishHold {
			t.Fatalf("hold: %v", v)
		}
		// The task reports back: Claude works on the report, then finishes.
		tr.working("k", at(600))
		gen = tr.ended("k", at(620))
		v, took := tr.settle("k", gen, proto.Session{Status: proto.StatusDone, WokeByTask: true}, false, false, false, at(680), hold)
		if v != finishPush || took != 620*time.Second {
			t.Fatalf("after background: %v %v", v, took)
		}
	})

	t.Run("running subagent counts as background", func(t *testing.T) {
		tr := newTurnTracker()
		tr.working("k", at(0))
		gen := tr.ended("k", at(30))
		s := proto.Session{Status: proto.StatusDone, Subagents: []proto.Subagent{{ID: "a", Running: true}}}
		if v, _ := tr.settle("k", gen, s, false, false, false, at(90), hold); v != finishHold {
			t.Fatalf("%v", v)
		}
	})

	t.Run("background that never finishes is given up", func(t *testing.T) {
		tr := newTurnTracker()
		tr.working("k", at(0))
		gen := tr.ended("k", at(30))
		busy := proto.Session{Status: proto.StatusDone, Background: []proto.BackgroundTask{{ID: "b1"}}}
		if v, _ := tr.settle("k", gen, busy, false, false, false, at(30).Add(2*hold), hold); v != finishDrop {
			t.Fatalf("%v", v)
		}
	})

	t.Run("a turn woken by a task on its own: no push", func(t *testing.T) {
		tr := newTurnTracker()
		tr.working("k", at(0))
		gen := tr.ended("k", at(120))
		if v, _ := tr.settle("k", gen, proto.Session{Status: proto.StatusDone, WokeByTask: true}, false, false, false, at(180), hold); v != finishDrop {
			t.Fatalf("%v", v)
		}
	})

	t.Run("muted, or the assistant already pushed: no push", func(t *testing.T) {
		tr := newTurnTracker()
		tr.working("k", at(0))
		gen := tr.ended("k", at(120))
		if v, _ := tr.settle("k", gen, done, false, true, false, at(180), hold); v != finishDrop {
			t.Fatalf("muted: %v", v)
		}
		tr.working("k", at(200))
		gen = tr.ended("k", at(400))
		if v, _ := tr.settle("k", gen, done, false, false, true, at(460), hold); v != finishDrop {
			t.Fatalf("assistant: %v", v)
		}
	})

	t.Run("sessions are independent", func(t *testing.T) {
		tr := newTurnTracker()
		tr.working("a", at(0))
		tr.working("b", at(50))
		ga := tr.ended("a", at(100))
		tr.ended("b", at(100))
		if v, took := tr.settle("a", ga, done, false, false, false, at(160), hold); v != finishPush || took != 100*time.Second {
			t.Fatalf("%v %v", v, took)
		}
	})
}

// Registration (every launch) must not wipe what the device chose; prefs can be set before it has a token.
func TestPusherPrefsSurviveRegistration(t *testing.T) {
	p := newPusher(t.TempDir(), nil, func(string, ...any) {})
	if err := p.setPrefs("d1", NotifyPrefs{Input: false, Done: DoneOff}); err != nil {
		t.Fatal(err)
	}
	p.register(Device{DeviceID: "d1", APNSToken: "tok"})
	if got := p.prefs("d1"); got != (NotifyPrefs{Input: false, Done: DoneOff, Island: IslandPrefs{Preset: "a"}}) {
		t.Fatalf("%+v", got)
	}
	if got := p.prefs("unknown"); got != defaultPrefs() {
		t.Fatalf("unknown device %+v", got)
	}
}

func TestDevicePrefsAPI(t *testing.T) {
	h := newTestHub(t)
	srv := httptest.NewServer(h.routes())
	defer srv.Close()
	do := func(method, body string) NotifyPrefs {
		t.Helper()
		req, _ := http.NewRequest(method, srv.URL+"/api/v1/devices/phone-1/prefs", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil || resp.StatusCode != 200 {
			t.Fatalf("%s: %v %v", method, err, resp.Status)
		}
		defer resp.Body.Close()
		var p NotifyPrefs
		_ = json.NewDecoder(resp.Body).Decode(&p)
		return p
	}
	if p := do("GET", ""); p != defaultPrefs() {
		t.Fatalf("default %+v", p)
	}
	req, _ := http.NewRequest("GET", srv.URL+"/api/v1/devices/phone-1/prefs", nil)
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var timings struct {
		Long  int `json:"long_turn_seconds"`
		Delay int `json:"done_delay_seconds"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&timings)
	resp.Body.Close()
	if timings.Long != 60 || timings.Delay != 60 {
		t.Fatalf("timings %+v", timings)
	}
	// A partial update keeps the other field.
	if p := do("PUT", `{"done":"always"}`); p != (NotifyPrefs{Input: true, Done: DoneAlways, LiveActivity: true, Island: IslandPrefs{Preset: "a"}}) {
		t.Fatalf("put %+v", p)
	}
	if p := do("PUT", `{"input":false}`); p != (NotifyPrefs{Input: false, Done: DoneAlways, LiveActivity: true, Island: IslandPrefs{Preset: "a"}}) {
		t.Fatalf("put input %+v", p)
	}
	if p := do("GET", ""); p != (NotifyPrefs{Input: false, Done: DoneAlways, LiveActivity: true, Island: IslandPrefs{Preset: "a"}}) {
		t.Fatalf("get after put %+v", p)
	}
	// The island is merged field by field like the rest.
	if p := do("PUT", `{"island":{"preset":"custom","left":"tpm"}}`); p.Island != (IslandPrefs{Preset: "custom", Left: "tpm"}) || p.Done != DoneAlways {
		t.Fatalf("put island %+v", p)
	}
	if p := do("PUT", `{"island":{"right":"load"}}`); p.Island != (IslandPrefs{Preset: "custom", Left: "tpm", Right: "load"}) {
		t.Fatalf("put island side %+v", p)
	}
}

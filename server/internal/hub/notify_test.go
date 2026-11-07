package hub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
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
	if p := devicePrefs(nil); !reflect.DeepEqual(p, NotifyPrefs{Input: true, Done: DoneLong, LiveActivity: true, Island: IslandPrefs{Preset: "a"}, MutedManagers: []string{}}) {
		t.Fatalf("default %+v", p)
	}
	if p := devicePrefs(json.RawMessage(`{"input":false,"done":"always","live_activity":false,"lock_details":true}`)); !reflect.DeepEqual(p, NotifyPrefs{Input: false, Done: DoneAlways, LockDetails: true, Island: IslandPrefs{Preset: "a"}, MutedManagers: []string{}}) {
		t.Fatalf("stored %+v", p)
	}
	// Only what was stored changes; nonsense falls back.
	if p := devicePrefs(json.RawMessage(`{"done":"sometimes"}`)); !reflect.DeepEqual(p, defaultPrefs()) {
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

// Registration (every launch) must not wipe what the device chose; prefs can be set before it has a token.
func TestPusherPrefsSurviveRegistration(t *testing.T) {
	p := newPusher(t.TempDir(), nil, func(string, ...any) {})
	if err := p.setPrefs("d1", NotifyPrefs{Input: false, Done: DoneOff}); err != nil {
		t.Fatal(err)
	}
	p.register(Device{DeviceID: "d1", APNSToken: "tok"})
	if got := p.prefs("d1"); !reflect.DeepEqual(got, NotifyPrefs{Input: false, Done: DoneOff, Island: IslandPrefs{Preset: "a"}, MutedManagers: []string{}}) {
		t.Fatalf("%+v", got)
	}
	if got := p.prefs("unknown"); !reflect.DeepEqual(got, defaultPrefs()) {
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
	if p := do("GET", ""); !reflect.DeepEqual(p, defaultPrefs()) {
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
	if p := do("PUT", `{"done":"always"}`); !reflect.DeepEqual(p, NotifyPrefs{Input: true, Done: DoneAlways, LiveActivity: true, Island: IslandPrefs{Preset: "a"}, MutedManagers: []string{}}) {
		t.Fatalf("put %+v", p)
	}
	if p := do("PUT", `{"input":false}`); !reflect.DeepEqual(p, NotifyPrefs{Input: false, Done: DoneAlways, LiveActivity: true, Island: IslandPrefs{Preset: "a"}, MutedManagers: []string{}}) {
		t.Fatalf("put input %+v", p)
	}
	if p := do("GET", ""); !reflect.DeepEqual(p, NotifyPrefs{Input: false, Done: DoneAlways, LiveActivity: true, Island: IslandPrefs{Preset: "a"}, MutedManagers: []string{}}) {
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

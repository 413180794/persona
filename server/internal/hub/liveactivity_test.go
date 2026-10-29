package hub

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chyroc/jarvis/server/internal/db"
	"github.com/chyroc/jarvis/server/internal/proto"
)

func TestBuildFleet(t *testing.T) {
	t0 := time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)
	agent := func(id, state string, mod func(*proto.Session)) fleetInput {
		s := proto.Session{MachineID: "m", SessionID: id, Title: "agent " + id, State: state, UpdatedAt: t0, SpaceName: "web", SlotName: "2", SlotIndex: 2}
		if mod != nil {
			mod(&s)
		}
		return fleetInput{Machine: "box", Session: s}
	}
	agents := []fleetInput{
		agent("w1", proto.StateWorking, nil),
		agent("w2", proto.StateWorking, nil),
		agent("p", proto.StateNeedsYou, nil),
		agent("u", proto.StateUnread, func(s *proto.Session) { s.LastText = "All tests pass.\nDetails…" }),
		agent("idle", proto.StateIdle, nil),
		agent("off", proto.StateOffline, nil),
		agent("loop", proto.StateUnread, func(s *proto.Session) { s.Loop = &proto.LoopInfo{Kind: "wakeup"} }),
		agent("asst", proto.StateWorking, func(s *proto.Session) { s.Assistant = true }),
	}
	agents[0].TurnStart = t0.Add(-5 * time.Minute) // w1 runs longer
	agents[1].TurnStart = t0.Add(-1 * time.Minute)
	muted := agent("muted", proto.StateWorking, nil)
	muted.Muted = true
	agents = append(agents, muted)
	ixs := []proto.Interaction{{ID: "ix1", MachineID: "m", SessionID: "p", Kind: proto.IxPermission, Summary: "Bash: go test ./...", CreatedAt: t0}}

	st := buildFleet(agents, ixs)
	if st.Phase != "needsYou" || st.NeedsYou != 1 || st.Working != 2 || st.Unread != 1 {
		t.Fatalf("counts %+v", st)
	}
	if len(st.Items) != fleetMaxItems {
		t.Fatalf("items %d", len(st.Items))
	}
	first := st.Items[0]
	if first.ID != "m/p" || first.Kind != proto.IxPermission || first.Interaction != "ix1" || first.Detail != "Bash: go test ./..." {
		t.Fatalf("first %+v", first)
	}
	if first.Place != "box · web" { // slot "2" is just the index: not repeated
		t.Fatalf("place %q", first.Place)
	}
	if st.Items[1].ID != "m/w1" || st.Items[2].ID != "m/w2" {
		t.Fatalf("working order %+v", st.Items)
	}
	if st.WorkingSince != float64(t0.Add(-5*time.Minute).Unix()) {
		t.Fatalf("working since %v", st.WorkingSince)
	}

	// Without the request and the workers: only the finished one, its first line.
	st = buildFleet([]fleetInput{agents[3], agents[4]}, nil)
	if st.Phase != "done" || len(st.Items) != 1 || st.Items[0].Detail != "All tests pass." {
		t.Fatalf("done %+v", st)
	}
	if st := buildFleet([]fleetInput{agents[4], agents[5], agents[6], agents[7], muted}, nil); !st.empty() || len(st.Items) != 0 {
		t.Fatalf("should be empty: %+v", st)
	}
	// A blocked agent with no interaction yet counts as a terminal prompt.
	st = buildFleet([]fleetInput{agent("b", proto.StateNeedsYou, nil)}, nil)
	if st.Phase != "needsYou" || st.Items[0].Kind != proto.IxScreen {
		t.Fatalf("screen %+v", st)
	}
	// An interaction on a muted agent is left out.
	if st := buildFleet([]fleetInput{muted}, []proto.Interaction{{ID: "x", MachineID: "m", SessionID: "muted", Kind: proto.IxPlan}}); !st.empty() {
		t.Fatalf("muted interaction %+v", st)
	}
}

// The JSON is the Swift ContentState: field names and Unix-second times must not drift.
func TestFleetStateJSON(t *testing.T) {
	st := FleetState{Phase: "working", Working: 1, WorkingSince: 1761700000, Items: []FleetItem{{ID: "m/s", Title: "api", Kind: "working", Since: 1761700000}}}
	b, _ := json.Marshal(st)
	want := `{"phase":"working","needsYou":0,"working":1,"unread":0,"items":[{"id":"m/s","title":"api","place":"","kind":"working","detail":"","since":1761700000}],"workingSince":1761700000,"details":false}`
	if string(b) != want {
		t.Fatalf("got  %s\nwant %s", b, want)
	}
}

func TestCoalescer(t *testing.T) {
	var c coalescer
	working := FleetState{Phase: "working", Working: 1, Items: []FleetItem{{ID: "m/a", Kind: "working"}}}
	if now, sched := c.offer(working); now || !sched {
		t.Fatalf("first change should wait: %v %v", now, sched)
	}
	if now, sched := c.offer(working); now || sched {
		t.Fatalf("already scheduled: %v %v", now, sched)
	}
	c.sentState(working)
	if now, sched := c.offer(working); now || sched {
		t.Fatalf("unchanged: %v %v", now, sched)
	}
	asks := working
	asks.Phase, asks.NeedsYou = "needsYou", 1
	asks.Items = append([]FleetItem{{ID: "m/b", Kind: proto.IxPermission, Interaction: "ix1"}}, asks.Items...)
	if now, _ := c.offer(asks); !now {
		t.Fatal("a new request goes out at once")
	}
	c.sentState(asks)
	// The same request with the worker gone: an ordinary change.
	less := asks
	less.Working, less.Items = 0, asks.Items[:1]
	if now, sched := c.offer(less); now || !sched {
		t.Fatalf("no new request: %v %v", now, sched)
	}
}

func TestLiveActivityPayload(t *testing.T) {
	now := time.Unix(1761700000, 0)
	st := FleetState{Phase: "needsYou", NeedsYou: 2, Working: 3}
	start := liveActivityPayload("start", st, now)["aps"].(map[string]any)
	if start["event"] != "start" || start["attributes-type"] != "JarvisActivityAttributes" || start["timestamp"] != now.Unix() {
		t.Fatalf("start %v", start)
	}
	if start["alert"].(map[string]any)["body"] != "2 个需要你 · 3 个在工作" {
		t.Fatalf("alert %v", start["alert"])
	}
	if start["stale-date"] != now.Add(fleetStaleIn).Unix() || start["relevance-score"] != 1.0 {
		t.Fatalf("stale / relevance %v", start)
	}
	update := liveActivityPayload("update", st, now)["aps"].(map[string]any)
	if _, ok := update["alert"]; ok {
		t.Fatal("updates do not alert: the interaction push already did")
	}
	if _, ok := update["category"]; ok {
		t.Fatal("no category on activity pushes")
	}
	end := liveActivityPayload("end", st, now)["aps"].(map[string]any)
	if end["dismissal-date"] != now.Unix() {
		t.Fatalf("end %v", end)
	}
}

// fakeAPNs records what the hub sends.
type fakeAPNs struct {
	mu   sync.Mutex
	got  []apnsCall
	gone map[string]bool // tokens answered with 410
}

type apnsCall struct {
	Token, Topic, Type, Priority, Event string
	State                               FleetState
}

func (f *fakeAPNs) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	var body struct {
		APS struct {
			Event string     `json:"event"`
			State FleetState `json:"content-state"`
		} `json:"aps"`
	}
	_ = json.Unmarshal(b, &body)
	token := strings.TrimPrefix(r.URL.Path, "/3/device/")
	f.mu.Lock()
	f.got = append(f.got, apnsCall{token, r.Header.Get("apns-topic"), r.Header.Get("apns-push-type"), r.Header.Get("apns-priority"), body.APS.Event, body.APS.State})
	gone := f.gone[token]
	f.mu.Unlock()
	if gone {
		w.WriteHeader(410)
		_, _ = w.Write([]byte(`{"reason":"Unregistered"}`))
	}
}

func (f *fakeAPNs) calls(t *testing.T, n int) []apnsCall {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		if len(f.got) >= n {
			out := append([]apnsCall{}, f.got...)
			f.mu.Unlock()
			return out
		}
		f.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	t.Fatalf("want %d APNs calls, got %d: %+v", n, len(f.got), f.got)
	return nil
}

func testLive(t *testing.T) (*Hub, *fakeAPNs) {
	h := newTestHub(t)
	f := &fakeAPNs{gone: map[string]bool{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	h.push.cfg, h.push.key, h.push.base = &apnsConfig{KeyID: "K", TeamID: "T", Topic: "app.example"}, key, srv.URL
	h.push.register(Device{DeviceID: "phone", Name: "phone", APNSToken: "devtok", Topic: "app.example"})
	return h, f
}

func TestLiveActivityFlush(t *testing.T) {
	h, f := testLive(t)
	la := h.live
	working := FleetState{Phase: "working", Working: 1, Items: []FleetItem{{ID: "m/a", Title: "api", Kind: "working"}}}

	// Only a push-to-start token: start once per episode.
	la.update("phone", func(v *db.LiveActivity) { v.PushToStartToken = "p2s" })
	la.flush(working, false)
	la.flush(working, false)
	c := f.calls(t, 1)
	if c[0].Token != "p2s" || c[0].Event != "start" || c[0].Type != "liveactivity" || c[0].Topic != "app.example.push-type.liveactivity" {
		t.Fatalf("start %+v", c[0])
	}
	if c[0].State.Details {
		t.Fatal("lock screen details are off by default")
	}

	// The app reports the started activity's token: updates go there, the device's settings decide the details.
	la.update("phone", func(v *db.LiveActivity) { v.ActivityID, v.PushToken, v.StartedAt = "act1", "acttok", time.Now() })
	if err := h.push.setPrefs("phone", NotifyPrefs{Input: true, Done: DoneLong, LiveActivity: true, LockDetails: true}); err != nil {
		t.Fatal(err)
	}
	la.flush(working, true)
	c = f.calls(t, 2)
	if c[1].Token != "acttok" || c[1].Event != "update" || c[1].Priority != "10" || !c[1].State.Details {
		t.Fatalf("update %+v", c[1])
	}

	// Empty for a while: end, and forget the activity token.
	la.flush(FleetState{}, false)
	la.mu.Lock()
	la.emptyAt = time.Now().Add(-2 * fleetEndAfter)
	la.mu.Unlock()
	la.tick()
	c = f.calls(t, 3)
	if c[2].Token != "acttok" || c[2].Event != "end" {
		t.Fatalf("end %+v", c[2])
	}
	if r := la.rows["phone"]; r.PushToken != "" || r.PushToStartToken != "p2s" {
		t.Fatalf("row after end %+v", r)
	}

	// A new episode starts again; turning the activity off stops everything.
	la.flush(working, false)
	f.calls(t, 4)
	_ = h.push.setPrefs("phone", NotifyPrefs{Input: true, Done: DoneLong, LiveActivity: false})
	la.flush(FleetState{}, false)
	la.flush(working, false)
	time.Sleep(50 * time.Millisecond)
	if n := len(f.calls(t, 4)); n != 4 {
		t.Fatalf("activity off still pushed: %d", n)
	}
}

func TestLiveActivityDeadToken(t *testing.T) {
	h, f := testLive(t)
	f.gone["acttok"] = true
	h.live.update("phone", func(v *db.LiveActivity) { v.ActivityID, v.PushToken, v.StartedAt = "act1", "acttok", time.Now() })
	h.live.flush(FleetState{Phase: "working", Working: 1}, false)
	f.calls(t, 1)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		h.live.mu.Lock()
		tok := h.live.rows["phone"].PushToken
		h.live.mu.Unlock()
		if tok == "" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("a 410 should forget the activity token")
}

func TestLiveActivityTokenAPI(t *testing.T) {
	h := newTestHub(t)
	srv := httptest.NewServer(h.routes())
	defer srv.Close()
	post := func(body string) {
		t.Helper()
		req, _ := http.NewRequest("POST", srv.URL+"/api/v1/devices/phone/live-activity", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil || resp.StatusCode != 200 {
			t.Fatalf("%v %v", err, resp.Status)
		}
		resp.Body.Close()
	}
	post(`{"push_to_start_token":"p2s"}`)
	post(`{"activity_id":"a1","push_token":"t1"}`)
	r := h.live.rows["phone"]
	if r.PushToStartToken != "p2s" || r.ActivityID != "a1" || r.PushToken != "t1" || r.StartedAt.IsZero() {
		t.Fatalf("%+v", r)
	}
	post(`{"activity_id":"a1","push_token":"t2"}`) // rotated
	if h.live.rows["phone"].PushToken != "t2" {
		t.Fatal("rotation")
	}
	post(`{"activity_id":"old","ended":true}`) // an older activity ending must not drop the current one
	if h.live.rows["phone"].PushToken != "t2" {
		t.Fatal("ended an unrelated activity")
	}
	post(`{"activity_id":"a1","ended":true}`)
	if r := h.live.rows["phone"]; r.PushToken != "" || r.PushToStartToken != "p2s" {
		t.Fatalf("after end %+v", r)
	}
}

// TestManualLiveActivityPush sends one real update through APNs (sandbox) with this machine's ~/.jarvis/apns.json, to
// check a running activity end to end (docs/testing.md). Opt-in: JARVIS_LA_TOKEN=<activity push token>
// JARVIS_LA_TOPIC=<app bundle id>.
func TestManualLiveActivityPush(t *testing.T) {
	token, topic := os.Getenv("JARVIS_LA_TOKEN"), os.Getenv("JARVIS_LA_TOPIC")
	if token == "" || topic == "" {
		t.Skip("set JARVIS_LA_TOKEN and JARVIS_LA_TOPIC")
	}
	p := newPusher(dataDir(), nil, t.Logf)
	if p.key == nil {
		t.Skip("no APNs key here")
	}
	now := time.Now()
	st := FleetState{Phase: "needsYou", NeedsYou: 1, Working: 1, WorkingSince: float64(now.Add(-100 * time.Second).Unix()), Details: true,
		Items: []FleetItem{{ID: "m/s9", Title: "APNs 推送", Place: "测试", Kind: proto.IxPermission, Detail: "Bash: echo hi", Interaction: "ix9", Since: float64(now.Unix())}}}
	done := make(chan struct{})
	p.logf = func(f string, a ...any) { t.Logf(f, a...) }
	p.sendLiveActivity(Device{Name: "simulator", APNSEnv: "sandbox", Topic: topic}, token, liveActivityPayload("update", st, now), "10", func() { t.Error("token gone") })
	go func() { time.Sleep(3 * time.Second); close(done) }()
	<-done
}

// Turning lock screen details (or the activity) on / off changes what each device gets though the fleet did not:
// the next signal must push anyway.
func TestPrefsChangeResends(t *testing.T) {
	h := newTestHub(t)
	srv := httptest.NewServer(h.routes())
	defer srv.Close()
	req, _ := http.NewRequest("PUT", srv.URL+"/api/v1/devices/phone/prefs", strings.NewReader(`{"lock_details":true}`))
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("%v %v", err, resp.Status)
	}
	resp.Body.Close()
	h.live.mu.Lock()
	resend := h.live.resend
	h.live.mu.Unlock()
	if !resend {
		t.Fatal("a prefs change should force the next fleet push")
	}
	if p := h.push.prefs("phone"); !p.LockDetails || !p.LiveActivity {
		t.Fatalf("%+v", p)
	}
}

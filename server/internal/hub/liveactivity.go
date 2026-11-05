package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/i18n"
)

// The "Fleet" Live Activity (docs/live-activity.md): one activity per phone, on the lock screen and in the Dynamic
// Island, summing up every agent that needs you, is working, or finished unseen. The hub computes the state
// (FleetState, the activity's ContentState) from what it already knows and pushes it over APNs (push type
// liveactivity) to each device's activity token; push-to-start opens one when the app is not running.
//
// Budget: Apple throttles activity updates (numbers unpublished), and each one redraws the island and the lock screen
// on the phone. So what needs the user (a request arriving, or answered) goes out at once; anything else is merged
// for fleetCoalesce and reaches a device at most every fleetMinGap; the timer (working since…) runs on the phone; a
// phone whose app is open draws the activity itself from the fleet events and gets no push; and nothing is sent
// while nothing changes except a refresh before the content goes stale (docs/live-activity.md「推送节奏」).

const (
	fleetCoalesce = 2500 * time.Millisecond
	// fleetMinGap: an ordinary change (counts, titles, the assistant, an agent done) reaches a device at most this
	// often, so a device's activity is at most fleetMinGap + fleetCoalesce behind the hub (TestFleetPacingBound).
	// On the user's hub (2026-11-02) the fleet changed ~180 times an hour, each one a push.
	fleetMinGap     = 30 * time.Second
	fleetStaleIn    = 15 * time.Minute // stale-date: buttons disappear if the hub goes quiet this long
	fleetRefresh    = 10 * time.Minute // resend unchanged content before it goes stale
	fleetEndAfter   = time.Minute      // empty this long: end the activity
	fleetDoneLinger = 5 * time.Minute  // only finished agents left: keep showing them this long
	fleetMaxItems   = 3
	fleetMaxAge     = 8 * time.Hour // the system ends an activity after about this long; past it, start a new one
)

// forDevice is what one device gets: its settings and persona, and of the assistant and the numbers only what it
// shows (so a change it does not show costs no push).
func forDevice(s FleetState, p NotifyPrefs, stats FleetStats) FleetState {
	d := s
	d.Details = p.LockDetails
	d.Look = p.Look
	d.Left, d.Right = p.Island.sides()
	shows := func(slot string) bool { return d.Left == slot || d.Right == slot }
	if !shows(SlotAssistant) {
		d.Assistant = false
	}
	var ds FleetStats
	if shows(SlotTPM) {
		ds.TPM = stats.TPM
	}
	if shows(SlotTokens) {
		ds.Tokens = stats.Tokens
	}
	if shows(SlotRequests) {
		ds.Requests = stats.Requests
	}
	if shows(SlotLoad) {
		ds.Loads = pickLoad(stats.Loads, p.Island.Machine)
	}
	if ds.TPM != 0 || ds.Tokens != 0 || ds.Requests != 0 || ds.Loads != nil {
		d.Stats = &ds
	} else {
		d.Stats = nil
	}
	return d
}

// pickLoad: the chosen machine, or else the busiest.
func pickLoad(loads []FleetLoad, machine string) []FleetLoad {
	var best *FleetLoad
	for i := range loads {
		if loads[i].ID == machine {
			return []FleetLoad{loads[i]}
		}
		if best == nil || loads[i].Percent > best.Percent {
			best = &loads[i]
		}
	}
	if best == nil {
		return nil
	}
	return []FleetLoad{*best}
}

func fleetEmpty(s FleetState) bool { return s.Phase == "" }

// needsYouIDs: the items waiting for the user, to tell a new one (push now) from anything else (merge).
func needsYouIDs(s FleetState) map[string]bool {
	out := map[string]bool{}
	for _, it := range s.Items {
		if it.Kind != "working" && it.Kind != "unread" {
			out[it.ID+"#"+it.Interaction] = true
		}
	}
	return out
}

func fleetRelevance(s FleetState) float64 {
	switch s.Phase {
	case "needsYou":
		return 1
	case "working":
		return 0.5
	}
	return 0.2
}

// coalescer decides when a new fleet state goes out: a change in what needs the user (a request arriving, or answered
// or gone) at once, anything else after fleetCoalesce (more changes in the meantime ride along).
type coalescer struct {
	sent    []byte          // last state pushed
	needs   map[string]bool // its needs-you items
	pending bool
}

// offer returns whether to push now, or else whether a delayed flush should be scheduled.
func (c *coalescer) offer(st FleetState) (now, schedule bool) {
	b, _ := json.Marshal(st)
	if bytes.Equal(b, c.sent) {
		c.pending = false
		return false, false
	}
	if needs := needsYouIDs(st); !maps.Equal(needs, c.needs) && (len(needs) > 0 || len(c.needs) > 0) {
		return true, false
	}
	if c.pending {
		return false, false
	}
	c.pending = true
	return false, true
}

func (c *coalescer) sentState(st FleetState) {
	c.sent, _ = json.Marshal(st)
	c.needs = needsYouIDs(st)
	c.pending = false
}

// ---- the manager ----

type liveActivities struct {
	h      *Hub
	signal chan struct{}

	mu        sync.Mutex
	rows      map[string]db.LiveActivity // device -> its activity
	coal      coalescer
	shown     []byte // last state broadcast to the open apps
	resend    bool   // a device's settings changed: push even if the state did not
	last      FleetState
	lastPush  time.Time
	emptyAt   time.Time // since when the fleet has been over (empty, or only finished agents for a while)
	doneSince time.Time
	episode   int                  // bumps each time the fleet becomes active after being over
	started   map[string]int       // device -> episode it was push-started in (once per episode)
	sent      map[string][]byte    // device -> the content-state it last got
	sentAt    map[string]time.Time // device -> when
	stats     []byte               // last numbers broadcast to the open apps
	// The last fleet broadcast to the open apps, and when: what an app that closed its connection cleanly (gone to
	// the background) left its activity showing (phoneLeft).
	shownState FleetState
	shownStats FleetStats
	shownAt    time.Time
	repair     bool // a device's activity shows something unknown: flush without waiting for a change
	// wake: when the run loop's timer goes off next (zero: not set), for a merge or a device's turn after fleetMinGap.
	wake   time.Time
	now    func() time.Time
	minGap time.Duration // fleetMinGap (0 in the replay of how it was before, pushreplay_test.go)
}

func newLiveActivities(h *Hub) *liveActivities {
	return &liveActivities{h: h, signal: make(chan struct{}, 1), rows: map[string]db.LiveActivity{}, started: map[string]int{},
		sent: map[string][]byte{}, sentAt: map[string]time.Time{}, now: time.Now, minGap: fleetMinGap}
}

func (la *liveActivities) load(ctx context.Context) {
	if la.h.store == nil {
		return
	}
	list, err := la.h.store.LiveActivities(ctx)
	if err != nil {
		la.h.logf("load live activities: %v", err)
		return
	}
	la.mu.Lock()
	for _, v := range list {
		la.rows[v.DeviceID] = v
	}
	la.mu.Unlock()
}

// changed is called from broadcast (with the hub lock held): never block.
func (la *liveActivities) changed() {
	if la == nil {
		return
	}
	select {
	case la.signal <- struct{}{}:
	default:
	}
}

// settingsChanged: a device turned the activity or its lock screen details on / off.
func (la *liveActivities) settingsChanged() {
	la.mu.Lock()
	la.resend = true
	la.mu.Unlock()
	la.changed()
}

// snapshot is the fleet as the manager worked it out last (fleet.live, fleet_link.go): it stays while the manager is
// away.
func (la *liveActivities) snapshot() FleetState {
	st, _ := la.h.fleetLive()
	return st
}

// The activity's content and numbers are the manager's (assistant/fleet_live.go): the hub gives each device its part
// and pushes it.
type (
	FleetState = assistantwire.FleetState
	FleetLook  = assistantwire.FleetLook
	FleetStats = assistantwire.FleetStats
	FleetLoad  = assistantwire.FleetLoad
	FleetItem  = assistantwire.FleetItem
)

// fleetStats are the island's numbers as the manager told them last.
func (h *Hub) fleetStats() FleetStats {
	_, stats := h.fleetLive()
	return stats
}

func (la *liveActivities) run(ctx context.Context) {
	la.load(ctx)
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-la.signal:
			la.mu.Lock()
			resend, repair := la.resend, la.repair
			la.resend, la.repair = false, false
			la.mu.Unlock()
			st := la.snapshot()
			if b, _ := json.Marshal(st); !bytes.Equal(b, la.shown) { // the open app updates its activity itself
				la.shown = b
				la.broadcast(st)
			}
			if resend {
				// A device's settings changed (details on the lock screen, the island's sides): show it now.
				la.coal.sent = nil
				la.wakeAt(timer, la.flush(st, false, true))
				continue
			}
			now, schedule := la.coal.offer(st)
			switch {
			case now:
				la.wakeAt(timer, la.flush(st, true, false))
			case schedule:
				la.wakeAt(timer, la.now().Add(fleetCoalesce))
			case repair:
				la.wakeAt(timer, la.flush(st, false, false))
			}
		case <-timer.C:
			la.wake = time.Time{}
			la.wakeAt(timer, la.flush(la.snapshot(), false, false))
		case <-tick.C:
			la.tick()
		}
	}
}

// wakeAt sets the run loop's timer for t, unless it goes off sooner already (zero t: nothing to do).
func (la *liveActivities) wakeAt(timer *time.Timer, t time.Time) {
	if t.IsZero() || (!la.wake.IsZero() && !t.Before(la.wake)) {
		return
	}
	la.wake = t
	timer.Reset(max(t.Sub(la.now()), time.Millisecond))
}

// broadcast sends the open apps the fleet with every number (each app picks what its own settings show).
func (la *liveActivities) broadcast(st FleetState) {
	stats := la.h.fleetStats()
	la.mu.Lock()
	la.shownState, la.shownStats, la.shownAt = st, stats, la.now()
	la.mu.Unlock()
	st.Stats = &stats
	la.stats, _ = json.Marshal(stats)
	la.h.broadcast(map[string]any{"type": "fleet", "fleet": st})
}

// tick: end an activity that has been over for a while, refresh one about to go stale, update the numbers.
func (la *liveActivities) tick() {
	if la.h.isMember() { // the center's Live Activity is the group's (mesh_group.go)
		return
	}
	now := la.now()
	la.mu.Lock()
	over := !la.emptyAt.IsZero() && now.Sub(la.emptyAt) >= fleetEndAfter
	stale := !fleetEmpty(la.last) && now.Sub(la.lastPush) >= fleetRefresh
	doneLong := la.last.Phase == "done" && !la.doneSince.IsZero() && now.Sub(la.doneSince) >= fleetDoneLinger
	last := la.last
	la.mu.Unlock()
	switch {
	case over || doneLong:
		la.endAll()
	case stale:
		la.flush(la.snapshot(), false, true)
	case !fleetEmpty(last):
		la.metrics(last, now)
	}
}

// metrics pushes the numbers alone: low priority, at most every live_metrics_seconds per device, and only to devices
// whose shown (rounded) numbers changed.
func (la *liveActivities) metrics(last FleetState, now time.Time) {
	stats := la.h.fleetStats()
	if b, _ := json.Marshal(stats); !bytes.Equal(b, la.stats) {
		la.broadcast(last)
	}
	every := la.h.cfg.Notify.liveMetrics()
	la.mu.Lock()
	rows := make([]db.LiveActivity, 0, len(la.rows))
	for _, r := range la.rows {
		if r.PushToken != "" && now.Sub(r.StartedAt) < fleetMaxAge && now.Sub(la.sentAt[r.DeviceID]) >= every {
			rows = append(rows, r)
		}
	}
	la.mu.Unlock()
	for _, r := range rows {
		d, ok := la.h.push.device(r.DeviceID)
		if !ok || !devicePrefs(d.Prefs).LiveActivity || la.h.phoneOpen(r.DeviceID) {
			continue // an open app has every number from the fleet event
		}
		dst := forDevice(last, devicePrefs(d.Prefs), stats)
		if la.markSent(r.DeviceID, dst, now, false) {
			la.send(d, r, liveActivityPayload("update", dst, now), false)
		}
	}
}

// notYet says whether dst, a change for device, waits for its turn (fleetMinGap after its last push), and until when.
// What needs the user, a forced resend, and a device whose activity shows something unknown go now.
func (la *liveActivities) notYet(device string, dst FleetState, now time.Time, urgent bool) (due time.Time, wait bool) {
	if urgent {
		return time.Time{}, false
	}
	b, _ := json.Marshal(dst)
	la.mu.Lock()
	defer la.mu.Unlock()
	at, ok := la.sentAt[device]
	if !ok || bytes.Equal(b, la.sent[device]) {
		return time.Time{}, false
	}
	due = at.Add(la.minGap)
	return due, now.Before(due)
}

// phoneLeft: the app on device closed its connection. Gone to the background (a clean close), its activity shows the
// last fleet it was sent, which is what the device has now. Otherwise (the network dropped, the app was killed or
// suspended with the socket open) what it shows is unknown, and so is it when that fleet is old enough to have gone
// stale: the current state goes out at once, without waiting for a change or fleetMinGap.
func (la *liveActivities) phoneLeft(device string, clean bool) {
	if la == nil || device == "" {
		return
	}
	d, ok := la.h.push.device(device)
	now := la.now()
	la.mu.Lock()
	if clean && ok && !la.shownAt.IsZero() && now.Sub(la.shownAt) < fleetRefresh {
		b, _ := json.Marshal(forDevice(la.shownState, devicePrefs(d.Prefs), la.shownStats))
		la.sent[device], la.sentAt[device] = b, la.shownAt
	} else {
		delete(la.sent, device)
		delete(la.sentAt, device)
		la.repair = true
	}
	la.mu.Unlock()
	la.changed()
}

// markSent records dst as the device's content-state and says whether it is new (always, when forced).
func (la *liveActivities) markSent(device string, dst FleetState, now time.Time, force bool) bool {
	b, _ := json.Marshal(dst)
	la.mu.Lock()
	defer la.mu.Unlock()
	if !force && bytes.Equal(b, la.sent[device]) {
		return false
	}
	la.sent[device], la.sentAt[device] = b, now
	return true
}

// flush pushes st to every device that wants the activity: update a running one, or start one (once per episode).
// A device whose own version did not change (the change was something it does not show) is skipped unless forced; one
// whose app is open is not pushed (it draws the activity itself); one pushed less than fleetMinGap ago gets an ordinary
// change later: next is when the first of those is due (zero: none).
func (la *liveActivities) flush(st FleetState, urgent, force bool) (next time.Time) {
	if la.h.isMember() { // the center pushes the group's Live Activity; a member's devices are the center's now
		return time.Time{}
	}
	la.coal.sentState(st)
	now := la.now()
	la.mu.Lock()
	wasOver := fleetEmpty(la.last) || la.last.Phase == "done"
	if fleetEmpty(st) {
		if la.emptyAt.IsZero() {
			la.emptyAt = now
		}
	} else {
		la.emptyAt = time.Time{}
		if wasOver && st.Phase != "done" {
			la.episode++
		}
	}
	if st.Phase == "done" {
		if la.doneSince.IsZero() {
			la.doneSince = now
		}
	} else {
		la.doneSince = time.Time{}
	}
	la.last, la.lastPush = st, now
	episode := la.episode
	rows := make([]db.LiveActivity, 0, len(la.rows))
	for _, r := range la.rows {
		rows = append(rows, r)
	}
	la.mu.Unlock()
	if fleetEmpty(st) {
		return time.Time{} // ended by tick after fleetEndAfter, unless something comes back first
	}
	stats := la.h.fleetStats()
	for _, r := range rows {
		d, ok := la.h.push.device(r.DeviceID)
		if !ok {
			continue
		}
		prefs := devicePrefs(d.Prefs)
		if !prefs.LiveActivity {
			continue
		}
		dst := forDevice(st, prefs, stats)
		switch {
		case r.PushToken != "" && now.Sub(r.StartedAt) < fleetMaxAge:
			if la.h.phoneOpen(r.DeviceID) {
				la.markSent(r.DeviceID, dst, now, false) // what its app just drew from the fleet event
				continue
			}
			if due, wait := la.notYet(r.DeviceID, dst, now, urgent || force); wait {
				if next.IsZero() || due.Before(next) {
					next = due
				}
				continue
			}
			if la.markSent(r.DeviceID, dst, now, force) {
				la.send(d, r, liveActivityPayload("update", dst, now), urgent)
			}
		case r.PushToStartToken != "" && st.Phase != "done":
			la.mu.Lock()
			already := la.started[r.DeviceID] == episode
			la.started[r.DeviceID] = episode
			la.mu.Unlock()
			if !already {
				la.markSent(r.DeviceID, dst, now, true)
				// 手机静音 while a desktop is online: the activity still starts, without its alert (desktops.go).
				quiet := prefs.MuteWhileDesktop && la.h.push.desktopOnline != nil && la.h.push.desktopOnline()
				la.sendStart(d, r, startPayloadIn(la.h.userLang(), dst, now, quiet))
			}
		}
	}
	return next
}

func (la *liveActivities) endAll() {
	now := time.Now()
	la.mu.Lock()
	rows := make([]db.LiveActivity, 0, len(la.rows))
	for _, r := range la.rows {
		if r.PushToken != "" {
			rows = append(rows, r)
		}
	}
	la.emptyAt, la.doneSince = time.Time{}, time.Time{}
	last := la.last
	la.last = FleetState{}
	la.sent, la.sentAt = map[string][]byte{}, map[string]time.Time{}
	la.mu.Unlock()
	for _, r := range rows {
		if d, ok := la.h.push.device(r.DeviceID); ok {
			la.send(d, r, liveActivityPayload("end", last, now), false)
		}
		la.update(r.DeviceID, func(v *db.LiveActivity) { v.PushToken, v.ActivityID = "", "" })
	}
}

// liveActivityPayload is the APNs body for one event. No `category`, no sound: the interaction push already rang.
func liveActivityPayload(event string, st FleetState, now time.Time) map[string]any {
	return liveActivityPayloadIn(i18n.Default, event, st, now)
}

// liveActivityPayloadIn is liveActivityPayload with a start's alert in l.
func liveActivityPayloadIn(l i18n.Lang, event string, st FleetState, now time.Time) map[string]any {
	aps := map[string]any{"timestamp": now.Unix(), "event": event, "content-state": st}
	switch event {
	case "end":
		aps["dismissal-date"] = now.Unix()
	default:
		aps["stale-date"] = now.Add(fleetStaleIn).Unix()
		aps["relevance-score"] = fleetRelevance(st)
	}
	if event == "start" {
		aps["attributes-type"] = "JarvisActivityAttributes"
		aps["attributes"] = map[string]any{"kind": "fleet"}
		aps["alert"] = map[string]any{"title": "Persona", "body": fleetSentence(l, st)}
	}
	return map[string]any{"aps": aps}
}

// startPayload is a push-to-start; quiet drops its alert (the phone is muted while a desktop is online).
func startPayload(st FleetState, now time.Time, quiet bool) map[string]any {
	return startPayloadIn(i18n.Default, st, now, quiet)
}

func startPayloadIn(l i18n.Lang, st FleetState, now time.Time, quiet bool) map[string]any {
	p := liveActivityPayloadIn(l, "start", st, now)
	if quiet {
		delete(p["aps"].(map[string]any), "alert")
	}
	return p
}

// fleetSentence: "2 个需要你 · 3 个在工作", the alert of a push-started activity.
func fleetSentence(l i18n.Lang, st FleetState) string {
	var parts []string
	if st.NeedsYou > 0 {
		parts = append(parts, i18n.T(l, "hub-liveactivity.needs_you", i18n.Args{"count": st.NeedsYou}))
	}
	if st.Working > 0 {
		parts = append(parts, i18n.T(l, "hub-liveactivity.working", i18n.Args{"count": st.Working}))
	}
	if st.Unread > 0 {
		parts = append(parts, i18n.T(l, "hub-liveactivity.unread", i18n.Args{"count": st.Unread}))
	}
	return strings.Join(parts, " · ")
}

func (la *liveActivities) send(d Device, r db.LiveActivity, payload map[string]any, urgent bool) {
	prio := "5"
	if urgent {
		prio = "10"
	}
	la.h.push.sendLiveActivity(d, r.PushToken, payload, prio, func() {
		la.update(r.DeviceID, func(v *db.LiveActivity) {
			if v.PushToken == r.PushToken {
				v.PushToken, v.ActivityID = "", ""
			}
		})
	})
}

func (la *liveActivities) sendStart(d Device, r db.LiveActivity, payload map[string]any) {
	la.h.logf("live activity: push-start on %s", d.Name)
	la.h.push.sendLiveActivity(d, r.PushToStartToken, payload, "10", func() {
		la.update(r.DeviceID, func(v *db.LiveActivity) {
			if v.PushToStartToken == r.PushToStartToken {
				v.PushToStartToken = ""
			}
		})
	})
}

// update changes one device's row and saves it.
func (la *liveActivities) update(device string, fn func(*db.LiveActivity)) {
	la.mu.Lock()
	v := la.rows[device]
	v.DeviceID = device
	fn(&v)
	la.rows[device] = v
	la.mu.Unlock()
	if la.h.store != nil {
		if err := la.h.store.SaveLiveActivity(context.Background(), v); err != nil {
			la.h.logf("save live activity: %v", err)
		}
	}
	la.changed() // a new token gets the current state right away
}

func (la *liveActivities) routes(api *http.ServeMux) {
	// The app reports its activity's push token (it rotates), the push-to-start token, or that the activity ended.
	api.HandleFunc("POST /api/v1/devices/{id}/live-activity", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ActivityID       string `json:"activity_id"`
			PushToken        string `json:"push_token"`
			PushToStartToken string `json:"push_to_start_token"`
			Ended            bool   `json:"ended"`
		}
		if err := decodeJSONBody(w, r, &body, 16<<10); err != nil {
			writeBodyErr(w, err, err)
			return
		}
		la.update(r.PathValue("id"), func(v *db.LiveActivity) {
			if body.PushToStartToken != "" {
				v.PushToStartToken = body.PushToStartToken
			}
			switch {
			case body.Ended:
				if body.ActivityID == "" || body.ActivityID == v.ActivityID {
					v.ActivityID, v.PushToken = "", ""
				}
			case body.PushToken != "":
				if body.ActivityID != v.ActivityID {
					v.StartedAt = time.Now().UTC()
				}
				v.ActivityID, v.PushToken = body.ActivityID, body.PushToken
			}
		})
		writeJSON(w, map[string]any{"ok": true})
	})
	// The current state, for the app to start or refresh its activity while it is open.
	api.HandleFunc("GET /api/v1/fleet", func(w http.ResponseWriter, r *http.Request) {
		st := la.snapshot()
		stats := la.h.fleetStats()
		st.Stats = &stats
		writeJSON(w, map[string]any{"fleet": st})
	})
}

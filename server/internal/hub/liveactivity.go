package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/chyroc/jarvis/server/internal/db"
	"github.com/chyroc/jarvis/server/internal/proto"
)

// The "Fleet" Live Activity (docs/live-activity.md): one activity per phone, on the lock screen and in the Dynamic
// Island, summing up every agent that needs you, is working, or finished unseen. The hub computes the state
// (FleetState, the activity's ContentState) from what it already knows and pushes it over APNs (push type
// liveactivity) to each device's activity token; push-to-start opens one when the app is not running.
//
// Budget: Apple throttles activity updates (numbers unpublished), so changes are merged for fleetCoalesce, a new
// "needs you" goes out at once, the timer (working since…) runs on the phone, and nothing is sent while nothing
// changes except a refresh before the content goes stale.

const (
	fleetCoalesce   = 2500 * time.Millisecond
	fleetStaleIn    = 15 * time.Minute // stale-date: buttons disappear if the hub goes quiet this long
	fleetRefresh    = 10 * time.Minute // resend unchanged content before it goes stale
	fleetEndAfter   = time.Minute      // empty this long: end the activity
	fleetDoneLinger = 5 * time.Minute  // only finished agents left: keep showing them this long
	fleetMaxItems   = 3
	fleetMaxAge     = 8 * time.Hour // the system ends an activity after about this long; past it, start a new one
)

// FleetState is the activity's ContentState, field for field (apple/LiveActivity/FleetActivity.swift). Times are Unix
// seconds: ActivityKit decodes pushed JSON with a default JSONDecoder, which would read a Date as seconds since 2001.
type FleetState struct {
	Phase    string      `json:"phase"` // needsYou | working | done | "" (nothing to show)
	NeedsYou int         `json:"needsYou"`
	Working  int         `json:"working"`
	Unread   int         `json:"unread"`
	Items    []FleetItem `json:"items"`
	// WorkingSince is when the longest-running working agent started its turn (the phone runs the clock).
	WorkingSince float64 `json:"workingSince,omitempty"`
	// Details: the lock screen may show titles (the device's lock_details setting); otherwise only counts.
	Details bool `json:"details"`
	// Assistant: the assistant (对话 tab) is answering. Pushed only to devices that show it.
	Assistant bool `json:"assistant,omitempty"`
	// Left / Right: the compact Dynamic Island's sides (the device's island setting, notify.go IslandPrefs);
	// empty means status counts.
	Left  string `json:"left,omitempty"`
	Right string `json:"right,omitempty"`
	// Stats: the numbers the device shows, rounded so that small changes do not cost a push (usage.go).
	Stats *FleetStats `json:"stats,omitempty"`
}

// FleetStats are the island's numbers. A push carries only those the device shows; the app's own copy has them all.
type FleetStats struct {
	TPM      int64       `json:"tpm,omitempty"`
	Tokens   int64       `json:"tokens,omitempty"`
	Requests int         `json:"requests,omitempty"`
	Loads    []FleetLoad `json:"loads,omitempty"`
}

// FleetLoad is a machine's 1-minute load average over its cores, in percent (rounded to 5).
type FleetLoad struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Percent int    `json:"percent"`
}

// round2 keeps two significant digits: 12345 → 12000, 987 → 990. The island shows "12k", so finer changes would
// only spend the push budget.
func round2(n int64) int64 {
	if n < 100 {
		return n
	}
	p := int64(1)
	for n/p >= 100 {
		p *= 10
	}
	return (n + p/2) / p * p
}

// fleetStats turns the usage summary into the island's numbers.
func (h *Hub) fleetStats() FleetStats {
	u := h.usageSummary()
	st := FleetStats{TPM: round2(u.TPM), Tokens: round2(u.Tokens), Requests: int(round2(int64(u.Requests)))}
	for _, m := range u.Machines {
		name := m.ID
		if mm := h.byID[m.ID]; mm != nil && mm.cfg.Name != "" {
			name = mm.cfg.Name
		}
		st.Loads = append(st.Loads, FleetLoad{ID: m.ID, Name: name, Percent: int(math.Round(m.Load*20)) * 5})
	}
	return st
}

// forDevice is what one device gets: its settings, and of the assistant and the numbers only what it shows (so a
// change it does not show costs no push).
func (s FleetState) forDevice(p NotifyPrefs, stats FleetStats) FleetState {
	d := s
	d.Details = p.LockDetails
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

type FleetItem struct {
	ID          string  `json:"id"` // machine/session, also the deep link
	Title       string  `json:"title"`
	Place       string  `json:"place"`
	Kind        string  `json:"kind"` // permission | plan | question | screen | working | unread
	Detail      string  `json:"detail"`
	Interaction string  `json:"interaction,omitempty"` // what 允许 / 拒绝 / 批准 resolve
	Since       float64 `json:"since,omitempty"`
}

func (s FleetState) empty() bool { return s.Phase == "" }

// needsYouIDs: the items waiting for the user, to tell a new one (push now) from anything else (merge).
func (s FleetState) needsYouIDs() map[string]bool {
	out := map[string]bool{}
	for _, it := range s.Items {
		if it.Kind != "working" && it.Kind != "unread" {
			out[it.ID+"#"+it.Interaction] = true
		}
	}
	return out
}

func (s FleetState) relevance() float64 {
	switch s.Phase {
	case "needsYou":
		return 1
	case "working":
		return 0.5
	}
	return 0.2
}

// fleetInput is one agent as the fleet sees it.
type fleetInput struct {
	Machine   string // display name
	Session   proto.Session
	Muted     bool
	TurnStart time.Time // zero: unknown
}

// buildFleet sums agents up the way the Agents list does (docs/ios.md): needs you first (oldest request first), then
// working (longest first), then finished unseen. Muted agents, idle / offline ones, and /loop agents waiting for
// their next tick are left out; the assistant only counts when it asks something.
func buildFleet(agents []fleetInput, ixs []proto.Interaction) FleetState {
	byKey := map[string]fleetInput{}
	for _, a := range agents {
		byKey[a.Session.MachineID+"/"+a.Session.SessionID] = a
	}
	var st FleetState
	var needs, working, unread []FleetItem
	asked := map[string]bool{}
	for _, ix := range ixs { // already oldest first
		key := ix.MachineID + "/" + ix.SessionID
		a, ok := byKey[key]
		if !ok || a.Muted {
			continue
		}
		asked[key] = true
		needs = append(needs, FleetItem{
			ID: key, Title: agentTitle(a.Session), Place: agentPlace(a.Machine, a.Session), Kind: ix.Kind,
			Detail: clip(oneLine(interactionDetail(ix)), 60), Interaction: ix.ID, Since: unix(ix.CreatedAt),
		})
	}
	for _, a := range agents {
		s := a.Session
		key := s.MachineID + "/" + s.SessionID
		if a.Muted || asked[key] {
			continue
		}
		switch s.State {
		case proto.StateNeedsYou: // a TUI dialog with no interaction yet
			if s.Assistant {
				continue
			}
			needs = append(needs, FleetItem{ID: key, Title: agentTitle(s), Place: agentPlace(a.Machine, s), Kind: proto.IxScreen, Detail: "终端等你处理"})
		case proto.StateWorking:
			if s.Assistant {
				st.Assistant = true
				continue
			}
			since := a.TurnStart
			if since.IsZero() {
				since = s.UpdatedAt
			}
			working = append(working, FleetItem{ID: key, Title: agentTitle(s), Place: agentPlace(a.Machine, s), Kind: "working", Since: unix(since)})
		case proto.StateUnread:
			if s.Assistant || s.Loop != nil {
				continue
			}
			unread = append(unread, FleetItem{ID: key, Title: agentTitle(s), Place: agentPlace(a.Machine, s), Kind: "unread",
				Detail: clip(oneLine(firstLine(s.LastText)), 60), Since: unix(s.UpdatedAt)})
		}
	}
	sort.SliceStable(working, func(i, j int) bool { return working[i].Since < working[j].Since })
	sort.SliceStable(unread, func(i, j int) bool { return unread[i].Since > unread[j].Since })
	st.NeedsYou, st.Working, st.Unread = len(needs), len(working), len(unread)
	switch {
	case st.NeedsYou > 0:
		st.Phase = "needsYou"
	case st.Working > 0:
		st.Phase = "working"
	case st.Unread > 0:
		st.Phase = "done"
	}
	if len(working) > 0 {
		st.WorkingSince = working[0].Since
	}
	st.Items = append(append(append([]FleetItem{}, needs...), working...), unread...)
	if len(st.Items) > fleetMaxItems {
		st.Items = st.Items[:fleetMaxItems]
	}
	return st
}

func agentTitle(s proto.Session) string {
	t := s.Title
	if t == "" || t == "Claude Code" {
		t = path.Base(s.Cwd)
	}
	return clip(oneLine(t), 24)
}

// agentPlace is "机器 · 空间 › 标签", the subtitle pushes use too.
func agentPlace(machine string, s proto.Session) string {
	place := machine
	if s.SpaceName != "" {
		place += " · " + s.SpaceName
		if s.SlotName != "" && s.SlotName != fmt.Sprint(s.SlotIndex) {
			place += " › " + s.SlotName
		}
	}
	return clip(oneLine(place), 32)
}

func interactionDetail(ix proto.Interaction) string {
	switch ix.Kind {
	case proto.IxPermission:
		if ix.Summary != "" {
			return ix.Summary
		}
		return ix.ToolName
	case proto.IxPlan:
		return firstLine(ix.Plan)
	case proto.IxQuestion:
		if len(ix.Questions) > 0 {
			return ix.Questions[0].Question
		}
		return ix.Summary
	}
	return "终端等你处理"
}

func unix(t time.Time) float64 {
	if t.IsZero() {
		return 0
	}
	return float64(t.Unix())
}

// coalescer decides when a new fleet state goes out: a new "needs you" at once, anything else after fleetCoalesce
// (more changes in the meantime ride along).
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
	for id := range st.needsYouIDs() {
		if !c.needs[id] {
			return true, false
		}
	}
	if c.pending {
		return false, false
	}
	c.pending = true
	return false, true
}

func (c *coalescer) sentState(st FleetState) {
	c.sent, _ = json.Marshal(st)
	c.needs = st.needsYouIDs()
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
}

func newLiveActivities(h *Hub) *liveActivities {
	return &liveActivities{h: h, signal: make(chan struct{}, 1), rows: map[string]db.LiveActivity{}, started: map[string]int{},
		sent: map[string][]byte{}, sentAt: map[string]time.Time{}}
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

// snapshot computes the fleet from every machine's sessions and the pending interactions.
func (la *liveActivities) snapshot() FleetState {
	h := la.h
	var agents []fleetInput
	for _, m := range h.machines {
		for _, s := range h.decorated(m) {
			agents = append(agents, fleetInput{
				Machine: m.cfg.Name, Session: s, Muted: h.muted(m.cfg.ID, s.SessionID),
				TurnStart: h.turns.startOf(m.cfg.ID + "/" + s.SessionID),
			})
		}
	}
	return buildFleet(agents, h.pendingInteractions())
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
			if la.resend {
				la.resend, la.coal.sent = false, nil
			}
			la.mu.Unlock()
			st := la.snapshot()
			if b, _ := json.Marshal(st); !bytes.Equal(b, la.shown) { // the open app updates its activity itself
				la.shown = b
				la.broadcast(st)
			}
			now, schedule := la.coal.offer(st)
			switch {
			case now:
				la.flush(st, true, false)
			case schedule:
				timer.Reset(fleetCoalesce)
			}
		case <-timer.C:
			la.flush(la.snapshot(), false, false)
		case <-tick.C:
			la.tick()
		}
	}
}

// broadcast sends the open apps the fleet with every number (each app picks what its own settings show).
func (la *liveActivities) broadcast(st FleetState) {
	stats := la.h.fleetStats()
	st.Stats = &stats
	la.stats, _ = json.Marshal(stats)
	la.h.broadcast(map[string]any{"type": "fleet", "fleet": st})
}

// tick: end an activity that has been over for a while, refresh one about to go stale, update the numbers.
func (la *liveActivities) tick() {
	la.mu.Lock()
	over := !la.emptyAt.IsZero() && time.Since(la.emptyAt) >= fleetEndAfter
	stale := !la.last.empty() && time.Since(la.lastPush) >= fleetRefresh
	doneLong := la.last.Phase == "done" && !la.doneSince.IsZero() && time.Since(la.doneSince) >= fleetDoneLinger
	last := la.last
	la.mu.Unlock()
	switch {
	case over || doneLong:
		la.endAll()
	case stale:
		la.flush(la.snapshot(), false, true)
	case !last.empty():
		la.metrics(last, time.Now())
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
		if !ok || !devicePrefs(d.Prefs).LiveActivity {
			continue
		}
		dst := last.forDevice(devicePrefs(d.Prefs), stats)
		if la.markSent(r.DeviceID, dst, now, false) {
			la.send(d, r, liveActivityPayload("update", dst, now), false)
		}
	}
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
// A device whose own version did not change (the change was something it does not show) is skipped unless forced.
func (la *liveActivities) flush(st FleetState, urgent, force bool) {
	la.coal.sentState(st)
	now := time.Now()
	la.mu.Lock()
	wasOver := la.last.empty() || la.last.Phase == "done"
	if st.empty() {
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
	if st.empty() {
		return // ended by tick after fleetEndAfter, unless something comes back first
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
		dst := st.forDevice(prefs, stats)
		switch {
		case r.PushToken != "" && now.Sub(r.StartedAt) < fleetMaxAge:
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
				la.sendStart(d, r, liveActivityPayload("start", dst, now))
			}
		}
	}
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
	aps := map[string]any{"timestamp": now.Unix(), "event": event, "content-state": st}
	switch event {
	case "end":
		aps["dismissal-date"] = now.Unix()
	default:
		aps["stale-date"] = now.Add(fleetStaleIn).Unix()
		aps["relevance-score"] = st.relevance()
	}
	if event == "start" {
		aps["attributes-type"] = "JarvisActivityAttributes"
		aps["attributes"] = map[string]any{"kind": "fleet"}
		aps["alert"] = map[string]any{"title": "Jarvis", "body": fleetSentence(st)}
	}
	return map[string]any{"aps": aps}
}

// fleetSentence: "2 个需要你 · 3 个在工作", the alert of a push-started activity.
func fleetSentence(st FleetState) string {
	var parts []string
	if st.NeedsYou > 0 {
		parts = append(parts, fmt.Sprintf("%d 个需要你", st.NeedsYou))
	}
	if st.Working > 0 {
		parts = append(parts, fmt.Sprintf("%d 个在工作", st.Working))
	}
	if st.Unread > 0 {
		parts = append(parts, fmt.Sprintf("%d 个做完了", st.Unread))
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
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, err)
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

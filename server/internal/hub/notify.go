package hub

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/chyroc/jarvis/server/internal/proto"
)

// When a "finished" push is worth sending (docs/server.md 推送). Borrowed from Ovis (github.com/devswha/ovis,
// server/push.ts, MIT): a push is for someone away from the computer, so
//   - wait a while after the turn ends; if the agent resumes, or the user looks at it (herdr idle, or opened in the
//     app), nothing is sent;
//   - only turns that took a while are worth it ("long"); a quick answer is read where it was asked;
//   - background tasks and subagents still running mean the work is not done: hold, and push once when the turn that
//     follows their reports ends.
// Each device picks what it wants (NotifyPrefs); the timings are hub.json "notify".

// NotifyConfig is hub.json "notify". Zero values fall back to the defaults.
type NotifyConfig struct {
	// DoneDelaySeconds: how long a finished agent must stay finished (and unseen) before the push. Default 60.
	DoneDelaySeconds int `json:"done_delay_seconds,omitempty"`
	// LongTurnSeconds: turns at least this long count as "long" (NotifyPrefs.Done = "long"). Default 60.
	LongTurnSeconds int `json:"long_turn_seconds,omitempty"`
	// BackgroundHoldMinutes: give up on a turn whose background work has not finished after this long. Default 120.
	BackgroundHoldMinutes int `json:"background_hold_minutes,omitempty"`
}

func (c NotifyConfig) doneDelay() time.Duration { return secondsOr(c.DoneDelaySeconds, 60) }
func (c NotifyConfig) longTurn() time.Duration  { return secondsOr(c.LongTurnSeconds, 60) }
func (c NotifyConfig) backgroundHold() time.Duration {
	if c.BackgroundHoldMinutes > 0 {
		return time.Duration(c.BackgroundHoldMinutes) * time.Minute
	}
	return 120 * time.Minute
}

func secondsOr(n, def int) time.Duration {
	if n > 0 {
		return time.Duration(n) * time.Second
	}
	return time.Duration(def) * time.Second
}

// NotifyPrefs is what one device wants pushed, set in the app (设置 › 通知) and stored per device.
type NotifyPrefs struct {
	// Input: questions, plans, permissions and terminal prompts (anything that needs you).
	Input bool `json:"input"`
	// Done: finished turns. "off", "long" (turns of at least LongTurnSeconds) or "always".
	Done string `json:"done"`
	// LiveActivity: keep the Fleet activity on the lock screen / Dynamic Island (liveactivity.go).
	LiveActivity bool `json:"live_activity"`
	// LockDetails: the activity may show agent names on the lock screen; off, it shows only counts.
	LockDetails bool `json:"lock_details"`
}

const (
	DoneOff    = "off"
	DoneLong   = "long"
	DoneAlways = "always"
)

func defaultPrefs() NotifyPrefs { return NotifyPrefs{Input: true, Done: DoneLong, LiveActivity: true} }

// devicePrefs decodes what is stored for a device; anything missing or unknown is the default.
func devicePrefs(raw json.RawMessage) NotifyPrefs {
	p := defaultPrefs()
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &p)
	}
	switch p.Done {
	case DoneOff, DoneLong, DoneAlways:
	default:
		p.Done = DoneLong
	}
	return p
}

// prefsView is a device's prefs plus the hub's timings, so the app can say what "long" means.
func (h *Hub) prefsView(p NotifyPrefs) any {
	return struct {
		NotifyPrefs
		LongTurnSeconds  int `json:"long_turn_seconds"`
		DoneDelaySeconds int `json:"done_delay_seconds"`
	}{p, int(h.cfg.Notify.longTurn().Seconds()), int(h.cfg.Notify.doneDelay().Seconds())}
}

// wantsDone: does a device with these prefs get the push for a turn that took d?
func (p NotifyPrefs) wantsDone(d, long time.Duration) bool {
	switch p.Done {
	case DoneAlways:
		return true
	case DoneLong:
		return d >= long
	}
	return false
}

// turnTracker follows each session's turns between status snapshots: when the turn started, which end is the latest
// (gen), and whether it is held for background work.
type turnTracker struct {
	mu    sync.Mutex
	turns map[string]*turn
}

type turn struct {
	start      time.Time
	endedAt    time.Time
	gen        int
	background bool // ended once with background work still running; the next end decides
}

func newTurnTracker() *turnTracker { return &turnTracker{turns: map[string]*turn{}} }

// working: the session is working (again). A turn that ended moments ago and resumes is the same turn, so its start
// is kept; any pending end is cancelled.
func (t *turnTracker) working(key string, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	tr := t.turns[key]
	if tr == nil {
		tr = &turn{start: now}
		t.turns[key] = tr
	}
	tr.gen++
}

// startOf: when the session's current (or last held) turn started; zero when not tracked.
func (t *turnTracker) startOf(key string) time.Time {
	t.mu.Lock()
	defer t.mu.Unlock()
	if tr := t.turns[key]; tr != nil {
		return tr.start
	}
	return time.Time{}
}

// ended: working → done / idle. Returns the generation the delayed check must still see.
func (t *turnTracker) ended(key string, now time.Time) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	tr := t.turns[key]
	if tr == nil { // the hub came up mid-turn: count from now
		tr = &turn{start: now}
		t.turns[key] = tr
	}
	tr.gen++
	tr.endedAt = now
	return tr.gen
}

// finishVerdict is what the delayed check decided.
type finishVerdict int

const (
	finishStale finishVerdict = iota // something happened since; a later check decides
	finishDrop                       // no push for this turn
	finishHold                       // background work still running: wait for the turn its reports start
	finishPush                       // push, to the devices whose prefs take a turn this long
)

// settle runs after the delay. s is the session now, seen whether the user has looked at it since it finished
// (in herdr or in the app). Everything that makes a push pointless is checked here, at the last moment.
func (t *turnTracker) settle(key string, gen int, s proto.Session, seen, muted, assistantPushed bool, now time.Time, hold time.Duration) (finishVerdict, time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	tr := t.turns[key]
	if tr == nil || tr.gen != gen {
		return finishStale, 0
	}
	if s.Status == proto.StatusWorking {
		return finishStale, 0
	}
	took := tr.endedAt.Sub(tr.start)
	if s.Status != proto.StatusDone || seen || muted || assistantPushed {
		delete(t.turns, key)
		return finishDrop, took
	}
	if backgroundRunning(s) {
		if now.Sub(tr.start) > hold {
			delete(t.turns, key)
			return finishDrop, took
		}
		tr.background = true
		return finishHold, took
	}
	// A turn started by a background task's report: on its own the user did not ask for anything; as the end of
	// held work it is exactly the "all done" they want.
	if s.WokeByTask && !tr.background {
		delete(t.turns, key)
		return finishDrop, took
	}
	delete(t.turns, key)
	return finishPush, took
}

func backgroundRunning(s proto.Session) bool {
	if len(s.Background) > 0 {
		return true
	}
	for _, a := range s.Subagents {
		if a.Running {
			return true
		}
	}
	return false
}

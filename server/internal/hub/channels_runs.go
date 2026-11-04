package hub

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/channel"
	"github.com/nwverse/persona/server/internal/proto"
)

// The turns channels wait on (docs/channels.md「跟踪这一轮」): one copy for every channel. A message from a channel
// starts a run; the hub watches the conversation (like an open chat in the app), shows progress where the channel can
// (a card it changes, 「正在输入」), and when the turn ends sends its answer back through the channel's sink. The rules
// are channel.Run's; what reaches the chat app is the sink's.

// channelSink is how a channel's runs reach its chat app.
type channelSink interface {
	// answer sends a run's answer and note (into its progress message when there is one). final: the run is over
	// (its messages are marked done); quiet: there is nothing to say (a held run whose background work ended).
	answer(ctx context.Context, r *channel.Run, answer, note string, took time.Duration, final, quiet bool)
}

// channelProgress is a sink that shows a run's progress in a message it changes (the Feishu bot's card).
type channelProgress interface {
	progress(ctx context.Context, r *channel.Run, took time.Duration, steps []string, live string)
	// folded: o went into a later run; its progress message says so.
	folded(ctx context.Context, o *channel.Run)
}

// channelTyping is a sink that shows 「正在输入」 while a run goes on.
type channelTyping interface {
	typing(ctx context.Context, on bool)
}

// channelsStateKey holds the runs, the asks waiting in text and the messages already taken, so a restart (another
// deploy) still answers them.
const channelsStateKey = "channels.state"

type channelsState struct {
	Runs []*channel.Run `json:"runs,omitempty"`
	Asks []*channel.Ask `json:"asks,omitempty"`
	// Seen are, by channel, the ids of the latest messages taken (the chat apps deliver some again).
	Seen map[string][]string `json:"seen,omitempty"`
}

const channelSeenKeep = 300

// chanLive is a run's live state, in memory only.
type chanLive struct {
	mu       chanMutex // held while its progress is being written
	steps    []string
	live     string
	dirty    bool
	updated  time.Time
	checked  time.Time // the answer was last read (while the turn was not seen to end, or the message not found)
	notFound bool      // the last read did not find the message in the conversation yet
	finished bool
	done     chan struct{}
	// checking is held by checkChannelRun: the watch loop and a turn's end may both check at once, and two checks
	// would send the same answer twice.
	checking chanMutex
}

// chanMutex is a mutex the watch loop can try without blocking.
type chanMutex chan struct{}

func newChanMutex() chanMutex { return make(chanMutex, 1) }
func (m chanMutex) Lock()     { m <- struct{}{} }
func (m chanMutex) Unlock()   { <-m }
func (m chanMutex) TryLock() bool {
	select {
	case m <- struct{}{}:
		return true
	default:
		return false
	}
}

// loadChannelStateLocked reads the runs once (the caller holds h.channels.mu).
func (h *Hub) loadChannelStateLocked(ctx context.Context) {
	c := &h.channels
	if c.stLoaded {
		return
	}
	if h.store != nil {
		if _, err := h.store.Setting(ctx, channelsStateKey, &c.st); err != nil {
			h.logf("channels: load state: %v", err)
			return
		}
	}
	if c.st.Seen == nil {
		c.st.Seen = map[string][]string{}
	}
	c.stLoaded = true
}

func (h *Hub) saveChannelStateLocked() {
	if h.store == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := h.store.PutSetting(ctx, channelsStateKey, h.channels.st); err != nil {
		h.logf("channels: save state: %v", err)
	}
}

// claimChannelMessage: the first time channel id's message is seen.
func (h *Hub) claimChannelMessage(id, msg string) bool {
	c := &h.channels
	c.mu.Lock()
	defer c.mu.Unlock()
	h.loadChannelStateLocked(context.Background())
	seen := c.st.Seen[id]
	if msg == "" || slices.Contains(seen, msg) {
		return false
	}
	seen = append(seen, msg)
	if len(seen) > channelSeenKeep {
		seen = seen[len(seen)-channelSeenKeep:]
	}
	c.st.Seen[id] = seen
	h.saveChannelStateLocked()
	return true
}

// sinkFor is how channel id's runs reach it (nil when it has gone: a plugin uninstalled).
func (h *Hub) sinkFor(id string) channelSink {
	if id == feishuChannelID {
		return feishuSink{h}
	}
	if _, ok := h.extensionChannel(id); ok {
		return extensionSink{h: h, id: id}
	}
	return nil
}

// channelRuns are channel id's runs now (copies).
func (h *Hub) channelRuns(id string) []channel.Run {
	c := &h.channels
	c.mu.Lock()
	defer c.mu.Unlock()
	h.loadChannelStateLocked(context.Background())
	var out []channel.Run
	for _, r := range c.st.Runs {
		if id == "" || r.Channel == id {
			out = append(out, *r)
		}
	}
	return out
}

// startChannelRun records a run and watches it. An earlier run of the same channel on the same conversation that has
// not answered yet is folded into this one: one answer covers both messages.
func (h *Hub) startChannelRun(r *channel.Run) {
	c := &h.channels
	c.mu.Lock()
	h.loadChannelStateLocked(context.Background())
	var folded []*channel.Run
	keep := c.st.Runs[:0]
	for _, o := range c.st.Runs {
		if o.Channel == r.Channel && o.MachineID == r.MachineID && o.SessionID == r.SessionID {
			folded = append(folded, o)
			r.Fold(o)
			continue
		}
		keep = append(keep, o)
	}
	c.st.Runs = append(keep, r)
	gone := map[string]*chanLive{}
	for _, o := range folded {
		if p := c.live[o.ID]; p != nil {
			gone[o.ID] = p
			delete(c.live, o.ID)
		}
	}
	h.saveChannelStateLocked()
	c.mu.Unlock()
	sink := h.sinkFor(r.Channel)
	for _, o := range folded {
		ended := true
		if p := gone[o.ID]; p != nil {
			p.mu.Lock()
			ended = !p.finished // finishChannelRun may have answered it just now: then its message holds the answer
			if ended {
				p.finished = true
				close(p.done)
			}
			p.mu.Unlock()
		}
		if fs, ok := sink.(channelProgress); ok && ended {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			fs.folded(ctx, o)
			cancel()
		}
	}
	h.noteChannelConversation(r.Channel, r.MachineID, r.SessionID)
	if ts, ok := sink.(channelTyping); ok {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		ts.typing(ctx, true)
		cancel()
	}
	go h.watchChannelRun(r)
}

func (h *Hub) channelRunByID(id string) (*channel.Run, *chanLive) {
	c := &h.channels
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, r := range c.st.Runs {
		if r.ID == id {
			return r, c.live[id]
		}
	}
	return nil, nil
}

// watchChannelRun follows a run until it is answered: the conversation's items reach the hub (a watcher), for the
// progress shown meanwhile.
func (h *Hub) watchChannelRun(r *channel.Run) {
	c := &h.channels
	m := h.localMachine(r.MachineID)
	if m == nil { // the machine left the configuration: nothing will answer it
		c.mu.Lock()
		c.st.Runs = slices.DeleteFunc(c.st.Runs, func(o *channel.Run) bool { return o == r })
		h.saveChannelStateLocked()
		c.mu.Unlock()
		return
	}
	c.mu.Lock()
	if c.live == nil {
		c.live = map[string]*chanLive{}
	}
	if c.live[r.ID] != nil {
		c.mu.Unlock()
		return // already watched
	}
	prog := &chanLive{mu: newChanMutex(), checking: newChanMutex(), done: make(chan struct{})}
	c.live[r.ID] = prog
	c.mu.Unlock()
	ctx := context.Background()
	m.addWatcher(ctx, r.SessionID)
	defer m.removeWatcher(r.SessionID)
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-prog.done:
			return
		case <-t.C:
		}
		if run, _ := h.channelRunByID(r.ID); run == nil { // dropped (unbound, folded into a later message)
			c.mu.Lock()
			if c.live[r.ID] == prog {
				delete(c.live, r.ID)
			}
			c.mu.Unlock()
			return
		}
		h.checkChannelRun(r.ID)
		h.channelProgressTick(r.ID)
	}
}

// channelProgressTick shows a run's progress when it is due: the progress message after a few seconds and then every
// few, 「正在输入」 again every few seconds.
func (h *Hub) channelProgressTick(id string) {
	r, prog := h.channelRunByID(id)
	if r == nil || prog == nil {
		return
	}
	sink := h.sinkFor(r.Channel)
	c := &h.channels
	if ts, ok := sink.(channelTyping); ok {
		working := false
		if m := h.localMachine(r.MachineID); m != nil {
			s, _ := m.session(r.SessionID)
			working = s.Status == proto.StatusWorking || s.Starting
		}
		c.mu.Lock()
		now := c.clock()
		due := !prog.finished && (working || !r.Held) && now.Sub(prog.updated) >= channel.TypingEvery
		if due {
			prog.updated = now
		}
		c.mu.Unlock()
		if due {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			ts.typing(ctx, true)
			cancel()
		}
		return
	}
	ps, ok := sink.(channelProgress)
	if !ok || !prog.mu.TryLock() {
		return
	}
	defer prog.mu.Unlock()
	working := false
	if m := h.localMachine(r.MachineID); m != nil {
		s, _ := m.session(r.SessionID)
		working = s.Status == proto.StatusWorking || s.Starting
	}
	c.mu.Lock()
	now := c.clock()
	card, started := r.Ext["card"], r.Started
	if card == "" && !working {
		c.mu.Unlock()
		return // nothing going on to show (a held run waiting on its background work)
	}
	steps, live, dirty := append([]string{}, prog.steps...), prog.live, prog.dirty
	due := !prog.finished && (card == "" && now.Sub(started) >= channel.ProgressAfter ||
		card != "" && (dirty && now.Sub(prog.updated) >= channel.ProgressEvery || now.Sub(prog.updated) >= channel.ProgressIdle))
	if due {
		prog.dirty, prog.updated = false, now
	}
	c.mu.Unlock()
	if !due {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ps.progress(ctx, r, now.Sub(started), steps, live)
}

// setRunExt sets one of a run's channel values and keeps it.
func (h *Hub) setRunExt(r *channel.Run, key, value string) {
	c := &h.channels
	c.mu.Lock()
	defer c.mu.Unlock()
	if r.Ext == nil {
		r.Ext = map[string]string{}
	}
	r.Ext[key] = value
	h.saveChannelStateLocked()
}

// channelItems: new items of a watched conversation (machine.go): the runs on it note the steps.
func (h *Hub) channelItems(machine, session string, items []proto.Item) {
	c := &h.channels
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.live) == 0 {
		return
	}
	for _, r := range c.st.Runs {
		if r.MachineID != machine || r.SessionID != session {
			continue
		}
		p := c.live[r.ID]
		if p == nil {
			continue
		}
		for _, it := range items {
			if it.Seq <= r.Base {
				continue
			}
			if s := botStep(it); s != "" {
				p.steps = append(p.steps, s)
				p.dirty = true
			}
			if it.Kind == proto.KindText && it.Role == "assistant" {
				p.live = ""
			}
		}
		if len(p.steps) > 200 {
			p.steps = p.steps[len(p.steps)-200:]
		}
	}
}

// channelDelta: the text a Persona conversation is writing right now.
func (h *Hub) channelDelta(machine, session, text string) {
	c := &h.channels
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.live) == 0 {
		return
	}
	for _, r := range c.st.Runs {
		if r.MachineID == machine && r.SessionID == session {
			if p := c.live[r.ID]; p != nil && p.live != text {
				p.live, p.dirty = text, true
			}
		}
	}
}

// checkChannelRun answers a run whose turn ended (channel.Run.Check and Decide).
func (h *Hub) checkChannelRun(id string) {
	r, prog := h.channelRunByID(id)
	if r == nil || prog == nil {
		return
	}
	m := h.localMachine(r.MachineID)
	if m == nil {
		return
	}
	if !prog.checking.TryLock() {
		return // another check is on it
	}
	defer prog.checking.Unlock()
	c := &h.channels
	s, exists := m.session(r.SessionID)
	look := channel.Look{Exists: exists, Online: m.snapshot().Online, Working: s.Status == proto.StatusWorking || s.Starting,
		Waiting: s.Status == proto.StatusBlocked || h.hasPending(m.cfg.ID, r.SessionID), Offline: s.Status == proto.StatusOffline,
		Background: channel.Background(s)}
	c.mu.Lock()
	look.Now = c.clock()
	step := r.Check(look)
	snap := *r
	snap.MsgIDs = slices.Clone(r.MsgIDs)
	c.mu.Unlock()
	switch step.Do {
	case channel.Wait:
		return
	case channel.Finish:
		h.finishChannelRun(r, &channel.Result{Note: step.Note, Quiet: step.Note == ""})
		return
	}
	// The end was not seen (it came before the watch, or never), or the message was not there last time: look now and
	// then, not every second.
	c.mu.Lock()
	if (!snap.Seen || prog.notFound) && look.Now.Sub(prog.checked) < 10*time.Second {
		c.mu.Unlock()
		return
	}
	prog.checked = look.Now
	c.mu.Unlock()
	rd, err := h.readTurn(m, &snap)
	if err != nil {
		h.logf("channels: read the answer: %v", err)
		return // next tick
	}
	c.mu.Lock()
	prog.notFound = !rd.Found
	c.mu.Unlock()
	if !rd.Found {
		if look.Now.Sub(snap.Started) < channel.QueuedMost {
			return // the message waits behind the turn that just ended: it starts the next one
		}
		h.logf("channels: %s: the message never showed in the conversation; answering with its last words", r.ID)
		snap.MsgIDs, snap.Probe = nil, ""
		if rd, err = h.readTurn(m, &snap); err != nil {
			return
		}
	}
	res := snap.Decide(rd, look)
	if res == nil {
		return
	}
	h.finishChannelRun(r, res)
}

// readTurn is what the conversation answered after the run's base: after its message when the run names one. It
// reads to the end of the transcript (a long turn has more lines than one page), keeping the items from that message
// on, or the last ones without one.
func (h *Hub) readTurn(m *Machine, r *channel.Run) (channel.Reading, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	sid := r.SessionID
	holds := func(it proto.Item) bool {
		if strings.Contains(it.Text, r.Probe) {
			return true
		}
		if it.FullSize == 0 {
			return false
		}
		// A long message (a forwarded chat) is cut in pages, maybe before what the user wrote.
		var full proto.Item
		return m.call(ctx, "session.item", map[string]any{"session_id": sid, "item_id": it.ID}, &full) == nil && strings.Contains(full.Text, r.Probe)
	}
	named := len(r.MsgIDs) > 0 || r.Probe != ""
	var kept []proto.Item
	found := !named
	after := r.Base
	for page := 0; page < 100; page++ {
		var res struct {
			Items     []proto.Item `json:"items"`
			MoreAfter bool         `json:"more_after"`
			ToSeq     int          `json:"to_seq"`
		}
		if err := m.call(ctx, "session.items", map[string]any{"session_id": sid, "after_seq": after, "lazy_images": true}, &res); err != nil {
			return channel.Reading{Last: r.Base}, err
		}
		if named {
			if i, ok := r.Find(res.Items, holds); ok {
				kept, found = slices.Clone(res.Items[i:]), true
			} else if found {
				kept = append(kept, res.Items...)
			}
		} else {
			kept = append(kept, res.Items...)
			if len(kept) > 1000 { // the answer is at the end
				kept = slices.Clone(kept[len(kept)-1000:])
			}
		}
		if res.ToSeq > after {
			after = res.ToSeq
		}
		if !res.MoreAfter {
			break
		}
	}
	if !found {
		return channel.Reading{Last: after}, nil
	}
	var out []string
	for _, it := range channel.Answer(kept) {
		text := it.Text
		if it.FullSize > 0 {
			var full proto.Item
			if err := m.call(ctx, "session.item", map[string]any{"session_id": sid, "item_id": it.ID}, &full); err == nil && full.Text != "" {
				text = full.Text
			}
		}
		out = append(out, strings.TrimSpace(text))
	}
	return channel.Reading{Answer: strings.Join(out, "\n\n"), Found: true, Last: after}, nil
}

// finishChannelRun sends a run's answer and ends it, or — held — keeps it for the turn its background work starts.
func (h *Hub) finishChannelRun(r *channel.Run, res *channel.Result) {
	c := &h.channels
	c.mu.Lock()
	prog := c.live[r.ID]
	c.mu.Unlock()
	if prog == nil {
		return
	}
	prog.mu.Lock()
	if prog.finished {
		prog.mu.Unlock()
		return
	}
	c.mu.Lock()
	now := c.clock()
	took := now.Sub(r.Started)
	snap := *r
	snap.Ext = cloneExt(r.Ext)
	snap.Folded = slices.Clone(r.Folded)
	if res.Held {
		r.Hold(res.Last, now.UTC())
		delete(r.Ext, "card") // the next turn gets its own progress message
		prog.steps, prog.live, prog.dirty = nil, "", false
	} else {
		prog.finished = true
		delete(c.live, r.ID)
		c.st.Runs = slices.DeleteFunc(c.st.Runs, func(o *channel.Run) bool { return o == r })
		if c.answered == nil {
			c.answered = map[string]time.Time{}
		}
		for k, at := range c.answered {
			if now.Sub(at) > time.Hour {
				delete(c.answered, k)
			}
		}
		c.answered[r.SessionID] = now
	}
	others := slices.ContainsFunc(c.st.Runs, func(o *channel.Run) bool { return o.Channel == r.Channel })
	h.saveChannelStateLocked()
	c.mu.Unlock()
	defer prog.mu.Unlock()
	if !res.Held {
		defer close(prog.done)
	}
	sink := h.sinkFor(r.Channel)
	if sink == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	quiet := res.Quiet || strings.TrimSpace(res.Answer) == "" && res.Note == "" && snap.Held
	answer := res.Answer
	if strings.TrimSpace(answer) == "" && res.Note == "" && !quiet {
		answer = "（这一轮没有文字回答）"
	}
	sink.answer(ctx, &snap, answer, res.Note, took, !res.Held, quiet)
	if ts, ok := sink.(channelTyping); ok && !res.Held && !others {
		ts.typing(ctx, false)
	}
}

func cloneExt(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// channelAnswered: the conversation's turn is (or just was) answered in a channel, so the phone's "finished" push and
// the sync mirror leave it alone. "Just": within the finished push's delay (notify.go), which is when it looks — not
// much longer, or the push of the next turn, asked in the app, would be dropped too.
func (h *Hub) channelAnswered(session string) bool {
	window := 30 * time.Second
	if h.cfg != nil {
		window += h.cfg.Notify.doneDelay()
	}
	c := &h.channels
	c.mu.Lock()
	defer c.mu.Unlock()
	h.loadChannelStateLocked(context.Background())
	for _, r := range c.st.Runs {
		if r.SessionID == session {
			return true
		}
	}
	at, ok := c.answered[session]
	return ok && c.clock().Sub(at) < window
}

// channelTurnEnded: a conversation finished a turn (machine.go). Its runs are checked at once rather than on the next
// tick; a turn nobody asked for in a channel is mirrored to those that sync.
func (h *Hub) channelTurnEnded(machine, session string) {
	c := &h.channels
	c.mu.Lock()
	h.loadChannelStateLocked(context.Background())
	var ids []string
	for _, r := range c.st.Runs {
		if r.MachineID == machine && r.SessionID == session {
			r.Seen = true
			if p := c.live[r.ID]; p != nil {
				p.checked = time.Time{} // a turn ended: look now, whatever the last look found
			}
			ids = append(ids, r.ID)
		}
	}
	c.mu.Unlock()
	for _, id := range ids {
		h.checkChannelRun(id)
	}
	if len(ids) == 0 && !h.channelAnswered(session) {
		h.botMirror(machine, session)
		h.extensionMirror(machine, session)
	}
}

// sweepChannelRuns makes sure every run has its watcher (one lost to a restart).
func (h *Hub) sweepChannelRuns() {
	c := &h.channels
	c.mu.Lock()
	h.loadChannelStateLocked(context.Background())
	var lost []*channel.Run
	for _, r := range c.st.Runs {
		if c.live[r.ID] == nil && h.sinkReady(r.Channel) {
			lost = append(lost, r)
		}
	}
	c.mu.Unlock()
	for _, r := range lost {
		go h.watchChannelRun(r)
	}
}

// sinkReady: channel id can be answered now (the Feishu bot is connected; a plugin's is there).
func (h *Hub) sinkReady(id string) bool {
	if id == feishuChannelID {
		api, _ := h.botAPI()
		return api != nil
	}
	_, ok := h.extensionChannel(id)
	return ok
}

// forgetChannelTurns drops what channel id waits on (unbound).
func (h *Hub) forgetChannelTurns(id string) {
	c := &h.channels
	c.mu.Lock()
	defer c.mu.Unlock()
	h.loadChannelStateLocked(context.Background())
	c.st.Runs = slices.DeleteFunc(c.st.Runs, func(r *channel.Run) bool { return r.Channel == id })
	c.st.Asks = slices.DeleteFunc(c.st.Asks, func(a *channel.Ask) bool { return a.Channel == id })
	h.saveChannelStateLocked()
}

// channelWaiting is how many answers and questions channel id waits on.
func (h *Hub) channelWaiting(id string) channel.Waiting {
	c := &h.channels
	c.mu.Lock()
	defer c.mu.Unlock()
	h.loadChannelStateLocked(context.Background())
	var w channel.Waiting
	for _, r := range c.st.Runs {
		if r.Channel == id {
			w.Answers++
		}
	}
	for _, a := range c.st.Asks {
		if a.Channel == id {
			w.Asks++
		}
	}
	return w
}

func (c *channelsHub) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

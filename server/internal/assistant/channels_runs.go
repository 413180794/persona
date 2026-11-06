package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/channel"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
)

// The turns channels wait on (docs/channels.md「跟踪这一轮」): one copy for every channel. A message from a channel
// starts a run; the assistant follows the conversation (the hub sends on its items and the text it writes, like an
// open chat in the app), shows progress where the channel can (a card it changes, 「正在输入」), and when the turn
// ends sends its answer back through the channel's sink. The rules are channel.Run's; what reaches the chat app is
// the sink's.

// channelSink is how a channel's runs reach its chat app.
type channelSink interface {
	// answer sends a run's answer and note (into its progress message when there is one). final: the run is over
	// (its messages are marked done); quiet: there is nothing to say (a held run whose background work ended); end:
	// how a final one ended. An error: it did not go out and may when tried again (the same run, the same refs).
	answer(ctx context.Context, r *channel.Run, answer, note string, took time.Duration, final, quiet bool, end channel.End) error
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
	failing  time.Time // its answer first failed to go out (answerAgain)
	// unsettled: the turn was first read as over without words at its end (its answer may not be written yet)
	unsettled time.Time
	// items are the turn's thinking, words and tool calls (for the Feishu bot's agent message); cot what of them it
	// has shown, under mu.
	items []proto.Item
	cot   cotProgress
	// from is the seq of the run's own message once found (0: not yet: queued behind another turn, or not in);
	// upTo the last item noted; looked when locateRun last read the conversation.
	from, upTo int
	looked     time.Time
	// sentCard is the progress card last sent (its hash), not sent again; backoff and quietUntil hold updates back
	// after Feishu's rate limit. All under mu.
	sentCard   [32]byte
	backoff    time.Duration
	quietUntil time.Time
	// What the progress card shows besides the steps, noted as the items come (liveNote): the calls with no result
	// yet (oldest first), how many calls, the files read and changed, the subagents.
	open         []openCall
	calls        int
	read, edited map[string]bool
	subs         []subtask
	subsMore     int // subtasks dropped from subs (it keeps the latest)
}

// openCall is a tool call with no result yet.
type openCall struct {
	id, step string
	since    time.Time
}

// subtask is a Task / Agent call of the turn.
type subtask struct {
	id, desc, kind string
	done           bool
}

// Bounds of chanLive's notes: a turn of thousands of calls stays small.
const (
	liveOpenMost  = 50
	liveFilesMost = 1000
	liveSubsMost  = 50
)

// liveNote notes a new item for the progress card; now is when it was seen (for an item with no time).
func (p *chanLive) liveNote(l i18n.Lang, it proto.Item, now time.Time) {
	switch it.Kind {
	case proto.KindToolUse:
		p.calls++
		since := it.TS
		if since.IsZero() {
			since = now
		}
		if it.ToolUseID != "" && !slices.ContainsFunc(p.open, func(o openCall) bool { return o.id == it.ToolUseID }) {
			p.open = append(p.open, openCall{id: it.ToolUseID, step: botStep(l, it), since: since})
			if len(p.open) > liveOpenMost {
				p.open = p.open[len(p.open)-liveOpenMost:]
			}
		}
		var in struct {
			FilePath     string `json:"file_path"`
			NotebookPath string `json:"notebook_path"`
			Description  string `json:"description"`
			SubagentType string `json:"subagent_type"`
		}
		_ = json.Unmarshal(it.ToolInput, &in)
		file := in.FilePath
		if file == "" {
			file = in.NotebookPath
		}
		switch it.ToolName {
		case "Read":
			p.read = noteFile(p.read, file)
		case "Edit", "MultiEdit", "Write", "NotebookEdit":
			p.edited = noteFile(p.edited, file)
		case "Task", "Agent":
			p.subs = append(p.subs, subtask{id: it.ToolUseID, desc: clip(oneLine(in.Description), 60), kind: clip(oneLine(in.SubagentType), 30)})
			if len(p.subs) > liveSubsMost {
				p.subsMore += len(p.subs) - liveSubsMost
				p.subs = p.subs[len(p.subs)-liveSubsMost:]
			}
		}
	case proto.KindToolResult:
		p.open = slices.DeleteFunc(p.open, func(o openCall) bool { return o.id == it.ToolUseID })
		for i := range p.subs {
			if p.subs[i].id == it.ToolUseID {
				p.subs[i].done = true
			}
		}
	}
}

func noteFile(m map[string]bool, file string) map[string]bool {
	if file == "" || len(m) >= liveFilesMost {
		return m
	}
	if m == nil {
		m = map[string]bool{}
	}
	m[file] = true
	return m
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

// claimChannelMessage: the first time channel id's message is seen; an error while the messages seen can't be read.
func (s *Service) claimChannelMessage(id, msg string) (bool, error) {
	c := &s.chans
	c.mu.Lock()
	defer c.mu.Unlock()
	s.loadChannelStateLocked(context.Background())
	if !c.stLoaded {
		return false, errChannelsUnread
	}
	seen := c.st.Seen[id]
	if msg == "" || slices.Contains(seen, msg) {
		return false, nil
	}
	seen = append(seen, msg)
	if len(seen) > channelSeenKeep {
		seen = seen[len(seen)-channelSeenKeep:]
	}
	c.st.Seen[id] = seen
	s.saveChannelStateLocked()
	return true, nil
}

var errChannelsUnread = errors.New("the channels' state could not be read")

// sinkFor is how channel id's runs reach it: nil when it has gone (a plugin uninstalled), an error when that can't be
// told now (the hub could not be asked).
func (s *Service) sinkFor(ctx context.Context, id string) (channelSink, error) {
	if id == channel.Feishu {
		return feishuSink{s}, nil
	}
	_, ok, err := s.pluginChannelNow(ctx, id)
	if err != nil || !ok {
		return nil, err
	}
	return extensionSink{s: s, id: id}, nil
}

// channelRuns are channel id's runs now (copies; "" is every channel's).
func (s *Service) channelRuns(id string) []channel.Run {
	c := &s.chans
	c.mu.Lock()
	defer c.mu.Unlock()
	s.loadChannelStateLocked(context.Background())
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
func (s *Service) startChannelRun(r *channel.Run) {
	if r.MachineID == "" || r.SessionID == "" {
		s.logf("channels: %s: no conversation to follow", r.ID)
		return
	}
	c := &s.chans
	c.mu.Lock()
	s.loadChannelStateLocked(context.Background())
	var folded []*channel.Run
	keep := c.st.Runs[:0]
	for _, o := range c.st.Runs {
		if o.Channel == r.Channel && o.MachineID == r.MachineID && o.SessionID == r.SessionID && !c.answering[o.ID] {
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
	s.saveChannelStateLocked()
	c.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	sink, _ := s.sinkFor(ctx, r.Channel)
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
			fs.folded(ctx, o)
		}
	}
	s.noteChannelConversation(r.Channel, r.MachineID, r.SessionID)
	s.channelsChanged()
	if ts, ok := sink.(channelTyping); ok {
		ts.typing(ctx, true)
	}
	go s.watchChannelRun(r)
}

func (s *Service) channelRunByID(id string) (*channel.Run, *chanLive) {
	c := &s.chans
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, r := range c.st.Runs {
		if r.ID == id {
			return r, c.live[id]
		}
	}
	return nil, nil
}

// followSession has the hub send on a conversation's items and the text it writes (session.follow), once for every
// run on it; unfollowSession is the other end.
func (s *Service) followSession(ctx context.Context, machine, session string) {
	k := [2]string{machine, session}
	c := &s.chans
	c.mu.Lock()
	if c.follows == nil {
		c.follows = map[[2]string]int{}
	}
	c.follows[k]++
	first := c.follows[k] == 1
	c.mu.Unlock()
	if first {
		s.askFollow(ctx, machine, session)
	}
}

// followRetryEvery is how long a run's watcher waits before it asks again for a follow that failed.
var followRetryEvery = 30 * time.Second

// askFollow asks for session.follow; when it fails, a run's watcher asks again later (retryFollow): nothing else does
// while the node stays connected, and a machine not known yet is not followed again when it connects.
func (s *Service) askFollow(ctx context.Context, machine, session string) {
	err := s.call(ctx, machine, DeviceSessionFollow, map[string]any{"session_id": session}, nil)
	if err != nil {
		s.logf("channels: follow %s: %v", short(session), err)
	}
	k := [2]string{machine, session}
	c := &s.chans
	c.mu.Lock()
	defer c.mu.Unlock()
	if err == nil || c.follows[k] <= 0 {
		delete(c.followRetry, k)
		return
	}
	if c.followRetry == nil {
		c.followRetry = map[[2]string]time.Time{}
	}
	c.followRetry[k] = time.Now().Add(followRetryEvery)
}

// retryFollow asks again for a follow of the run's conversation that failed, once it is due. The caller is a run's
// watcher, so the conversation stays followed by it while the call is out.
func (s *Service) retryFollow(machine, session string) {
	k := [2]string{machine, session}
	c := &s.chans
	c.mu.Lock()
	at, ok := c.followRetry[k]
	if !ok || time.Now().Before(at) {
		c.mu.Unlock()
		return
	}
	delete(c.followRetry, k) // one watcher asks; askFollow puts it back if it fails again
	c.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s.askFollow(ctx, machine, session)
}

func (s *Service) unfollowSession(machine, session string) {
	k := [2]string{machine, session}
	c := &s.chans
	c.mu.Lock()
	c.follows[k]--
	last := c.follows[k] <= 0
	if last {
		delete(c.follows, k)
		delete(c.followRetry, k)
	}
	c.mu.Unlock()
	if last {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = s.call(ctx, machine, DeviceSessionUnfollow, map[string]any{"session_id": session}, nil)
	}
}

// deviceKnown: the hub has machine id (an error: it could not be asked).
func (s *Service) deviceKnown(ctx context.Context, id string) (Device, bool, error) {
	var res struct {
		Devices []Device `json:"devices"`
	}
	if err := s.call(ctx, DeviceAll, DeviceDevicesList, map[string]any{}, &res); err != nil {
		return Device{}, false, err
	}
	for _, d := range res.Devices {
		if d.ID == id {
			return d, true, nil
		}
	}
	return Device{}, false, nil
}

// watchChannelRun follows a run until it is answered: the conversation's items come to the assistant (followed), for
// the progress shown meanwhile.
func (s *Service) watchChannelRun(r *channel.Run) {
	c := &s.chans
	ctx := context.Background()
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	_, known, err := s.deviceKnown(cctx, r.MachineID)
	cancel()
	if err == nil && !known { // the machine left the configuration: nothing will answer it
		c.mu.Lock()
		c.st.Runs = slices.DeleteFunc(c.st.Runs, func(o *channel.Run) bool { return o == r })
		s.saveChannelStateLocked()
		c.mu.Unlock()
		s.channelsChanged()
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
	fctx, fcancel := context.WithTimeout(ctx, 10*time.Second) // a node that never answers does not hold the watcher
	s.followSession(fctx, r.MachineID, r.SessionID)
	fcancel()
	defer s.unfollowSession(r.MachineID, r.SessionID)
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-prog.done:
			return
		case <-t.C:
		}
		if run, _ := s.channelRunByID(r.ID); run == nil { // dropped (unbound, folded into a later message)
			c.mu.Lock()
			if c.live[r.ID] == prog {
				delete(c.live, r.ID)
			}
			c.mu.Unlock()
			return
		}
		s.retryFollow(r.MachineID, r.SessionID)
		s.checkChannelRun(r.ID)
		s.channelProgressTick(r.ID)
	}
}

// sessionNow is a conversation as the hub lists it now; an error when the hub could not be asked.
func (s *Service) sessionNow(ctx context.Context, machine, session string) (proto.Session, bool, error) {
	var res struct {
		Session proto.Session `json:"session"`
		OK      bool          `json:"ok"`
	}
	if err := s.call(ctx, machine, DeviceSessionGet, map[string]any{"session_id": session}, &res); err != nil {
		return proto.Session{}, false, err
	}
	return res.Session, res.OK, nil
}

func working(c proto.Session) bool { return c.Status == proto.StatusWorking || c.Starting }

// channelProgressTick shows a run's progress when it is due: the progress message after a few seconds and then every
// few, 「正在输入」 again every few seconds.
func (s *Service) channelProgressTick(id string) {
	r, prog := s.channelRunByID(id)
	if r == nil || prog == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	sink, _ := s.sinkFor(ctx, r.Channel)
	c := &s.chans
	if ts, ok := sink.(channelTyping); ok {
		c.mu.Lock()
		now := c.clock()
		maybe := !prog.finished && now.Sub(prog.updated) >= channel.TypingEvery
		held := r.Held
		c.mu.Unlock()
		if !maybe {
			return
		}
		if held {
			if sess, _, err := s.sessionNow(ctx, r.MachineID, r.SessionID); err != nil || !working(sess) {
				return
			}
		}
		c.mu.Lock()
		prog.updated = now
		c.mu.Unlock()
		ts.typing(ctx, true)
		return
	}
	ps, ok := sink.(channelProgress)
	if !ok || !prog.mu.TryLock() {
		return
	}
	defer prog.mu.Unlock()
	c.mu.Lock()
	card, started, held := r.Ext["card"]+r.Ext["cot"], r.Started, r.Held
	c.mu.Unlock()
	after, idle := channel.ProgressAfter, channel.ProgressIdle
	if pa, ok := sink.(interface {
		progressAfter(*channel.Run) time.Duration
	}); ok {
		after = pa.progressAfter(r)
	}
	if pi, ok := sink.(interface{ progressIdle() time.Duration }); ok {
		idle = pi.progressIdle()
	}
	// A new message gets its card before the turn is seen working (it may wait behind another turn, or be starting);
	// a held run only once its background work starts a turn.
	if card == "" && held {
		sess, _, err := s.sessionNow(ctx, r.MachineID, r.SessionID)
		if err != nil || !working(sess) {
			return // nothing going on to show (a held run waiting on its background work)
		}
	}
	s.locateRun(ctx, r, prog)
	c.mu.Lock()
	now := c.clock()
	steps, live, dirty := append([]string{}, prog.steps...), prog.live, prog.dirty
	due := !prog.finished && (card == "" && now.Sub(started) >= after ||
		card != "" && (dirty && now.Sub(prog.updated) >= channel.ProgressEvery || now.Sub(prog.updated) >= idle))
	if due {
		prog.dirty, prog.updated = false, now
	}
	c.mu.Unlock()
	if !due {
		return
	}
	ps.progress(ctx, r, now.Sub(started), steps, live)
}

// setRunExt sets one of a run's channel values and keeps it.
func (s *Service) setRunExt(r *channel.Run, key, value string) {
	c := &s.chans
	c.mu.Lock()
	defer c.mu.Unlock()
	if r.Ext == nil {
		r.Ext = map[string]string{}
	}
	r.Ext[key] = value
	s.saveChannelStateLocked()
}

// SessionItems is session.items: new items of a conversation the runs follow; they note the steps.
func (s *Service) SessionItems(ctx context.Context, p SessionItems) {
	s.registerInlineViews(ctx, p)
	l := s.botLang(ctx) // before s.chans.mu
	c := &s.chans
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.live) == 0 {
		return
	}
	for _, r := range c.st.Runs {
		if r.MachineID != p.MachineID || r.SessionID != p.SessionID {
			continue
		}
		if lp := c.live[r.ID]; lp != nil {
			noteRunItems(l, r, lp, p.Items, c.clock())
		}
	}
}

// noteRunItems notes a run's new items as its steps, only from its own message on: a message queued behind another
// turn would show that turn's steps. Under c.mu.
func noteRunItems(l i18n.Lang, r *channel.Run, lp *chanLive, items []proto.Item, now time.Time) {
	items = slices.DeleteFunc(slices.Clone(items), func(it proto.Item) bool { return it.Seq <= max(r.Base, lp.upTo) })
	if lp.from == 0 {
		i, ok := r.Find(items, nil)
		if !ok || len(items) == 0 {
			return
		}
		items, lp.from = items[i:], max(items[i].Seq, 1)
	}
	for _, it := range items {
		lp.upTo = it.Seq
		lp.liveNote(l, it, now)
		if st := botStep(l, it); st != "" {
			lp.steps = append(lp.steps, st)
			lp.dirty = true
		}
		if cotItem(it) && len(lp.items) < cotItemsMost {
			lp.items = append(lp.items, it)
			lp.dirty = true
		}
		if it.Kind == proto.KindText && it.Role == "assistant" {
			lp.live = ""
		}
	}
	if len(lp.steps) > 200 {
		lp.steps = lp.steps[len(lp.steps)-200:]
	}
}

// locateRun looks for a run's message in the conversation when the followed items have not shown it (it went in
// before the run followed the conversation, or the assistant restarted): one page after Base, every few seconds.
func (s *Service) locateRun(ctx context.Context, r *channel.Run, prog *chanLive) {
	l := s.botLang(ctx) // before s.chans.mu
	c := &s.chans
	c.mu.Lock()
	now := c.clock()
	due := prog.from == 0 && now.Sub(prog.looked) >= channel.ProgressEvery
	if due {
		prog.looked = now
	}
	base := r.Base
	c.mu.Unlock()
	if !due {
		return
	}
	var res struct {
		Items []proto.Item `json:"items"`
	}
	if s.call(ctx, r.MachineID, DeviceSessionItems, map[string]any{"session_id": r.SessionID, "after_seq": base, "lazy_images": true}, &res) != nil {
		return
	}
	c.mu.Lock()
	noteRunItems(l, r, prog, res.Items, c.clock())
	c.mu.Unlock()
}

// SessionDelta is session.delta: the text a followed conversation is writing right now.
func (s *Service) SessionDelta(_ context.Context, p SessionDelta) {
	c := &s.chans
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.live) == 0 {
		return
	}
	for _, r := range c.st.Runs {
		if r.MachineID == p.MachineID && r.SessionID == p.SessionID {
			if lp := c.live[r.ID]; lp != nil && lp.live != p.Text {
				lp.live, lp.dirty = p.Text, true
			}
		}
	}
}

// hasPending: a card of the conversation waits for an answer.
func (s *Service) hasPending(ctx context.Context, machine, session string) bool {
	var res struct {
		Interactions []proto.Interaction `json:"interactions"`
	}
	if err := s.call(ctx, DeviceAll, DeviceInteractionsList, map[string]any{}, &res); err != nil {
		return false
	}
	return slices.ContainsFunc(res.Interactions, func(ix proto.Interaction) bool { return ix.MachineID == machine && ix.SessionID == session })
}

// pendingIDs are the cards waiting for an answer now (nil when the hub could not be asked).
func (s *Service) pendingIDs(ctx context.Context) map[string]bool {
	var res struct {
		Interactions []proto.Interaction `json:"interactions"`
	}
	if err := s.call(ctx, DeviceAll, DeviceInteractionsList, map[string]any{}, &res); err != nil {
		return nil
	}
	out := make(map[string]bool, len(res.Interactions))
	for _, ix := range res.Interactions {
		out[ix.ID] = true
	}
	return out
}

// checkChannelRun answers a run whose turn ended (channel.Run.Check and Decide).
func (s *Service) checkChannelRun(id string) {
	r, prog := s.channelRunByID(id)
	if r == nil || prog == nil {
		return
	}
	if !prog.checking.TryLock() {
		return // another check is on it
	}
	defer prog.checking.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	sess, exists, err := s.sessionNow(ctx, r.MachineID, r.SessionID)
	if err != nil {
		return // the hub could not be asked: next tick
	}
	look := channel.Look{Exists: exists, Working: working(sess), Offline: sess.Status == proto.StatusOffline, Background: channel.Background(sess),
		Lang: s.botLang(ctx)}
	if !exists {
		d, _, _ := s.deviceKnown(ctx, r.MachineID)
		look.Online = d.Online
	} else if !look.Working {
		look.Waiting = sess.Status == proto.StatusBlocked || s.hasPending(ctx, r.MachineID, r.SessionID)
		look.Failed = !look.Waiting && func() bool { e, _ := turnError(sess); return e != "" }()
	}
	c := &s.chans
	c.mu.Lock()
	look.Now = c.clock()
	step := r.Check(look)
	snap := *r
	snap.MsgIDs = slices.Clone(r.MsgIDs)
	c.mu.Unlock()
	switch step.Do {
	case channel.Wait:
		c.mu.Lock()
		prog.unsettled = time.Time{} // working or waiting again: a later read of its end settles afresh
		c.mu.Unlock()
		return
	case channel.Finish:
		s.finishChannelRun(r, &channel.Result{Note: step.Note, Quiet: step.Note == "", End: step.End})
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
	rd, err := s.readTurn(ctx, &snap)
	if err != nil {
		s.logf("channels: read the answer: %v", err)
		return // next tick
	}
	c.mu.Lock()
	prog.notFound = !rd.Found
	c.mu.Unlock()
	if !rd.Found {
		if snap.Queued(look.Now) {
			return // the message waits behind the turn that just ended: it starts the next one
		}
		// The message never reached the conversation even past QueuedMost. Reading the whole tail would answer with
		// the previous turn's last words, and the run's deletion would leave the real answer (when the message does
		// start) unclaimed: give up and ask the owner to resend instead (#4455).
		s.logf("channels: %s: the message never showed in the conversation; asking for a resend", r.ID)
		s.finishChannelRun(r, &channel.Result{Note: i18n.T(look.Lang, "assistant-channels_runs.not_delivered"), End: channel.EndGaveUp})
		return
	}
	if rd.Stopped == "" && !look.Working {
		rd.Stopped = channel.StopReason(sess.TurnError) // a Codex turn the model service failed writes no line of it
	}
	res := snap.Decide(rd, look)
	if res == nil {
		return
	}
	if res.Note != "" && channel.Placeholder(res.Answer) {
		res.Answer = "" // the note (its background work goes on) without the placeholder
	}
	if !res.Quiet && res.Note == "" && res.End == "" {
		empty := strings.TrimSpace(res.Answer) == "" || channel.Placeholder(res.Answer)
		// The node says a turn is over a moment before its last words are in the transcript: a turn read as over
		// without words at its end is read again for a few seconds before it counts as one without an answer.
		c.mu.Lock()
		unsure := !rd.Words || empty
		if !unsure {
			prog.unsettled = time.Time{}
		} else if prog.unsettled.IsZero() {
			prog.unsettled = look.Now
		}
		if unsure {
			prog.checked = time.Time{}
		}
		settling := unsure && look.Now.Sub(prog.unsettled) < channel.AnswerSettle
		c.mu.Unlock()
		if settling {
			return
		}
		if empty && res.Stopped == "" {
			if !snap.Asked() && s.askAgain(ctx, r, prog, rd.Last) {
				return
			}
			reason := s.noAnswerReason(look.Lang, res.Answer)
			s.logf("channels: %s: no answer this turn: %s", r.ID, reason)
			res.Answer = i18n.T(look.Lang, "assistant-channels_runs.no_answer", i18n.Args{"reason": reason})
		}
	}
	s.finishChannelRun(r, res)
}

// askAgain sends the conversation of a turn that ended without an answer (nothing, or a placeholder like 「No
// response requested.」) a message asking for it, once; the run then waits for that turn. False when it was not
// sent (a coding agent's conversation is not asked: the message would land in its work).
func (s *Service) askAgain(ctx context.Context, r *channel.Run, prog *chanLive, last int) bool {
	if r.TargetKind == channel.TargetAgent {
		return false
	}
	l := s.botLang(ctx)
	name := r.Channel
	if r.Channel == channel.Feishu {
		name = i18n.T(l, "assistant-channels_runs.feishu")
	} else if v, ok := s.pluginChannel(ctx, r.Channel); ok && v.Name != "" {
		name = v.Name
	}
	text := prompts.RenderChannelAnswerAgain(prompts.ChannelAnswerAgain{Name: name})
	_, body, _ := strings.Cut(text, "\n") // the first line is the context tag
	id := channel.ClientMsgID(r.Channel, r.ID+"-again")
	p, err := s.SendParams(ctx, SendParams{MachineID: r.MachineID, SessionID: r.SessionID,
		Params: map[string]any{"session_id": r.SessionID, "text": text, "client_msg_id": id}})
	if err == nil {
		err = s.sendOnce(ctx, r.MachineID, p)
	}
	if err != nil {
		s.logf("channels: %s: no answer this turn; asking again failed: %v", r.ID, err)
		return false
	}
	s.logf("channels: %s: no answer this turn; asked the conversation again", r.ID)
	c := &s.chans
	c.mu.Lock()
	r.AskAgain(last, id, channel.Probe(body), c.clock().UTC())
	prog.unsettled, prog.checked, prog.notFound = time.Time{}, time.Time{}, false
	s.saveChannelStateLocked()
	c.mu.Unlock()
	return true
}

// noAnswerReason is why a turn has no answer, for the owner.
func (s *Service) noAnswerReason(l i18n.Lang, answer string) string {
	if channel.Placeholder(answer) {
		return i18n.T(l, "assistant-channels_runs.reason_placeholder", i18n.Args{"text": strings.TrimSpace(answer)})
	}
	return i18n.T(l, "assistant-channels_runs.reason_silent")
}

// busTranscript is a conversation's transcript as the assistant reads it, over the hub's bus (channel.ReadTurn's source).
type busTranscript struct {
	s                *Service
	machine, session string
}

func (b busTranscript) ItemsAfter(ctx context.Context, after int) ([]proto.Item, bool, int, error) {
	var res struct {
		Items     []proto.Item `json:"items"`
		MoreAfter bool         `json:"more_after"`
		ToSeq     int          `json:"to_seq"`
	}
	err := b.s.call(ctx, b.machine, DeviceSessionItems, map[string]any{"session_id": b.session, "after_seq": after, "lazy_images": true}, &res)
	return res.Items, res.MoreAfter, res.ToSeq, err
}

func (b busTranscript) Full(ctx context.Context, id string) (proto.Item, error) {
	var full proto.Item
	err := b.s.call(ctx, b.machine, "session.item", map[string]any{"session_id": b.session, "item_id": id}, &full)
	return full, err
}

// readTurn is what the run's conversation answered after its base (channel.ReadTurn, read over the hub's bus).
func (s *Service) readTurn(ctx context.Context, r *channel.Run) (channel.Reading, error) {
	return channel.ReadTurn(ctx, busTranscript{s: s, machine: r.MachineID, session: r.SessionID}, r)
}

// finishChannelRun sends a run's answer and ends it, or — held — keeps it for the turn its background work starts.
// The run stays until its answer is handed to the hub: a sink that can't be reached now (the bus, the hub, a
// plugin's program restarting) is tried again on the next look, for a while (channelAnswerTries), with the same
// refs (a plugin sends a ref once).
func (s *Service) finishChannelRun(r *channel.Run, res *channel.Result) {
	c := &s.chans
	c.mu.Lock()
	prog := c.live[r.ID]
	if prog != nil {
		// Its answer is about to go out, so a later message must not fold this run in. It is marked before waiting
		// for prog.mu: a progress update may hold that lock for a while. When it was marked only after the lock, a
		// message that came in that time folded this run in, and the same answer went out twice: here, under its own
		// message, and again in the later message's answer.
		if c.answering == nil {
			c.answering = map[string]bool{}
		}
		c.answering[r.ID] = true
	}
	c.mu.Unlock()
	if prog == nil {
		return
	}
	defer func() {
		c.mu.Lock()
		delete(c.answering, r.ID)
		c.mu.Unlock()
	}()
	prog.mu.Lock()
	defer prog.mu.Unlock()
	if prog.finished {
		return
	}
	c.mu.Lock()
	now := c.clock()
	took := now.Sub(r.Started)
	snap := *r
	snap.Ext = cloneExt(r.Ext)
	snap.Folded = slices.Clone(r.Folded)
	c.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	sink, err := s.sinkFor(ctx, r.Channel)
	if err != nil {
		if s.answerAgain(prog, now) {
			s.logf("channels: %s: where to answer: %v; trying again", r.ID, err)
			return
		}
		s.logf("channels: %s: where to answer: %v; giving up on it", r.ID, err)
	}
	if sink != nil {
		quiet := res.Quiet || strings.TrimSpace(res.Answer) == "" && res.Note == "" && snap.Held
		answer := res.Answer
		if strings.TrimSpace(answer) == "" && res.Note == "" && !quiet {
			answer = emptyAnswer(s.botLang(ctx), res.Stopped)
		}
		if err := sink.answer(ctx, &snap, answer, res.Note, took, !res.Held, quiet, res.End); err != nil {
			if s.answerAgain(prog, now) {
				s.logf("channels: %s: the answer did not go out: %v; trying again", r.ID, err)
				return
			}
			s.logf("channels: %s: the answer did not go out: %v; giving up on it", r.ID, err)
		}
	}
	c.mu.Lock()
	prog.failing = time.Time{}
	if res.Held {
		r.Hold(res.Last, now.UTC())
		prog.unsettled = time.Time{}
		delete(r.Ext, "card") // the next turn gets its own progress message
		for _, k := range []string{"cot", "cot_msg", "cot_para", "cot_calls", "cot_done", "cot_off"} {
			delete(r.Ext, k) // the next turn gets its own agent message (and asks for one again)
		}
		prog.steps, prog.live, prog.dirty = nil, "", false
		prog.from, prog.upTo = 0, 0
		prog.open, prog.calls, prog.read, prog.edited, prog.subs, prog.subsMore = nil, 0, nil, nil, nil, 0
		prog.items, prog.cot = nil, cotProgress{}
	} else {
		prog.finished = true
		delete(c.live, r.ID)
		c.st.Runs = slices.DeleteFunc(c.st.Runs, func(o *channel.Run) bool { return o == r })
		if c.answered == nil {
			c.answered = map[[2]string]time.Time{}
		}
		for k, at := range c.answered {
			if now.Sub(at) > time.Hour {
				delete(c.answered, k)
			}
		}
		c.answered[[2]string{r.MachineID, r.SessionID}] = now
	}
	others := slices.ContainsFunc(c.st.Runs, func(o *channel.Run) bool { return o.Channel == r.Channel })
	s.saveChannelStateLocked()
	c.mu.Unlock()
	if res.Held {
		return
	}
	close(prog.done)
	s.channelsChanged()
	if ts, ok := sink.(channelTyping); ok && !others {
		ts.typing(ctx, false)
	}
}

// channelAnswerTries is how long an answer that can't be handed over is tried again before the run gives up on it.
var channelAnswerTries = 2 * time.Minute

// answerAgain: the answer failed now; true while it is worth trying again (the first failure starts the clock).
func (s *Service) answerAgain(prog *chanLive, now time.Time) bool {
	c := &s.chans
	c.mu.Lock()
	defer c.mu.Unlock()
	if prog.failing.IsZero() {
		prog.failing = now
	}
	prog.checked = time.Time{} // the next look reads again at once
	return now.Sub(prog.failing) < channelAnswerTries
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

// channelAnswered: the conversation's turn is (or just was) answered in a channel, so the sync mirror leaves it
// alone (the hub's finished push goes by the same, from the snapshot). "Just": within the finished push's delay.
func (s *Service) channelAnswered(ctx context.Context, machine, session string) bool {
	window := 30*time.Second + time.Duration(s.hello(ctx).DoneDelayMS)*time.Millisecond
	c := &s.chans
	c.mu.Lock()
	defer c.mu.Unlock()
	s.loadChannelStateLocked(ctx)
	for _, r := range c.st.Runs {
		if r.SessionID == session {
			return true
		}
	}
	at, ok := c.answered[[2]string{machine, session}]
	return ok && c.clock().Sub(at) < window
}

// channelTurnEnded: a conversation finished a turn (session.turn_ended). Its runs are checked at once rather than on
// the next tick; a turn nobody asked for in a channel is mirrored to those that sync — not a quiet task's normal finish
// (quietTaskTurn, #2396), whose news, if any, went out with notify_user.
func (s *Service) channelTurnEnded(ctx context.Context, machine, session string) {
	c := &s.chans
	c.mu.Lock()
	s.loadChannelStateLocked(ctx)
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
		s.checkChannelRun(id)
	}
	if len(ids) == 0 && !s.quietTaskTurn(ctx, machine, session) && !s.channelAnswered(ctx, machine, session) {
		s.feishuMirror(ctx, machine, session)
		s.extensionMirror(ctx, machine, session)
	}
}

// sweepChannelRuns makes sure every run has its watcher (one lost to a restart).
func (s *Service) sweepChannelRuns() {
	c := &s.chans
	c.mu.Lock()
	s.loadChannelStateLocked(context.Background())
	var lost []*channel.Run
	for _, r := range c.st.Runs {
		if c.live[r.ID] == nil {
			lost = append(lost, r)
		}
	}
	c.mu.Unlock()
	if len(lost) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for _, r := range lost {
		if s.sinkReady(ctx, r.Channel) {
			go s.watchChannelRun(r)
		}
	}
}

// sinkReady: channel id can be answered now (the Feishu bot is connected and paired; a plugin's is there).
func (s *Service) sinkReady(ctx context.Context, id string) bool {
	if id == channel.Feishu {
		st, err := s.feishuStatusKept(ctx)
		return err == nil && st.Ready
	}
	_, ok := s.pluginChannel(ctx, id)
	return ok
}

// forgetChannelTurns drops what channel id waits on (unbound).
func (s *Service) forgetChannelTurns(id string) {
	c := &s.chans
	c.mu.Lock()
	s.loadChannelStateLocked(context.Background())
	c.st.Runs = slices.DeleteFunc(c.st.Runs, func(r *channel.Run) bool { return r.Channel == id })
	c.st.Asks = slices.DeleteFunc(c.st.Asks, func(a *channel.Ask) bool { return a.Channel == id })
	s.saveChannelStateLocked()
	c.mu.Unlock()
	s.channelsChanged()
}

// channelWaiting is how many answers and questions channel id waits on.
func (s *Service) channelWaiting(id string) channel.Waiting {
	c := &s.chans
	c.mu.Lock()
	defer c.mu.Unlock()
	s.loadChannelStateLocked(context.Background())
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

// errCannotSend: the channel has nowhere to send to yet (WeChat before the owner's first message); errNoChannel: the
// channel is gone (a plugin uninstalled).
var (
	errCannotSend = errors.New("cannot send yet")
	errNoChannel  = errors.New("no channel")
)

// emptyAnswer is what a channel is told of a turn that ended without words: why it stopped, when the conversation
// says (an API error, an interruption), so the owner need not open the app to find out.
func emptyAnswer(l i18n.Lang, stopped string) string {
	if stopped = strings.TrimSpace(stopped); stopped != "" {
		return i18n.T(l, "assistant-channels_runs.no_text_stopped", i18n.Args{"reason": stopped})
	}
	return i18n.T(l, "assistant-channels_runs.no_text")
}

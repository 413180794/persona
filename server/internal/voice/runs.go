package voice

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/channel"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
)

// A task the voice model hands over is a run (docs/voice.md「一轮跟踪」): one channel.Run, as the assistant's channels
// have, found in the conversation by its message's client_msg_id (voice-<random>) or its first line, followed until
// that turn is over and then read whole (channel.ReadTurn over the hub's HTTP API), so what the voice model hears is
// that turn's whole answer: not the conversation's last 200 characters, not another turn's. Its words go back on the
// delegation it came from (that delegation's one commentary) while that is still open, else as something to say.
//
// The target is the Persona main conversation (protocol 1). Each conversation has a tracker: one goroutine that owns
// its runs and checks them when the hub link says their conversation changed, and every 5 s regardless.

// voiceRun is one handed-over task being followed.
type voiceRun struct {
	channel.Run
	Tasks []string // what was asked, oldest first (a run that folded earlier ones answers them too)
	// Delegation is the upstream delegation its words go back on, of upstream session Gen; "" once it had its one
	// commentary (a held run's second answer is said).
	Delegation string
	Gen        int
	appIDs     []string // the app's 「Persona 主对话处理中」 lines that end with it

	checked  time.Time // its answer was last read
	notFound bool      // that read did not find its message
	ended    bool      // the hub said a turn ended there since
	waiting  bool      // the user was told it waits for them (until it works again)
	rev      int64     // the conversation's transcript before the send (proto.Session.Rev)
}

const (
	taskMost     = 80 // characters of a task the voice model is reminded of
	checkTimeout = time.Minute
)

var (
	lookEvery = 5 * time.Second  // a run is checked this often whatever the hub link says
	readEvery = 10 * time.Second // a run not seen working, or whose message was not there, is read this often
	clock     = time.Now         // what a check takes as now (a test moves it on)
)

type tracker struct {
	c     *conversation
	link  *linkListener
	ops   chan func()
	runs  []*voiceRun
	last  time.Time             // the last check of everything
	ended map[linkKey]time.Time // when the hub last said a turn ended there (one may end while its message is sent)
}

func newTracker(c *conversation, link *linkListener) *tracker {
	return &tracker{c: c, link: link, ops: make(chan func(), 16), ended: map[linkKey]time.Time{}}
}

// loop owns the runs until the conversation ends: then they are dropped (they are not kept anywhere; what was sent goes
// on in its conversation as typed).
func (t *tracker) loop(ctx context.Context) {
	defer func() {
		for _, r := range t.runs {
			t.link.unwatch(linkKey{r.MachineID, r.SessionID})
		}
		t.runs = nil
		t.link.close()
	}()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		due := false
		select {
		case <-ctx.Done():
			return
		case op := <-t.ops:
			op()
		case ev := <-t.link.events:
			t.event(ev)
			due = true
		case now := <-tick.C:
			due = t.link.dirty.Swap(false) || now.Sub(t.last) >= lookEvery
		}
		if due && len(t.runs) > 0 {
			t.checkAll(ctx)
		}
	}
}

// event marks the runs a turn ended on: they are read at once, not on readEvery.
func (t *tracker) event(ev linkEvent) {
	if !ev.ended {
		return
	}
	t.ended[ev.key] = time.Now()
	for _, r := range t.runs {
		if ev.key == (linkKey{r.MachineID, r.SessionID}) {
			r.ended, r.Seen = true, true
		}
	}
}

func (t *tracker) checkAll(ctx context.Context) {
	t.last = time.Now()
	for _, r := range slices.Clone(t.runs) {
		if ctx.Err() != nil {
			return
		}
		t.check(ctx, r)
	}
}

// adopt starts following r; the runs not yet answered on the same conversation are folded into it (the user said more
// while it worked): one answer covers them, in the order the conversation took the messages.
func (t *tracker) adopt(r *voiceRun) {
	keep := t.runs[:0]
	for _, o := range t.runs {
		if o.MachineID != r.MachineID || o.SessionID != r.SessionID {
			keep = append(keep, o)
			continue
		}
		r.Fold(&o.Run)
		if !o.Held {
			r.Tasks = append(slices.Clone(o.Tasks), r.Tasks...)
		}
		r.appIDs = append(o.appIDs, r.appIDs...)
		t.link.unwatch(linkKey{o.MachineID, o.SessionID}) // r holds its own watch
	}
	t.runs = append(keep, r)
	if at, ok := t.ended[linkKey{r.MachineID, r.SessionID}]; ok && !at.Before(r.Started) {
		r.ended, r.Seen = true, true // a turn ended while its message was being sent
	}
	t.last = time.Time{} // look at it on the next tick
}

func (t *tracker) drop(r *voiceRun) {
	t.runs = slices.DeleteFunc(t.runs, func(o *voiceRun) bool { return o == r })
	t.link.unwatch(linkKey{r.MachineID, r.SessionID})
}

func working(s *proto.Session) bool { return s.Status == proto.StatusWorking || s.Starting }

// check is channel.Run's rules against the conversation now (the assistant's checkChannelRun, over HTTP): wait, read
// the answer, or give up; an answer, a stop or a card waiting for the user is told to the voice model.
func (t *tracker) check(ctx context.Context, r *voiceRun) {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	hub, l := t.c.pl.hub, t.c.language()
	sess, err := hub.session(ctx, r.MachineID, r.SessionID)
	if err != nil {
		return // the hub could not be asked: the next look
	}
	now := clock()
	look := channel.Look{Now: now, Exists: sess != nil, Lang: l}
	if sess != nil {
		look.Working, look.Offline, look.Background = working(sess), sess.Status == proto.StatusOffline, channel.Background(*sess)
		if !look.Working {
			look.Waiting = sess.Status == proto.StatusBlocked || sess.State == proto.StateNeedsYou || sess.PendingInteractions > 0
		}
		if !r.Seen && !look.Working && sess.Rev != 0 && sess.Rev != r.rev {
			// Its transcript moved since the send: a short turn may have come and gone between two looks (no hub link
			// to say so). Reading finds out: a message not answered yet reads as not found.
			r.Seen = true
		}
	} else {
		look.Online, _ = hub.machineOnline(ctx, r.MachineID)
	}
	switch {
	case look.Waiting && !r.waiting:
		r.waiting = true // once until it works again: the user answers on the phone
		t.c.tell(r, plainWords(prompts.RenderVoiceReport(prompts.VoiceReport{Lang: string(l), Status: "needs_you", Tasks: r.Tasks})), false, false)
	case look.Working:
		r.waiting = false
	}
	step := r.Check(look)
	switch step.Do {
	case channel.Wait:
		return
	case channel.Finish:
		t.finish(r, &channel.Result{Note: step.Note, Quiet: step.Note == ""}, look)
		return
	}
	// The end was not seen (it came between looks, or never), or the message was not there last time: read now and
	// then, not every look.
	if (!r.Seen || r.notFound) && !r.ended && now.Sub(r.checked) < readEvery {
		return
	}
	r.checked, r.ended = now, false
	src := httpItems{hub: hub, machine: r.MachineID, session: r.SessionID}
	rd, err := channel.ReadTurn(ctx, src, &r.Run)
	if err == nil && !rd.Found && r.Base > 0 {
		// The transcript ends before the base (rewound, rewritten): find the message from the start.
		if last, lerr := hub.lastSeq(ctx, r.MachineID, r.SessionID); lerr == nil && last < r.Base {
			t.c.pl.logf("run %s: the conversation ends at %d, before its base %d: reading it from the start", r.ID, last, r.Base)
			r.Base = 0
			rd, err = channel.ReadTurn(ctx, src, &r.Run)
		}
	}
	if err != nil {
		t.c.pl.logf("run %s: read the answer: %v", r.ID, err)
		return
	}
	r.notFound = !rd.Found
	if !rd.Found {
		if now.Sub(r.Started) < channel.QueuedMost {
			return // it waits behind another turn: it starts the next one
		}
		t.c.pl.logf("run %s: its message never showed in the conversation: answering with its last words", r.ID)
		snap := r.Run
		snap.MsgIDs, snap.Probe = nil, ""
		if rd, err = channel.ReadTurn(ctx, src, &snap); err != nil {
			return
		}
	}
	if rd.Stopped == "" && !look.Working && sess != nil {
		rd.Stopped = channel.StopReason(sess.TurnError) // a Codex turn the model service failed writes no line of it
	}
	if res := r.Decide(rd, look); res != nil {
		t.finish(r, res, look)
	}
}

// finish tells the voice model a run's result: its answer, or why there is none. A held one (its background work goes
// on) is answered now and followed on for the turn that work starts.
func (t *tracker) finish(r *voiceRun, res *channel.Result, look channel.Look) {
	l := string(t.c.language())
	t.c.toolsDone(r.appIDs)
	r.appIDs = nil
	switch {
	case res.Quiet:
		// nothing to say: a held run whose background work ended without words
	case res.Note != "" && strings.TrimSpace(res.Answer) == "" && !res.Held:
		t.c.tell(r, plainWords(prompts.RenderVoiceReport(prompts.VoiceReport{Lang: l, Status: "failed", Tasks: r.Tasks, Reason: res.Note})), true, false)
	default:
		rep := prompts.VoiceReport{Lang: l, Status: "done", Tasks: r.Tasks, Stopped: res.Stopped, Again: r.Held}
		if res.Held {
			rep.Background = look.Background
		}
		t.c.tell(r, reportWords(i18n.Lang(l), rep, res.Answer), true, true)
	}
	if res.Held {
		r.Hold(res.Last, time.Now())
		r.Delegation, r.waiting = "", false
		return
	}
	t.drop(r)
}

// handOver sends task to the main conversation and follows the run it starts, in the background. delegation is where
// its words go back (of upstream session gen); appID the app's line for it.
func (t *tracker) handOver(task, delegation string, gen int, appID string, history []prompts.VoiceLine) {
	c := t.c
	l := c.language()
	r := &voiceRun{Tasks: []string{clipRunes(task, taskMost)}, Delegation: delegation, Gen: gen, appIDs: []string{appID}}
	r.ID, r.Channel, r.Probe, r.Started = appID, "voice", channel.Probe(task), time.Now()
	message := prompts.RenderVoiceDelegated(prompts.VoiceDelegated{Lang: string(l), Task: task, History: history})
	go func() {
		if err := t.send(c.ctx, r, message); err != nil {
			if c.ctx.Err() != nil {
				return
			}
			c.pl.logf("hand over: %v", err)
			code, reason := sendFailure(err)
			c.toolsDone(r.appIDs)
			c.tell(r, plainWords(prompts.RenderVoiceRefused(prompts.VoiceRefused{Lang: string(l), Code: code, Task: r.Tasks[0], Reason: reason})), true, false)
			return
		}
		select {
		case t.ops <- func() { t.adopt(r) }:
		case <-c.ctx.Done():
			t.link.unwatch(linkKey{r.MachineID, r.SessionID})
		}
	}()
}

// send puts the message into the main conversation (starting one when there is none) and fills in where r's answer
// will be: the conversation, its last line before the message, the message's id. The run's conversation is watched
// from before the send on, so its turn's end is not missed.
func (t *tracker) send(ctx context.Context, r *voiceRun, message string) error {
	hub := t.c.pl.hub
	machine, main, err := hub.assistant(ctx)
	if err != nil {
		return err
	}
	if main == "" {
		sess, err := hub.startMain(ctx, message)
		if err != nil {
			return maybeSent(err)
		}
		r.MachineID, r.SessionID = firstNonEmpty(sess.MachineID, machine), sess.SessionID
		t.link.watch(linkKey{r.MachineID, r.SessionID}, 0)
		return nil // a new conversation: the message is its first line (found by the probe)
	}
	r.MachineID, r.SessionID = machine, main
	key := linkKey{machine, main}
	sess, _ := hub.session(ctx, machine, main)
	if sess != nil {
		r.rev = sess.Rev
	}
	base, err := hub.lastSeq(ctx, machine, main)
	if err != nil {
		t.c.pl.logf("run %s: the conversation's last line: %v (its message is found from the start)", r.ID, err)
	}
	r.Base = base
	t.link.watch(key, base)
	id := voiceMessageID()
	sctx, cancel := context.WithTimeout(ctx, sendTimeout(sess))
	defer cancel()
	if err := hub.send(sctx, machine, main, message, id); err != nil {
		t.link.unwatch(key)
		return maybeSent(err)
	}
	r.MsgIDs = []string{id}
	return nil
}

// words is what the model is told: text, and short for a service that holds a commentary to an append's tokens (it
// refuses text, and short is sent instead: session.go, deliver).
type words struct{ text, short string }

func plainWords(text string) words { return words{text: text, short: clipTokens(text, appendTokens)} }

// reportWords is a done report with as much of the turn's answer, made speakable, as fits: in a commentary
// (commentaryRunes, with the report's own words and the tasks around it) and, for short, in an append's tokens. A cut
// answer keeps its start and its end (clipMiddle), so the block around it is always whole.
func reportWords(l i18n.Lang, rep prompts.VoiceReport, answer string) words {
	whole := speakable(l, answer)
	render := func(most int) string {
		rep.Answer = clipMiddle(l, whole, most)
		return prompts.RenderVoiceReport(rep)
	}
	top := min(spokenMost, runeLen(whole))
	short := fit(render, top, func(s string) bool { return tokens(s) <= appendTokens })
	return words{
		text:  fit(render, top, func(s string) bool { return runeLen(s) <= commentaryRunes }),
		short: clipTokens(short, appendTokens), // the tasks alone may be more
	}
}

// fit is render(most) for the largest most up to top whose text is ok (render(0) when none is).
func fit(render func(most int) string, top int, ok func(string) bool) string {
	if s := render(top); ok(s) {
		return s
	}
	best := render(0)
	for lo, hi := 1, top-1; lo <= hi; {
		mid := (lo + hi) / 2
		if s := render(mid); ok(s) {
			best, lo = s, mid+1
		} else {
			hi = mid - 1
		}
	}
	return best
}

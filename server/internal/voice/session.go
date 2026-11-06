package voice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/prompts"
)

// One voice conversation: the app's WebSocket on one side (docs/extensions.md "Voice"), a GPT-Live session on the other.
// The upstream session can drop mid-conversation: the app's socket stays open, shows "Reconnecting" and the
// conversation goes on in a new session told what was said so far.

// stableAfter is how long an upstream session holds before a drop of it no longer counts toward maxReconnects.
var stableAfter = time.Minute

const (
	maxReconnects = 3  // in a row: a session that held stableAfter starts the count over
	historyLines  = 40 // what a reconnected session is told was said
	recentLines   = 6  // what a handed-over task is sent with of the conversation
	startTimeout  = 25 * time.Second
	// What was sent with an event id and not acked this long is let go (a service that does not ack after all).
	unackedMost = 5 * time.Minute
	// A delegation the model makes while it also calls a tool for it (live dialect with tools) is only the tool's
	// envelope; one with no tool call this soon is work for the main conversation.
	toolCallWait = 2 * time.Second
	repliedMost  = 8 // the model's replies that were over a later word of them is checked against
)

type line struct {
	id, text, source string
}

type conversation struct {
	pl     *Plugin
	app    *websocket.Conn
	ctx    context.Context
	cancel context.CancelFunc
	out    chan outFrame // to the app, in order (one writer)
	// role is the caller's (X-Persona-Role: full, extension; "" from a hub that does not say, taken as full) and lang
	// the language the hub answers it in ("" when not said: the hub's own).
	role string
	lang i18n.Lang
	runs *tracker
	// version is the voice wire the app speaks: 1, or 2 when its start says so (docs/voice.md "Protocol changes"). target is
	// the conversation a protocol 2 call is bound to, fixed before ready; prep its binding, taken while the upstream
	// connects; resume the runs the app handed back (a call picked up again).
	version int
	target  *boundTarget
	prep    chan prepared
	resume  []resumeRun
	resumed bool // the app picked the call up again: its first session is told what was said
	wake    chan struct{}

	mu         sync.Mutex
	closed     bool
	p          *Provider
	w          wire
	model      string // the provider's model id
	modelID    string // <provider>/<model>, what the app knows
	voice      string
	up         upLink
	gen        int // which upstream session: a reconnect makes a new one
	ready      bool
	started    bool // the app got its ready
	outRate    int
	reconnects int

	userSeq, asstSeq int
	user, asst       *line
	history          []prompts.VoiceLine
	sawTranscript    bool // the service sends the model's speech as transcript deltas (text deltas are then the same words)
	dropAudio        bool // the app interrupted: drop the rest of the answer
	lastAudio        time.Time
	delegations      map[string]int         // open delegation id -> the session (gen) it belongs to
	pending          map[string]*time.Timer // delegations waiting for their tool call
	tasks            int
	gate             speechGate
	unacked          map[string]*sentWords // what deliver sent with an event id, until the service acks or refuses it
	events           int                   // event ids given
	snap             *prompts.VoiceTarget  // what the voice model is told of the bound conversation
	snapWords        string                // what of the snapshot the session took in came from agents (agentWords)
	open             []openTask            // the runs not over yet (a new session is told what it waits for)
	pace             pacing                // what the insertion gate goes by (gate.go)
	queue            []*sentWords          // what waits for the insertion gate
	userAt           time.Time             // the user's open caption last grew
	connectedAt      time.Time             // the upstream session came up
	// askCur is the card the tracker asks by voice (asks.go), askTold whether the model was told it: from then on what
	// it hands over is the card's answer.
	askCur  string
	askTold bool
	// stopAt is when "Stop:" began to interrupt the bound conversation (zero: it did not, or that failed): a card
	// that goes from then on was closed by it (asks.go).
	stopAt time.Time

	barge barge // the user's voice over the model's speech (gate.go, protocol 2)
	// cutAt: the energy rule cut the model off then (dropAudio); its reply to the user comes once they are quiet.
	cutAt time.Time
	// signals: the service says when the model's replies start, end and are cut off (the measured one's
	// response.event): a cut-in is its to tell (evInterrupted), not the energy rule's. replied are its last replies
	// that were over (completed) before such a word of theirs came.
	signals bool
	replied []string
}

type outFrame struct {
	text  []byte
	audio []byte
	close *websocket.StatusCode
	done  bool // the conversation is over: stop after what was queued before
}

func (pl *Plugin) converse(ctx context.Context, app *websocket.Conn, role string, lang i18n.Lang) {
	ctx, cancel := context.WithCancel(ctx)
	c := &conversation{pl: pl, app: app, ctx: ctx, cancel: cancel, out: make(chan outFrame, 2048), role: role, lang: lang,
		delegations: map[string]int{}, pending: map[string]*time.Timer{}, unacked: map[string]*sentWords{}, version: 1,
		wake: make(chan struct{}, 1)}
	pl.active.Add(1)
	defer pl.active.Add(-1)
	writerDone, loopsDone := make(chan struct{}), make(chan struct{}, 2)
	go c.writeApp(writerDone)
	c.runs = newTracker(c, pl.link.listen())
	go func() { c.runs.loop(ctx); loopsDone <- struct{}{} }()
	go func() { c.speaker(); loopsDone <- struct{}{} }()
	defer func() {
		c.shutdown()
		// What is queued for the app (a fatal error, its close) goes out before the socket is dropped.
		select {
		case c.out <- outFrame{done: true}:
			select {
			case <-writerDone:
			case <-time.After(3 * time.Second):
			}
		case <-writerDone:
		case <-time.After(3 * time.Second):
		}
		cancel()
		<-writerDone
		_ = app.CloseNow()
		<-loopsDone // the conversation is over once its runs and what it says are (GET /health counts it till then)
		<-loopsDone
	}()
	if err := c.begin(); err != nil {
		pl.logf("start failed: %v", err)
		var ce *codedError
		switch {
		case errors.Is(err, errNoUpstream):
			c.failCode(codeNoUpstream, "还没配实时语音服务：在 设置 › 扩展 › 实时语音 里填服务地址和 Key", true)
		case errors.As(err, &ce):
			c.failCode(ce.code, ce.msg, true)
		default:
			c.failCode(upstreamCode(err), "语音会话没能建立："+err.Error(), true)
		}
		return
	}
	c.readApp()
}

// writeApp is the only writer of the app's socket.
func (c *conversation) writeApp(done chan<- struct{}) {
	defer close(done)
	for {
		select {
		case <-c.ctx.Done():
			return
		case f := <-c.out:
			var err error
			switch {
			case f.done:
				return
			case f.close != nil:
				_ = c.app.Close(*f.close, "voice session ended")
				return
			case f.audio != nil:
				err = c.app.Write(c.ctx, websocket.MessageBinary, f.audio)
			default:
				err = c.app.Write(c.ctx, websocket.MessageText, f.text)
			}
			if err != nil {
				c.cancel()
				return
			}
		}
	}
}

// sendApp queues a frame for the app (mu may be held).
func (c *conversation) sendApp(v any) {
	b, _ := json.Marshal(v)
	select {
	case c.out <- outFrame{text: b}:
	case <-c.ctx.Done():
	}
}

func (c *conversation) fail(msg string, fatal bool) { c.failCode("", msg, fatal) }

// failCode is fail with the error's code (docs/voice.md "Extension → app"), none when code is "".
func (c *conversation) failCode(code, msg string, fatal bool) {
	f := map[string]any{"type": "error", "message": msg, "fatal": fatal}
	if code != "" {
		f["code"] = code
	}
	c.sendApp(f)
	if fatal {
		code := websocket.StatusInternalError
		select {
		case c.out <- outFrame{close: &code}:
		case <-c.ctx.Done():
		}
	}
}

// begin reads the app's start and opens the upstream session.
func (c *conversation) begin() error {
	rctx, cancel := context.WithTimeout(c.ctx, 30*time.Second)
	typ, b, err := c.app.Read(rctx)
	cancel()
	if err != nil {
		return err
	}
	var start startFrame
	if typ != websocket.MessageText || json.Unmarshal(b, &start) != nil || start.Type != "start" {
		return errors.New("the first frame must be start")
	}
	if err := c.negotiate(&start); err != nil {
		return err
	}
	p, model, err := c.pl.cat.resolve(c.ctx, start.Model)
	gone := errors.Is(err, errNoProvider)
	if gone {
		// The app's model is of an upstream no longer configured (its address cleared, hub.json changed): the default
		// one, rather than no conversation.
		p, model, err = c.pl.cat.resolve(c.ctx, "")
	}
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.p, c.w, c.model, c.modelID = p, newWire(p.Dialect), model, p.ID+"/"+model
	if voicePattern.MatchString(start.Voice) && !gone { // a voice of the other upstream's may not exist here
		c.voice = start.Voice
	}
	c.mu.Unlock()
	c.pl.logf("conversation: %s, voice %q", c.modelID, c.voice)
	if err := c.connect(); err != nil {
		return err
	}
	if gone {
		c.pl.logf("model %q is gone: %s instead", start.Model, c.modelID)
		c.failCode(codeModelUnavailable, "选的模型 "+start.Model+" 已经不在了，这次用默认模型 "+c.modelID, false)
	}
	return nil
}

// negotiate reads what the app's start asks for: the protocol, the conversation to bind (protocol 2: its binding starts
// now, while the upstream connects), what it hands back of a call it picks up again. Another extension calling has
// protocol 1 only (docs/voice.md "Permissions"): the main conversation, never a target of its choosing.
func (c *conversation) negotiate(s *startFrame) error {
	full := c.role == "" || c.role == roleFull
	if s.Target != nil && !full {
		return &codedError{code: codeTargetForbidden, msg: "别的扩展调来的语音只能交给 Persona 主对话，不能绑定对话"}
	}
	if s.Protocol < 2 || !full {
		return nil // protocol 1: its frames, the main conversation
	}
	if s.Target == nil || s.Target.MachineID == "" || s.Target.SessionID == "" {
		return errors.New("protocol 2 starts with the conversation to bind (target)")
	}
	c.version = 2
	machine, session, recent := s.Target.MachineID, s.Target.SessionID, appRecent(c.language(), s.Recent)
	c.prep = make(chan prepared, 1)
	go func() { c.prep <- c.prepare(c.ctx, machine, session, recent) }()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.history = historyOf(s.History)
	if s.Resume != nil {
		for _, r := range s.Resume.Runs {
			if r.ID == "" || !openState(r.State) || len(c.resume) == resumeMost {
				continue
			}
			c.resume = append(c.resume, r)
			c.open = append(c.open, openTask{id: r.ID, task: clipRunes(r.Task, taskMost)})
		}
	}
	c.resumed = len(c.history) > 0 || len(c.resume) > 0
	return nil
}

// bound waits for the binding negotiate started (the first session), or takes a new snapshot of the bound
// conversation (a new session after a drop: the last one is told again when it cannot be read). online: its machine
// is connected, as far as the hub said.
func (c *conversation) bound(ctx context.Context) (online bool, err error) {
	if c.version < 2 {
		return true, nil
	}
	if c.prep != nil {
		var p prepared
		select {
		case p = <-c.prep:
		case <-ctx.Done():
			return false, ctx.Err()
		}
		c.prep = nil
		if p.err != nil {
			return false, p.err
		}
		c.mu.Lock()
		c.target, c.snap = p.target, p.snap
		c.mu.Unlock()
		return p.online, nil
	}
	if snap := c.refreshSnapshot(ctx, c.target); snap != nil {
		c.mu.Lock()
		c.snap = snap
		c.mu.Unlock()
	}
	return true, nil
}

// openTask is a run not over yet, as a new upstream session is told (instructions' Waiting).
type openTask struct{ id, task string }

// waiting are the tasks of the runs not over yet, oldest first (mu held).
func (c *conversation) waiting() []string {
	var out []string
	for _, o := range c.open {
		out = append(out, o.task)
	}
	return out
}

// setOpen records run id's task as not over yet (task "": over).
func (c *conversation) setOpen(id, task string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.open = slices.DeleteFunc(c.open, func(o openTask) bool { return o.id == id })
	if task != "" {
		c.open = append(c.open, openTask{id: id, task: task})
	}
}

// upstreamError is a failure of the realtime service the app shows by its code.
type upstreamError struct {
	code string
	err  error
}

func (e *upstreamError) Error() string { return e.err.Error() }
func (e *upstreamError) Unwrap() error { return e.err }

// upstreamCode is the code of a failure to start the upstream session ("" when it is not one the app knows).
func upstreamCode(err error) string {
	var ue *upstreamError
	if errors.As(err, &ue) {
		return ue.code
	}
	return ""
}

// connect opens an upstream session and waits for it to start; the first one answers the app's start with ready.
func (c *conversation) connect() error {
	c.mu.Lock()
	p, w, model, voice := c.p, c.w, c.model, c.voice
	history := append([]prompts.VoiceLine(nil), c.history...)
	if !c.started && !c.resumed {
		history = nil
	}
	c.mu.Unlock()

	dctx, cancel := context.WithTimeout(c.ctx, startTimeout)
	defer cancel()
	up, err := c.dial(dctx, p, w, model)
	if err != nil {
		return err
	}
	online, err := c.bound(dctx)
	if err != nil {
		_ = up.CloseNow()
		return err
	}
	tools, mode := c.toolSet(), ""
	switch {
	case c.target != nil && len(tools) > 0:
		mode = "bound"
	case len(tools) == len(toolNames):
		mode = "all"
	case len(tools) > 0:
		mode = "ask_jarvis"
	}
	c.mu.Lock()
	snap, waiting := c.snap, c.waiting()
	c.mu.Unlock()
	s := session{
		Model: model, Voice: voice, InputRate: p.inputRate(),
		Instructions: prompts.RenderVoiceInstructions(prompts.VoiceInstructions{
			Lang: string(c.language()), Owner: c.pl.cfg.Owner, Notes: c.pl.cfg.Notes, Tools: mode, History: clipHistory(history),
			Target: snap, Waiting: waiting,
		}),
		Tools: toolDecls(tools),
	}
	if err := writeJSON(dctx, up, w.start(s)); err != nil {
		_ = up.CloseNow()
		return err
	}
	rate := 0
	for started := false; !started; {
		typ, b, err := up.Read(dctx)
		if err != nil {
			_ = up.CloseNow()
			return fmt.Errorf("the session did not start: %w", err)
		}
		if typ != websocket.MessageText {
			continue
		}
		for _, e := range w.decode(b) {
			switch e.kind {
			case evStarted:
				started, rate = true, e.rate
			case evError:
				_ = up.CloseNow()
				err := errors.New(firstNonEmpty(e.text, e.code, "the service refused the session"))
				if e.code == codeUpstreamUnavailable {
					err = &upstreamError{code: codeUpstreamUnavailable, err: err}
				}
				return err
			}
		}
	}
	if rate == 0 {
		rate = w.outputRate(p.inputRate())
	}

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		_ = up.CloseNow()
		return errors.New("closed")
	}
	c.up, c.ready, c.connectedAt = up, true, time.Now()
	gen := c.gen
	first := !c.started
	// A session after a drop is told the bound conversation as it is now: an agent's words the last session had not
	// taken in (its turn went on meanwhile) close the user-speech gate, as a result sent does. The same snapshot does
	// not: a request the user made just before the drop still counts.
	if words := agentWords(snap); words != c.snapWords {
		if !first {
			c.gate.told(c.connectedAt)
		}
		c.snapWords = words
	}
	if first {
		c.started, c.outRate = true, rate
		ready := map[string]any{"type": "ready", "input_rate": p.inputRate(), "output_rate": rate, "model": c.modelID, "voice": nilIfEmpty(voice)}
		if c.version >= 2 {
			ready["protocol"], ready["features"], ready["target"] = infoProtocol, features, c.target.frame()
		}
		c.sendApp(ready)
		if !online {
			c.failCode(codeMachineOffline, "这台电脑离线，说的话要等它上线才发得出去", false)
		}
	} else {
		if rate != c.outRate {
			c.pl.logf("reconnected session speaks at %d Hz, the app plays %d", rate, c.outRate)
		}
		if c.version >= 2 {
			c.sendApp(map[string]any{"type": "link", "state": "live"})
		} else {
			c.sendApp(map[string]any{"type": "tool", "id": "reconnect", "done": true})
		}
		c.pl.logf("reconnected (%d)", c.reconnects)
	}
	resume := c.resume
	c.resume = nil
	c.mu.Unlock()
	go c.readUpstream(up, gen)
	if !first {
		c.runs.reconnected()
		return nil
	}
	c.runs.arm() // its cards (asks.go): the call is set up; before the runs handed back, which a card may cover
	if len(resume) > 0 {
		c.runs.resume(resume)
	}
	if c.version >= 2 {
		c.runs.observeTarget(linkKey{c.target.MachineID, c.target.SessionID}) // after the runs handed back
	}
	return nil
}

// dial opens the upstream socket: the service's WebSocket, or the fake dialect's stand-in.
func (c *conversation) dial(ctx context.Context, p *Provider, w wire, model string) (upLink, error) {
	if p.Dialect == DialectFake {
		return newFakeLink(), nil
	}
	key, err := p.credential(ctx)
	if err != nil {
		return nil, err
	}
	u, err := w.url(p.URL, model)
	if err != nil {
		return nil, err
	}
	header := w.header()
	if key != "" {
		header.Set("Authorization", "Bearer "+key)
	}
	opts := &websocket.DialOptions{HTTPHeader: header}
	if p.SSH != "" {
		opts.HTTPClient = p.httpClient()
	}
	up, res, err := websocket.Dial(ctx, u, opts)
	if err != nil {
		return nil, dialError(err, res)
	}
	up.SetReadLimit(16 << 20)
	return up, nil
}

// toolSet is which of the plugin's function tools the session declares (docs/voice.md "Permissions"): all of them for a full
// device; for another extension calling (role extension) only ask_jarvis, to the main conversation, which is what it may
// reach: the others reach past that (the knowledge base, any agent), and without a tool the measured service hands
// nothing over at all (docs/lessons/voice.md "Other behavior"); none for an upstream that takes no tools.
func (c *conversation) toolSet() []string {
	switch {
	case !c.p.tools():
		return nil
	case c.version >= 2:
		// Bound to a conversation: what hands work over goes to it (delegate_task); send_to_agent and ask_jarvis would
		// go past it. The measured service hands nothing over without a tool (docs/lessons/voice.md "Other behavior").
		return boundTools
	case c.role == "" || c.role == roleFull:
		return toolNames
	}
	return []string{"ask_jarvis"}
}

// tools: the session declares function tools (a delegation comes with its call).
func (c *conversation) tools() bool { return len(c.toolSet()) > 0 }

// roleFull is X-Persona-Role of a full device (and of the hub's own token).
const roleFull = "full"

// language is what the voice turn's prompts are written in: the caller's, else the hub's, else Chinese.
func (c *conversation) language() i18n.Lang {
	if c.lang != "" {
		return c.lang
	}
	if l := c.pl.link.language(); l != "" {
		return l
	}
	return i18n.ZhHans
}

// dialError adds what the service answered to a refused handshake (the library keeps the first KB of its body).
func dialError(err error, res *http.Response) error {
	if res == nil {
		return err
	}
	if res.Body == nil {
		res.Body = http.NoBody
	}
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
	var e struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	msg := strings.TrimSpace(string(raw))
	if json.Unmarshal(raw, &e) == nil && e.Error.Message != "" {
		msg = e.Error.Message
	}
	err = fmt.Errorf("HTTP %d", res.StatusCode)
	if msg != "" {
		err = fmt.Errorf("HTTP %d: %s", res.StatusCode, clipRunes(msg, 200))
	}
	switch res.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return &upstreamError{code: codeUpstreamForbidden, err: err}
	case http.StatusServiceUnavailable:
		return &upstreamError{code: codeUpstreamUnavailable, err: err}
	}
	return err
}

func (c *conversation) readUpstream(up upLink, gen int) {
	for {
		typ, b, err := up.Read(c.ctx)
		if err != nil {
			c.lost(gen, err)
			return
		}
		if typ != websocket.MessageText {
			continue
		}
		c.mu.Lock()
		if c.gen != gen || c.closed {
			c.mu.Unlock()
			return
		}
		for _, e := range c.w.decode(b) {
			c.handle(gen, e)
		}
		c.mu.Unlock()
	}
}

// handle applies one upstream event (mu held).
func (c *conversation) handle(gen int, e event) {
	switch e.kind {
	case evAudio:
		now := time.Now()
		if c.dropAudio && (now.Sub(c.lastAudio) > 1500*time.Millisecond || c.replyAfterCut(now)) {
			c.dropAudio, c.cutAt = false, time.Time{} // a pause: this is the next answer
		}
		c.lastAudio = now
		if c.dropAudio {
			return
		}
		c.pace.played(now, len(e.audio), c.outRate)
		c.closeUser()
		select {
		case c.out <- outFrame{audio: e.audio}:
		case <-c.ctx.Done():
		}
	case evUser:
		c.dropAudio, c.cutAt = false, time.Time{}
		if strings.TrimSpace(e.text) != "" {
			c.gate.user = time.Now()
			c.userAt = c.gate.user
		}
		c.userText(e.text, e.replace, e.final)
	case evAssistant:
		c.asstText(e.text, e.replace, e.final, e.name)
	case evReply:
		c.signals = true
	case evTurnEnd:
		c.dropAudio, c.cutAt = false, time.Time{}
		if e.id != "" {
			c.signals = true
			if c.replied = append(c.replied, e.id); len(c.replied) > repliedMost {
				c.replied = c.replied[1:]
			}
		}
		c.closeAsst()
	case evInterrupted:
		if e.id != "" {
			c.signals = true
		}
		if e.id != "" && slices.Contains(c.replied, e.id) {
			// A reply that was over already, superseded by the next one (a tool's result, a say): nothing of it is left
			// to drop, and a clear would cut the end of it the app still plays (docs/lessons/voice.md"Other behavior"). On
			// either protocol (#4719): protocol 1's apps play behind the upstream the same way.
			return
		}
		c.sendApp(map[string]any{"type": "clear"})
		c.pace.playEnd = time.Now() // the app drops what it had not played
		c.closeAsst()
	case evDelegation:
		c.closeUser()
		c.pace.delegated = time.Now()
		if e.id == "" {
			return
		}
		c.delegations[e.id] = gen
		at := c.gate.agent // the gate as it was when the model handed this over
		if c.tools() {
			id, text := e.id, e.text
			c.pending[id] = time.AfterFunc(toolCallWait, func() {
				c.mu.Lock()
				defer c.mu.Unlock()
				if _, ok := c.pending[id]; ok && !c.closed {
					delete(c.pending, id)
					c.startTask(id, text, at)
				}
			})
			return
		}
		c.startTask(e.id, e.text, at)
	case evToolCall:
		c.closeUser()
		c.pace.delegated = time.Now()
		if t := c.pending[e.id]; t != nil {
			t.Stop()
			delete(c.pending, e.id)
		}
		c.runTool(gen, e)
	case evAck:
		if s := c.unacked[e.eventID]; s != nil {
			delete(c.unacked, e.eventID)
			if s.agent {
				c.gate.told(time.Now()) // the model has read them only now
			}
		}
	case evError:
		c.pl.logf("session error (%s, %s): %s", e.code, firstNonEmpty(e.param, "-"), e.text)
		c.serviceRefused(e)
	}
}

func (c *conversation) remember(role, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	c.history = append(c.history, prompts.VoiceLine{Role: role, Text: text})
	if len(c.history) > historyLines {
		c.history = c.history[len(c.history)-historyLines:]
	}
}

func (c *conversation) userText(text string, replace, final bool) {
	if c.user == nil {
		if text == "" && !final {
			return
		}
		c.closeAsst()
		c.userSeq++
		c.user = &line{id: fmt.Sprintf("u%d", c.userSeq)}
	}
	if replace {
		c.user.text = text
	} else {
		c.user.text += text
	}
	c.sendApp(map[string]any{"type": "transcript", "id": c.user.id, "role": "user", "text": strings.TrimSpace(c.user.text), "final": final})
	if final {
		c.remember("user", c.user.text)
		c.user = nil
	}
}

func (c *conversation) closeUser() {
	if c.user != nil {
		c.userText("", false, true)
	}
}

// asstText: source "transcript" or "text" for a service that sends the model's words both ways (only one is shown).
func (c *conversation) asstText(text string, replace, final bool, source string) {
	if source == "transcript" {
		c.sawTranscript = true
	} else if source == "text" && c.sawTranscript {
		return
	}
	if c.asst == nil {
		if text == "" && !final {
			return
		}
		c.closeUser()
		c.asstSeq++
		c.asst = &line{id: fmt.Sprintf("a%d", c.asstSeq), source: source}
	}
	if source == "transcript" && c.asst.source == "text" {
		c.asst.text, c.asst.source = "", source
	}
	if replace {
		c.asst.text = text
	} else {
		c.asst.text += text
	}
	c.sendApp(map[string]any{"type": "transcript", "id": c.asst.id, "role": "assistant", "text": strings.TrimSpace(c.asst.text), "final": final})
	if final {
		c.remember("assistant", c.asst.text)
		c.asst = nil
	}
}

func (c *conversation) closeAsst() {
	if c.asst != nil {
		c.asstText("", false, true, c.asst.source)
	}
}

// sendUp writes to the current upstream session, if it is still gen (any gen when gen < 0).
func (c *conversation) sendUp(gen int, frames ...any) bool {
	c.mu.Lock()
	up, cur, ready := c.up, c.gen, c.ready
	c.mu.Unlock()
	if !ready || up == nil || (gen >= 0 && gen != cur) {
		return false
	}
	for _, f := range frames {
		if err := writeJSON(c.ctx, up, f); err != nil {
			return false
		}
	}
	return true
}

// runTool runs a function call in the background and answers it (mu held). ask_jarvis is a task for the main
// conversation: its call stays open and its one result is the task's answer (docs/lessons/voice.md: a call can wait
// minutes; answering at once had the model speak twice and left the answer to be said out of turn). What writes
// (ask_jarvis, send_to_agent) waits for the user-speech gate, as it was at the call.
func (c *conversation) runTool(gen int, e event) {
	if !slices.Contains(c.toolSet(), e.name) {
		c.pl.logf("a call of %s, a tool this session did not declare: ignored", e.name)
		return
	}
	at := c.gate.agent
	label := toolLabels[e.name]
	if label == "" {
		label = e.name
	}
	switch e.name {
	case "ask_jarvis", "delegate_task":
		// A handover: protocol 2 reports it with run frames, protocol 1 with a tool line.
		var in struct {
			Task string `json:"task"`
		}
		_ = json.Unmarshal([]byte(e.args), &in)
		if c.version < 2 {
			c.sendApp(map[string]any{"type": "tool", "id": e.callID, "label": label, "done": false})
			c.sendApp(map[string]any{"type": "tool", "id": e.callID, "done": true})
		}
		if e.id != "" {
			c.delegations[e.id] = gen // its result is the call's answer, whether or not the call came with its delegation
		}
		c.handOver(strings.TrimSpace(in.Task), e.id, gen, at)
		return
	}
	c.sendApp(map[string]any{"type": "tool", "id": e.callID, "label": label, "done": false})
	delete(c.delegations, e.id) // its delegation is the tool's: the result answers it
	answer := func(out string) {
		// A tool's result goes on its call only (not said once that is gone: the model has moved on).
		c.deliver(&sentWords{words: plainWords(out), delegation: e.id, gen: gen, onCall: true})
		c.sendApp(map[string]any{"type": "tool", "id": e.callID, "done": true})
	}
	if e.name == "send_to_agent" {
		var in struct {
			Text string `json:"text"`
		}
		_ = json.Unmarshal([]byte(e.args), &in)
		c.whenSpoken(at, func(spoken bool) {
			if !spoken {
				c.pl.logf("send_to_agent not run: the user had not spoken")
				answer(prompts.RenderVoiceRefused(prompts.VoiceRefused{Lang: string(c.language()), Code: "unprompted",
					Task: clipRunes(in.Text, taskMost), Agent: true}))
				return
			}
			answer(c.toolOutput(e))
		})
		return
	}
	go func() { answer(c.toolOutput(e)) }()
}

func (c *conversation) toolOutput(e event) string {
	return runTool(c.ctx, c.pl.hub, c.language(), e.name, e.args)
}

// startTask hands a delegation the model made to the main conversation (mu held). Nothing is sent back on the
// delegation until the task's answer: the model has already told the user it handed it over, and anything on the
// delegation has it speak again (docs/lessons/voice.md "Conclusions" 4).
func (c *conversation) startTask(delegationID, text string, at time.Time) {
	c.handOver(strings.TrimSpace(text), delegationID, c.delegations[delegationID], at)
}

// handOver gives task to the main conversation once the user-speech gate lets it (gate.go; at is its agent time when
// the model handed this over), else tells the model it was not sent (mu held; it goes on in the background). The answer
// goes back on delegation, of upstream session gen, while that is open.
func (c *conversation) handOver(task, delegation string, gen int, at time.Time) {
	if c.version >= 2 {
		c.handOverBound(task, delegation, gen, at)
		return
	}
	if card := c.askingCardLocked(); card != "" { // a question asked by voice: this is its answer (asks.go)
		c.answerCard(card, task, delegation, gen, at)
		return
	}
	c.whenSpoken(at, func(spoken bool) {
		// What was said, taken now: the caption of the user's request often comes after the model's call (gate.go).
		c.mu.Lock()
		if task == "" { // the model said nothing of what to do: what the user said
			task = c.lastUserLine()
		}
		recent := c.recent(recentLines)
		c.mu.Unlock()
		r := &voiceRun{Tasks: []string{clipRunes(task, taskMost)}, Delegation: delegation, Gen: gen}
		refuse := func(code string) {
			c.tell(r, plainWords(prompts.RenderVoiceRefused(prompts.VoiceRefused{Lang: string(c.language()), Code: code, Task: r.Tasks[0]})), true, false)
		}
		switch {
		case !spoken:
			c.pl.logf("not handed over: the user had not spoken since the last answer, or not lately")
			refuse("unprompted")
		case task == "":
			refuse("empty")
		default:
			c.mu.Lock()
			c.tasks++
			id := fmt.Sprintf("d%d", c.tasks)
			c.sendApp(map[string]any{"type": "tool", "id": id, "label": "Persona 主对话处理中", "done": false})
			c.mu.Unlock()
			c.runs.handOver(task, delegation, gen, id, recent)
		}
	})
}

// whenSpoken calls f, in a goroutine, with whether the user-speech gate is open for what the model did when the gate's
// agent time was at: at once when it is, else once it opens within gateWait (the caption of what the user just said
// can come after the model acted on it).
func (c *conversation) whenSpoken(at time.Time, f func(spoken bool)) {
	go func() {
		deadline := time.Now().Add(gateWait)
		for {
			c.mu.Lock()
			open := c.gate.openSince(at, time.Now())
			c.mu.Unlock()
			if open || !time.Now().Before(deadline) {
				f(open)
				return
			}
			select {
			case <-c.ctx.Done():
				return
			case <-time.After(100 * time.Millisecond):
			}
		}
	}()
}

// tell gives the model a run's words: on its delegation (that delegation's one commentary) when final and the
// delegation is still open in this upstream session, else as something to say. agent: they hold an agent's words,
// which closes the user-speech gate until the user speaks again.
func (c *conversation) tell(r *voiceRun, w words, final, agent bool) {
	c.mu.Lock()
	gen, delegation := c.gen, ""
	if final && r.Delegation != "" {
		delegation = c.claim(r.Delegation, r.Gen)
		r.Delegation = ""
	}
	if r.ID != "" {
		// What is said of the run now says more than a heartbeat of it still waiting to be said.
		c.queue = slices.DeleteFunc(c.queue, func(s *sentWords) bool { return s.run == r.ID && s.prio == prioHeartbeat })
	}
	c.mu.Unlock()
	c.deliver(&sentWords{words: w, delegation: delegation, gen: gen, agent: agent})
}

// answerOn gives the model s as the one commentary of delegation (of upstream session gen) while that is open in this
// session, else as something to say.
func (c *conversation) answerOn(delegation string, gen int, s *sentWords) {
	c.mu.Lock()
	s.delegation, s.gen = c.claim(delegation, gen), c.gen
	c.mu.Unlock()
	c.deliver(s)
}

// claim takes delegation, of upstream session gen, for its one commentary (mu held): "" when it is not open in this
// session (its session is gone, or it had its commentary).
func (c *conversation) claim(delegation string, gen int) string {
	if g, open := c.delegations[delegation]; delegation != "" && open && g == gen && g == c.gen {
		delete(c.delegations, delegation)
		return delegation
	}
	return ""
}

// sentWords is what deliver sends, kept by its event id until the service acks or refuses it.
type sentWords struct {
	words             // text is what goes; short, for a service that refuses it as too long
	delegation string // the delegation it answers, of upstream session gen ("" said)
	gen        int
	agent      bool // an agent's words (gate.go)
	onCall     bool // a tool's result: on its call only, never said
	at         time.Time
	queued     time.Time // it went into the queue: the user's voice holds it back at most answerHoldMost / sayHoldMost
	prio       int       // which goes first of what waits for the insertion gate: higher first, then the oldest
	card       string    // the card it asks (asks.go): once it is sent, the model asks that card
	cards      []string  // the cards it says wait on the screen (asks.go): not said once they are all gone
	// run is the run it is about (a heartbeat: it goes when the run has more to say); expires, when it is not said by
	// then it is not said (a heartbeat gone stale).
	run     string
	expires time.Time
}

// deliver sends s once the insertion gate lets it (gate.go; a tool's result on its call goes at once).
func (c *conversation) deliver(s *sentWords) {
	if s.onCall {
		c.sendNow(s)
		return
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	s.queued = time.Now()
	c.queue = append(c.queue, s)
	c.mu.Unlock()
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// speaker sends what waits for the insertion gate as the gate lets it, and ends the user's open caption after
// captionQuiet without more of it (and without their voice).
func (c *conversation) speaker() {
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-c.wake:
		case <-tick.C:
		}
		for {
			s := c.nextToSay(time.Now())
			if s == nil {
				break
			}
			c.sendNow(s)
		}
		c.mu.Lock()
		if now := time.Now(); c.user != nil && now.Sub(c.userAt) >= captionQuiet && now.Sub(c.pace.voice) >= captionQuiet {
			c.closeUser()
		}
		c.mu.Unlock()
	}
}

// nextToSay takes the first of the queue the insertion gate lets go now: the highest prio, then the oldest.
func (c *conversation) nextToSay(now time.Time) *sentWords {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.queue = slices.DeleteFunc(c.queue, func(s *sentWords) bool { return !s.expires.IsZero() && now.After(s.expires) })
	best := -1
	for i, s := range c.queue {
		say := s.delegation == "" || s.gen != c.gen
		if now.Before(c.pace.readyAt(say, s.queued)) {
			continue
		}
		if best < 0 || s.prio > c.queue[best].prio {
			best = i
		}
	}
	if best < 0 {
		return nil
	}
	s := c.queue[best]
	c.queue = slices.Delete(c.queue, best, best+1)
	return s
}

// sendNow sends s: as the answer on its delegation while that is open in its session, else as something to say. On a
// service that acks (liveWire) it goes with an event id and is kept until the ack, which closes the gate again when it
// holds an agent's words (gate.go), or until the service refuses it (serviceRefused). An agent's words close the gate
// as they are sent.
func (c *conversation) sendNow(s *sentWords) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	w, now := c.w, time.Now()
	if s.delegation != "" && s.gen != c.gen {
		s.delegation = "" // that session is gone, and its delegations with it
	}
	if s.delegation == "" && s.onCall {
		c.mu.Unlock()
		return
	}
	id := ""
	if w.acks() {
		c.events++
		id = fmt.Sprintf("persona-%d", c.events)
		s.at = now
		for k, o := range c.unacked {
			if now.Sub(o.at) > unackedMost {
				delete(c.unacked, k)
			}
		}
		c.unacked[id] = s
	}
	if s.agent {
		c.gate.told(now)
	}
	c.told(s.card)
	delegation, gen, text, onCall := s.delegation, s.gen, s.text, s.onCall
	c.mu.Unlock()
	if delegation != "" && c.sendUp(gen, w.reply(delegation, text, true, id)...) || onCall {
		return
	}
	if delegation != "" {
		c.mu.Lock()
		s.delegation = ""
		c.mu.Unlock()
	}
	c.sendUp(-1, w.say(text, id)...)
}

// serviceRefused handles the service refusing what deliver sent (mu held; docs/voice.md 4.5 rule 4). It names it by
// its event id; an error about a delegation id that names none is taken for the oldest still waiting on a delegation.
// Too long for this service (param content): sent again short. On a delegation that is gone (expired, closed, or any
// other refusal of it): said instead. A refused say is let go (logged).
func (c *conversation) serviceRefused(e event) {
	id := e.eventID
	if c.unacked[id] == nil && e.param == "delegation_id" {
		id = ""
		for k, o := range c.unacked {
			if o.delegation != "" && (id == "" || o.at.Before(c.unacked[id].at)) {
				id = k
			}
		}
	}
	s := c.unacked[id]
	if s == nil {
		return
	}
	delete(c.unacked, id)
	again := *s
	switch {
	case e.param == "content" && s.text != s.short:
		again.text = s.short
		c.pl.logf("the service refused %s as too long: sending it again shorter", id)
	case s.delegation != "" && !s.onCall:
		again.delegation = ""
		c.pl.logf("the service refused %s on its delegation: saying it instead", id)
	default:
		c.pl.logf("the service refused %s for good: dropped", id)
		return
	}
	go c.deliver(&again)
}

// recent is the conversation's last n lines, the user's line still open (its caption so far) last.
func (c *conversation) recent(n int) []prompts.VoiceLine {
	lines := append([]prompts.VoiceLine(nil), c.history...)
	if c.user != nil && strings.TrimSpace(c.user.text) != "" {
		lines = append(lines, prompts.VoiceLine{Role: "user", Text: strings.TrimSpace(c.user.text)})
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return clipHistory(lines)
}

// heard is when the call was last not quiet: the model's speech played out, the user's voice, a caption or a line of
// theirs (a heartbeat goes after a long quiet, docs/voice.md "Announcements").
func (c *conversation) heard() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return latest(c.pace.playEnd, c.pace.voice, c.gate.user, c.userAt)
}

// toolsDone ends the app's lines of a run.
func (c *conversation) toolsDone(ids []string) {
	for _, id := range ids {
		c.sendApp(map[string]any{"type": "tool", "id": id, "done": true})
	}
}

func (c *conversation) lastUserLine() string {
	if c.user != nil && strings.TrimSpace(c.user.text) != "" {
		return strings.TrimSpace(c.user.text)
	}
	for i := len(c.history) - 1; i >= 0; i-- {
		if c.history[i].Role == "user" {
			return c.history[i].Text
		}
	}
	return ""
}

// lost: the upstream session went away while the app is still here; a new one is told what was said.
func (c *conversation) lost(gen int, why error) {
	c.mu.Lock()
	if c.closed || gen != c.gen {
		c.mu.Unlock()
		return
	}
	c.pl.logf("session lost: %v", why)
	if !c.connectedAt.IsZero() && time.Since(c.connectedAt) >= stableAfter {
		c.reconnects = 0 // it held: a drop now is not one of a run of them (docs/voice.md "Reconnect and resume")
	}
	if c.reconnects >= maxReconnects {
		c.mu.Unlock()
		c.failCode(codeUpstreamFailed, "语音连接断了，重连也没成功。点下面的按钮重新开始", true)
		return
	}
	c.reconnects++
	c.gen++
	c.ready = false
	old := c.up
	c.up = nil
	for id, t := range c.pending {
		t.Stop()
		delete(c.pending, id)
	}
	clear(c.unacked) // that session's: an ack or refusal of it never comes
	c.pace.playEnd = time.Now()
	c.sendApp(map[string]any{"type": "clear"})
	c.closeAsst()
	c.closeUser()
	if c.version >= 2 {
		c.sendApp(map[string]any{"type": "link", "state": "reconnecting"})
	} else {
		c.sendApp(map[string]any{"type": "tool", "id": "reconnect", "label": "连接断了，重新连接中", "done": false})
	}
	c.mu.Unlock()
	if old != nil {
		_ = old.CloseNow()
	}
	go func() {
		var err error
		for attempt := 0; attempt < 3; attempt++ {
			if attempt > 0 {
				select {
				case <-c.ctx.Done():
					return
				case <-time.After(time.Duration(attempt) * 2 * time.Second):
				}
			}
			if err = c.connect(); err == nil || c.ctx.Err() != nil {
				return
			}
			c.pl.logf("reconnect: %v", err)
		}
		c.failCode(codeUpstreamFailed, "语音连接断了，重连没成功："+err.Error(), true)
	}()
}

// readApp pumps the app's frames until it hangs up.
func (c *conversation) readApp() {
	for {
		typ, b, err := c.app.Read(c.ctx)
		if err != nil {
			return
		}
		if typ == websocket.MessageBinary {
			ms, now := meanSquare(b), time.Now()
			c.mu.Lock()
			up, ready, w := c.up, c.ready, c.w
			c.cutIn(now, c.pace.heard(now, ms), len(b))
			c.mu.Unlock()
			if ready && len(b) > 0 {
				_ = writeJSON(c.ctx, up, w.audio(b))
			}
			continue
		}
		var msg struct {
			Type       string `json:"type"`
			Text       string `json:"text"`
			ID         string `json:"id"`
			ClearDraft string `json:"clear_draft"`
		}
		if json.Unmarshal(b, &msg) != nil {
			continue
		}
		switch msg.Type {
		case "interrupt":
			c.mu.Lock()
			c.dropAudio, c.lastAudio, c.cutAt = true, time.Now(), time.Time{}
			c.pace.playEnd = c.lastAudio // the app drops what it had not played
			c.closeAsst()
			c.mu.Unlock()
			c.sendApp(map[string]any{"type": "clear"})
		case "text":
			text := strings.TrimSpace(msg.Text)
			if text == "" {
				continue
			}
			c.mu.Lock()
			c.closeUser()
			c.userText(text, true, true)
			c.gate.user = time.Now()
			w := c.w
			c.mu.Unlock()
			if !c.sendUp(-1, w.text(text)) {
				c.fail("语音会话还没连上，这句没发出去", false)
			}
		case "resend":
			// The user tapped "Resend" / "Clear and send" under a run a 409 refused (protocol 2): their own act, not the model's,
			// so not held to the user-speech gate.
			if c.version >= 2 && msg.ID != "" {
				c.runs.resend(msg.ID, msg.ClearDraft)
			}
		}
	}
}

// cutIn takes a frame of the app's audio, n bytes long, voiced or not (mu held): the user talking over the model's
// speech for bargeMin drops what the app has not played of it (gate.go "Barging in"). Protocol 2 only, and only on
// an upstream that does not say when the model is cut in on (signals): there clear follows its word. Protocol 1 does
// as it always did (a clear when the upstream says so, or the app asks).
func (c *conversation) cutIn(now time.Time, voiced bool, n int) {
	if c.version < 2 || c.p == nil {
		return
	}
	rate := c.p.inputRate()
	if rate <= 0 {
		return
	}
	talked := c.barge.add(voiced, time.Duration(n/2)*time.Second/time.Duration(rate))
	if talked < bargeMin || c.signals || c.dropAudio || !now.Before(c.pace.playEnd) {
		return
	}
	c.pl.logf("the user cut in on the model (%v of voice): its speech is dropped", talked.Round(10*time.Millisecond))
	c.barge = barge{}
	c.dropAudio, c.lastAudio, c.cutAt = true, now, now
	c.pace.playEnd = now // the app drops what it had not played
	c.closeAsst()
	c.sendApp(map[string]any{"type": "clear"})
}

// replyAfterCut: after the energy rule cut the model off (cutAt), audio coming now is its next reply, not the rest of the
// one it dropped (mu held): the user has been quiet replyAfterVoice, and what came before has paused for cutGap (gate.go
// "Barging in"). The reply to a short cut-in comes before the 1.5 s pause that ends a drop otherwise.
func (c *conversation) replyAfterCut(now time.Time) bool {
	return !c.cutAt.IsZero() && now.Sub(c.pace.voice) >= replyAfterVoice && now.Sub(c.lastAudio) >= cutGap
}

func (c *conversation) shutdown() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	up, w := c.up, c.w
	for id, t := range c.pending {
		t.Stop()
		delete(c.pending, id)
	}
	c.mu.Unlock()
	c.pl.logf("conversation closed")
	if up != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = writeJSON(ctx, up, w.close())
		cancel()
		_ = up.Close(websocket.StatusNormalClosure, "bye")
	}
}

func writeJSON(ctx context.Context, c interface {
	Write(context.Context, websocket.MessageType, []byte) error
}, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.Write(ctx, websocket.MessageText, b)
}

func clipHistory(lines []prompts.VoiceLine) []prompts.VoiceLine {
	out := make([]prompts.VoiceLine, len(lines))
	for i, l := range lines {
		out[i] = prompts.VoiceLine{Role: l.Role, Text: clipRunes(l.Text, 200)}
	}
	return out
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

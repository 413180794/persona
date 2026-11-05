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

// One voice conversation: the app's WebSocket on one side (docs/extensions.md「语音」), a GPT-Live session on the other.
// The upstream session can drop mid-conversation: the app's socket stays open, shows 「重新连接中」 and the
// conversation goes on in a new session told what was said so far.

const (
	maxReconnects = 3  // per conversation
	historyLines  = 40 // what a reconnected session is told was said
	recentLines   = 6  // what a handed-over task is sent with of the conversation
	startTimeout  = 25 * time.Second
	// What was sent with an event id and not acked this long is let go (a service that does not ack after all).
	unackedMost = 5 * time.Minute
	// A delegation the model makes while it also calls a tool for it (live dialect with tools) is only the tool's
	// envelope; one with no tool call this soon is work for the main conversation.
	toolCallWait = 2 * time.Second
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
		delegations: map[string]int{}, pending: map[string]*time.Timer{}, unacked: map[string]*sentWords{}}
	pl.active.Add(1)
	defer pl.active.Add(-1)
	writerDone := make(chan struct{})
	go c.writeApp(writerDone)
	c.runs = newTracker(c, pl.link.listen())
	go c.runs.loop(ctx)
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
	}()
	if err := c.begin(); err != nil {
		pl.logf("start failed: %v", err)
		if errors.Is(err, errNoUpstream) {
			c.failCode(codeNoUpstream, "还没配实时语音服务：在 设置 › 扩展 › 实时语音 里填服务地址和 Key", true)
			return
		}
		c.fail("语音会话没能建立："+err.Error(), true)
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

// failCode is fail with the error's code (docs/voice.md「扩展 → App」), none when code is "".
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
	var start struct {
		Type  string `json:"type"`
		Model string `json:"model"`
		Voice string `json:"voice"`
	}
	if typ != websocket.MessageText || json.Unmarshal(b, &start) != nil || start.Type != "start" {
		return errors.New("the first frame must be start")
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

// connect opens an upstream session and waits for it to start; the first one answers the app's start with ready.
func (c *conversation) connect() error {
	c.mu.Lock()
	p, w, model, voice := c.p, c.w, c.model, c.voice
	history := append([]prompts.VoiceLine(nil), c.history...)
	if !c.started {
		history = nil
	}
	c.mu.Unlock()

	dctx, cancel := context.WithTimeout(c.ctx, startTimeout)
	defer cancel()
	up, err := c.dial(dctx, p, w, model)
	if err != nil {
		return err
	}
	tools, mode := c.toolSet(), ""
	switch {
	case len(tools) == len(toolNames):
		mode = "all"
	case len(tools) > 0:
		mode = "ask_jarvis"
	}
	s := session{
		Model: model, Voice: voice, InputRate: p.inputRate(),
		Instructions: prompts.RenderVoiceInstructions(prompts.VoiceInstructions{
			Owner: c.pl.cfg.Owner, Notes: c.pl.cfg.Notes, Tools: mode, History: clipHistory(history),
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
				return errors.New(firstNonEmpty(e.text, e.code, "the service refused the session"))
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
	c.up, c.ready = up, true
	gen := c.gen
	first := !c.started
	if first {
		c.started, c.outRate = true, rate
		c.sendApp(map[string]any{"type": "ready", "input_rate": p.inputRate(), "output_rate": rate, "model": c.modelID, "voice": nilIfEmpty(voice)})
	} else {
		if rate != c.outRate {
			c.pl.logf("reconnected session speaks at %d Hz, the app plays %d", rate, c.outRate)
		}
		c.sendApp(map[string]any{"type": "tool", "id": "reconnect", "done": true})
		c.pl.logf("reconnected (%d)", c.reconnects)
	}
	c.mu.Unlock()
	go c.readUpstream(up, gen)
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

// toolSet is which of the plugin's function tools the session declares (docs/voice.md「权限」): all of them for a full
// device; for another extension calling (role extension) only ask_jarvis, to the main conversation, which is what it may
// reach: the others reach past that (the knowledge base, any agent), and without a tool the measured service hands
// nothing over at all (docs/lessons/voice.md「其他行为」); none for an upstream that takes no tools.
func (c *conversation) toolSet() []string {
	switch {
	case !c.p.tools():
		return nil
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
	if res == nil || res.Body == nil {
		return err
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
	if msg == "" {
		return fmt.Errorf("HTTP %d", res.StatusCode)
	}
	return fmt.Errorf("HTTP %d: %s", res.StatusCode, clipRunes(msg, 200))
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
		if c.dropAudio && now.Sub(c.lastAudio) > 1500*time.Millisecond {
			c.dropAudio = false // a pause: this is the next answer
		}
		c.lastAudio = now
		if c.dropAudio {
			return
		}
		c.closeUser()
		select {
		case c.out <- outFrame{audio: e.audio}:
		case <-c.ctx.Done():
		}
	case evUser:
		c.dropAudio = false
		if strings.TrimSpace(e.text) != "" {
			c.gate.user = time.Now()
		}
		c.userText(e.text, e.replace, e.final)
	case evAssistant:
		c.asstText(e.text, e.replace, e.final, e.name)
	case evTurnEnd:
		c.dropAudio = false
		c.closeAsst()
	case evInterrupted:
		c.sendApp(map[string]any{"type": "clear"})
		c.closeAsst()
	case evDelegation:
		c.closeUser()
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
	c.sendApp(map[string]any{"type": "tool", "id": e.callID, "label": label, "done": false})
	switch e.name {
	case "ask_jarvis":
		var in struct {
			Task string `json:"task"`
		}
		_ = json.Unmarshal([]byte(e.args), &in)
		c.sendApp(map[string]any{"type": "tool", "id": e.callID, "done": true})
		if e.id != "" {
			c.delegations[e.id] = gen // its result is the call's answer, whether or not the call came with its delegation
		}
		c.handOver(strings.TrimSpace(in.Task), e.id, gen, at)
		return
	}
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
// delegation has it speak again (docs/lessons/voice.md「结论」4).
func (c *conversation) startTask(delegationID, text string, at time.Time) {
	c.handOver(strings.TrimSpace(text), delegationID, c.delegations[delegationID], at)
}

// handOver gives task to the main conversation once the user-speech gate lets it (gate.go; at is its agent time when
// the model handed this over), else tells the model it was not sent (mu held; it goes on in the background). The answer
// goes back on delegation, of upstream session gen, while that is open.
func (c *conversation) handOver(task, delegation string, gen int, at time.Time) {
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
		if g, open := c.delegations[r.Delegation]; open && g == r.Gen && g == gen {
			delegation = r.Delegation
			delete(c.delegations, r.Delegation)
		}
		r.Delegation = ""
	}
	c.mu.Unlock()
	c.deliver(&sentWords{words: w, delegation: delegation, gen: gen, agent: agent})
}

// sentWords is what deliver sends, kept by its event id until the service acks or refuses it.
type sentWords struct {
	words             // text is what goes; short, for a service that refuses it as too long
	delegation string // the delegation it answers, of upstream session gen ("" said)
	gen        int
	agent      bool // an agent's words (gate.go)
	onCall     bool // a tool's result: on its call only, never said
	at         time.Time
}

// deliver sends s: as the answer on its delegation while that is open in its session, else as something to say. On a
// service that acks (liveWire) it goes with an event id and is kept until the ack, which closes the gate again when it
// holds an agent's words (gate.go), or until the service refuses it (serviceRefused). An agent's words close the gate
// as they are sent.
func (c *conversation) deliver(s *sentWords) {
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
	if c.reconnects >= maxReconnects {
		c.mu.Unlock()
		c.fail("语音连接断了，重连也没成功。点下面的按钮重新开始", true)
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
	c.sendApp(map[string]any{"type": "clear"})
	c.closeAsst()
	c.closeUser()
	c.sendApp(map[string]any{"type": "tool", "id": "reconnect", "label": "连接断了，重新连接中", "done": false})
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
		c.fail("语音连接断了，重连没成功："+err.Error(), true)
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
			c.mu.Lock()
			up, ready, w := c.up, c.ready, c.w
			c.mu.Unlock()
			if ready && len(b) > 0 {
				_ = writeJSON(c.ctx, up, w.audio(b))
			}
			continue
		}
		var msg struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(b, &msg) != nil {
			continue
		}
		switch msg.Type {
		case "interrupt":
			c.mu.Lock()
			c.dropAudio, c.lastAudio = true, time.Now()
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
		}
	}
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

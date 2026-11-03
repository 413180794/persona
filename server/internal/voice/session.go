package voice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/nwverse/persona/server/internal/prompts"
)

// One voice conversation: the app's WebSocket on one side (docs/plugins.md「语音」), a GPT-Live session on the other.
// The upstream session can drop mid-conversation: the app's socket stays open, shows 「重新连接中」 and the
// conversation goes on in a new session told what was said so far.

const (
	maxReconnects = 3  // per conversation
	historyLines  = 40 // what a reconnected session is told was said
	startTimeout  = 25 * time.Second
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

	mu         sync.Mutex
	closed     bool
	p          *Provider
	w          wire
	model      string // the provider's model id
	modelID    string // <provider>/<model>, what the app knows
	voice      string
	up         *websocket.Conn
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
}

type outFrame struct {
	text  []byte
	audio []byte
	close *websocket.StatusCode
	done  bool // the conversation is over: stop after what was queued before
}

func (pl *Plugin) converse(ctx context.Context, app *websocket.Conn) {
	ctx, cancel := context.WithCancel(ctx)
	c := &conversation{pl: pl, app: app, ctx: ctx, cancel: cancel, out: make(chan outFrame, 2048),
		delegations: map[string]int{}, pending: map[string]*time.Timer{}}
	pl.active.Add(1)
	defer pl.active.Add(-1)
	writerDone := make(chan struct{})
	go c.writeApp(writerDone)
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

func (c *conversation) fail(msg string, fatal bool) {
	c.sendApp(map[string]any{"type": "error", "message": msg, "fatal": fatal})
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
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.p, c.w, c.model, c.modelID = p, newWire(p.Dialect), model, p.ID+"/"+model
	if voicePattern.MatchString(start.Voice) {
		c.voice = start.Voice
	}
	c.mu.Unlock()
	c.pl.logf("conversation: %s, voice %q", c.modelID, c.voice)
	return c.connect()
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

	key, err := p.credential(c.ctx)
	if err != nil {
		return err
	}
	u, err := w.url(p.URL, model)
	if err != nil {
		return err
	}
	header := w.header()
	if key != "" {
		header.Set("Authorization", "Bearer "+key)
	}
	opts := &websocket.DialOptions{HTTPHeader: header}
	if p.SSH != "" {
		opts.HTTPClient = p.httpClient()
	}
	dctx, cancel := context.WithTimeout(c.ctx, startTimeout)
	defer cancel()
	up, res, err := websocket.Dial(dctx, u, opts)
	if err != nil {
		return dialError(err, res)
	}
	up.SetReadLimit(16 << 20)
	s := session{
		Model: model, Voice: voice, InputRate: p.inputRate(),
		Instructions: prompts.RenderVoiceInstructions(prompts.VoiceInstructions{
			Owner: c.pl.cfg.Owner, Notes: c.pl.cfg.Notes, Tools: p.tools(), History: clipHistory(history),
		}),
	}
	if p.tools() {
		s.Tools = toolDecls()
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

func (c *conversation) readUpstream(up *websocket.Conn, gen int) {
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
		if c.p.tools() {
			id, text := e.id, e.text
			c.pending[id] = time.AfterFunc(toolCallWait, func() {
				c.mu.Lock()
				defer c.mu.Unlock()
				if _, ok := c.pending[id]; ok && !c.closed {
					delete(c.pending, id)
					c.startTask(id, text)
				}
			})
			return
		}
		c.startTask(e.id, e.text)
	case evToolCall:
		c.closeUser()
		if t := c.pending[e.id]; t != nil {
			t.Stop()
			delete(c.pending, e.id)
		}
		c.runTool(gen, e)
	case evError:
		c.pl.logf("session error (%s): %s", e.code, e.text)
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

// runTool runs a function call in the background and answers it (mu held).
func (c *conversation) runTool(gen int, e event) {
	label := toolLabels[e.name]
	if label == "" {
		label = e.name
	}
	c.sendApp(map[string]any{"type": "tool", "id": e.callID, "label": label, "done": false})
	if e.name == "ask_jarvis" {
		var in struct {
			Task string `json:"task"`
		}
		_ = json.Unmarshal([]byte(e.args), &in)
		go c.sendUp(gen, c.w.toolResult(e.id, e.callID, prompts.RenderVoiceHandoff())...)
		c.sendApp(map[string]any{"type": "tool", "id": e.callID, "done": true})
		c.delegateTask(firstNonEmpty(strings.TrimSpace(in.Task), c.lastUserLine()), "", gen)
		return
	}
	go func() {
		out, err := runTool(c.ctx, c.pl.hub, e.name, e.args)
		if err != nil {
			out = "出错了：" + err.Error()
		}
		c.sendUp(gen, c.w.toolResult(e.id, e.callID, out)...)
		c.sendApp(map[string]any{"type": "tool", "id": e.callID, "done": true})
	}()
}

// startTask hands a delegation the model made to the main conversation (mu held).
func (c *conversation) startTask(delegationID, text string) {
	task := firstNonEmpty(strings.TrimSpace(text), c.lastUserLine())
	gen := c.delegations[delegationID]
	// Quiet context while it works: the model already told the user (or is about to).
	frames := c.w.reply(delegationID, prompts.RenderVoiceHandoff(), false)
	go c.sendUp(gen, frames...)
	c.delegateTask(task, delegationID, gen)
}

// delegateTask runs task in the main conversation; the answer goes back on the delegation while its session lives,
// else as something to say (mu held).
func (c *conversation) delegateTask(task, delegationID string, gen int) {
	if task == "" {
		task = "（语音里没听清要做什么）"
	}
	c.tasks++
	id := fmt.Sprintf("d%d", c.tasks)
	c.sendApp(map[string]any{"type": "tool", "id": id, "label": "Persona 主对话处理中", "done": false})
	recent := c.history
	if len(recent) > 6 {
		recent = recent[len(recent)-6:]
	}
	message := prompts.RenderVoiceDelegated(prompts.VoiceDelegated{Task: task, History: clipHistory(recent)})
	go func() {
		d := delegate(c.ctx, c.pl.hub, message, c.pl.delegate)
		c.sendApp(map[string]any{"type": "tool", "id": id, "done": true})
		if d.Status == "aborted" {
			return
		}
		text := report(task, d)
		c.mu.Lock()
		_, open := c.delegations[delegationID]
		delete(c.delegations, delegationID)
		w := c.w
		c.mu.Unlock()
		if delegationID != "" && open && c.sendUp(gen, w.reply(delegationID, text, true)...) {
			return
		}
		c.sendUp(-1, w.say(text))
	}()
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

func writeJSON(ctx context.Context, c *websocket.Conn, v any) error {
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

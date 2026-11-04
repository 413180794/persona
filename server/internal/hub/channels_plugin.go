package hub

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/channel"
	"github.com/nwverse/persona/server/internal/plugin"
	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// The channels plugins contribute (the channel extension point, docs/channels.md「插件怎么贡献渠道」): the program
// talks to its chat app — it hands the hub the owner's messages (channel.receive) and sends what the hub gives it
// (channel.send) — and everything else is here: commands, where a message goes, the turn (channels_runs.go), what is
// asked answered in text, pushes and the sync mirror.

// channelSendTries: a send that fails (not cannot_send) goes again after 1, 2, 4 s, with the same ref.
var channelSendTries = []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}

// errCannotSend: the channel has nowhere to send to yet (WeChat before the owner's first message).
var errCannotSend = errors.New("cannot send yet")

// pluginCall calls channel id's program.
func (h *Hub) pluginCall(ctx context.Context, id, method string, params, out any) error {
	if call := h.channels.call; call != nil { // tests: a program of their own
		err := call(ctx, id, method, params, out)
		if errors.Is(err, channel.ErrCannotSend) {
			return errCannotSend
		}
		return err
	}
	pc, ok := h.pluginChannel(id)
	if !ok || h.plugins == nil {
		return fmt.Errorf("no channel %s", id)
	}
	hp := h.plugins.get(pc.plugin.Record.ID)
	if hp == nil || hp.proc == nil {
		return fmt.Errorf("%s 没在运行", pc.item.Name)
	}
	p, err := plugin.Targeted(params, id)
	if err != nil {
		return err
	}
	err = hp.proc.Call(ctx, channel.InPlugin(method), p, out)
	var re *rpc.Error
	if errors.As(err, &re) && re.Code == channel.CodeCannotSend {
		return errCannotSend
	}
	return err
}

// channelSend sends text to channel id's owner: in pieces its messages hold, each once by its ref.
func (h *Hub) channelSend(ctx context.Context, id, ref, text, kind, replyTo string) error {
	pc, ok := h.pluginChannel(id)
	if !ok {
		return fmt.Errorf("no channel %s", id)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	pieces := channel.Split(text, pc.item.Capabilities.MaxText)
	for i, piece := range pieces {
		p := channel.SendParams{Ref: ref, Text: piece, Kind: kind, ReplyTo: replyTo}
		if len(pieces) > 1 {
			p.Ref = fmt.Sprintf("%s#%d", ref, i)
		}
		var err error
		for attempt := 0; ; attempt++ {
			var out channel.SendResult
			if err = h.pluginCall(ctx, id, channel.MSend, p, &out); err == nil || errors.Is(err, errCannotSend) || attempt >= len(channelSendTries) {
				break
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(channelSendTries[attempt]):
			}
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// channelTell sends a line to channel id's owner, logging a failure (never the text).
func (h *Hub) channelTell(ctx context.Context, id, replyTo, text, kind string) error {
	err := h.channelSend(ctx, id, newRef(), text, kind, replyTo)
	if err != nil {
		h.logf("channels: %s: send: %v", id, err)
	}
	return err
}

func newRef() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// pluginSink is a plugin's channel as the pipeline's sink: text, and 「正在输入」 when it has it.
type pluginSink struct {
	h  *Hub
	id string
}

func (s pluginSink) answer(ctx context.Context, r *channel.Run, answer, note string, _ time.Duration, _, quiet bool) {
	if quiet {
		return
	}
	text := strings.TrimSpace(strings.TrimSpace(answer) + "\n\n" + note)
	if err := s.h.channelSend(ctx, s.id, r.ID+":"+fmt.Sprint(r.Base), text, channel.OutAnswer, r.ReplyTo); err != nil {
		s.h.logf("channels: %s: an answer could not be sent: %v", s.id, err)
	}
}

func (s pluginSink) typing(ctx context.Context, on bool) {
	if pc, ok := s.h.pluginChannel(s.id); ok && pc.item.Capabilities.Typing {
		_ = s.h.pluginCall(ctx, s.id, channel.MTyping, channel.TypingParams{On: on}, nil)
	}
}

// pluginChannelCall answers a plugin's calls in the channel namespace (plugins.go): method without it.
func (h *Hub) pluginChannelCall(pluginID, method string, params json.RawMessage) (any, error) {
	id := contributionOf(params)
	p := h.plugins.reg.Get(pluginID)
	if p == nil {
		return nil, &rpc.Error{Code: plugin.CodeNoContribution, Message: "no plugin " + pluginID}
	}
	items := p.Manifest.Channels()
	if id == "" && len(items) == 1 {
		id = items[0].ID
	}
	if !slices.ContainsFunc(items, func(it channel.Item) bool { return it.ID == id }) {
		return nil, &rpc.Error{Code: plugin.CodeNoContribution, Message: "no channel " + id}
	}
	switch method {
	case channel.MReceive:
		var in channel.ReceiveParams
		if err := json.Unmarshal(params, &in); err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.Message.ID) == "" {
			return nil, errors.New("a message without an id")
		}
		if !h.claimChannelMessage(id, in.Message.ID) {
			return struct{}{}, nil // delivered again
		}
		go h.channelReceive(id, in.Message)
		return struct{}{}, nil
	case channel.MStatus:
		var in channel.StatusParams
		if err := json.Unmarshal(params, &in); err != nil {
			return nil, err
		}
		c := &h.channels
		c.mu.Lock()
		if c.statuses == nil {
			c.statuses = map[string]*channel.StatusParams{}
		}
		c.statuses[id] = &in
		c.mu.Unlock()
		return struct{}{}, nil
	}
	return nil, &rpc.Error{Code: plugin.CodeUnknownMethod, Message: "unknown method channel." + method}
}

// channelStatus is what a channel without a connector last said of itself (nil before it said anything).
func (h *Hub) channelStatus(id string) *channel.StatusParams {
	c := &h.channels
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.statuses[id]
}

// --- a message from the owner ---

// channelReceive takes one of the owner's messages: a command, the answer to what was asked, or a message for the
// conversation the channel talks to.
func (h *Hub) channelReceive(id string, m channel.Message) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	text := strings.TrimSpace(m.Text)
	if strings.HasPrefix(text, "/") && len(m.Images) == 0 && len(m.Files) == 0 {
		handled, rest := h.channelCommand(ctx, id, m, text)
		if handled {
			return
		}
		m.Text = rest
	}
	if h.channelAnswerAsk(ctx, id, m, text) {
		return
	}
	if err := h.channelForward(ctx, id, m); err != nil {
		h.logf("channels: %s: send: %v", id, err)
		_ = h.channelTell(ctx, id, m.ID, "没发出去："+clip(oneLine(err.Error()), 200), channel.OutNote)
	}
}

const channelHelp = `在这里说的话会发给 Persona 的主对话，回答发回这里。
/new 开一段新对话（后面可以直接跟第一句话）
/main 回到主对话
/stop 停下正在做的这一轮
/status 现在发给谁、在等几个回答
/help 这些命令
Agent 问你问题或要你批准时，直接回复序号或文字。`

// channelCommand runs one of the commands every channel has; false (and the text to send on) for one it does not
// know: it may be the conversation's own (/compact).
func (h *Hub) channelCommand(ctx context.Context, id string, m channel.Message, text string) (bool, string) {
	name, rest, _ := strings.Cut(text, " ")
	rest = strings.TrimSpace(rest)
	reply := func(s string) { _ = h.channelTell(ctx, id, m.ID, s, channel.OutNote) }
	setTarget := func(t channel.Target) {
		c := &h.channels
		c.mu.Lock()
		h.loadChannelsLocked(ctx)
		h.channelPrefLocked(id).Target = &t
		h.saveChannelsLocked()
		c.mu.Unlock()
	}
	switch strings.ToLower(name) {
	case "/help", "/帮助":
		reply(channelHelp)
	case "/main", "/主对话":
		setTarget(channel.Target{Kind: channel.TargetMain})
		reply("之后的消息发给主对话。")
	case "/new", "/新对话":
		setTarget(channel.Target{Kind: channel.TargetNew})
		if rest == "" {
			reply("好，下一条消息会开一段新对话。/main 回到主对话。")
			return true, ""
		}
		return false, rest // "/new 帮我…": the rest starts it
	case "/stop", "/停":
		rt, err := h.channelTarget(ctx, id)
		switch {
		case err != nil:
			reply("没停下来：" + err.Error())
		case rt.isNew:
			reply("现在没有在做的。")
		default:
			if err := rt.m.call(ctx, "session.interrupt", map[string]any{"session_id": rt.session.SessionID}, nil); err != nil {
				reply("没停下来：" + clip(oneLine(err.Error()), 200))
				return true, ""
			}
			c := &h.channels
			c.mu.Lock()
			c.st.Runs = slices.DeleteFunc(c.st.Runs, func(r *channel.Run) bool { return r.Channel == id && r.SessionID == rt.session.SessionID })
			h.saveChannelStateLocked()
			c.mu.Unlock()
			pluginSink{h: h, id: id}.typing(ctx, false)
			reply("已停下。")
		}
	case "/status", "/状态":
		reply(h.channelStatusText(ctx, id))
	default:
		return false, text
	}
	return true, ""
}

func (h *Hub) channelStatusText(ctx context.Context, id string) string {
	where := "主对话"
	if rt, err := h.channelTarget(ctx, id); err == nil {
		switch {
		case rt.isNew && !rt.main:
			where = "下一条消息开的新对话"
		case !rt.main:
			where = rt.label
		}
	}
	w := h.channelWaiting(id)
	out := "消息发给：" + where
	if w.Answers > 0 {
		out += fmt.Sprintf("\n在等 %d 个回答", w.Answers)
	}
	if w.Asks > 0 {
		out += fmt.Sprintf("\n有 %d 个问题等你回复", w.Asks)
	}
	return out
}

// channelTarget is where channel id's messages go now.
func (h *Hub) channelTarget(ctx context.Context, id string) (resolvedTarget, error) {
	c := &h.channels
	c.mu.Lock()
	h.loadChannelsLocked(ctx)
	t := channel.Target{Kind: channel.TargetMain}
	if p := h.channelPrefLocked(id); p.Target != nil {
		t = *p.Target
	}
	c.mu.Unlock()
	switch t.Kind {
	case channel.TargetNew:
		return h.resolveBotTarget(ctx, botTarget{Kind: targetJarvis})
	case channel.TargetConversation:
		m := h.localMachine(t.MachineID)
		if m == nil {
			return resolvedTarget{}, errors.New("那段对话所在的电脑不在了：发 /main 回到主对话")
		}
		s, ok := m.session(t.SessionID)
		if !ok {
			return resolvedTarget{}, errors.New("那段对话不见了：发 /main 回到主对话")
		}
		s.SessionID, s.MachineID = t.SessionID, m.cfg.ID
		main := h.assistantConfig(ctx).Main
		return resolvedTarget{m: m, session: s, label: conversationName(s, main), main: t.SessionID == main}, nil
	}
	return h.resolveBotTarget(ctx, botTarget{Kind: targetMain})
}

// channelForward sends the owner's message to the conversation the channel talks to (starting it when there is none
// yet) and follows its turn.
func (h *Hub) channelForward(ctx context.Context, id string, m channel.Message) error {
	pc, ok := h.pluginChannel(id)
	if !ok {
		return fmt.Errorf("no channel %s", id)
	}
	caps := pc.item.Capabilities.Normalized()
	rt, err := h.channelTarget(ctx, id)
	if err != nil {
		return err
	}
	in := prompts.ChannelMessage{Persona: true, Name: pc.item.Name, Owner: h.ownerName(), Plain: caps.Format == channel.FormatPlain,
		Asks: caps.Asks != channel.AsksNone, Text: strings.TrimSpace(m.Text), Quote: m.Quote, Forward: m.Forward,
		Unsupported: slices.Clone(m.Unsupported)}
	var images []proto.Image
	for _, img := range m.Images {
		if len(images) < channel.ImagesMost && len(img.Data) <= channel.ImageMost {
			mime := img.MIME
			if !strings.HasPrefix(mime, "image/") {
				mime = http.DetectContentType(img.Data)
			}
			images = append(images, proto.Image{Mime: mime, Data: base64.StdEncoding.EncodeToString(img.Data)})
			continue
		}
		in.Unsupported = append(in.Unsupported, "更多的图片")
	}
	for _, f := range m.Files {
		if !caps.FilesIn || len(f.Data) > channel.FileMost || rt.m == nil {
			in.Unsupported = append(in.Unsupported, "文件「"+firstNonEmpty(f.Name, "未命名")+"」")
			continue
		}
		path, err := h.uploadBytes(ctx, rt.m, firstNonEmpty(f.Name, "file"), f.Data)
		if err != nil {
			return fmt.Errorf("把文件存到 %s：%w", rt.m.cfg.Name, err)
		}
		in.Files = append(in.Files, path)
	}
	in.Unsupported = slices.Compact(in.Unsupported)
	if m.Empty() && len(images) == 0 {
		return nil
	}
	text := prompts.RenderChannelMessage(in)
	run := &channel.Run{ID: id + "_" + m.ID, Channel: id, ReplyTo: m.ID, TargetKind: channel.TargetConversation, Label: rt.label,
		Started: time.Now().UTC(), Probe: channel.Probe(in.Text, append(slices.Clone(in.Files), in.Forward, in.Quote)...)}
	if rt.isNew {
		start := h.startAssistant
		if rt.main {
			start = h.startMainWith
		}
		s, err := start(ctx, text, images, "", "", "")
		if err != nil {
			return fmt.Errorf("没开成新对话：%w", err)
		}
		h.asst().ConversationStarted(ctx, assistantwire.ConversationStarted{MachineID: s.MachineID, SessionID: s.SessionID, Started: time.Now()})
		if !rt.main { // /new: the chat goes on in it
			c := &h.channels
			c.mu.Lock()
			h.loadChannelsLocked(ctx)
			h.channelPrefLocked(id).Target = &channel.Target{Kind: channel.TargetConversation, MachineID: s.MachineID, SessionID: s.SessionID}
			h.saveChannelsLocked()
			c.mu.Unlock()
		}
		run.MachineID, run.SessionID, run.Probe = s.MachineID, s.SessionID, ""
	} else {
		sid := rt.session.SessionID
		run.MachineID, run.SessionID = rt.m.cfg.ID, sid
		run.Base = h.lastSeq(ctx, rt.m, sid)
		cid := channel.ClientMsgID(id, m.ID)
		run.MsgIDs = []string{cid}
		p := map[string]any{"session_id": sid, "text": text, "client_msg_id": cid}
		if len(images) > 0 {
			p["images"] = images
		}
		h.asst().UserSpoke(ctx, assistantwire.SessionRef{MachineID: rt.m.cfg.ID, SessionID: sid}) // unarchived, and its title the user's to keep
		if rt.main {
			if err := h.applyModel(ctx, "assistant", p); err != nil {
				return err
			}
		}
		if err := h.botDeliver(ctx, rt.m, p); err != nil {
			return err
		}
	}
	h.startChannelRun(run)
	return nil
}

// --- what is asked, in text (asks: text) ---

// textChannels are the plugin channels that are connected, by id, with their settings: what asks, pushes and the
// mirror go to.
func (h *Hub) textChannels(ctx context.Context) []channel.View {
	var out []channel.View
	for _, pc := range h.pluginChannels() {
		v := h.pluginChannelView(ctx, pc)
		if v.State == channel.StateConnected {
			out = append(out, v)
		}
	}
	return out
}

func featureOn(v channel.View, id string) bool {
	for _, f := range v.Features {
		if f.ID == id {
			return f.On
		}
	}
	return false
}

// channelDrives: channel id drives this conversation — its target, or a run of it is on it.
func (h *Hub) channelDrives(ctx context.Context, id, machine, session string) bool {
	for _, r := range h.channelRuns(id) {
		if r.MachineID == machine && r.SessionID == session {
			return true
		}
		// The hub's own approvals (mail_send) do not name the conversation: one on the assistant's machine while a
		// run goes on is most likely that run's.
		if session == "" && r.MachineID == machine {
			return true
		}
	}
	if session == "" {
		return false
	}
	rt, err := h.channelTarget(ctx, id)
	return err == nil && !rt.isNew && rt.m.cfg.ID == machine && rt.session.SessionID == session
}

// channelInteraction: a new interaction (hub.go announce): sent as text to the plugin channels that drive its
// conversation (or take every agent's).
func (h *Hub) channelInteraction(ix proto.Interaction) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for _, v := range h.textChannels(ctx) {
			if v.Capabilities.Asks != channel.AsksText || !featureOn(v, channel.FeatureAsks) {
				continue
			}
			if !featureOn(v, channel.FeatureAllAsks) && !h.channelDrives(ctx, v.ID, ix.MachineID, ix.SessionID) {
				continue
			}
			h.askInChannel(ctx, v.ID, ix)
		}
	}()
}

func (h *Hub) askInChannel(ctx context.Context, id string, ix proto.Interaction) {
	c := &h.channels
	c.mu.Lock()
	h.loadChannelStateLocked(ctx)
	known := slices.ContainsFunc(c.st.Asks, func(a *channel.Ask) bool { return a.Channel == id && a.ID == ix.ID })
	now := c.clock()
	c.mu.Unlock()
	if known {
		return
	}
	a := channel.NewAsk(id, ix, now)
	text, answerable := channel.AskText(ix, a)
	reply := ""
	for _, r := range h.channelRuns(id) {
		if r.MachineID == ix.MachineID && r.SessionID == ix.SessionID {
			reply = r.ReplyTo
		}
	}
	if err := h.channelSend(ctx, id, "ask:"+ix.ID, text, channel.OutAsk, reply); err != nil {
		return // not asked there (nowhere to send to yet): the app has it
	}
	if !answerable {
		return
	}
	c.mu.Lock()
	if !slices.ContainsFunc(c.st.Asks, func(x *channel.Ask) bool { return x.Channel == id && x.ID == a.ID }) {
		c.st.Asks = append(c.st.Asks, a)
		h.saveChannelStateLocked()
	}
	c.mu.Unlock()
}

// channelInteractionDone: an interaction is over (answered anywhere, withdrawn, expired). What was sent as text
// cannot be taken back: it only stops waiting, and a reply is then an ordinary message.
func (h *Hub) channelInteractionDone(ix proto.Interaction) {
	c := &h.channels
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.stLoaded {
		return
	}
	n := len(c.st.Asks)
	c.st.Asks = slices.DeleteFunc(c.st.Asks, func(a *channel.Ask) bool { return a.ID == ix.ID })
	if len(c.st.Asks) != n {
		h.saveChannelStateLocked()
	}
}

// pendingIDs are the interactions waiting for an answer now.
func (h *Hub) pendingIDs() map[string]bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make(map[string]bool, len(h.interactions))
	for id := range h.interactions {
		out[id] = true
	}
	return out
}

// channelAnswerAsk takes the owner's message as the answer to the latest thing asked in the channel; false when
// nothing waits (or what waited was answered elsewhere): the message is then an ordinary one.
func (h *Hub) channelAnswerAsk(ctx context.Context, id string, m channel.Message, text string) bool {
	if text == "" {
		return false
	}
	pending := h.pendingIDs() // before h.channels.mu: h.mu is never taken under it
	c := &h.channels
	c.mu.Lock()
	h.loadChannelStateLocked(ctx)
	n := len(c.st.Asks)
	c.st.Asks = slices.DeleteFunc(c.st.Asks, func(a *channel.Ask) bool { return !pending[a.ID] })
	var a *channel.Ask
	for _, x := range c.st.Asks {
		if x.Channel == id {
			a = x // the latest one asked
		}
	}
	if len(c.st.Asks) != n {
		h.saveChannelStateLocked()
	}
	if a == nil {
		c.mu.Unlock()
		return false
	}
	r, done, again := channel.ReadAnswer(a, text)
	h.saveChannelStateLocked()
	c.mu.Unlock()
	reply := func(s string) { _ = h.channelTell(ctx, id, m.ID, s, channel.OutNote) }
	switch {
	case again != "":
		reply(again)
		return true
	case !done:
		reply(channel.QuestionText(a, a.Step)) // a question answered, the next one asked
		return true
	}
	err := h.resolve(a.ID, r)
	c.mu.Lock()
	c.st.Asks = slices.DeleteFunc(c.st.Asks, func(x *channel.Ask) bool { return x.ID == a.ID })
	left := 0
	for _, x := range c.st.Asks {
		if x.Channel == id {
			left++
		}
	}
	h.saveChannelStateLocked()
	c.mu.Unlock()
	switch {
	case errors.Is(err, errNotPending):
		reply("这个已经在别处处理过了。")
	case err != nil:
		reply("没答上：" + clip(oneLine(err.Error()), 200))
	default:
		reply(channel.Answered(r))
	}
	if left > 0 {
		reply(fmt.Sprintf("还有 %d 个在等你回复，最近的那个是上面倒数第二条。", left))
	}
	return true
}

// --- pushes and the sync mirror ---

// channelPush sends what notify_user pushed to the phone to the plugin channels with pushes on too.
func (h *Hub) channelPush(title, body string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for _, v := range h.textChannels(ctx) {
			if v.Capabilities.Push && featureOn(v, channel.FeaturePushes) {
				_ = h.channelTell(ctx, v.ID, "", strings.TrimSpace("🔔 "+title+"\n\n"+body), channel.OutPush)
			}
		}
	}()
}

// pluginMirror sends a turn that ended without being asked for in a channel: the main conversation's to those with
// sync on, a scheduled task's own conversation's to those with pushes on.
func (h *Hub) pluginMirror(machine, session string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var to []channel.View
	for _, v := range h.textChannels(ctx) {
		if v.Capabilities.Push && (featureOn(v, channel.FeatureSync) || featureOn(v, channel.FeaturePushes)) {
			to = append(to, v)
		}
	}
	if len(to) == 0 {
		return
	}
	cfg := h.assistantConfig(ctx)
	main := cfg.Main != "" && cfg.Main == session && cfg.MachineID == machine
	task := h.taskOfConversation(ctx, machine, session)
	if !main && task == "" {
		return
	}
	m := h.localMachine(machine)
	if m == nil {
		return
	}
	if s, ok := m.session(session); !ok || s.WokeByTask && !main {
		return
	}
	var res struct {
		Items []proto.Item `json:"items"`
	}
	if err := m.call(ctx, "session.items", map[string]any{"session_id": session, "turns": 1, "lazy_images": true}, &res); err != nil {
		return
	}
	asked := ""
	for _, it := range res.Items {
		if it.Role == "user" && it.Kind == proto.KindText && asked == "" {
			asked = strings.TrimSpace(it.Text)
		}
	}
	var texts []string
	for _, p := range channel.Answer(res.Items) {
		texts = append(texts, p.Text)
	}
	answer := strings.TrimSpace(strings.Join(texts, "\n\n"))
	if answer == "" {
		return
	}
	h.mu.Lock()
	pushed := time.Since(h.notified[session]) < 3*time.Minute
	h.mu.Unlock()
	for _, v := range to {
		switch {
		case strings.Contains(asked, `title="`+v.Name+`机器人"`):
			continue // its own message: answered by the run
		case main && featureOn(v, channel.FeatureSync):
		case task != "" && featureOn(v, channel.FeaturePushes) && !pushed:
		default:
			continue
		}
		head := "主对话"
		if !main {
			head = "定时任务「" + task + "」"
		} else if asked != "" {
			head += " · 在别处问：" + clip(oneLine(stripContext(asked)), 80)
		}
		_ = h.channelTell(ctx, v.ID, "", head+"\n\n"+answer, channel.OutPush)
	}
}

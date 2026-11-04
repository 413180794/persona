package hub

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/channel"
	"github.com/nwverse/persona/server/internal/feishubot"
	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
)

// Messages to the bot (docs/bots.md「一条消息怎么走」): who may talk to it, pairing, where a message goes, what of it is
// passed on (text, images, files, quotes, forwards) and sending it. The answer is followed in bots_runs.go.

// botEvent takes one event off the long connection. Messages are handled in the background (Feishu waits 3 s for the
// answer); card buttons answer right away.
func (h *Hub) botEvent(ctx context.Context, payload []byte) (any, error) {
	env, err := feishubot.ParseEnvelope(payload)
	if err != nil {
		return nil, nil // something else (a 1.0 event, the url check): nothing to do
	}
	switch env.Header.EventType {
	case feishubot.EventMessage:
		var ev feishubot.MessageEvent
		if err := json.Unmarshal(env.Event, &ev); err != nil {
			return nil, err
		}
		if !h.claimBotMessage(ev.Message.MessageID) {
			return nil, nil // redelivered
		}
		go h.botMessage(ev)
	case feishubot.EventCardAction:
		var a feishubot.CardAction
		if err := json.Unmarshal(env.Event, &a); err != nil {
			return nil, err
		}
		return h.botCardAction(ctx, &a), nil
	case feishubot.EventP2PEntered:
		var ev feishubot.P2PEntered
		if json.Unmarshal(env.Event, &ev) == nil {
			go h.botWelcome(ev.OperatorID.OpenID, ev.ChatID)
		}
	}
	return nil, nil
}

// claimBotMessage: the first time this message is seen (Feishu delivers again what it thinks was missed).
func (h *Hub) claimBotMessage(id string) bool {
	b := &h.bots
	b.mu.Lock()
	defer b.mu.Unlock()
	h.loadBotsLocked(context.Background())
	if id == "" || slices.Contains(b.st.Seen, id) {
		return false
	}
	b.st.Seen = append(b.st.Seen, id)
	h.saveBotStateLocked()
	return true
}

// botTell sends a short note to someone, at most every 10 minutes per person (pairing hints, refusals).
func (h *Hub) botTell(api *feishubot.Client, openID, chatID, text string) {
	b := &h.bots
	b.mu.Lock()
	if b.told == nil {
		b.told = map[string]time.Time{}
	}
	if t, ok := b.told[openID]; ok && b.clock().Sub(t) < 10*time.Minute {
		b.mu.Unlock()
		return
	}
	b.told[openID] = b.clock()
	b.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := api.Send(ctx, "chat_id", chatID, feishubot.TypeText, feishubot.TextContent(text), ""); err != nil {
		h.logf("bots: tell: %v", err)
	}
}

var regexpDigits = regexp.MustCompile(`^\d{4,8}$`)

const botPairHint = "还没配对：在 Persona App 的「设置 › 连接器 › 飞书机器人」里点「配对」，把显示的 6 位配对码发给我。"

func (h *Hub) botWelcome(openID, chatID string) {
	b := &h.bots
	b.mu.Lock()
	api, owner := b.api, b.set.Owner
	b.mu.Unlock()
	if api == nil || owner != nil || openID == "" || chatID == "" {
		return
	}
	h.botTell(api, openID, chatID, botPairHint)
}

// botMessage handles one message to the bot.
func (h *Hub) botMessage(ev feishubot.MessageEvent) {
	b := &h.bots
	b.mu.Lock()
	api, set := b.api, b.set.copy()
	b.mu.Unlock()
	msg := ev.Message
	sender := ev.Sender.SenderID.OpenID
	if api == nil || ev.Sender.SenderType != "user" || sender == "" {
		return
	}
	if msg.ChatType != "p2p" {
		return // only its private chat: in a group anyone could reach the owner's Persona
	}
	content := feishubot.ParseContent(msg.MessageType, msg.Content, msg.Mentions)
	if set.Owner == nil {
		h.botPair(api, sender, msg.ChatID, strings.TrimSpace(content.Text))
		return
	}
	if sender != set.Owner.OpenID {
		h.botTell(api, sender, msg.ChatID, "这个机器人只为它的主人服务。")
		return
	}
	if msg.ChatID != set.Owner.ChatID { // the owner's chat with the bot is the same one; keep it current anyway
		b.mu.Lock()
		if b.set.Owner != nil {
			b.set.Owner.ChatID = msg.ChatID
			h.saveBotSettingsLocked()
		}
		b.mu.Unlock()
		set.Owner.ChatID = msg.ChatID // the copy
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if strings.HasPrefix(content.Text, "/") && !content.Forward && len(content.Media) == 0 {
		handled, rest := h.botCommand(ctx, api, set, msg.MessageID, content.Text)
		if handled {
			return
		}
		content.Text = rest
	}
	target := h.botTargetFor(msg.RootID, msg.ParentID)
	if err := h.botAllowed(set.Features, target); err != nil {
		h.botNote(ctx, api, msg.MessageID, err.Error())
		return
	}
	if err := h.botSend(ctx, api, set, &ev, content, target); err != nil {
		h.logf("bots: send: %v", err)
		h.botNote(ctx, api, msg.MessageID, "没能发出去："+clip(oneLine(err.Error()), 200))
	}
}

// botPair: the message is the code shown in the app, or the sender is told how to pair.
func (h *Hub) botPair(api *feishubot.Client, openID, chatID, text string) {
	b := &h.bots
	b.mu.Lock()
	p := b.set.Pairing
	ok := p != nil && b.clock().Before(p.Expires) && text == p.Code
	if !ok && p != nil && regexpDigits.MatchString(text) {
		if p.Fails++; p.Fails >= botPairTries {
			b.set.Pairing = nil // guessed at: the user asks for a new code in the app
		}
		h.saveBotSettingsLocked()
	}
	if ok {
		b.set.Owner = &BotOwner{OpenID: openID, ChatID: chatID, At: b.clock().UTC()}
		b.set.Pairing = nil
		b.st.Current = botTarget{Kind: targetMain}
		h.saveBotSettingsLocked()
		h.saveBotStateLocked()
	}
	b.mu.Unlock()
	if !ok {
		h.botTell(api, openID, chatID, botPairHint)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	name := h.botName(ctx, api, openID)
	b.mu.Lock()
	if b.set.Owner != nil && b.set.Owner.OpenID == openID {
		b.set.Owner.Name = name
		h.saveBotSettingsLocked()
	}
	b.mu.Unlock()
	card := feishubot.NewCard("已配对", feishubot.Green, "已配对").Markdown("以后在这里说的话会交给 Persona。\n\n" + botHelp(h.botFeatures()))
	if _, err := h.botPost(ctx, api, chatID, "", card, ""); err != nil {
		h.logf("bots: paired note: %v", err)
	}
}

func (h *Hub) botFeatures() BotFeatures {
	h.bots.mu.Lock()
	defer h.bots.mu.Unlock()
	return h.bots.set.Features
}

// botName is a user's name (the contact API; the app may not have that permission, then the hub's owner name).
func (h *Hub) botName(ctx context.Context, api *feishubot.Client, openID string) string {
	b := &h.bots
	b.mu.Lock()
	if n, ok := b.names[openID]; ok {
		b.mu.Unlock()
		return n
	}
	owner := b.set.Owner
	b.mu.Unlock()
	name, err := api.UserName(ctx, openID)
	if err != nil || name == "" {
		name = "某人"
		if owner != nil && owner.OpenID == openID {
			name = h.ownerName()
		}
	}
	b.mu.Lock()
	if b.names == nil {
		b.names = map[string]string{}
	}
	b.names[openID] = name
	b.mu.Unlock()
	return name
}

// botTargetFor is where a message goes: the target of the bot message it replies to (or of its thread), else the
// chat's current one.
func (h *Hub) botTargetFor(root, parent string) botTarget {
	b := &h.bots
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, id := range []string{parent, root} {
		if t := b.st.Threads[id]; id != "" && t != nil {
			return t.Target
		}
	}
	return b.st.Current
}

func (h *Hub) botAllowed(f BotFeatures, t botTarget) error {
	switch {
	case t.agent() && !f.Agents:
		return errors.New("在飞书里驱动 Agent 没有打开（App 设置 › 连接器 › 飞书机器人）。发 /main 回到主对话。")
	case !t.agent() && !f.Main:
		if f.Agents {
			return errors.New("和 Persona 对话没有打开，只能驱动 Agent：发 /agents 选一个。")
		}
		return errors.New("飞书机器人的对话功能没有打开（App 设置 › 连接器 › 飞书机器人）。")
	}
	return nil
}

// botNote replies to a message with a line of text.
func (h *Hub) botNote(ctx context.Context, api *feishubot.Client, messageID, text string) {
	if _, err := api.Reply(ctx, messageID, feishubot.TypeText, feishubot.TextContent(text), "", false); err != nil {
		h.logf("bots: note: %v", err)
	}
}

// botPost sends a card: as a reply to replyTo, else into the chat. A card Feishu refuses goes again as rich text
// (fallback, else the card's own Markdown is lost: then a line saying so).
func (h *Hub) botPost(ctx context.Context, api *feishubot.Client, chatID, replyTo string, card *feishubot.Card, fallback string) (string, error) {
	send := func(typ, content string) (string, error) {
		if replyTo != "" {
			return api.Reply(ctx, replyTo, typ, content, "", false)
		}
		return api.Send(ctx, "chat_id", chatID, typ, content, "")
	}
	id, err := send(feishubot.TypeInteractive, card.JSON())
	if err != nil && feishubot.CardRejected(err) {
		h.logf("bots: card refused (%v); sending it as rich text", err)
		if fallback == "" {
			fallback = "（这条消息没能显示成卡片，请到 App 里看。）"
		}
		return send(feishubot.TypePost, feishubot.PostMarkdown(fallback))
	}
	return id, err
}

// remember binds a message the bot posted to a target: replying to it talks to that target.
func (h *Hub) rememberBotThread(messageID string, t botTarget, title string) {
	if messageID == "" || t.Kind == "" {
		return
	}
	b := &h.bots
	b.mu.Lock()
	defer b.mu.Unlock()
	b.st.Threads[messageID] = &botThread{Target: t, Title: title, At: b.clock().UTC()}
	h.saveBotStateLocked()
}

// --- sending ---

// resolvedTarget is a target as it is now: the machine and conversation, and what to call it.
type resolvedTarget struct {
	m       *Machine
	session proto.Session
	label   string
	main    bool
	isNew   bool // no conversation yet: the message starts one
}

func (h *Hub) resolveBotTarget(ctx context.Context, t botTarget) (resolvedTarget, error) {
	switch t.Kind {
	case targetAgent:
		m, s, err := h.findAgent(t.MachineID, t.SessionID)
		if err != nil {
			return resolvedTarget{}, fmt.Errorf("找不到这个 Agent（%s）：发 /agents 重新选", short(t.SessionID))
		}
		if s.SourceKind != "" && (s.Capabilities == nil || !s.Capabilities.Send) {
			return resolvedTarget{}, fmt.Errorf("这个 Agent 仅支持查看：%s", s.ReadOnlyReason)
		}
		return resolvedTarget{m: m, session: s, label: agentLabel(s) + " · " + m.cfg.Name}, nil
	default:
		cfg := h.assistantConfig(ctx)
		m := h.localMachine(cfg.MachineID)
		if m == nil {
			return resolvedTarget{}, errors.New("助手所在的机器没有配置")
		}
		sid := t.SessionID
		if t.Kind == targetMain {
			sid = cfg.Main
		}
		r := resolvedTarget{m: m, main: sid != "" && sid == cfg.Main}
		if sid == "" {
			r.isNew, r.main, r.label = true, t.Kind == targetMain, "新对话"
			if r.main {
				r.label = "主对话"
			}
			return r, nil
		}
		s, _ := m.session(sid)
		s.SessionID, s.MachineID = sid, m.cfg.ID
		r.session = s
		r.label = conversationName(s, cfg.Main)
		return r, nil
	}
}

func (h *Hub) targetLabel(ctx context.Context, t botTarget) string {
	r, err := h.resolveBotTarget(ctx, t)
	if err != nil {
		return "（找不到）"
	}
	if t.Kind == targetJarvis && r.isNew {
		return "新对话（下一条消息开始）"
	}
	return r.label
}

// botSend passes a message on to its target and starts the run that answers it.
func (h *Hub) botSend(ctx context.Context, api *feishubot.Client, set FeishuBotSettings, ev *feishubot.MessageEvent, content feishubot.Content, t botTarget) error {
	rt, err := h.resolveBotTarget(ctx, t)
	if err != nil {
		return err
	}
	msg := ev.Message
	reaction, err := api.React(ctx, msg.MessageID, "OnIt")
	if err != nil {
		h.logf("bots: react: %v", err)
	}
	started := false
	defer func() {
		if !started && reaction != "" {
			_ = api.Unreact(context.WithoutCancel(ctx), msg.MessageID, reaction)
		}
	}()
	in := prompts.BotMessage{Jarvis: !t.agent(), Owner: h.ownerName(), Text: content.Text}
	h.bots.mu.Lock()
	_, ours := h.bots.st.Threads[msg.ParentID]
	h.bots.mu.Unlock()
	if msg.ParentID != "" && !ours {
		in.Quote = h.botQuote(ctx, api, msg.ParentID)
	}
	if content.Forward {
		in.Forward = h.botForward(ctx, api, msg.MessageID)
	}
	var images []proto.Image
	for _, md := range content.Media {
		data, mime, err := api.Resource(ctx, msg.MessageID, md.Key, md.Kind, botFileMax)
		if err != nil {
			return fmt.Errorf("下载%s：%w", map[bool]string{true: "图片", false: "文件"}[md.Kind == "image"], err)
		}
		if md.Kind == "image" {
			if !strings.HasPrefix(mime, "image/") {
				mime = "image/png"
			}
			if len(images) < botImagesMax && len(data) <= botImageMax {
				images = append(images, proto.Image{Mime: mime, Data: base64.StdEncoding.EncodeToString(data)})
				continue
			}
			// More pictures than a message takes, or one too big for the model: as a file.
			md.Name = md.Key + map[string]string{"image/jpeg": ".jpg", "image/gif": ".gif", "image/webp": ".webp"}[mime]
			if !strings.Contains(md.Name, ".") {
				md.Name += ".png"
			}
		}
		p, err := h.uploadBytes(ctx, rt.m, md.Name, data)
		if err != nil {
			return fmt.Errorf("把文件存到 %s：%w", rt.m.cfg.Name, err)
		}
		in.Files = append(in.Files, p)
	}
	text := prompts.RenderBotMessage(in)
	if strings.TrimSpace(content.Text) == "" && in.Quote == nil && in.Forward == "" && len(images) == 0 && len(in.Files) == 0 {
		return errors.New("这种消息还读不了")
	}

	run := &channel.Run{ID: "fb_" + msg.MessageID, Channel: feishuChannelID, ReplyTo: msg.MessageID, Started: time.Now().UTC(), Label: rt.label,
		Probe: botProbe(content.Text, in), Ext: map[string]string{}}
	if reaction != "" {
		run.Ext["reaction"] = reaction
	}
	if rt.isNew {
		start := h.startAssistant
		if rt.main {
			start = h.startMainWith
		}
		s, err := start(ctx, text, images, "", "", "")
		if err != nil {
			return err
		}
		h.asst().ConversationStarted(ctx, assistantwire.ConversationStarted{MachineID: s.MachineID, SessionID: s.SessionID, Started: time.Now()})
		run.MachineID, run.SessionID, run.TargetKind, run.Probe = s.MachineID, s.SessionID, targetJarvis, ""
		if t.Kind == targetJarvis { // /new: the chat goes on in it
			h.bots.mu.Lock()
			h.bots.st.Current = botTarget{Kind: targetJarvis, MachineID: s.MachineID, SessionID: s.SessionID}
			h.saveBotStateLocked()
			h.bots.mu.Unlock()
		}
	} else {
		sid := rt.session.SessionID
		run.MachineID, run.SessionID, run.TargetKind = rt.m.cfg.ID, sid, t.Kind
		if run.TargetKind == targetMain {
			run.TargetKind = targetJarvis
		}
		run.Base = h.lastSeq(ctx, rt.m, sid)
		id := channel.ClientMsgID(feishuChannelID, msg.MessageID)
		run.MsgIDs = []string{id}
		p := map[string]any{"session_id": sid, "text": text, "client_msg_id": id}
		if len(images) > 0 {
			p["images"] = images
		}
		if !t.agent() {
			h.asst().UserSpoke(ctx, assistantwire.SessionRef{MachineID: rt.m.cfg.ID, SessionID: sid})
			if rt.main {
				if err := h.applyModel(ctx, "assistant", p); err != nil {
					return err
				}
			}
		}
		if err := h.botDeliver(ctx, rt.m, p); err != nil {
			return err
		}
	}
	started = true
	h.startChannelRun(run)
	return nil
}

// botProbe picks what of a message to look for in the transcript: its first line, else something only it has.
func botProbe(text string, in prompts.BotMessage) string {
	var fallbacks []string
	if len(in.Files) > 0 {
		fallbacks = append(fallbacks, in.Files[0])
	}
	fallbacks = append(fallbacks, in.Forward)
	if in.Quote != nil {
		fallbacks = append(fallbacks, in.Quote.Text)
	}
	return channel.Probe(text, fallbacks...)
}

// botDeliver sends with the same once-only guard as the app (send.go): a message id is sent at most once.
func (h *Hub) botDeliver(ctx context.Context, m *Machine, p map[string]any) error {
	id, _ := p["client_msg_id"].(string)
	sid, _ := p["session_id"].(string)
	if h.store != nil {
		state, err := h.store.ClaimSend(ctx, id, m.cfg.ID, sid)
		switch {
		case err != nil:
			return err
		case state == sendDelivered:
			return nil
		case state == sendSending || state == sendUnknown:
			return errors.New("这条消息之前发送时连接断了，先到 App 里看看有没有送到")
		}
	}
	err := m.call(ctx, "session.send", p, nil)
	if h.store != nil {
		if serr := h.store.SettleSend(context.WithoutCancel(ctx), id, sendOutcome(err)); serr != nil {
			h.logf("bots: settle send: %v", serr)
		}
	}
	return err
}

// lastSeq is the conversation's last line now (0 when it has none yet).
func (h *Hub) lastSeq(ctx context.Context, m *Machine, sid string) int {
	var res struct {
		ToSeq int `json:"to_seq"`
	}
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := m.call(cctx, "session.items", map[string]any{"session_id": sid, "limit": 1, "lazy_images": true}, &res); err != nil {
		return 0
	}
	return res.ToSeq
}

// uploadBytes saves a file on a machine (its temp directory, like the app's uploads) and returns the path.
func (h *Hub) uploadBytes(ctx context.Context, m *Machine, name string, data []byte) (string, error) {
	var begin struct {
		UploadID string `json:"upload_id"`
	}
	if err := m.call(ctx, "file.upload.begin", map[string]any{"name": name, "size": len(data)}, &begin); err != nil {
		return "", err
	}
	for off := 0; off < len(data); off += proto.UploadChunkBytes {
		end := min(off+proto.UploadChunkBytes, len(data))
		if err := m.call(ctx, "file.upload.chunk", map[string]any{"upload_id": begin.UploadID, "offset": off, "data": data[off:end]}, nil); err != nil {
			_ = m.call(context.WithoutCancel(ctx), "file.upload.abort", map[string]any{"upload_id": begin.UploadID}, nil)
			return "", err
		}
	}
	var out proto.UploadedFile
	if err := m.call(ctx, "file.upload.end", map[string]any{"upload_id": begin.UploadID}, &out); err != nil {
		return "", err
	}
	return out.Path, nil
}

// botQuote is the message a message replies to.
func (h *Hub) botQuote(ctx context.Context, api *feishubot.Client, id string) *prompts.BotQuote {
	items, err := api.Message(ctx, id)
	if err != nil || len(items) == 0 {
		return nil
	}
	it := items[0]
	c := feishubot.ParseContent(it.MsgType, it.Body.Content, nil)
	who := "Persona"
	if it.Sender.SenderType == "user" {
		who = h.botName(ctx, api, it.Sender.ID)
	}
	if c.Text == "" {
		c.Text = "[" + it.MsgType + "]"
	}
	return &prompts.BotQuote{Who: who, Text: clip(c.Text, 4000)}
}

// botForward reads a merged forward: one line per message, oldest first.
func (h *Hub) botForward(ctx context.Context, api *feishubot.Client, id string) string {
	items, err := api.Message(ctx, id)
	if err != nil {
		return "（读不到转发的内容：" + err.Error() + "）"
	}
	loc := h.userLocation()
	var lines []string
	n := 0
	for _, it := range items {
		if it.UpperMessageID != id {
			continue
		}
		c := feishubot.ParseContent(it.MsgType, it.Body.Content, nil)
		text := c.Text
		if text == "" {
			text = "[" + it.MsgType + "]"
		}
		for _, md := range c.Media {
			if md.Kind == "image" && !strings.Contains(text, "[图片]") {
				text += " [图片]"
			}
		}
		who := "某人"
		if it.Sender.SenderType == "user" {
			who = h.botName(ctx, api, it.Sender.ID)
		} else if it.Sender.SenderType == "app" {
			who = "机器人"
		}
		line := fmt.Sprintf("%s %s：%s", it.At().In(loc).Format("01-02 15:04"), who, text)
		n += len([]rune(line))
		if n > 30000 {
			lines = append(lines, "（后面的太长，略）")
			break
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return "（转发里没有能读的消息）"
	}
	return strings.Join(lines, "\n")
}

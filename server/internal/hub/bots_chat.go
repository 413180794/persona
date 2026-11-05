package hub

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/channel"
	"github.com/nwverse/persona/server/internal/feishubot"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
)

// Messages to the bot (docs/bots.md「一条消息怎么走」), the hub's part: who may talk to it, pairing, and handing the
// owner's messages and card presses to the assistant (feishu.message, feishu.card_action), which does the rest.
// A message waits for the assistant while it is not connected (a hub start, an assistant restart: toAssistant); one
// it can't be handed is answered that the assistant can't be reached.

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
		go h.botMessage(ev, env.Event)
	case feishubot.EventCardAction:
		var a feishubot.CardAction
		if err := json.Unmarshal(env.Event, &a); err != nil {
			return nil, err
		}
		return h.botCardAction(ctx, &a, env.Event), nil
	case feishubot.EventP2PEntered:
		var ev feishubot.P2PEntered
		if json.Unmarshal(env.Event, &ev) == nil {
			go h.botWelcome(ev.OperatorID.OpenID, ev.ChatID)
		}
	}
	return nil, nil
}

// botSeenKeep is how many message ids the hub remembers: Feishu delivers again what it thinks was missed. The
// assistant keeps its own (bots.feishu.state), across restarts.
const botSeenKeep = 300

// claimBotMessage: the first time this message is seen since the hub started.
func (h *Hub) claimBotMessage(id string) bool {
	b := &h.bots
	b.mu.Lock()
	defer b.mu.Unlock()
	if id == "" || slices.Contains(b.seen, id) {
		return false
	}
	b.seen = append(b.seen, id)
	if len(b.seen) > botSeenKeep {
		b.seen = b.seen[len(b.seen)-botSeenKeep:]
	}
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

func (h *Hub) botWelcome(openID, chatID string) {
	b := &h.bots
	b.mu.Lock()
	api, owner := b.api, b.set.Owner
	b.mu.Unlock()
	if api == nil || owner != nil || openID == "" || chatID == "" {
		return
	}
	h.botTell(api, openID, chatID, i18n.T(h.userLang(), "hub-bots_chat.pair_hint"))
}

// botMessage handles one message to the bot: the owner's go to the assistant.
func (h *Hub) botMessage(ev feishubot.MessageEvent, raw json.RawMessage) {
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
	if set.Owner == nil {
		content := feishubot.ParseContent(msg.MessageType, msg.Content, msg.Mentions)
		h.botPair(api, sender, msg.ChatID, strings.TrimSpace(content.Text))
		return
	}
	if sender != set.Owner.OpenID {
		h.botTell(api, sender, msg.ChatID, i18n.T(h.userLang(), "hub-bots_chat.owner_only"))
		return
	}
	if msg.ChatID != set.Owner.ChatID { // the owner's chat with the bot is the same one; keep it current anyway
		b.mu.Lock()
		if b.set.Owner != nil {
			b.set.Owner.ChatID = msg.ChatID
			h.saveBotSettingsLocked()
		}
		b.mu.Unlock()
	}
	err := h.toAssistant(assistantwire.MethodFeishuMessage, assistantwire.FeishuMessage{Event: raw})
	if err == nil {
		return
	}
	if !errors.Is(err, errAssistantOffline) {
		h.logf("bots: hand the message to the assistant: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	h.botNote(ctx, api, msg.MessageID, i18n.T(h.userLang(), "hub-bots_chat.not_sent", i18n.Args{"error": clip(oneLine(err.Error()), 200)}))
}

// botCardAction answers a press on one of the bot's cards: only the owner's count, and the assistant answers them.
func (h *Hub) botCardAction(ctx context.Context, a *feishubot.CardAction, raw json.RawMessage) any {
	_, set := h.botAPI()
	if set.Owner == nil || a.Operator.OpenID != set.Owner.OpenID {
		return toast("error", i18n.T(h.userLang(), "hub-bots_chat.owner_only_action"))
	}
	// The owner pressed it (checked here): the assistant answers the card with this, once (interactions.resolve).
	action := ""
	if a.Str("a") == "ix" && a.Str("id") != "" {
		action = h.issueOwnerAction(channel.Feishu, a.Str("id"))
	}
	var out json.RawMessage
	// Feishu waits 3 s: a moment for an assistant that is reconnecting, then the call itself.
	err := h.callAssistantWaiting(assistantwire.MethodFeishuAction, assistantwire.FeishuAction{Event: raw, Action: action}, &out,
		time.Now().Add(2500*time.Millisecond), 2*time.Second)
	switch {
	case errors.Is(err, errAssistantOffline):
		return toast("error", errAssistantOffline.Error())
	case err != nil:
		return toast("error", clip(oneLine(err.Error()), 80))
	case len(out) == 0 || string(out) == "null":
		return nil
	}
	return out
}

func toast(kind, text string) *feishubot.CardResponse {
	return &feishubot.CardResponse{Toast: &feishubot.Toast{Type: kind, Content: text}}
}

// botPair: the message is the code shown in the app, or the sender is told how to pair. Paired, the assistant is told
// (it starts the chat afresh and says hello); while it is not connected the hub says hello itself.
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
		h.saveBotSettingsLocked()
	}
	b.mu.Unlock()
	if !ok {
		h.botTell(api, openID, chatID, i18n.T(h.userLang(), "hub-bots_chat.pair_hint"))
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
	if h.notifyBus(assistantwire.AssistantID, assistantwire.EventFeishuOwner, assistantwire.FeishuOwner{Paired: true}) == nil {
		return
	}
	l := h.userLang()
	card := feishubot.NewCard(i18n.T(l, "hub-bots_chat.paired"), feishubot.Green, i18n.T(l, "hub-bots_chat.paired")).Markdown(i18n.T(l, "hub-bots_chat.paired_body"))
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

// botNote replies to a message with a line of text.
func (h *Hub) botNote(ctx context.Context, api *feishubot.Client, messageID, text string) {
	if _, err := api.Reply(ctx, messageID, feishubot.TypeText, feishubot.TextContent(text), "", false); err != nil {
		h.logf("bots: note: %v", err)
	}
}

// botPost sends a card: as a reply to replyTo, else into the chat. A card Feishu refuses goes again as rich text
// (fallback, else the card's own Markdown is lost: then a line saying so).
func (h *Hub) botPost(ctx context.Context, api *feishubot.Client, chatID, replyTo string, card *feishubot.Card, fallback string) (string, error) {
	return h.botPostJSON(ctx, api, chatID, replyTo, card.JSON(), fallback)
}

func (h *Hub) botPostJSON(ctx context.Context, api *feishubot.Client, chatID, replyTo, card, fallback string) (string, error) {
	send := func(typ, content string) (string, error) {
		if replyTo != "" {
			return api.Reply(ctx, replyTo, typ, content, "", false)
		}
		return api.Send(ctx, "chat_id", chatID, typ, content, "")
	}
	id, err := send(feishubot.TypeInteractive, card)
	if err != nil && feishubot.CardRejected(err) {
		h.logf("bots: card refused (%v); sending it as rich text", err)
		if fallback == "" {
			fallback = i18n.T(h.botLang(ctx), "hub-bots_chat.card_refused")
		}
		return send(feishubot.TypePost, feishubot.PostMarkdown(fallback))
	}
	return id, err
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

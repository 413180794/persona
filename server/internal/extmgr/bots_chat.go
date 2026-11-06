package extmgr

import (
	"context"
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	mrand "math/rand/v2"
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
func (h *Manager) botEvent(ctx context.Context, payload []byte) (any, error) {
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
func (h *Manager) claimBotMessage(id string) bool {
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

// feishuMaxUUID is the open platform's uuid length cap on the send/reply message APIs.
const feishuMaxUUID = 50

// feishuReplyUUID is the deterministic idempotency key for a reply: same parent message, same purpose (kind:
// c card, f rich-text fallback, t plain text, n note) and same content replay the same key even across a restart;
// different content (the several cards of one turn, or the card vs its fallback) gets a different key, so the
// gateway does not dedupe it.
func feishuReplyUUID(messageID, kind, content string) string {
	sum := sha256.Sum256([]byte(content))
	fp := hex.EncodeToString(sum[:6])
	u := "r:" + messageID + ":" + kind + fp
	if len(u) <= feishuMaxUUID {
		return u
	}
	keep := feishuMaxUUID - len("r:") - len(":"+kind) - len(fp)
	if keep < 1 {
		keep = 1
	}
	return "r:" + messageID[:keep] + ":" + kind + fp
}

// feishuSendUUID is a one-logical-send key for sends without a parent message to anchor to (pairing notes, test
// messages): random per call, never content-derived, so two legitimate sends are never deduped; the client's retries
// still replay this same key.
func feishuSendUUID() string {
	var b [8]byte
	if _, err := crand.Read(b[:]); err != nil {
		// crypto/rand failing is extraordinary; all-zero keys would make the gateway dedupe every send, so mix the
		// clock with the process RNG instead.
		binary.LittleEndian.PutUint64(b[:], uint64(time.Now().UnixNano())^mrand.Uint64())
	}
	return "s:" + hex.EncodeToString(b[:6])
}

// botTell refuses a non-owner, at most every 10 minutes per person: a stranger's client must not spam the owner's
// bot. The pairing path has its own short window (botPairTellFor), so a legitimate retry is never swallowed.
func (h *Manager) botTell(api *feishubot.Client, openID, chatID, text string) {
	b := &h.bots
	h.botNoteThrottled(api, openID, chatID, text, &b.told, 10*time.Minute)
}

// botPairTellFor is the pairing path's window: it only absorbs a redelivery of the same message, not a person's
// repeated tries.
const botPairTellFor = 30 * time.Second

// botPairTell answers the pairing welcome hint at most every botPairTellFor per person. Wrong-code and expired-code
// notes are not throttled here: botPair marks its own per-text redelivery key, then sends them via botSendNote.
func (h *Manager) botPairTell(api *feishubot.Client, openID, chatID, text string) {
	b := &h.bots
	h.botNoteThrottled(api, openID, chatID, text, &b.pairTold, botPairTellFor)
}

// botNoteThrottled sends one text note per openID per window, remembering when in the *told map (held under b.mu).
func (h *Manager) botNoteThrottled(api *feishubot.Client, openID, chatID, text string, told *map[string]time.Time, window time.Duration) {
	b := &h.bots
	b.mu.Lock()
	if *told == nil {
		*told = map[string]time.Time{}
	}
	if t, ok := (*told)[openID]; ok && b.clock().Sub(t) < window {
		b.mu.Unlock()
		return
	}
	(*told)[openID] = b.clock()
	b.mu.Unlock()
	h.botSendNote(api, chatID, text)
}

// botSendNote sends one text note into a chat without any throttling.
func (h *Manager) botSendNote(api *feishubot.Client, chatID, text string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := api.Send(ctx, "chat_id", chatID, feishubot.TypeText, feishubot.TextContent(text), feishuSendUUID()); err != nil {
		h.logf("bots: tell: %v", err)
	}
}

var regexpDigits = regexp.MustCompile(`^\d{4,8}$`)

func (h *Manager) botWelcome(openID, chatID string) {
	b := &h.bots
	b.mu.Lock()
	api, owner := b.api, b.set.Owner
	b.mu.Unlock()
	if api == nil || owner != nil || openID == "" || chatID == "" {
		return
	}
	h.botPairTell(api, openID, chatID, i18n.T(h.userLang(), "hub-bots_chat.pair_hint"))
}

// botMessage handles one message to the bot: the owner's go to the assistant.
func (h *Manager) botMessage(ev feishubot.MessageEvent, raw json.RawMessage) {
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
	h.botNote(ctx, api, msg.ChatID, msg.MessageID, i18n.T(h.userLang(), "hub-bots_chat.not_sent", i18n.Args{"error": clip(oneLine(err.Error()), 200)}))
}

// botCardAction answers a press on one of the bot's cards: only the owner's count, and the assistant answers them.
func (h *Manager) botCardAction(ctx context.Context, a *feishubot.CardAction, raw json.RawMessage) any {
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
func (h *Manager) botPair(api *feishubot.Client, openID, chatID, text string) {
	b := &h.bots
	b.mu.Lock()
	// Feishu can redeliver the very same message; claimBotMessage drops a repeated id, but a short per-text window
	// also covers a duplicate that reached us under another id. It must not swallow a genuine retry: a different
	// answer (the next code guessed) is a new try, so the key is the person plus the exact text.
	dupKey := openID + "|" + text
	if t, ok := b.pairTold[dupKey]; ok && b.clock().Sub(t) < botPairTellFor {
		b.mu.Unlock()
		return
	}
	p := b.set.Pairing
	live := p != nil && b.clock().Before(p.Expires)
	ok := live && text == p.Code
	noteKey := "hub-bots_chat.pair_hint"
	var noteArgs i18n.Args
	if !ok && live && regexpDigits.MatchString(text) {
		// A code-shaped answer while a pairing is still live is a real try: say specifically that it was wrong and
		// how many tries remain, instead of the generic hint. At the cap the pairing is dropped, so the note says
		// the code is gone. A time-expired pairing is not "wrong": it keeps the generic hint and no failure.
		if p.Fails++; p.Fails >= botPairTries {
			b.set.Pairing = nil // guessed at: the user asks for a new code in the app
			noteKey = "hub-bots_chat.pair_expired"
		} else {
			noteKey = "hub-bots_chat.pair_wrong"
			noteArgs = i18n.Args{"n": botPairTries - p.Fails}
		}
		h.saveBotSettingsLocked()
	}
	if ok {
		b.set.Owner = &BotOwner{OpenID: openID, ChatID: chatID, At: b.clock().UTC()}
		b.set.Pairing = nil
		h.saveBotSettingsLocked()
	} else {
		now := b.clock()
		if b.pairTold == nil {
			b.pairTold = map[string]time.Time{}
		}
		b.pairTold[dupKey] = now
		if len(b.pairTold)%64 == 0 { // bound the map: keys are useless past the short window
			for k, at := range b.pairTold {
				if now.Sub(at) >= botPairTellFor {
					delete(b.pairTold, k)
				}
			}
		}
	}
	b.mu.Unlock()
	if !ok {
		h.botSendNote(api, chatID, i18n.T(h.userLang(), noteKey, noteArgs))
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
	if h.notifyAssistant(assistantwire.EventFeishuOwner, assistantwire.FeishuOwner{Paired: true}) == nil {
		return
	}
	l := h.userLang()
	body := i18n.T(l, "hub-bots_chat.paired_body")
	card := feishubot.NewCard(i18n.T(l, "hub-bots_chat.paired"), feishubot.Green, i18n.T(l, "hub-bots_chat.paired")).Markdown(body)
	if _, err := h.botPost(ctx, api, chatID, "", card, body); err != nil {
		h.logf("bots: paired note: %v", err)
	}
}

func (h *Manager) botFeatures() BotFeatures {
	h.bots.mu.Lock()
	defer h.bots.mu.Unlock()
	return h.bots.set.Features
}

// botName is a user's name (the contact API; the app may not have that permission, then the hub's owner name).
func (h *Manager) botName(ctx context.Context, api *feishubot.Client, openID string) string {
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

// replyOrSend replies to replyTo when its parent still stands, or sends into chatID when replyTo is "". A definitive
// 230011/230110 (parent withdrawn/deleted, which proves the reply never landed) reroutes the same content to a chat
// send with a fresh one-shot key and rerouted=true; transport or other failures come back untouched, so one blip
// never becomes two messages.
func (h *Manager) replyOrSend(ctx context.Context, api *feishubot.Client, chatID, replyTo, msgType, content, replyUUID string) (id string, rerouted bool, err error) {
	if replyTo == "" {
		id, err = api.Send(ctx, "chat_id", chatID, msgType, content, feishuSendUUID())
		return
	}
	id, err = api.Reply(ctx, replyTo, msgType, content, replyUUID, false)
	if !feishubot.MessageGone(err) {
		return
	}
	h.logf("bots: parent message %s was withdrawn or deleted; resending the answer into chat %s", replyTo, chatID)
	id, err = api.Send(ctx, "chat_id", chatID, msgType, content, feishuSendUUID())
	rerouted = true
	return
}

// botNote replies to a message with a line of text; a parent withdrawn meanwhile gets the note in the chat instead.
func (h *Manager) botNote(ctx context.Context, api *feishubot.Client, chatID, messageID, text string) {
	content := feishubot.TextContent(text)
	if _, _, err := h.replyOrSend(ctx, api, chatID, messageID, feishubot.TypeText, content, feishuReplyUUID(messageID, "n", text)); err != nil {
		h.logf("bots: note: %v", err)
	}
}

// botPost sends a card: as a reply to replyTo, else into the chat. A card Feishu refuses goes again as rich text
// (fallback, else the card's own Markdown is lost: then a line saying so).
func (h *Manager) botPost(ctx context.Context, api *feishubot.Client, chatID, replyTo string, card *feishubot.Card, fallback string) (string, error) {
	return h.botPostJSON(ctx, api, chatID, replyTo, card.JSON(), fallback)
}

func (h *Manager) botPostJSON(ctx context.Context, api *feishubot.Client, chatID, replyTo, card, fallback string) (string, error) {
	cardUUID := feishuSendUUID()
	if replyTo != "" {
		cardUUID = feishuReplyUUID(replyTo, "c", card)
	}
	send := func(typ, content, uuid string) (string, error) {
		id, rerouted, err := h.replyOrSend(ctx, api, chatID, replyTo, typ, content, uuid)
		if rerouted {
			replyTo = "" // a fallback after the detour must go to the chat too, never retry the dead reply
		}
		return id, err
	}
	id, err := send(feishubot.TypeInteractive, card, cardUUID)
	if err != nil && feishubot.CardRejected(err) {
		h.logf("bots: card refused (%v); sending it as rich text", err)
		if fallback == "" {
			fallback = i18n.T(h.botLang(ctx), "hub-bots_chat.card_refused")
		}
		fallbackUUID := feishuSendUUID()
		if replyTo != "" {
			fallbackUUID = feishuReplyUUID(replyTo, "f", fallback)
		}
		return send(feishubot.TypePost, feishubot.PostMarkdown(fallback), fallbackUUID)
	}
	return id, err
}

// uploadBytes saves a file on a machine (its temp directory, like the app's uploads) and returns the path.
func (h *Manager) uploadBytes(ctx context.Context, m *Machine, name string, data []byte) (string, error) {
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

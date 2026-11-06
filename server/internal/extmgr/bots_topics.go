package extmgr

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/channel"
	"github.com/nwverse/persona/server/internal/feishubot"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/rpc"
)

// Topic group chats (#5279, docs/bots.md "Topic group chats"): the bot's chat with the owner is the private one, and
// anyone in a group could write to a bot in it, so a group is talked to only where the owner said so. The owner opts a
// group in (the /topics command in it, or the bot's settings), and from then on, in that group:
//
//   - a message reaches the assistant when five things all hold: the bot is paired, the sender is the owner, the chat
//     is on the list, the message is in a topic (it has a thread_id), and it is not the command below. Any other
//     message is dropped without a word: nobody is told the bot is there, and nothing is said in the group.
//   - the bot may send into the chat (botChatAllowed).
//
// A group message never changes where the owner's pushes go (BotOwner.ChatID), which stays the private chat.

// BotChat is a group chat the owner opted in.
type BotChat struct {
	ChatID string    `json:"chat_id"`
	Name   string    `json:"name,omitempty"` // a label the app may set; the command in the chat leaves it empty
	At     time.Time `json:"at"`
}

type botChatView struct {
	ChatID  string `json:"chat_id"`
	Name    string `json:"name,omitempty"`
	AddedAt int64  `json:"added_at"`
}

// botTopicChatsMax bounds the list: it is the owner's own, so this only stops a runaway client.
const botTopicChatsMax = 50

// topicChat says whether chatID is a group chat the owner opted in.
func (s FeishuBotSettings) topicChat(chatID string) bool {
	return chatID != "" && slices.ContainsFunc(s.TopicChats, func(c BotChat) bool { return c.ChatID == chatID })
}

// checkTopicChats says whether the list a client sent is one the hub keeps: ids that look like a chat id, no repeats.
func checkTopicChats(list []BotChat) error {
	if len(list) > botTopicChatsMax {
		return i18n.Err("hub-bots.topic_chats_many", i18n.Args{"n": botTopicChatsMax})
	}
	seen := map[string]bool{}
	for _, c := range list {
		id := strings.TrimSpace(c.ChatID)
		if id == "" || len(id) > 100 || strings.ContainsFunc(id, unicode.IsSpace) || seen[id] {
			return i18n.Err("hub-bots.topic_chat_bad")
		}
		seen[id] = true
	}
	return nil
}

var errNoTopicOwner = i18n.Err("hub-bots.topic_needs_owner")

// topicChatsPossible says whether a PUT that carries a list can keep it: the bot has an owner now, and the same request
// does not change the App ID (which unpairs the owner, and the list with it).
func (h *Manager) topicChatsPossible(ctx context.Context, appID *string) error {
	b := &h.bots
	b.mu.Lock()
	h.loadBotsLocked(ctx)
	owner, saved := b.set.Owner != nil, b.set.AppID
	b.mu.Unlock()
	if !owner {
		return errNoTopicOwner
	}
	if appID != nil && strings.TrimSpace(*appID) != saved {
		return i18n.Err("hub-bots.topic_chats_new_app")
	}
	return nil
}

// setTopicChats replaces the owner's list (PUT /api/v1/bots/feishu): the chats that stay keep the time they were
// added, new ones get now. A bot with no owner has no list to keep.
func (h *Manager) setTopicChats(ctx context.Context, want []BotChat) error {
	b := &h.bots
	b.mu.Lock()
	h.loadBotsLocked(ctx)
	if b.set.Owner == nil {
		nothing := len(want) == 0 && len(b.set.TopicChats) == 0 // read under the lock
		b.mu.Unlock()
		if nothing {
			return nil
		}
		return errNoTopicOwner
	}
	before := b.set.TopicChats
	next := make([]BotChat, 0, len(want))
	for _, c := range want {
		c.ChatID, c.Name = strings.TrimSpace(c.ChatID), strings.TrimSpace(c.Name)
		c.At = b.clock().UTC()
		if i := slices.IndexFunc(before, func(o BotChat) bool { return o.ChatID == c.ChatID }); i >= 0 {
			c.At = before[i].At
		}
		next = append(next, c)
	}
	records := topicRecords(before, next)
	b.set.TopicChats = next
	if len(records) > 0 || !slices.EqualFunc(before, next, func(a, b BotChat) bool { return a == b }) {
		h.saveBotSettingsLocked()
	}
	b.mu.Unlock()
	h.auditBot(ctx, records...)
	return nil
}

// topicRecords are the audit records for the chats that came and went between two lists.
func topicRecords(before, after []BotChat) []botRecord {
	var out []botRecord
	for _, c := range after {
		if !slices.ContainsFunc(before, func(o BotChat) bool { return o.ChatID == c.ChatID }) {
			out = append(out, botRecord{auditBotTopicChatAdded, map[string]any{"chat_id": c.ChatID}})
		}
	}
	for _, c := range before {
		if !slices.ContainsFunc(after, func(o BotChat) bool { return o.ChatID == c.ChatID }) {
			out = append(out, botRecord{auditBotTopicChatRemoved, map[string]any{"chat_id": c.ChatID}})
		}
	}
	return out
}

// topicsCommand reads the owner's message as the command that opts a group in or out: /topics or /话题群 (a full-width
// slash too), alone or with "on" / "off" (关), after the @ of the bot that a group message may start with. on is
// false for "off". ok is false for any other text, which is then an ordinary message.
func topicsCommand(text string, mentions []feishubot.Mention) (on, ok bool) {
	t := strings.TrimSpace(text)
	for stripped := true; stripped; {
		stripped = false
		for _, m := range mentions {
			for _, tok := range []string{m.Key, "@" + m.Name} {
				if tok != "" && tok != "@" && strings.HasPrefix(t, tok) {
					t, stripped = strings.TrimSpace(strings.TrimPrefix(t, tok)), true
				}
			}
		}
	}
	if after, found := strings.CutPrefix(t, "／"); found { // i18n-ignore: the full-width slash
		t = "/" + after
	}
	word, ok := strings.CutPrefix(t, "/")
	if !ok {
		return false, false
	}
	fields := strings.Fields(word)
	if len(fields) == 0 || len(fields) > 2 {
		return false, false
	}
	switch strings.ToLower(fields[0]) {
	case "topics", "话题群": // i18n-ignore: the command's own name
	default:
		return false, false
	}
	switch arg := strings.ToLower(strings.Join(fields[1:], "")); arg {
	case "", "on", "开": // i18n-ignore: the command's own word
		return true, true
	case "off", "关", "关闭": // i18n-ignore: the command's own words
		return false, true
	}
	return false, false
}

// botGroupMessage takes a message from a group chat (not the bot's private chat): see the five conditions above. Every
// one that fails is a plain return: no note, no reaction, nothing in the chat.
func (h *Manager) botGroupMessage(api *feishubot.Client, set FeishuBotSettings, ev feishubot.MessageEvent, raw json.RawMessage, since time.Time, pass func()) {
	msg := ev.Message
	if set.Owner == nil || ev.Sender.SenderID.OpenID != set.Owner.OpenID || msg.ChatID == "" {
		return // not paired, or not the owner: in a group anyone could write to the owner's Persona
	}
	if msg.MessageType == "text" || msg.MessageType == "post" {
		content := feishubot.ParseContent(msg.MessageType, msg.Content, msg.Mentions)
		if on, ok := topicsCommand(content.Text, msg.Mentions); ok && !content.Forward {
			// The list is changed before the next message is let go: the one right after /topics off must not pass the
			// gate, and the one right after /topics must.
			key, rec := h.applyTopicsCommand(set.Owner.OpenID, msg.ChatID, on)
			pass()
			if key == "" {
				return // unpaired meanwhile
			}
			if rec != nil {
				h.audit(context.Background(), channel.Feishu, rec.action, botChatActor(set.Owner.OpenID), rec.detail)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			h.botNoteIn(ctx, api, msg.ChatID, msg.MessageID, i18n.T(h.userLang(), key), msg.ThreadID != "")
			return // the hub's own: it never goes to the assistant
		}
	}
	if !set.topicChat(msg.ChatID) || msg.ThreadID == "" {
		return
	}
	// No action: a message in a topic answers no card (the assistant does not look for one), so none is issued.
	h.handToAssistant(api, ev, raw, "", since, pass, true)
}

// applyTopicsCommand opts the group chat in or out for the owner (whose open id it is, checked by the caller) and
// returns the key of the line that says what came of it, with the audit record when the list changed; "" when the
// owner is gone or another by now.
func (h *Manager) applyTopicsCommand(ownerID, chatID string, on bool) (key string, rec *botRecord) {
	b := &h.bots
	b.mu.Lock()
	defer b.mu.Unlock()
	cur := &b.set
	if cur.Owner == nil || cur.Owner.OpenID != ownerID {
		return "", nil
	}
	has := cur.topicChat(chatID)
	switch {
	case on && has:
		return "hub-bots_chat.topics_already", nil
	case on && len(cur.TopicChats) >= botTopicChatsMax:
		return "hub-bots_chat.topics_full", nil
	case on:
		cur.TopicChats = append(slices.Clone(cur.TopicChats), BotChat{ChatID: chatID, At: b.clock().UTC()})
		h.saveBotSettingsLocked()
		return "hub-bots_chat.topics_on", &botRecord{auditBotTopicChatAdded, map[string]any{"chat_id": chatID}}
	case has:
		cur.TopicChats = slices.DeleteFunc(slices.Clone(cur.TopicChats), func(c BotChat) bool { return c.ChatID == chatID })
		h.saveBotSettingsLocked()
		return "hub-bots_chat.topics_off", &botRecord{auditBotTopicChatRemoved, map[string]any{"chat_id": chatID}}
	}
	return "hub-bots_chat.topics_not_on", nil
}

// ---- which chat a message is in ----

// botMsgChatKeep is how many message ids the hub remembers the chat of.
const botMsgChatKeep = 500

// idRing is a bounded set of message ids with what is known of each, oldest dropped first. Held under botHub.mu.
type idRing struct {
	m     map[string]string
	order []string
}

func (r *idRing) put(id, v string) {
	if r.m == nil {
		r.m = map[string]string{}
	}
	if _, ok := r.m[id]; !ok {
		r.order = append(r.order, id)
		if len(r.order) > botMsgChatKeep {
			delete(r.m, r.order[0])
			r.order = r.order[1:]
		}
	}
	r.m[id] = v
}

// rememberMessageChat notes the chat a message the bot may use is in (the owner's own messages in the DM and in a
// listed group, and what the bot itself sent there), for checkReplyParent. Messages of anyone else, and of chats the
// bot may not use, are not kept: they would only push out the ones a reply goes to.
func (h *Manager) rememberMessageChat(id, chatID string) {
	if id == "" || chatID == "" {
		return
	}
	b := &h.bots
	b.mu.Lock()
	defer b.mu.Unlock()
	b.msgChat.put(id, chatID)
}

func (h *Manager) messageChat(id string) string {
	b := &h.bots
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.msgChat.m[id]
}

// rememberHandedGroup notes a group message handed to the assistant: only a recall of one of these is passed on.
func (h *Manager) rememberHandedGroup(id string) {
	b := &h.bots
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handedGroup.put(id, "")
}

func (h *Manager) handedGroupMessage(id string) bool {
	b := &h.bots
	b.mu.Lock()
	defer b.mu.Unlock()
	_, ok := b.handedGroup.m[id]
	return ok
}

var errBotReplyChat = &rpc.Error{Code: assistantwire.CodeChatDenied, Message: "feishu: a reply goes into the chat of the message it answers, and that chat is not one the bot may send into"}

// checkReplyParent refuses a reply whose parent message is in a chat the bot may not send into: a reply lands in the
// parent's chat whatever chat_id says. It returns that chat. The chat is the one remembered for the message, else the
// open platform's answer (read again by the client when it is busy or the network blips; then remembered, if it is a
// chat the bot may use). A parent that is withdrawn or deleted cannot be replied to at all, so the send falls back to
// the chat the call names (replyOrSend), which botSendChat checked: the chat returned is "" then. A chat that cannot be
// told is refused. With no parent there is nothing to check.
func (h *Manager) checkReplyParent(ctx context.Context, api *feishubot.Client, set FeishuBotSettings, replyTo string) (string, error) {
	if replyTo == "" {
		return "", nil
	}
	chat := h.messageChat(replyTo)
	if chat == "" {
		items, err := api.Message(ctx, replyTo)
		if feishubot.MessageGone(err) {
			return "", nil
		}
		if err != nil {
			return "", fmt.Errorf("feishu: could not tell which chat message %s is in: %w", replyTo, err)
		}
		for _, it := range items {
			if it.MessageID == replyTo {
				chat = it.ChatID
			}
		}
		if chat == "" {
			return "", fmt.Errorf("feishu: could not tell which chat message %s is in", replyTo)
		}
		if botChatAllowed(set, chat) {
			h.rememberMessageChat(replyTo, chat)
		}
	}
	if !botChatAllowed(set, chat) {
		return "", errBotReplyChat
	}
	return chat, nil
}

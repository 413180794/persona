package assistant

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/channel"
	"github.com/nwverse/persona/server/internal/feishubot"
	"github.com/nwverse/persona/server/internal/i18n"
)

// Asking from any group (#6146, docs/bots.md "Asking from any group", docs/todos.md "Where it came from"). People who
// work with many groups do not want an AI conversation in each: the owner writes "@bot /ai <what>" where the question
// comes up, the work happens as a topic in the home topic group, and the outcome comes back as a reply to that message.
//
// openFromElsewhere is the one way in: it takes a Source (the chat and message a request was made in, its words, the
// message it replied to) and does not care who received it. The bot's gate (feishuElsewhere, below) is the first caller;
// a channel that sees the owner's own chats as the owner (an extension, where the bot is not a member) is the next, and
// hands the same Source over the bus when the bus method for it exists. How the outcome goes back is the Source's
// channel (sendSourceReport in todo_source.go).

// Source is where a request to open a to-do came from. Channel is a channel id (internal/channel: "feishu"); ChatID and
// MessageID are the chat and the message of the request in that channel, which the report back is a reply to. Text is the
// words (the command taken off); Quote the text of the message it replied to, "" for none (only text: nothing is
// expanded, and it is clipped here).
type Source struct {
	Channel   string
	ChatID    string
	MessageID string
	Text      string
	Quote     string
}

var (
	// errNoHome: no group chat is on as the home topic group, so there is nowhere to open the topic.
	errNoHome = errors.New("no home topic group")
	// errNoWords: the request has no words to name a to-do by.
	errNoWords = errors.New("no words")
)

const (
	// elsewhereQuoteMax and elsewhereRootMax bound the quoted message and the topic's first message, in runes.
	elsewhereQuoteMax = 300
	elsewhereRootMax  = 1500
	// topicLookupTries and topicLookupWait: a message just sent into the home group may not show its topic at once.
	topicLookupTries = 4
	topicLookupWait  = 700 * time.Millisecond
	// noHomeKeep bounds the group chats remembered as told there is no home group.
	noHomeKeep = 200
)

// openFromElsewhere opens a new topic in the home topic group for the request in src, then does what a topic the owner
// started does (openTodoIn): a to-do made of the words, bound to the topic, handed to the main conversation, with src
// recorded on it so the outcome can be told there. The error says why not; errNoHome and errNoWords are the ones the
// caller answers in its own words, every other error's text is fit to show.
func (s *Service) openFromElsewhere(ctx context.Context, src Source) (Todo, error) {
	l := s.botLang(ctx)
	if !slices.Contains(sourceChannels, src.Channel) || src.ChatID == "" || src.MessageID == "" {
		return Todo{}, errors.New(i18n.T(l, "assistant-feishu.elsewhere_channel", i18n.Args{"channel": src.Channel}))
	}
	text := strings.TrimSpace(src.Text)
	if text == "" {
		return Todo{}, errNoWords
	}
	if err := s.ensureTodosLoaded(ctx); err != nil {
		return Todo{}, err
	}
	st, err := s.feishuStatus(ctx)
	if err != nil {
		return Todo{}, err
	}
	if !st.Ready || st.Owner == "" {
		return Todo{}, errors.New(i18n.T(l, "assistant-feishu.elsewhere_not_ready"))
	}
	home := st.HomeChat
	if home == "" || !slices.Contains(st.Chats, home) {
		return Todo{}, errNoHome
	}
	if _, _, why := s.openTarget(ctx, st); why != "" {
		return Todo{}, errors.New(why) // before anything is posted: no stray topic when there is no one to take it
	}

	quote := clip(oneLine(src.Quote), elsewhereQuoteMax)
	root := []string{i18n.T(l, "assistant-feishu.elsewhere_root", i18n.Args{"text": clip(text, elsewhereRootMax)})}
	if quote != "" {
		root = append(root, i18n.T(l, "assistant-feishu.elsewhere_root_quote", i18n.Args{"quote": quote}))
	}
	root = append(root, i18n.T(l, "assistant-feishu.elsewhere_root_hint"))
	rootText := strings.Join(root, "\n")

	var posted struct {
		MessageID string `json:"message_id"`
	}
	// The key makes the same request, made again, land once.
	err = s.appCall(ctx, AppFeishu, FeishuText, map[string]any{"chat_id": home, "text": rootText, "uuid": rootKey(src)}, &posted)
	if err != nil || posted.MessageID == "" {
		if err == nil {
			err = errors.New("no message id")
		}
		return Todo{}, errors.New(i18n.T(l, "assistant-feishu.thread_open_failed", i18n.Args{"error": clip(oneLine(err.Error()), 200)}))
	}
	thread, err := s.topicOfMessage(ctx, posted.MessageID)
	if err != nil {
		s.logf("bots: elsewhere: topic of %s: %v", posted.MessageID, err)
		return Todo{}, errors.New(i18n.T(l, "assistant-feishu.elsewhere_no_topic"))
	}
	// Any message of the topic will do for the bot's lines into it (feishu_thread_posts.go).
	s.rememberTopicMessage(ctx, home, thread, posted.MessageID)
	topic := &feishuTopic{Chat: home, Thread: thread}

	// The message the topic opened with is the bot's own; the run that answers goes under it, into the topic.
	var ev feishubot.MessageEvent
	ev.Message.MessageID, ev.Message.ChatID, ev.Message.ChatType, ev.Message.ThreadID = posted.MessageID, home, "group", thread
	ev.Message.MessageType = "text"
	c, _ := json.Marshal(map[string]string{"text": rootText})
	ev.Message.Content = string(c)
	ev.Sender.SenderType, ev.Sender.SenderID.OpenID = "app", ""
	todo, fail := s.openTodoIn(ctx, ev, st, feishubot.Content{Text: text}, text, quote, topic,
		&TodoSource{Channel: src.Channel, ChatID: src.ChatID, MessageID: src.MessageID})
	if fail != "" {
		s.botNoteTo(ctx, topic.target(), posted.MessageID, fail) // the topic was opened: it says why nothing came of it
		return Todo{}, errors.New(fail)
	}
	return todo, nil
}

// rootKey is the idempotency key of the first message of the topic a request opens.
func rootKey(src Source) string {
	sum := sha256.Sum256([]byte("root\x00" + src.Channel + "\x00" + src.ChatID + "\x00" + src.MessageID))
	return "ae:" + hex.EncodeToString(sum[:])[:32]
}

// topicOfMessage is the topic a message of the bot's in the home group opened: its thread_id, read back from the message
// (the send answers only with its id). A message may not show its topic the moment it is sent, so it is read a few times.
func (s *Service) topicOfMessage(ctx context.Context, id string) (string, error) {
	wait := s.fbot.lookupWait
	if wait <= 0 {
		wait = topicLookupWait
	}
	var last error
	for i := 0; i < topicLookupTries; i++ {
		if i > 0 {
			select {
			case <-time.After(wait):
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}
		items, err := s.botMessages(ctx, id)
		if err != nil {
			last = err
			continue
		}
		for _, it := range items {
			if it.MessageID == id && it.ThreadID != "" {
				return it.ThreadID, nil
			}
		}
		last = errors.New("the message is in no topic")
	}
	return "", last
}

// feishuElsewhere takes the owner's "@bot /ai …" from a group chat that is not opted in (FeishuFromElsewhere): the hub
// handed it over after its own checks, and this is the second line of them. It opens the request in the home topic group
// (openFromElsewhere) and puts an emoji on the message so the owner sees it was taken; what keeps it from opening is
// answered under the message, in that group, and nowhere else. Anything that is not the owner's "/ai" is dropped.
func (s *Service) feishuElsewhere(ev feishubot.MessageEvent, st FeishuStatus) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	msg := ev.Message
	if ev.Sender.SenderType != "user" || st.Owner == "" || ev.Sender.SenderID.OpenID != st.Owner {
		s.logf("bots: an /ai from a group chat that was not the owner's was dropped")
		return
	}
	// A group on the list has its own road (threadMessage); a private chat is not "elsewhere".
	if msg.ChatType == "p2p" || msg.ChatID == "" || msg.MessageID == "" || slices.Contains(st.Chats, msg.ChatID) {
		s.logf("bots: an /ai marked as from elsewhere was from a chat that is not: dropped")
		return
	}
	if msg.MessageType != "text" && msg.MessageType != "post" {
		return
	}
	content := feishubot.ParseContent(msg.MessageType, msg.Content, msg.Mentions)
	if content.Forward {
		return
	}
	rest, asked := aiCommand(openingWords(ev, content))
	if !asked {
		s.logf("bots: a message marked as an /ai from elsewhere was not one: dropped")
		return
	}
	l := s.botLang(ctx)
	where := feishuTarget{ChatID: msg.ChatID} // a reply under the message: the only thing the hub lets the bot write there
	say := func(key string, args ...i18n.Args) {
		s.botNoteTo(ctx, where, msg.MessageID, i18n.T(l, "assistant-feishu."+key, args...))
	}
	src := Source{Channel: channel.Feishu, ChatID: msg.ChatID, MessageID: msg.MessageID, Text: rest}
	if msg.ParentID != "" {
		src.Quote = s.elsewhereQuote(ctx, msg.ParentID)
	}
	_, err := s.openFromElsewhere(ctx, src)
	switch {
	case errors.Is(err, errNoHome):
		if s.noHomeHintDue(ctx, msg.ChatID) {
			say("elsewhere_no_home")
		}
		return
	case errors.Is(err, errNoWords):
		say("elsewhere_needs_text")
		return
	case err != nil:
		s.logf("bots: elsewhere: %v", err)
		say("elsewhere_failed", i18n.Args{"error": clip(oneLine(err.Error()), 200)})
		return
	}
	if _, err := s.botReact(ctx, msg.MessageID, "OnIt"); err != nil {
		s.logf("bots: elsewhere: react: %v", err)
	}
}

// elsewhereQuote is the text of the message a request replied to, to go with it as context: only a text or rich-text
// message has any (a merged forward is not expanded, and a picture, a card or the like is no text), clipped. "" when
// there is none or it cannot be read.
func (s *Service) elsewhereQuote(ctx context.Context, id string) string {
	items, err := s.botMessages(ctx, id)
	if err != nil || len(items) == 0 {
		return ""
	}
	it := items[0]
	if it.MessageID != id || it.Deleted || it.MsgType != "text" && it.MsgType != "post" {
		return ""
	}
	c := feishubot.ParseContent(it.MsgType, it.Body.Content, nil)
	if c.Forward {
		return ""
	}
	return clip(oneLine(c.Text), elsewhereQuoteMax)
}

// noHomeHintDue says whether the group chat is to be told, now, that there is no home topic group: once ever (kept with
// the chat's state, so a restart does not tell it again).
func (s *Service) noHomeHintDue(ctx context.Context, chatID string) bool {
	b := &s.fbot
	b.mu.Lock()
	defer b.mu.Unlock()
	s.loadBotLocked(ctx)
	if !b.loaded {
		return false // the state cannot be read: better silent than told again and again
	}
	if _, ok := b.st.NoHome[chatID]; ok {
		return false
	}
	if b.st.NoHome == nil {
		b.st.NoHome = map[string]time.Time{}
	}
	b.st.NoHome[chatID] = b.clock()
	for len(b.st.NoHome) > noHomeKeep { // the oldest are told again if they ask again
		oldest := ""
		for k, at := range b.st.NoHome {
			if oldest == "" || at.Before(b.st.NoHome[oldest]) {
				oldest = k
			}
		}
		delete(b.st.NoHome, oldest)
	}
	s.saveBotStateLocked()
	return true
}

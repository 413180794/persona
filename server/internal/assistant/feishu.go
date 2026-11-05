package assistant

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/bus"
	"github.com/nwverse/persona/server/internal/channel"
	"github.com/nwverse/persona/server/internal/feishubot"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// The Feishu bot's chat (docs/bots.md): the owner talks to Persona in the bot's private chat — the main conversation,
// another one picked with /new and /list, or an agent picked with /agents — and the answer comes back as cards;
// what is asked shows as cards there too. The hub keeps the bot's transport (its credentials, the long connection,
// pairing, the open platform's REST): it hands the owner's messages and presses here (feishu.message,
// feishu.card_action), and posts what is given it (app.call feishu.*). The bot's switches (bots.feishu features) are
// the hub's, kept with its credentials; the chat's state (bots.feishu.state) is the assistant's.

// botStateKey is the chat's state: where messages go, the threads bound to targets, the messages already taken.
const botStateKey = "bots.feishu.state"

// botTarget is where a message goes: the main conversation (whichever it is when the message is sent), another
// Persona conversation, or a coding agent.
type botTarget struct {
	Kind      string `json:"kind"` // main | jarvis | agent
	MachineID string `json:"machine_id,omitempty"`
	SessionID string `json:"session_id,omitempty"`
}

const (
	targetMain   = "main"
	targetJarvis = "jarvis"
	targetAgent  = "agent"
)

func (t botTarget) agent() bool { return t.Kind == targetAgent }

// botThread is a message the bot posted for a target (an agent picked with /agents): replies to it, and messages in its
// thread, go to that target.
type botThread struct {
	Target botTarget `json:"target"`
	Title  string    `json:"title,omitempty"`
	At     time.Time `json:"at"`
}

// botState is the chat's state (botStateKey).
type botState struct {
	Current botTarget             `json:"current"`
	Threads map[string]*botThread `json:"threads,omitempty"`
	// Runs are an older hub's turns being answered: moved to the channels' runs (takeLegacyBotRuns).
	Runs []*botRun `json:"runs,omitempty"`
	Seen []string  `json:"seen,omitempty"`
	// Pairing is the pairing the state belongs to (the owner and when they paired, feishu.status; "" not paired; nil:
	// not recorded yet, a state from before). Another one, or none, starts the chat afresh (feishuReconcile).
	Pairing *string `json:"pairing,omitempty"`
}

const (
	botSeenKeep    = 300
	botThreadsKeep = 100
)

// feishuBook is the chat's state in memory, the cards asking what is asked, and the bot's status a moment.
type feishuBook struct {
	mu       sync.Mutex
	loaded   bool
	st       botState
	cards    map[string]botCard // interaction id -> the card asking it
	status   *FeishuStatus
	statusAt time.Time
	now      func() time.Time // tests
	cotOff   bool             // Feishu refused an agent message (feishu_cot.go)
}

type botCard struct {
	MessageID string
	Ix        proto.Interaction // what it asks (to close it when it ended unseen)
}

func (b *feishuBook) clock() time.Time {
	if b.now != nil {
		return b.now()
	}
	return time.Now()
}

// loadBotLocked reads the chat's state once; b.mu is held.
func (s *Service) loadBotLocked(ctx context.Context) {
	b := &s.fbot
	if b.loaded {
		return
	}
	if st := s.store(); st != nil {
		if _, err := st.Setting(ctx, botStateKey, &b.st); err != nil {
			s.logf("bots: load state: %v", err)
			return // the next look tries again
		}
	}
	b.loaded = true
	if b.st.Threads == nil {
		b.st.Threads = map[string]*botThread{}
	}
	if b.st.Current.Kind == "" {
		b.st.Current = botTarget{Kind: targetMain}
	}
}

// saveBotStateLocked writes the chat's state, once it was read: what is in memory after a failed read would be
// written over the stored target, threads and pairing.
func (s *Service) saveBotStateLocked() {
	b := &s.fbot
	if !b.loaded {
		return
	}
	if len(b.st.Seen) > botSeenKeep {
		b.st.Seen = b.st.Seen[len(b.st.Seen)-botSeenKeep:]
	}
	for len(b.st.Threads) > botThreadsKeep {
		oldest := ""
		for id, t := range b.st.Threads {
			if oldest == "" || t.At.Before(b.st.Threads[oldest].At) {
				oldest = id
			}
		}
		delete(b.st.Threads, oldest)
	}
	st := s.store()
	if st == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := st.PutSetting(ctx, botStateKey, b.st); err != nil {
		s.logf("bots: save state: %v", err)
	}
}

// feishuStatus is the bot as the hub has it now (feishu.status): on and paired, its switches.
func (s *Service) feishuStatus(ctx context.Context) (FeishuStatus, error) {
	var st FeishuStatus
	if err := s.appCall(ctx, AppFeishu, FeishuStatusGet, map[string]any{}, &st); err != nil {
		return FeishuStatus{}, err
	}
	b := &s.fbot
	b.mu.Lock()
	b.status, b.statusAt = &st, time.Now()
	b.mu.Unlock()
	s.feishuReconcile(st)
	return st, nil
}

// feishuReconcile starts the chat afresh when the bot was paired with someone else, paired again or unpaired since
// the state was written (the hub's feishu.owner may have come while the assistant was away): back to the main
// conversation, no threads, no cards, nothing waiting. A state from before pairings were recorded takes the one now.
func (s *Service) feishuReconcile(st FeishuStatus) {
	key := ""
	if st.Owner != "" {
		key = st.Owner + "@" + strconv.FormatInt(st.PairedAt.Unix(), 10)
	}
	b := &s.fbot
	b.mu.Lock()
	s.loadBotLocked(context.Background())
	if !b.loaded || b.st.Pairing != nil && *b.st.Pairing == key {
		b.mu.Unlock()
		return
	}
	fresh := b.st.Pairing != nil // nil: recorded for the first time, the state stays
	if fresh {
		b.st = botState{Current: botTarget{Kind: targetMain}, Threads: map[string]*botThread{}, Seen: b.st.Seen}
		b.cards = nil
	}
	b.st.Pairing = &key
	s.saveBotStateLocked()
	b.mu.Unlock()
	if fresh {
		s.logf("bots: the bot's pairing changed: the chat starts afresh")
		s.forgetChannelTurns(channel.Feishu)
	}
}

// feishuStatusKept is feishuStatus kept a moment: for what looks every second (a run's sink, the cards' links).
func (s *Service) feishuStatusKept(ctx context.Context) (FeishuStatus, error) {
	b := &s.fbot
	b.mu.Lock()
	if b.status != nil && time.Since(b.statusAt) < 3*time.Second {
		st := *b.status
		b.mu.Unlock()
		return st, nil
	}
	b.mu.Unlock()
	return s.feishuStatus(ctx)
}

func (s *Service) feishuForgetStatus() {
	s.fbot.mu.Lock()
	s.fbot.status = nil
	s.fbot.mu.Unlock()
}

func (s *Service) feishuFeatures(ctx context.Context) (FeishuFeatures, bool) {
	st, err := s.feishuStatus(ctx)
	return st.Features, err == nil && st.Ready
}

// ---- the hub's transport ----

// botPost sends a card: as a reply to replyTo, else into the owner's chat; one Feishu refuses goes as rich text
// (fallback).
func (s *Service) botPost(ctx context.Context, replyTo string, card *feishubot.Card, fallback string) (string, error) {
	var res struct {
		MessageID string `json:"message_id"`
	}
	err := s.appCall(ctx, AppFeishu, FeishuPost, map[string]any{"reply_to": replyTo, "card": card.JSON(), "fallback": fallback}, &res)
	return res.MessageID, err
}

// botNote replies to a message with a line of text.
func (s *Service) botNote(ctx context.Context, messageID, text string) {
	if err := s.appCall(ctx, AppFeishu, FeishuText, map[string]any{"reply_to": messageID, "text": text}, nil); err != nil {
		s.logf("bots: note: %v", err)
	}
}

// botSay sends a line of text into the owner's chat.
func (s *Service) botSay(ctx context.Context, text string) {
	if err := s.appCall(ctx, AppFeishu, FeishuText, map[string]any{"text": text}, nil); err != nil {
		s.logf("bots: say: %v", err)
	}
}

func (s *Service) botUpdateCard(ctx context.Context, messageID string, card *feishubot.Card) error {
	return s.appCall(ctx, AppFeishu, FeishuUpdateCard, map[string]any{"message_id": messageID, "card": card.JSON()}, nil)
}

func (s *Service) botReact(ctx context.Context, messageID, emoji string) (string, error) {
	var res struct {
		ReactionID string `json:"reaction_id"`
	}
	err := s.appCall(ctx, AppFeishu, FeishuReact, map[string]any{"message_id": messageID, "emoji": emoji}, &res)
	return res.ReactionID, err
}

func (s *Service) botUnreact(ctx context.Context, messageID, reactionID string) {
	_ = s.appCall(ctx, AppFeishu, FeishuUnreact, map[string]any{"message_id": messageID, "reaction_id": reactionID}, nil)
}

func (s *Service) botMessages(ctx context.Context, messageID string) ([]feishubot.Item, error) {
	var res struct {
		Items []feishubot.Item `json:"items"`
	}
	err := s.appCall(ctx, AppFeishu, FeishuMessages, map[string]any{"message_id": messageID}, &res)
	return res.Items, err
}

// botName is a user's name (the hub's contact lookup; 「某人」 when it can't be read).
func (s *Service) botName(ctx context.Context, openID string) string {
	var res struct {
		Name string `json:"name"`
	}
	if err := s.appCall(ctx, AppFeishu, FeishuUserName, map[string]any{"open_id": openID}, &res); err != nil || res.Name == "" {
		return "某人"
	}
	return res.Name
}

// ---- the chat's state ----

// claimBotMessage: the first time this message is taken (Feishu delivers again what it thinks was missed); an error
// while the chat's state can't be read.
func (s *Service) claimBotMessage(id string) (bool, error) {
	b := &s.fbot
	b.mu.Lock()
	defer b.mu.Unlock()
	s.loadBotLocked(context.Background())
	if !b.loaded {
		return false, errBotUnread
	}
	if id == "" || containsString(b.st.Seen, id) {
		return false, nil
	}
	b.st.Seen = append(b.st.Seen, id)
	s.saveBotStateLocked()
	return true, nil
}

var errBotUnread = errors.New("the chat's state could not be read")

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func (s *Service) setBotCurrent(t botTarget) {
	b := &s.fbot
	b.mu.Lock()
	s.loadBotLocked(context.Background())
	b.st.Current = t
	s.saveBotStateLocked()
	b.mu.Unlock()
}

// botTargetFor is where a message goes: the target of the bot message it replies to (or of its thread), else the
// chat's current one.
func (s *Service) botTargetFor(root, parent string) botTarget {
	b := &s.fbot
	b.mu.Lock()
	defer b.mu.Unlock()
	s.loadBotLocked(context.Background())
	for _, id := range []string{parent, root} {
		if t := b.st.Threads[id]; id != "" && t != nil {
			return t.Target
		}
	}
	return b.st.Current
}

// rememberBotThread binds a message the bot posted to a target: replying to it talks to that target.
func (s *Service) rememberBotThread(messageID string, t botTarget, title string) {
	if messageID == "" || t.Kind == "" {
		return
	}
	b := &s.fbot
	b.mu.Lock()
	defer b.mu.Unlock()
	s.loadBotLocked(context.Background())
	if !b.loaded {
		return // its threads could not be read: nothing to add to
	}
	b.st.Threads[messageID] = &botThread{Target: t, Title: title, At: b.clock().UTC()}
	s.saveBotStateLocked()
}

// feishuReset starts the chat afresh (unpaired from the channels' page): back to the main conversation, no threads,
// no cards, nothing waiting; the pairing it belongs to is read again.
func (s *Service) feishuReset() {
	b := &s.fbot
	b.mu.Lock()
	s.loadBotLocked(context.Background())
	pairing := b.st.Pairing
	b.st = botState{Current: botTarget{Kind: targetMain}, Threads: map[string]*botThread{}, Seen: b.st.Seen, Pairing: pairing}
	b.cards = nil
	b.status = nil
	s.saveBotStateLocked()
	b.mu.Unlock()
}

// feishuUnpaired: the owner went: the chat's state and what it waited on go.
func (s *Service) feishuUnpaired() {
	s.feishuReset()
	s.forgetChannelTurns(channel.Feishu)
}

// FeishuOwner is feishu.owner: the bot was paired (it says hello, with what it can do) or unpaired. The pairing now
// is read from the hub (feishuReconcile starts the chat afresh), the same as on any later look when this event was
// missed.
func (s *Service) FeishuOwner(_ context.Context, p FeishuOwner) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		st, err := s.feishuStatus(ctx)
		if err != nil {
			s.feishuUnpaired() // the hub could not be asked: start afresh anyway
		}
		if !p.Paired {
			return
		}
		l := s.botLang(ctx)
		card := feishubot.NewCard(i18n.T(l, "assistant-feishu.paired"), feishubot.Green, i18n.T(l, "assistant-feishu.paired")).Markdown(i18n.T(l, "assistant-feishu.paired_body") + botHelp(l, st.Features))
		if _, err := s.botPost(ctx, "", card, ""); err != nil {
			s.logf("bots: paired note: %v", err)
		}
	}()
}

// takeLegacyBotRuns are the runs an older hub kept in the chat's state, taken out of it (once).
func (s *Service) takeLegacyBotRuns(ctx context.Context) []*channel.Run {
	b := &s.fbot
	b.mu.Lock()
	defer b.mu.Unlock()
	s.loadBotLocked(ctx)
	old := b.st.Runs
	if len(old) == 0 {
		return nil
	}
	b.st.Runs = nil
	s.saveBotStateLocked()
	var out []*channel.Run
	for _, r := range old {
		if r != nil && r.Target.SessionID != "" {
			out = append(out, r.channelRun())
		}
	}
	return out
}

// botRun is a run as bots.feishu.state kept it before the channels' pipeline: read once and moved over.
type botRun struct {
	ID        string      `json:"id"`
	MessageID string      `json:"message_id"`
	Target    botTarget   `json:"target"`
	Label     string      `json:"label"`
	Base      int         `json:"base"`
	StartedAt time.Time   `json:"started_at"`
	Card      string      `json:"card,omitempty"`
	Reaction  string      `json:"reaction,omitempty"`
	Seen      bool        `json:"seen,omitempty"`
	Held      bool        `json:"held,omitempty"`
	Probe     string      `json:"probe,omitempty"`
	Folded    []botFolded `json:"folded,omitempty"`
}

type botFolded struct {
	MessageID string `json:"message_id"`
	Reaction  string `json:"reaction,omitempty"`
}

// channelRun is a legacy run as the pipeline's.
func (r *botRun) channelRun() *channel.Run {
	out := &channel.Run{ID: r.ID, Channel: channel.Feishu, ReplyTo: r.MessageID, MachineID: r.Target.MachineID, SessionID: r.Target.SessionID,
		TargetKind: r.Target.Kind, Label: r.Label, Base: r.Base, Probe: r.Probe, Started: r.StartedAt, Seen: r.Seen, Held: r.Held,
		Ext: map[string]string{}}
	if r.MessageID != "" && !r.Held {
		out.MsgIDs = []string{channel.ClientMsgID(channel.Feishu, r.MessageID)}
	}
	if r.Card != "" {
		out.Ext["card"] = r.Card
	}
	if r.Reaction != "" {
		out.Ext["reaction"] = r.Reaction
	}
	for _, f := range r.Folded {
		fo := channel.Folded{ReplyTo: f.MessageID}
		if f.Reaction != "" {
			fo.Ext = map[string]string{"reaction": f.Reaction}
		}
		out.Folded = append(out.Folded, fo)
	}
	return out
}

// ---- what the hub shows of the chat ----

// FeishuView is feishu.view: where the chat's messages go now, and the agents bound to its threads.
func (s *Service) FeishuView(ctx context.Context) FeishuView {
	b := &s.fbot
	b.mu.Lock()
	s.loadBotLocked(ctx)
	cur := b.st.Current
	var v FeishuView
	for _, t := range b.st.Threads {
		if t.Target.agent() {
			v.Agents = append(v.Agents, FeishuAgent{Title: t.Title, MachineID: t.Target.MachineID, SessionID: t.Target.SessionID})
		}
	}
	b.mu.Unlock()
	v.Target = s.targetLabel(ctx, cur)
	return v
}

// feishuChannelTarget is the Feishu channel's target as the channels' page shows it.
func (s *Service) feishuChannelTarget(ctx context.Context) channel.Target {
	b := &s.fbot
	b.mu.Lock()
	s.loadBotLocked(ctx)
	cur := b.st.Current
	b.mu.Unlock()
	t := channel.Target{Kind: channel.TargetMain}
	switch cur.Kind {
	case targetJarvis:
		t = channel.Target{Kind: channel.TargetConversation, MachineID: cur.MachineID, SessionID: cur.SessionID}
		if cur.SessionID == "" {
			t = channel.Target{Kind: channel.TargetNew}
		}
	case targetAgent:
		t = channel.Target{Kind: channel.TargetAgent, MachineID: cur.MachineID, SessionID: cur.SessionID}
	}
	if t.Kind != channel.TargetMain {
		t.Title = s.targetLabel(ctx, cur)
	}
	return t
}

// updateFeishuChannel maps the channel's settings onto the bot's: its switch and features are the hub's
// (feishu.configure), where its messages go is the chat's.
func (s *Service) updateFeishuChannel(ctx context.Context, enabled *bool, target *channel.Target, features map[string]bool) error {
	if enabled != nil || len(features) > 0 {
		p := map[string]any{}
		if enabled != nil {
			p["enabled"] = *enabled
		}
		if len(features) > 0 {
			p["features"] = features
		}
		if err := s.appCall(ctx, AppFeishu, FeishuConfigure, p, nil); err != nil {
			return err
		}
		s.feishuForgetStatus()
	}
	if target != nil {
		t := botTarget{Kind: targetMain}
		switch target.Kind {
		case channel.TargetNew:
			t = botTarget{Kind: targetJarvis}
		case channel.TargetConversation:
			t = botTarget{Kind: targetJarvis, MachineID: target.MachineID, SessionID: target.SessionID}
		}
		s.setBotCurrent(t)
	}
	return nil
}

func (s *Service) feishuCardsWaiting() int {
	b := &s.fbot
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.cards)
}

// ---- a message to the bot ----

// FeishuMessage is feishu.message: the owner's message in the bot's private chat (the hub checked who it is from),
// taken once and handled in the background.
func (s *Service) FeishuMessage(ctx context.Context, p FeishuMessage) error {
	var ev feishubot.MessageEvent
	if err := json.Unmarshal(p.Event, &ev); err != nil {
		return badRequest(err)
	}
	// What it is taken with first: a message taken is always answered, never dropped for want of it.
	st, err := s.feishuStatus(ctx)
	if err != nil {
		return &rpc.Error{Code: CodeTryAgain, Message: "the bot's status: " + err.Error()}
	}
	first, err := s.claimBotMessage(ev.Message.MessageID)
	if err != nil {
		return &rpc.Error{Code: CodeTryAgain, Message: err.Error()}
	}
	if !first {
		return nil // redelivered, or handed over again after a call that was cut off
	}
	go s.feishuMessage(ev, st)
	return nil
}

func (s *Service) feishuMessage(ev feishubot.MessageEvent, st FeishuStatus) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	msg := ev.Message
	content := feishubot.ParseContent(msg.MessageType, msg.Content, msg.Mentions)
	if strings.HasPrefix(content.Text, "/") && !content.Forward && len(content.Media) == 0 {
		handled, rest := s.botCommand(ctx, st.Features, msg.MessageID, content.Text, msg.RootID, msg.ParentID)
		if handled {
			return
		}
		content.Text = rest
	}
	if !content.Forward && s.acceptReply(ctx, msg.MessageID, content.Text, msg.ParentID, msg.RootID) {
		return // about a 请验收 card (acceptance.go)
	}
	target := s.botTargetFor(msg.RootID, msg.ParentID)
	if err := botAllowed(s.botLang(ctx), st.Features, target); err != nil {
		s.botNote(ctx, msg.MessageID, err.Error())
		return
	}
	if err := s.botSend(ctx, &ev, content, target); err != nil {
		s.logf("bots: send: %v", err)
		s.botNote(ctx, msg.MessageID, i18n.T(s.botLang(ctx), "assistant-feishu.not_sent", i18n.Args{"error": clip(oneLine(err.Error()), 200)}))
	}
}

// botAllowed says, in l, why the chat may not send to t now (nil: it may).
func botAllowed(l i18n.Lang, f FeishuFeatures, t botTarget) error {
	switch {
	case t.agent() && !f.Agents:
		return errors.New(i18n.T(l, "assistant-feishu.agents_off"))
	case !t.agent() && !f.Main:
		if f.Agents {
			return errors.New(i18n.T(l, "assistant-feishu.main_off_agents_on"))
		}
		return errors.New(i18n.T(l, "assistant-feishu.chat_off"))
	}
	return nil
}

// resolvedTarget is a target as it is now: the machine and conversation, and what to call it.
type resolvedTarget struct {
	machine     string
	machineName string
	session     proto.Session
	label       string
	main        bool
	isNew       bool // no conversation yet: the message starts one
}

func (s *Service) resolveBotTarget(ctx context.Context, t botTarget) (resolvedTarget, error) {
	l := s.botLang(ctx)
	switch t.Kind {
	case targetAgent:
		d, sess, err := s.findAgent(ctx, t.MachineID, t.SessionID)
		if err != nil {
			return resolvedTarget{}, errors.New(i18n.T(l, "assistant-feishu.agent_not_found", i18n.Args{"id": short(t.SessionID)}))
		}
		if sess.SourceKind != "" && (sess.Capabilities == nil || !sess.Capabilities.Send) {
			return resolvedTarget{}, errors.New(i18n.T(l, "assistant-feishu.agent_read_only", i18n.Args{"reason": sess.ReadOnlyReason}))
		}
		return resolvedTarget{machine: d.ID, machineName: d.Name, session: sess, label: agentLabel(sess) + " · " + d.Name}, nil
	default:
		cfg := s.settings(ctx)
		d, ok := s.device(ctx, cfg.MachineID)
		if !ok {
			return resolvedTarget{}, errors.New(i18n.T(l, "assistant-feishu.no_assistant_machine"))
		}
		sid := t.SessionID
		if t.Kind == targetMain {
			sid = cfg.Main
		}
		r := resolvedTarget{machine: d.ID, machineName: d.Name, main: sid != "" && sid == cfg.Main}
		if sid == "" {
			r.isNew, r.main, r.label = true, t.Kind == targetMain, i18n.T(l, "assistant-feishu.new_chat")
			if r.main {
				r.label = i18n.T(l, "assistant-feishu.main_chat")
			}
			return r, nil
		}
		sess, _ := s.session(ctx, d.ID, sid)
		sess.SessionID, sess.MachineID = sid, d.ID
		r.session = sess
		r.label = conversationName(sess, cfg.Main)
		return r, nil
	}
}

func (s *Service) targetLabel(ctx context.Context, t botTarget) string {
	r, err := s.resolveBotTarget(ctx, t)
	if err != nil {
		return i18n.T(s.botLang(ctx), "assistant-feishu.not_found")
	}
	if t.Kind == targetJarvis && r.isNew {
		return i18n.T(s.botLang(ctx), "assistant-feishu.new_chat_next")
	}
	return r.label
}

const (
	botAnswerPiece = 6000 // runes per card: a card holds about 30 KB
	botImagesMax   = 4
)

// botSend passes a message on to its target and starts the run that answers it.
func (s *Service) botSend(ctx context.Context, ev *feishubot.MessageEvent, content feishubot.Content, t botTarget) error {
	rt, err := s.resolveBotTarget(ctx, t)
	if err != nil {
		return err
	}
	msg := ev.Message
	reaction, err := s.botReact(ctx, msg.MessageID, "OnIt")
	if err != nil {
		s.logf("bots: react: %v", err)
	}
	started := false
	defer func() {
		if !started && reaction != "" {
			s.botUnreact(context.WithoutCancel(ctx), msg.MessageID, reaction)
		}
	}()
	in := prompts.BotMessage{Jarvis: !t.agent(), Owner: s.owner(ctx), Text: content.Text}
	s.fbot.mu.Lock()
	_, ours := s.fbot.st.Threads[msg.ParentID]
	s.fbot.mu.Unlock()
	if msg.ParentID != "" && !ours {
		in.Quote = s.botQuote(ctx, msg.ParentID)
	}
	if content.Forward {
		in.Forward = s.botForward(ctx, msg.MessageID)
	}
	var images []proto.Image
	for _, md := range content.Media {
		if md.Kind == "image" {
			var res struct {
				Data     []byte `json:"data"`
				MIME     string `json:"mime"`
				TooLarge bool   `json:"too_large"`
			}
			if err := s.appCall(ctx, AppFeishu, FeishuResource, map[string]any{"message_id": msg.MessageID, "key": md.Key, "kind": md.Kind}, &res); err != nil {
				return fmt.Errorf("%s%w", i18n.T(s.botLang(ctx), "assistant-feishu.download_image"), err)
			}
			mime := res.MIME
			if !strings.HasPrefix(mime, "image/") {
				mime = "image/png"
			}
			if len(images) < botImagesMax && !res.TooLarge {
				images = append(images, proto.Image{Mime: mime, Data: base64.StdEncoding.EncodeToString(res.Data)})
				continue
			}
			// More pictures than a message takes, or one too big for the model: as a file.
			md.Name = md.Key + map[string]string{"image/jpeg": ".jpg", "image/gif": ".gif", "image/webp": ".webp"}[mime]
			if !strings.Contains(md.Name, ".") {
				md.Name += ".png"
			}
		}
		var saved struct {
			Path string `json:"path"`
		}
		if err := s.appCall(ctx, AppFeishu, FeishuSave, map[string]any{"message_id": msg.MessageID, "key": md.Key, "kind": md.Kind, "name": md.Name,
			"machine_id": rt.machine}, &saved); err != nil {
			return fmt.Errorf("%s%w", i18n.T(s.botLang(ctx), "assistant-feishu.save_file", i18n.Args{"machine": rt.machineName}), err)
		}
		in.Files = append(in.Files, saved.Path)
	}
	text := prompts.RenderBotMessage(in)
	if strings.TrimSpace(content.Text) == "" && in.Quote == nil && in.Forward == "" && len(images) == 0 && len(in.Files) == 0 {
		return errors.New(i18n.T(s.botLang(ctx), "assistant-feishu.cannot_read"))
	}

	run := &channel.Run{ID: "fb_" + msg.MessageID, Channel: channel.Feishu, ReplyTo: msg.MessageID, Started: time.Now().UTC(), Label: rt.label,
		Probe: botProbe(content.Text, in), Ext: map[string]string{}}
	if reaction != "" {
		run.Ext["reaction"] = reaction
	}
	if rt.isNew {
		sess, err := s.Start(ctx, StartParams{Text: text, Images: images, Main: rt.main, Origin: OriginChannel, OriginRef: channel.Feishu})
		if err != nil {
			return err
		}
		run.MachineID, run.SessionID, run.TargetKind, run.Probe = sess.MachineID, sess.SessionID, targetJarvis, ""
		if t.Kind == targetJarvis { // /new: the chat goes on in it
			s.setBotCurrent(botTarget{Kind: targetJarvis, MachineID: sess.MachineID, SessionID: sess.SessionID})
		}
	} else {
		sid := rt.session.SessionID
		run.MachineID, run.SessionID, run.TargetKind = rt.machine, sid, t.Kind
		if run.TargetKind == targetMain {
			run.TargetKind = targetJarvis
		}
		run.Base = s.lastSeq(ctx, rt.machine, sid)
		id := channel.ClientMsgID(channel.Feishu, msg.MessageID)
		run.MsgIDs = []string{id}
		p := map[string]any{"session_id": sid, "text": text, "client_msg_id": id}
		if len(images) > 0 {
			p["images"] = images
		}
		if !t.agent() {
			s.UserSpoke(ctx, SessionRef{MachineID: rt.machine, SessionID: sid})
			if p, err = s.SendParams(ctx, SendParams{MachineID: rt.machine, SessionID: sid, Params: p}); err != nil {
				return err
			}
		}
		if err := s.sendOnce(ctx, rt.machine, p); err != nil {
			return err
		}
	}
	started = true
	s.startChannelRun(run)
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

// botQuote is the message a message replies to.
func (s *Service) botQuote(ctx context.Context, id string) *prompts.BotQuote {
	items, err := s.botMessages(ctx, id)
	if err != nil || len(items) == 0 {
		return nil
	}
	it := items[0]
	c := feishubot.ParseContent(it.MsgType, it.Body.Content, nil)
	who := "Persona"
	if it.Sender.SenderType == "user" {
		who = s.botName(ctx, it.Sender.ID)
	}
	if c.Text == "" {
		c.Text = "[" + it.MsgType + "]"
	}
	return &prompts.BotQuote{Who: who, Text: clip(c.Text, 4000)}
}

// botForward reads a merged forward: one line per message, oldest first.
func (s *Service) botForward(ctx context.Context, id string) string {
	items, err := s.botMessages(ctx, id)
	if err != nil {
		return "（读不到转发的内容：" + err.Error() + "）"
	}
	loc := s.location(ctx)
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
			who = s.botName(ctx, it.Sender.ID)
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

// ---- the turn, as it looks in Feishu ----

// botTargetOf is what a run's conversation is to the chat: replies to the bot's messages about it go there.
func botTargetOf(r *channel.Run) botTarget {
	kind := r.TargetKind
	if kind == "" || kind == targetMain {
		kind = targetJarvis
	}
	return botTarget{Kind: kind, MachineID: r.MachineID, SessionID: r.SessionID}
}

// feishuSink is the Feishu bot as the pipeline's sink: a progress card replying to the message after a few seconds,
// rewritten every few, the answer written into it (or cards of their own when long), and the message's reaction going
// from OnIt to DONE.
type feishuSink struct{ s *Service }

// The Feishu card comes a second or two after the message and its elapsed time moves every few seconds, new steps
// or not: a long turn must not look stuck.
const (
	feishuProgressAfter = time.Second
	feishuProgressIdle  = 3 * time.Second
)

func (feishuSink) progressIdle() time.Duration { return feishuProgressIdle }

// When Feishu says the card is updated too often, updates stop for 5 s, then 10, 20, at most 30; the answer is not held.
const (
	cardBackoffFirst = 5 * time.Second
	cardBackoffMost  = 30 * time.Second
)

// chansNow is the channels' clock.
func (s *Service) chansNow() time.Time {
	c := &s.chans
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.clock()
}

func (f feishuSink) progress(ctx context.Context, r *channel.Run, took time.Duration, steps []string, live string) {
	s := f.s
	if f.cotProgressNow(ctx, r) {
		return
	}
	c := progressCard(s.botLang(ctx), r.Label, took, steps, live, s.runWaiting(r))
	_, prog := s.channelRunByID(r.ID)
	if prog == nil {
		prog = &chanLive{} // a run not followed any more: nothing to remember
	}
	sum := sha256.Sum256([]byte(c.JSON()))
	if card := s.runExt(r, "card"); card != "" {
		now := s.chansNow()
		if sum == prog.sentCard || now.Before(prog.quietUntil) {
			return // nothing new, or Feishu asked us to slow down: a later tick sends the latest
		}
		err := s.botUpdateCard(ctx, card, c)
		switch {
		case err == nil:
			prog.sentCard, prog.backoff = sum, 0
		case feishubot.RateLimited(err):
			prog.backoff = min(max(2*prog.backoff, cardBackoffFirst), cardBackoffMost)
			prog.quietUntil = now.Add(prog.backoff)
			s.logf("bots: progress update: %v; quiet for %s", err, prog.backoff)
		case feishubot.MessageGone(err):
			// Withdrawn or deleted: the next tick posts a new card, and the answer does not go into this one.
			s.setRunExt(r, "card", "")
			prog.sentCard = [32]byte{}
			s.logf("bots: progress card gone: %v", err)
		default:
			s.logf("bots: progress update: %v", err)
		}
		return
	}
	mid, err := s.botPost(ctx, r.ReplyTo, c, "")
	if err != nil {
		s.logf("bots: progress card: %v", err)
		return
	}
	prog.sentCard, prog.backoff, prog.quietUntil = sum, 0, time.Time{}
	s.setRunExt(r, "card", mid)
	s.rememberBotThread(mid, botTargetOf(r), r.Label)
}

// runWaiting is the note for a run whose message is not found in its conversation yet: queued behind a turn that
// works ("queued_note"), or not in yet ("preparing_note"); "" once it is.
func (s *Service) runWaiting(r *channel.Run) string {
	c := &s.chans
	c.mu.Lock()
	defer c.mu.Unlock()
	switch p := c.live[r.ID]; {
	case p == nil || p.from != 0:
		return ""
	case r.Seen:
		return "assistant-feishu.queued_note"
	}
	return "assistant-feishu.preparing_note"
}

func (f feishuSink) folded(ctx context.Context, o *channel.Run) {
	if o.Ext["card"] != "" {
		_ = f.s.botUpdateCard(ctx, o.Ext["card"], feishubot.NewCard("", "", "").Note(i18n.T(f.s.botLang(ctx), "assistant-feishu.folded")))
	}
	if cot := o.Ext["cot"]; cot != "" {
		_ = f.s.cotCall(ctx, FeishuCotComplete, map[string]any{"cot_id": cot, "message_id": o.Ext["cot_msg"], "reason": "done"})
	}
}

// answer writes the answer into the progress card (more cards when long), and marks the messages DONE. When nothing
// of it got out because the hub could not be reached it says so (tried again later); once a piece is out, the rest
// is best effort (a card sent twice would show twice).
func (f feishuSink) answer(ctx context.Context, r *channel.Run, answer, note string, took time.Duration, final, quiet bool) error {
	s := f.s
	l := s.botLang(ctx)
	foot := i18n.T(l, "assistant-feishu.took", i18n.Args{"label": r.Label, "took": channel.Duration(l, took)})
	if note != "" {
		foot += "\n" + note
	}
	// The process is in the agent message above the answer, or else folded into the answer's first card.
	var steps []string
	if !f.cotFinishNow(ctx, r) {
		steps = append([]string{}, f.s.runSteps(r.ID)...)
	}
	card := r.Ext["card"]
	for i, ac := range answerCards(l, answer, quiet, steps, foot) {
		c, piece := ac.card, ac.fallback
		var mid string
		var err error
		if i == 0 && card != "" {
			if err = s.botUpdateCard(ctx, card, c); err == nil {
				mid = card
			} else {
				s.logf("bots: answer into the progress card: %v; sending it", err)
			}
		}
		if mid == "" {
			mid, err = s.botPost(ctx, r.ReplyTo, c, piece)
		}
		if err != nil {
			if i == 0 && unreachable(err) {
				return err
			}
			s.logf("bots: answer: %v", err)
			continue
		}
		s.rememberBotThread(mid, botTargetOf(r), r.Label)
	}
	if !final {
		return nil
	}
	for _, m := range append(r.Folded, channel.Folded{ReplyTo: r.ReplyTo, Ext: r.Ext}) {
		if m.ReplyTo == "" {
			continue
		}
		if rx := m.Ext["reaction"]; rx != "" {
			s.botUnreact(ctx, m.ReplyTo, rx)
		}
		if _, err := s.botReact(ctx, m.ReplyTo, "DONE"); err != nil {
			s.logf("bots: react: %v", err)
		}
	}
	return nil
}

// unreachable: the call did not reach the hub, or did not come back (worth trying again).
func unreachable(err error) bool {
	return errors.Is(err, bus.ErrOffline) || errors.Is(err, rpc.ErrClosed) || errors.Is(err, context.DeadlineExceeded)
}

// progressCard is a run's card while it goes on; waiting: the note while its message is not found in the
// conversation yet (runWaiting), "" once it is.
func progressCard(l i18n.Lang, label string, took time.Duration, steps []string, live, waiting string) *feishubot.Card {
	c := feishubot.NewCard("", "", i18n.T(l, "assistant-feishu.working_summary"))
	note := "assistant-feishu.working_note"
	if waiting != "" {
		note = waiting
	}
	c.Note("⏳ " + i18n.T(l, note, i18n.Args{"label": label, "took": channel.Duration(l, took)}))
	if n := len(steps); n > 0 {
		var b strings.Builder
		if n > 4 {
			b.WriteString(i18n.T(l, "assistant-feishu.earlier_steps", i18n.Args{"count": n - 4}))
			steps = steps[n-4:]
		}
		for _, st := range steps {
			b.WriteString("- " + st + "\n")
		}
		c.Markdown(strings.TrimSpace(b.String()))
	}
	if live = strings.TrimSpace(live); live != "" {
		r := []rune(live)
		if len(r) > 400 {
			live = "…" + string(r[len(r)-400:])
		}
		c.Markdown(feishubot.EscapeMarkdown(live))
	}
	return c
}

// botStep is one line of the progress card for a tool call, in l: the verb, then what it works on (code when it is a
// command, file or pattern) and a note.
func botStep(l i18n.Lang, it proto.Item) string {
	s := stepOf(l, it)
	line := s.verb
	switch {
	case s.target == "":
	case s.plain:
		line += " · " + feishubot.EscapeMarkdown(s.target)
	default:
		line += " " + feishubot.Code(s.target)
	}
	return withNote(line, feishubot.EscapeMarkdown(s.note))
}

// botStepParts is what a tool call did, in l: the verb ("" when it is no tool call) and what it did it to.
func botStepParts(l i18n.Lang, it proto.Item) (string, string) {
	s := stepOf(l, it)
	return s.verb, strings.TrimSpace(withNote(s.target, s.note))
}

// withNote puts a note after s: a space, none before a full-width bracket.
func withNote(s, note string) string {
	switch {
	case note == "":
		return s
	case strings.HasPrefix(note, "（"):
		return s + note
	}
	return s + " " + note
}

// step is a tool call said plainly: a verb, what it works on (plain: words, not code) and a short note after it.
type step struct {
	verb, target, note string
	plain              bool
}

// stepOf reads a tool call into a step. Codex's calls arrive mapped to Claude's tools (internal/transcript/codex.go).
func stepOf(l i18n.Lang, it proto.Item) step {
	if it.Kind != proto.KindToolUse {
		return step{}
	}
	var in map[string]any
	_ = json.Unmarshal(it.ToolInput, &in)
	str := func(k string) string { v, _ := in[k].(string); return strings.TrimSpace(v) }
	num := func(k string) int { v, _ := in[k].(float64); return int(v) }
	t := func(key string, a ...i18n.Args) string { return i18n.T(l, "assistant-feishu."+key, a...) }
	base := func(k string) string {
		if p := str(k); p != "" {
			return path.Base(p)
		}
		return ""
	}
	var s step
	switch name := it.ToolName; name {
	case "Bash":
		s.verb = t("step_run")
		if b, _ := in["run_in_background"].(bool); b {
			s.verb = t("step_run_background")
		}
		if d := str("description"); d != "" {
			s.target, s.plain = d, true
		} else {
			s.target = firstLine(str("command"))
		}
	case "Read":
		s.verb, s.target = t("step_read"), base("file_path")
		if from, n := num("offset"), num("limit"); from > 0 || n > 0 {
			from = max(from, 1)
			if n > 0 {
				s.note = t("step_lines", i18n.Args{"from": from, "to": from + n - 1})
			} else {
				s.note = t("step_lines_from", i18n.Args{"from": from})
			}
		}
	case "Edit", "MultiEdit", "NotebookEdit":
		s.verb, s.target = t("step_edit"), base("file_path")
		if s.target == "" {
			s.target = base("notebook_path")
		}
		switch str("change") { // Codex's file changes
		case "add":
			s.verb = t("step_create")
		case "delete":
			s.verb = t("step_delete")
		}
	case "Write":
		s.verb, s.target = t("step_write"), base("file_path")
	case "Grep", "Glob":
		s.verb, s.target = t("step_search"), str("pattern")
		where, glob := str("path"), ""
		if name == "Grep" {
			glob = str("glob")
		}
		switch {
		case where != "" && glob != "":
			s.note = t("step_where_glob", i18n.Args{"where": where, "glob": glob})
		case where != "":
			s.note = t("step_where", i18n.Args{"where": where})
		case glob != "":
			s.note = t("step_glob", i18n.Args{"glob": glob})
		}
	case "WebFetch":
		s.verb, s.target = t("step_open_web"), str("url")
	case "WebSearch":
		s.verb, s.target = t("step_search_web"), str("query")
	case "Task", "Agent":
		s.verb, s.target, s.plain = t("step_subtask"), str("description"), true
		if a := str("subagent_type"); a != "" {
			s.note = t("step_agent_type", i18n.Args{"type": a})
		}
	case "TodoWrite":
		s.verb, s.target, s.plain = t("step_todos"), stepNow(in), true
	case "Skill":
		s.verb, s.target = t("step_skill"), str("skill")
	default:
		tool := name
		if strings.HasPrefix(tool, "mcp__") {
			if i := strings.LastIndex(tool, "__"); i > 4 {
				tool = tool[i+2:]
			}
		}
		s.verb = tool // an unknown tool goes by its name
		key := stepMCP[tool]
		if key == "" && strings.HasPrefix(tool, "kb_") {
			key = "step_kb"
		}
		if key != "" {
			s.verb = t(key)
		}
		for _, k := range []string{"query", "q", "title", "to", "subject", "command", "cmd", "path", "file_path", "url", "name", "id"} {
			if v := str(k); v != "" {
				s.target = firstLine(v)
				break
			}
		}
	}
	s.target = clip(oneLine(s.target), 60)
	return s
}

// stepMCP is the verb for Persona's own MCP tools (internal/prompts/mcp), by the tool's name.
var stepMCP = map[string]string{
	"phone_calendar_events":       "step_calendar",
	"phone_calendar_create_event": "step_calendar_add",
	"phone_contacts_search":       "step_contacts",
	"phone_reminders_list":        "step_reminders_list",
	"phone_reminders_create":      "step_reminders_create",
	"phone_reminders_complete":    "step_reminders_complete",
	"mail_search":                 "step_mail_search",
	"mail_read":                   "step_mail_read",
	"mail_send":                   "step_mail_send",
	"kb_update":                   "step_kb_update",
	"search_knowledge":            "step_kb",
}

// stepNow is what a to-do list (TodoWrite's todos) is doing now: its in-progress item.
func stepNow(in map[string]any) string {
	list, _ := in["todos"].([]any)
	for _, x := range list {
		m, _ := x.(map[string]any)
		if st, _ := m["status"].(string); st != "in_progress" {
			continue
		}
		for _, k := range []string{"activeForm", "content"} {
			if v, _ := m[k].(string); strings.TrimSpace(v) != "" {
				return v
			}
		}
	}
	return ""
}

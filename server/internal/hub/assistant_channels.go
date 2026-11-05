package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/sdk/go/wire"
	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/channel"
	"github.com/nwverse/persona/server/internal/feishubot"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// The channels are the assistant's (internal/assistant, channels*.go and feishu*.go; docs/channels.md): which
// conversation a channel talks to, following the turn and sending the answer back, what is asked there, the commands,
// the pushes and the sync mirror. This file is the hub's end of them on the bus:
//
//   - in: the owner's messages (channels.receive, feishu.message) and card presses (feishu.card_action) go to the
//     assistant (bots_chat.go, channels_extension.go); new and ended cards are told (interaction.announced,
//     interaction.ended); a conversation it follows has its items and the text it writes sent on (session.items,
//     session.delta) until it unfollows or disconnects;
//   - out: plain transport methods (app.call feishu.*, channels.*, interactions.resolve; device.call session.follow,
//     session.unfollow, session.send_once) the assistant asks for;
//   - shown: the channels each conversation's messages came from (the session lists' channels) and the conversations
//     whose turn a channel answers (no finished push for those), from its snapshot (ChannelsReport).

// managerChannels is what the hub keeps of the assistant's channels.
type managerChannels struct {
	mu    sync.RWMutex
	got   bool                    // a report came in since the hub started
	marks map[[2]string][]string  // conversation → the channels its messages came from, newest first
	turns map[[2]string]time.Time // conversation → when a channel's turn in it was answered (zero: followed now)

	in inbound // the owner's messages waiting for the assistant, the owner actions handed over (assistant_inbound.go)

	followMu sync.Mutex
	follows  map[[2]string]bool // conversations the assistant follows (session.follow)

	// ended, when set, sees every interaction.ended (tests).
	ended func(ix proto.Interaction, r *proto.Resolution)
}

// takeChannelsReport keeps the assistant's report (nil: it could not read the channels' state; the last one stands)
// and says the machines whose conversations' channels look different now.
func (h *Hub) takeChannelsReport(r *assistantwire.ChannelsReport) []string {
	if r == nil {
		return nil
	}
	marks := make(map[[2]string][]string, len(r.Marks))
	for _, m := range r.Marks {
		marks[[2]string{m.MachineID, m.SessionID}] = m.Channels
	}
	turns := make(map[[2]string]time.Time, len(r.Turns))
	for _, t := range r.Turns {
		turns[[2]string{t.MachineID, t.SessionID}] = t.Answered
	}
	c := &h.chans
	c.mu.Lock()
	old := c.marks
	c.marks, c.turns, c.got = marks, turns, true
	c.mu.Unlock()
	var machines []string
	add := func(k [2]string) {
		if !slices.Contains(machines, k[0]) {
			machines = append(machines, k[0])
		}
	}
	for k, v := range marks {
		if !slices.Equal(old[k], v) {
			add(k)
		}
	}
	for k := range old {
		if _, ok := marks[k]; !ok {
			add(k)
		}
	}
	return machines
}

// channelsOfConversations are, by conversation, the channels messages came into it from (newest first): what the
// conversation list draws next to a title.
func (h *Hub) channelsOfConversations() map[[2]string][]string {
	c := &h.chans
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.marks
}

// channelAnswered: the conversation's turn is (or just was) answered in a channel, so the phone's "finished" push
// leaves it alone. "Just": within the finished push's delay (notify.go), which is when it looks — not much longer, or
// the push of the next turn, asked in the app, would be dropped too.
func (h *Hub) channelAnswered(session string) bool {
	window := 30 * time.Second
	if h.cfg != nil {
		window += h.cfg.Notify.doneDelay()
	}
	c := &h.chans
	// Right after a hub start the assistant has not reported yet: wait a little for its first report (the push waits
	// anyway); with none by then, as if nothing was answered in a channel.
	for wait := time.Now().Add(channelsReportWait); time.Now().Before(wait); time.Sleep(100 * time.Millisecond) {
		c.mu.RLock()
		got := c.got
		c.mu.RUnlock()
		if got {
			break
		}
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	for k, at := range c.turns {
		if k[1] == session && (at.IsZero() || time.Since(at) < window) {
			return true
		}
	}
	return false
}

// channelsReportWait is how long the finished push waits for the assistant's first report after a hub start.
var channelsReportWait = 10 * time.Second

// channelNoted tells the assistant a channel's message went into a conversation through the apps' routes (a voice
// conversation's errand, POST …/messages with client_msg_id voice-…): the conversation shows where it came from.
func (h *Hub) channelNoted(id, machine, session string) {
	_ = h.notifyBus(assistantwire.AssistantID, assistantwire.EventChannelNoted, assistantwire.ChannelNoted{Channel: id, MachineID: machine, SessionID: session})
}

// channelsAnnounce tells the assistant about a new card (hub.go announce): the Feishu bot's card, a channel's text.
func (h *Hub) channelsAnnounce(ix proto.Interaction) {
	_ = h.notifyBus(assistantwire.AssistantID, assistantwire.EventInteractionAnnounced, assistantwire.InteractionAnnounced{Interaction: ix})
}

// channelsEnded tells the assistant a card is over: r is how it was answered (nil: withdrawn, expired, or over after
// the answer that was told already).
func (h *Hub) channelsEnded(ix proto.Interaction, r *proto.Resolution) {
	if f := h.chans.ended; f != nil {
		f(ix, r)
	}
	_ = h.notifyBus(assistantwire.AssistantID, assistantwire.EventInteractionEnded, assistantwire.InteractionEnded{Interaction: ix, Resolution: r})
}

// ---- following a conversation for the assistant ----

func (h *Hub) follow(ctx context.Context, m *Machine, session string, on bool) {
	k := [2]string{m.cfg.ID, session}
	c := &h.chans
	c.followMu.Lock()
	had := c.follows[k]
	if on && !had {
		if c.follows == nil {
			c.follows = map[[2]string]bool{}
		}
		c.follows[k] = true
	} else if !on && had {
		delete(c.follows, k)
	}
	c.followMu.Unlock()
	switch {
	case on && !had:
		m.addWatcher(ctx, session)
	case !on && had:
		m.removeWatcher(session)
	}
}

// dropFollows ends what the assistant followed: it disconnected (it follows them again when it is back).
func (h *Hub) dropFollows() {
	c := &h.chans
	c.followMu.Lock()
	gone := c.follows
	c.follows = nil
	c.followMu.Unlock()
	for k := range gone {
		if m := h.localMachine(k[0]); m != nil {
			go m.removeWatcher(k[1])
		}
	}
}

func (h *Hub) followed(machine, session string) bool {
	c := &h.chans
	c.followMu.Lock()
	defer c.followMu.Unlock()
	return c.follows[[2]string{machine, session}]
}

// followedItems: new items of a watched conversation (machine.go), sent on when the assistant follows it.
func (h *Hub) followedItems(machine, session string, items []proto.Item) {
	if len(items) == 0 {
		return
	}
	if !h.followed(machine, session) {
		if !h.cfg.Assistant.PersonaView {
			return
		}
		m := h.localMachine(machine)
		if m == nil {
			return
		}
		conversation, known := m.session(session)
		if !known || !isAssistant(conversation, m.assistantCwd()) {
			return
		}
		var cards []proto.Item
		for _, it := range items {
			if it.Role == "assistant" && it.Kind == proto.KindText && (it.FullSize > 0 || wire.HasAssistantView(it.Text)) {
				it.Image = nil
				cards = append(cards, it)
			}
		}
		items = cards
		if len(items) == 0 {
			return
		}
	}
	_ = h.notifyBus(assistantwire.AssistantID, assistantwire.EventSessionItems, assistantwire.SessionItems{MachineID: machine, SessionID: session, Items: items})
}

// followedDelta: the text a watched conversation is writing now.
func (h *Hub) followedDelta(machine, session, text string) {
	if !h.followed(machine, session) {
		return
	}
	_ = h.notifyBus(assistantwire.AssistantID, assistantwire.EventSessionDelta, assistantwire.SessionDelta{MachineID: machine, SessionID: session, Text: text})
}

// channelsDeviceCall answers the device methods of the channels' turns; false for the others.
func (h *Hub) channelsDeviceCall(ctx context.Context, m *Machine, p assistantwire.DeviceCall) (json.RawMessage, bool, error) {
	switch p.Method {
	case assistantwire.DeviceSessionFollow, assistantwire.DeviceSessionUnfollow:
		var a struct {
			SessionID string `json:"session_id"`
		}
		if err := json.Unmarshal(p.Params, &a); err != nil || a.SessionID == "" {
			return nil, true, fmt.Errorf("%s needs session_id", p.Method)
		}
		h.follow(ctx, m, a.SessionID, p.Method == assistantwire.DeviceSessionFollow)
		return json.RawMessage(`{}`), true, nil
	case assistantwire.DeviceSessionSendOnce:
		var a map[string]any
		if err := json.Unmarshal(p.Params, &a); err != nil {
			return nil, true, err
		}
		if id, _ := a["client_msg_id"].(string); id == "" {
			return nil, true, errors.New("session.send_once needs client_msg_id")
		}
		return json.RawMessage(`{}`), true, h.sendOnce(ctx, m, a)
	}
	return nil, false, nil
}

// sendOnce sends with the same once-only guard as the app (send.go): a message id is sent at most once.
func (h *Hub) sendOnce(ctx context.Context, m *Machine, p map[string]any) error {
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
			h.logf("channels: settle send: %v", serr)
		}
	}
	return err
}

// ---- the transport the assistant asks for (app.call) ----

const (
	botImageMax = 3 << 20 // a picture's base64 must stay under the model's 5 MB
	botFileMax  = 50 << 20
)

// feishuChatView is what the assistant shows of the bot's chat (feishu.view), for GET /api/v1/bots/feishu.
func (h *Hub) feishuChatView(ctx context.Context) (assistantwire.FeishuView, error) {
	var v assistantwire.FeishuView
	if h.busConnOf(assistantwire.AssistantID) == nil {
		return v, errBusOffline
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	err := h.callBus(ctx, assistantwire.AssistantID, assistantwire.MethodFeishuView, map[string]any{}, &v)
	return v, err
}

// channelsAppCall answers the channels' transport methods; false for the others.
func (h *Hub) channelsAppCall(ctx context.Context, p assistantwire.AppCall) (json.RawMessage, bool, error) {
	var out any
	var err error
	switch p.AppID {
	case assistantwire.AppFeishu:
		out, err = h.feishuCall(ctx, p.Method, p.Params)
	case assistantwire.AppChannels:
		out, err = h.channelsCall(ctx, p.Method, p.Params)
	case assistantwire.AppInteractions:
		if p.Method != assistantwire.InteractionsResolve {
			return nil, false, nil
		}
		out, err = h.resolveFromChannel(ctx, p.Params)
	default:
		return nil, false, nil
	}
	if err != nil {
		return nil, true, err
	}
	raw, err := json.Marshal(out)
	return raw, true, err
}

// resolveFromChannel answers a card as the user answered it in a channel (interactions.resolve): only with the owner
// action the hub handed over with that press or message (a card's for that card), once. It is the user's own answer
// (no AnsweredBy), as when the hub's own bot answered it.
func (h *Hub) resolveFromChannel(ctx context.Context, params json.RawMessage) (any, error) {
	var a struct {
		InteractionID string           `json:"interaction_id"`
		Resolution    proto.Resolution `json:"resolution"`
		Action        string           `json:"action"`
	}
	if err := json.Unmarshal(params, &a); err != nil {
		return nil, err
	}
	id := strings.TrimSpace(a.InteractionID)
	if err := h.takeOwnerAction(a.Action, id); err != nil {
		return nil, err
	}
	a.Resolution.AnsweredBy = ""
	if err := h.resolve(id, a.Resolution); err != nil {
		if errors.Is(err, errNotPending) {
			return map[string]any{"ok": false, "status": h.interactionOutcome(ctx, id)}, nil
		}
		return nil, err
	}
	return map[string]any{"ok": true}, nil
}

func (h *Hub) feishuCall(ctx context.Context, method string, params json.RawMessage) (any, error) {
	var a struct {
		ReplyTo    string          `json:"reply_to"`
		Card       string          `json:"card"`
		Fallback   string          `json:"fallback"`
		Text       string          `json:"text"`
		MessageID  string          `json:"message_id"`
		Emoji      string          `json:"emoji"`
		ReactionID string          `json:"reaction_id"`
		Key        string          `json:"key"`
		Kind       string          `json:"kind"`
		Name       string          `json:"name"`
		MachineID  string          `json:"machine_id"`
		OpenID     string          `json:"open_id"`
		Enabled    *bool           `json:"enabled"`
		Features   map[string]bool `json:"features"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &a); err != nil {
			return nil, err
		}
	}
	switch method {
	case assistantwire.FeishuStatusGet:
		api, set := h.botAPI()
		st := assistantwire.FeishuStatus{Ready: api != nil, Enabled: set.Enabled, Features: assistantwire.FeishuFeatures(set.Features)}
		if set.Owner != nil {
			st.Owner, st.PairedAt = set.Owner.OpenID, set.Owner.At
		}
		if h.cfg != nil {
			st.PublicURL = h.cfg.PublicURL
		}
		return st, nil
	case assistantwire.FeishuConfigure:
		return map[string]any{}, h.configureFeishu(ctx, a.Enabled, a.Features)
	case assistantwire.FeishuTest:
		return map[string]any{}, h.testFeishuBot(ctx)
	}
	api, set := h.botAPI()
	if api == nil {
		return nil, errBotOff
	}
	switch method {
	case assistantwire.FeishuPost:
		if a.Card == "" {
			return nil, errors.New("feishu.post needs a card")
		}
		id, err := h.botPostJSON(ctx, api, set.Owner.ChatID, a.ReplyTo, a.Card, a.Fallback)
		return map[string]any{"message_id": id}, err
	case assistantwire.FeishuText:
		var id string
		var err error
		if a.ReplyTo != "" {
			id, err = api.Reply(ctx, a.ReplyTo, feishubot.TypeText, feishubot.TextContent(a.Text), "", false)
		} else {
			id, err = api.Send(ctx, "chat_id", set.Owner.ChatID, feishubot.TypeText, feishubot.TextContent(a.Text), "")
		}
		return map[string]any{"message_id": id}, err
	case assistantwire.FeishuUpdateCard:
		return map[string]any{}, api.UpdateCard(ctx, a.MessageID, a.Card)
	case assistantwire.FeishuReact:
		id, err := api.React(ctx, a.MessageID, a.Emoji)
		return map[string]any{"reaction_id": id}, err
	case assistantwire.FeishuUnreact:
		return map[string]any{}, api.Unreact(ctx, a.MessageID, a.ReactionID)
	case assistantwire.FeishuMessages:
		items, err := api.Message(ctx, a.MessageID)
		return map[string]any{"items": items}, err
	case assistantwire.FeishuUserName:
		return map[string]any{"name": h.botName(ctx, api, a.OpenID)}, nil
	case assistantwire.FeishuResource:
		data, mime, err := api.Resource(ctx, a.MessageID, a.Key, a.Kind, botFileMax)
		if err != nil {
			return nil, err
		}
		if len(data) > botImageMax {
			return map[string]any{"mime": mime, "too_large": true}, nil
		}
		return map[string]any{"data": data, "mime": mime}, nil
	case assistantwire.FeishuSave:
		m := h.localMachine(a.MachineID)
		if m == nil {
			return nil, noDevice(a.MachineID)
		}
		data, _, err := api.Resource(ctx, a.MessageID, a.Key, a.Kind, botFileMax)
		if err != nil {
			return nil, err
		}
		path, err := h.uploadBytes(ctx, m, firstNonEmpty(a.Name, a.Key), data)
		return map[string]any{"path": path}, err
	}
	return nil, fmt.Errorf("app %s has no method %s", assistantwire.AppFeishu, method)
}

func (h *Hub) channelsCall(ctx context.Context, method string, params json.RawMessage) (any, error) {
	var a struct {
		Channel string            `json:"channel"`
		On      bool              `json:"on"`
		Step    string            `json:"step"`
		Values  map[string]string `json:"values"`
		channel.SendParams
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &a); err != nil {
			return nil, err
		}
	}
	switch method {
	case assistantwire.ChannelsList:
		return map[string]any{"channels": h.channelList(ctx)}, nil
	case assistantwire.ChannelsSend:
		return h.channelSendPiece(ctx, a.Channel, a.SendParams)
	case assistantwire.ChannelsTyping:
		return map[string]any{}, h.extensionCall(ctx, a.Channel, channel.MTyping, channel.TypingParams{On: a.On}, nil)
	case assistantwire.ChannelsEnable:
		err := h.enableChannel(ctx, a.Channel, a.On)
		if errors.Is(err, errNeedsTriageModel) {
			return nil, &rpc.Error{Code: assistantwire.CodeNeedsTriageModel, Message: err.Error()}
		}
		return map[string]any{}, err
	case assistantwire.ChannelsUnbind:
		return map[string]any{}, h.unbindChannel(ctx, a.Channel)
	}
	return nil, fmt.Errorf("app %s has no method %s", assistantwire.AppChannels, method)
}

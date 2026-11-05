package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/channel"
	"github.com/nwverse/persona/server/internal/feishubot"
	"github.com/nwverse/persona/server/internal/i18n"
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
	mu  sync.RWMutex
	got bool // a report came in since the hub started

	in inbound // the owner's messages waiting for the assistant, the owner actions handed over (assistant_inbound.go)

	// ended, when set, sees every interaction.ended (tests).
	ended func(ix proto.Interaction, r *proto.Resolution)
}

// channelNoted tells the assistant a channel's message went into a conversation through the apps' routes (a voice
// conversation's errand, POST …/messages with client_msg_id voice-…): the conversation shows where it came from.
func (h *Hub) channelNoted(id, machine, session string) {
	_ = h.notifyBus(assistantwire.AssistantID, assistantwire.EventChannelNoted, assistantwire.ChannelNoted{Channel: id, MachineID: machine, SessionID: session})
}

// ---- following a conversation for the assistant ----

// channelsDeviceCall answers the device methods of the channels' turns; false for the others.
func (h *Hub) channelsDeviceCall(ctx context.Context, m *Machine, p assistantwire.DeviceCall) (json.RawMessage, bool, error) {
	switch p.Method {
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
			return i18n.Err("hub-assistant_channels.send_in_doubt")
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
	default:
		return nil, false, nil
	}
	if err != nil {
		return nil, true, err
	}
	raw, err := json.Marshal(out)
	return raw, true, err
}

func (h *Hub) feishuCall(ctx context.Context, method string, params json.RawMessage) (any, error) {
	var a struct {
		ReplyTo    string               `json:"reply_to"`
		Card       string               `json:"card"`
		Fallback   string               `json:"fallback"`
		Text       string               `json:"text"`
		MessageID  string               `json:"message_id"`
		Emoji      string               `json:"emoji"`
		ReactionID string               `json:"reaction_id"`
		Key        string               `json:"key"`
		Kind       string               `json:"kind"`
		Name       string               `json:"name"`
		MachineID  string               `json:"machine_id"`
		OpenID     string               `json:"open_id"`
		Enabled    *bool                `json:"enabled"`
		Features   map[string]bool      `json:"features"`
		CotID      string               `json:"cot_id"`
		Events     []feishubot.CotEvent `json:"events"`
		Reason     string               `json:"reason"`
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
	case assistantwire.FeishuCotCreate:
		cot, id, err := api.CotCreate(ctx, set.Owner.ChatID, a.ReplyTo)
		return map[string]any{"cot_id": cot, "message_id": id}, err
	case assistantwire.FeishuCotAppend:
		return map[string]any{}, api.CotAppend(ctx, a.CotID, a.MessageID, a.Events)
	case assistantwire.FeishuCotComplete:
		return map[string]any{}, api.CotComplete(ctx, a.CotID, a.MessageID, a.Reason)
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

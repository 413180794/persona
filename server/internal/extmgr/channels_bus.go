package extmgr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/channel"
	"github.com/nwverse/persona/server/internal/feishubot"
	"github.com/nwverse/persona/server/internal/rpc"
)

// The channels' transport the assistant asks for (app.call feishu.*, channels.*, interactions.resolve), moved here
// from the hub with the bots and the plugin channels: the assistant is in this process, so these calls come in
// directly (AppCall), not over the bus.

// ---- the transport the assistant asks for (app.call) ----

const (
	botImageMax = 3 << 20 // a picture's base64 must stay under the model's 5 MB
	botFileMax  = 50 << 20
)

// feishuChatView is what the assistant shows of the bot's chat (feishu.view), for GET /api/v1/bots/feishu.
func (h *Manager) feishuChatView(ctx context.Context) (assistantwire.FeishuView, error) {
	var v assistantwire.FeishuView
	if !h.assistantUp() {
		return v, errBusOffline
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	err := h.callAssistant(ctx, assistantwire.MethodFeishuView, map[string]any{}, &v)
	return v, err
}

// channelsAppCall answers the channels' transport methods; false for the others.
func (h *Manager) channelsAppCall(ctx context.Context, p assistantwire.AppCall) (json.RawMessage, bool, error) {
	var out any
	var err error
	switch p.AppID {
	case assistantwire.AppFeishu:
		out, err = h.feishuCall(ctx, p.Method, p.Params)
	case assistantwire.AppChannels:
		out, err = h.channelsCall(ctx, p.Method, p.Params)
	case assistantwire.AppInteractions:
		if p.Method != assistantwire.InteractionsOwnerAction {
			return nil, false, nil
		}
		var a struct {
			InteractionID string `json:"interaction_id"`
			Action        string `json:"action"`
		}
		if err := json.Unmarshal(p.Params, &a); err != nil {
			return nil, true, err
		}
		// The assistant answers a card from a channel only with the owner's action handed over with that press or
		// message (a card's for that card), once (fleet_routes.go channelResolve there).
		err = h.takeOwnerAction(a.Action, strings.TrimSpace(a.InteractionID))
		out = map[string]any{}
	default:
		return nil, false, nil
	}
	if err != nil {
		return nil, true, err
	}
	raw, err := json.Marshal(out)
	return raw, true, err
}

func (h *Manager) feishuCall(ctx context.Context, method string, params json.RawMessage) (any, error) {
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

func (h *Manager) channelsCall(ctx context.Context, method string, params json.RawMessage) (any, error) {
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

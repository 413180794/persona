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
	"github.com/nwverse/persona/server/internal/extension"
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
			Channel       string `json:"channel"`
			Kind          string `json:"kind"`
			Clarify       bool   `json:"clarify"`
			Text          string `json:"text"`
			ClientMsgID   string `json:"client_msg_id"`
			MachineID     string `json:"machine_id"`
			SessionID     string `json:"session_id"`
		}
		if err := json.Unmarshal(p.Params, &a); err != nil {
			return nil, true, err
		}
		var question assistantwire.ClarificationProof
		if a.Clarify {
			question = assistantwire.ClarificationProof{InteractionID: strings.TrimSpace(a.InteractionID), MachineID: a.MachineID,
				SessionID: a.SessionID, Text: a.Text, ClientMsgID: a.ClientMsgID}
			if a.Channel != channel.Feishu || !validClarificationProof(question) {
				return nil, true, errors.New("invalid clarification proof request")
			}
		}
		// The assistant answers a card from a channel only with the owner's action handed over with that press or
		// message in that channel (a card's for that card, a message's for a kind it takes typed), once
		// (fleet_routes.go channelResolve there; kind is the queue's).
		err = h.takeOwnerAction(a.Action, a.Channel, strings.TrimSpace(a.InteractionID), a.Kind)
		if err == nil && a.Clarify {
			out = map[string]any{"clarification_proof": h.clarificationProofs.issue(question)}
		} else {
			out = map[string]any{}
		}
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
	if method == assistantwire.FeishuBriefPost {
		return h.feishuBriefPost(ctx, params)
	}
	var a struct {
		ReplyTo    string               `json:"reply_to"`
		Card       string               `json:"card"`
		Fallback   string               `json:"fallback"`
		FullAnswer bool                 `json:"full_answer"`
		UUID       string               `json:"uuid"`
		NoDetour   bool                 `json:"no_detour"` // feishu.post / feishu.text: a reply to a parent that is gone fails, it is not sent into the chat
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
		ChatID     string               `json:"chat_id"`   // feishu.post / feishu.text: the chat to send into (default: the owner's)
		InThread   bool                 `json:"in_thread"` // feishu.post / feishu.text: a reply goes into the parent's thread
		Exclusive  bool                 `json:"exclusive"` // feishu.pin: the bot's other pins in the chat come off
		CardID     string               `json:"card_id"`   // feishu.card_*: the card entity
		Element    string               `json:"element"`   // feishu.card_stream: the element streamed into
		Sequence   int64                `json:"sequence"`  // feishu.card_*: the write's place, larger than the last
		Streaming  bool                 `json:"streaming"` // feishu.card_settings
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &a); err != nil {
			return nil, err
		}
	}
	switch method {
	case assistantwire.FeishuStatusGet:
		api, set := h.botAPI()
		nudge := set.nudgeAfter()
		reportDelay := set.sourceReportDelayMinutes()
		st := assistantwire.FeishuStatus{Ready: api != nil, Enabled: set.Enabled, Features: assistantwire.FeishuFeatures(set.Features),
			NudgeAfter: &nudge, SourceReportDelayMinutes: &reportDelay, SourceReportFullReply: set.SourceReportFullReply}
		if h.cfg != nil {
			st.OwnerCardChannel = h.cfg.Bots.Feishu.OwnerCardChannel
		}
		if set.Owner != nil {
			st.Owner, st.PairedAt = set.Owner.OpenID, set.Owner.At
			st.Chats = botSendChats(set)
			st.HomeChat = set.homeChat()
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
		// The answer to the owner's "/read" goes under that message, in a group the bot may not otherwise write in
		// (#11803, bots_read.go): a bounded number of replies, never a detour into the group.
		if chatID, found, err := readSourceReply(set, a.ChatID, a.ReplyTo, h.bots.clock().UTC()); found {
			if err != nil {
				return nil, err
			}
			if !h.useAISourceReply(a.ReplyTo, false) {
				return nil, errBotSourceReply
			}
			id, err := h.botPostJSONKeyedOpt(ctx, api, chatID, a.ReplyTo, a.Card, a.Fallback, callerKey(a.UUID, feishuMaxUUID-4), a.InThread, true)
			return map[string]any{"message_id": id}, err
		}
		chatID, err := botSendChat(set, a.ChatID)
		if err != nil {
			return nil, err
		}
		if h.cfg != nil && h.cfg.Bots.Feishu.OwnerCardChannel != "" && (a.Kind == "conclusion" || a.Kind == "push") {
			if a.ChatID != "" || a.ReplyTo != "" || a.InThread {
				return nil, errBotChatDenied
			}
			return h.sendOwnerCard(ctx, api, set, a.Kind, a.UUID, a.Fallback, a.Card)
		}
		parent, err := h.checkReplyParent(ctx, api, set, a.ReplyTo)
		if err != nil {
			return nil, err
		}
		id, err := h.botPostJSONKeyedOpt(ctx, api, chatID, a.ReplyTo, a.Card, a.Fallback, callerKey(a.UUID, feishuMaxUUID-4), a.InThread, a.NoDetour)
		if err == nil {
			h.rememberMessageChat(id, firstNonEmpty(parent, chatID)) // what it sent is in a chat the bot may use
		}
		return map[string]any{"message_id": id}, err
	case assistantwire.FeishuText:
		if a.FullAnswer && (!set.SourceReportFullReply || a.ReplyTo == "") {
			return nil, errBotSourceReply
		}
		noDetour := a.NoDetour || a.FullAnswer
		// A reply under the owner's "/ai" in a group that is not opted in (#6146) is the one send open to such a group.
		chatID, found, err := aiSourceReply(set, a.ChatID, a.ReplyTo, a.Text, h.bots.clock().UTC(), a.FullAnswer)
		if err != nil {
			return nil, err
		}
		parent := chatID
		if found {
			noDetour = true // its original gone: the reply is lost, not sent into a chat the bot may not write in
		} else {
			if chatID, err = botSendChat(set, a.ChatID); err != nil {
				return nil, err
			}
			if parent, err = h.checkReplyParent(ctx, api, set, a.ReplyTo); err != nil {
				return nil, err
			}
		}
		// With no parent the key is unused: replyOrSend sends with its own fresh one-shot key.
		// The caller's key, when it names one, in place of the reply's own (parent, purpose, content): two lines with
		// the same words under one parent are two messages, which the content-derived key would have Feishu drop as a repeat.
		replyUUID := feishuReplyUUIDIn(a.ReplyTo, "t", a.Text, a.InThread)
		if k := callerKey(a.UUID, feishuMaxUUID); k != "" {
			replyUUID = k
		}
		msgType, content := feishubot.TypeText, feishubot.TextContent(a.Text)
		if a.FullAnswer {
			msgType, content, err = h.feishuAnswerContent(ctx, api, a.ReplyTo, replyUUID, a.Text)
			if err != nil {
				return nil, err
			}
			current, settings := h.botAPI()
			if !settings.SourceReportFullReply {
				return nil, errBotSourceReply
			}
			if current != api {
				// The bot restarted during the upload (new credentials, turned off and on, the group's
				// center moved): a file key of the old client may not be usable. Failing for now lets
				// the report try again with the current client.
				return nil, errBotRestarted
			}
		}
		if found && !h.useAISourceReply(a.ReplyTo, a.FullAnswer) {
			return nil, errBotSourceReply
		}
		id, _, err := h.replyOrSendOpt(ctx, api, chatID, a.ReplyTo, msgType, content, replyUUID, a.InThread, noDetour)
		if err == nil && !found {
			h.rememberMessageChat(id, firstNonEmpty(parent, chatID))
		}
		return map[string]any{"message_id": id}, err
	case assistantwire.FeishuUpdateCard:
		receipt, err := api.UpdateCard(ctx, a.MessageID, a.Card)
		if err != nil {
			data, _ := json.Marshal(receipt)
			return nil, &rpc.Error{Code: assistantwire.CodeFeishuAPI, Message: err.Error(), Data: data}
		}
		return receipt, nil
	case assistantwire.FeishuCardCreate:
		if a.Card == "" {
			return nil, errors.New("feishu.card_create needs a card")
		}
		id, err := api.CardCreate(ctx, a.Card)
		return map[string]any{"card_id": id}, feishuCoded(err)
	case assistantwire.FeishuCardUpdate:
		return map[string]any{}, feishuCoded(api.CardUpdate(ctx, a.CardID, a.Card, a.Sequence))
	case assistantwire.FeishuCardStream:
		return map[string]any{}, feishuCoded(api.CardStream(ctx, a.CardID, a.Element, a.Text, a.Sequence))
	case assistantwire.FeishuCardSettings:
		return map[string]any{}, feishuCoded(api.CardSettings(ctx, a.CardID, a.Streaming, a.Sequence))
	case assistantwire.FeishuPin:
		if err := checkOwnDMMessage(ctx, api, set, a.MessageID, ""); err != nil {
			return nil, err
		}
		if err := api.Pin(ctx, a.MessageID); err != nil {
			return nil, feishuCoded(err)
		}
		if a.Exclusive {
			if err := unpinBotOthers(ctx, api, set.Owner.ChatID, a.MessageID); err != nil {
				return nil, feishuCoded(err)
			}
		}
		return map[string]any{}, nil
	case assistantwire.FeishuRecall:
		if err := checkOwnDMMessage(ctx, api, set, a.MessageID, ""); err != nil {
			return nil, err
		}
		return map[string]any{}, feishuCoded(api.Recall(ctx, a.MessageID))
	case assistantwire.FeishuCotCreate:
		chatID, err := botSendChat(set, a.ChatID)
		if err != nil {
			return nil, err
		}
		parent, err := h.checkReplyParent(ctx, api, set, a.ReplyTo)
		if err != nil {
			return nil, err
		}
		if a.InThread && a.ReplyTo == "" {
			return nil, errBotReplyChat
		}
		// As with other replies, the parent decides where the agent message lands; chat_id must still name a chat
		// the bot may use, and reply_in_thread keeps it in the parent's origin topic.
		chatID = firstNonEmpty(parent, chatID)
		cot, id, err := api.CotCreate(ctx, chatID, a.ReplyTo, a.InThread)
		if err == nil {
			h.rememberMessageChat(id, chatID)
		}
		return map[string]any{"cot_id": cot, "message_id": id}, feishuCoded(err)
	case assistantwire.FeishuCotAppend:
		return map[string]any{}, feishuCoded(api.CotAppend(ctx, a.CotID, a.MessageID, a.Events))
	case assistantwire.FeishuCotComplete:
		return map[string]any{}, feishuCoded(api.CotComplete(ctx, a.CotID, a.MessageID, a.Reason))
	case assistantwire.FeishuReact:
		if err := h.checkReactTarget(ctx, api, set, a.MessageID); err != nil {
			return nil, err
		}
		id, err := api.React(ctx, a.MessageID, a.Emoji)
		return map[string]any{"reaction_id": id}, err
	case assistantwire.FeishuUnreact:
		if err := h.checkReactTarget(ctx, api, set, a.MessageID); err != nil {
			return nil, err
		}
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
		if errors.Is(err, feishubot.ErrTooLarge) {
			return map[string]any{"too_large": true}, nil // the rest of the message still goes
		}
		if err != nil {
			return nil, err
		}
		path, err := h.uploadBytes(ctx, m, firstNonEmpty(a.Name, a.Key), data)
		return map[string]any{"path": path}, err
	}
	return nil, fmt.Errorf("app %s has no method %s", assistantwire.AppFeishu, method)
}

func (h *Manager) feishuBriefPost(ctx context.Context, raw json.RawMessage) (any, error) {
	var p assistantwire.FeishuBriefPostParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	if p.UUID == "" || callerKey(p.UUID, feishuMaxUUID) != p.UUID || p.Card == "" {
		return nil, errors.New("feishu.brief_post needs a valid UUID and card")
	}
	ctx, cancel := context.WithDeadline(ctx, p.ExpiresAt)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	api, set := h.botAPI()
	if api == nil {
		return nil, errBotOff
	}
	if p.Owner == "" || set.Owner.OpenID != p.Owner || !set.Owner.At.Equal(p.PairedAt) || set.Owner.ChatID == "" {
		return nil, errBotChatDenied
	}
	// The direct DM send uses exactly this UUID and payload. There is no alternate transport or fallback send.
	id, err := api.Send(ctx, "chat_id", set.Owner.ChatID, feishubot.TypeInteractive, p.Card, p.UUID)
	if err != nil {
		return nil, feishuCoded(err)
	}
	h.rememberMessageChat(id, set.Owner.ChatID)
	return map[string]string{"message_id": id}, nil
}

func (h *Manager) sendOwnerCard(ctx context.Context, api *feishubot.Client, set FeishuBotSettings, kind, ref, fallback, card string) (channel.SendResult, error) {
	if set.Owner == nil || set.Owner.OpenID == "" || set.Owner.ChatID == "" {
		return channel.SendResult{}, errBotOff
	}
	extensionID, channelID, ok := strings.Cut(h.cfg.Bots.Feishu.OwnerCardChannel, "/")
	if !ok || extensionID == "" || channelID == "" {
		return channel.SendResult{}, errors.New("bots.feishu.owner_card_channel must name extension/channel")
	}
	purpose := "conclusion"
	if kind == "push" {
		purpose = "notify"
	}
	if ref == "" {
		ref = feishuSendUUID()
	}
	msg := channel.SendParams{
		Ref: ref, Kind: channel.OutPush, Text: fallback,
		FeishuCard: &channel.FeishuOwnerCard{Purpose: purpose, AppID: api.AppID(), OwnerOpenID: set.Owner.OpenID, Card: card},
	}
	out, err := h.channelSendPiece(ctx, channelID, msg)
	if err != nil {
		return channel.SendResult{}, err
	}
	if out.Prepared == (out.MessageID != "") {
		return channel.SendResult{}, errors.New("owner card needs an outbox reservation or a delivery receipt")
	}
	id := out.MessageID
	if out.Prepared {
		// The extension owns the outbox. Only the existing paired client owns credentials and chooses the destination.
		// The same app and owner get one platform UUID per logical delivery even across restarts.
		current, now := h.botAPI()
		if current == nil || current.AppID() != api.AppID() || now.Owner == nil || now.Owner.OpenID != set.Owner.OpenID || now.Owner.ChatID != set.Owner.ChatID {
			return channel.SendResult{}, errBotOff
		}
		key := feishuReplyUUIDIn(set.Owner.ChatID, "owner", purpose+"\x00"+ref, false)
		id, err = current.Send(ctx, "chat_id", now.Owner.ChatID, feishubot.TypeInteractive, card, key)
		if err != nil {
			return channel.SendResult{}, feishuCoded(err)
		}
	}
	if err := checkOwnDMMessage(ctx, api, set, id, feishubot.TypeInteractive); err != nil {
		return channel.SendResult{}, err
	}
	if out.Prepared {
		msg.FeishuCard.MessageID = id
		confirmed, err := h.channelSendPiece(ctx, channelID, msg)
		if err != nil {
			return channel.SendResult{}, err
		}
		if confirmed.Prepared || confirmed.MessageID != id {
			return channel.SendResult{}, errors.New("owner card outbox returned a different receipt")
		}
	}
	h.rememberMessageChat(id, set.Owner.ChatID)
	return channel.SendResult{MessageID: id}, nil
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
	case assistantwire.ChannelsConfirmNotice:
		var p assistantwire.ConfirmChannelNotice
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		pc, ok := h.extensionChannel(p.Channel)
		if !ok || pc.extension.Record.ID != p.Extension || !h.extensions.reg.HubScopeGranted(p.Extension, extension.ScopeChannelNotice) {
			return nil, &rpc.Error{Code: extension.CodePermissionDenied, Message: "notice source unavailable"}
		}
		return struct{}{}, h.extensionCall(ctx, p.Channel, assistantwire.MConfirmNotice, map[string]string{"id": p.ID}, nil)
	case assistantwire.ChannelsSend:
		if a.FeishuCard != nil {
			return nil, errors.New("native owner cards require the paired Feishu transport")
		}
		if a.Kind == channel.OutPush && h.cfg != nil {
			if _, id, ok := strings.Cut(h.cfg.Bots.Feishu.OwnerCardChannel, "/"); ok && id == a.Channel {
				api, set := h.botAPI()
				if api == nil || a.ReplyTo != "" {
					return nil, errBotOff
				}
				card := feishubot.NewCard("", "", a.Text).Markdown(feishubot.EscapeMarkdown(a.Text))
				return h.sendOwnerCard(ctx, api, set, "push", a.Ref, a.Text, card.JSON())
			}
		}
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

// feishuCoded gives a Feishu API error its code on the bus: the assistant tells Feishu refusing agent messages from a
// call that failed for now (a rate limit, a busy server). A call that got no answer from Feishu (a timeout, a dropped
// connection, a gateway's page) has code -1: it failed for now too.
func feishuCoded(err error) error {
	if err == nil {
		return nil
	}
	code := -1
	var ae *feishubot.APIError
	if errors.As(err, &ae) {
		code = ae.Code
	}
	data, _ := json.Marshal(map[string]int{"code": code})
	return &rpc.Error{Code: assistantwire.CodeFeishuAPI, Message: err.Error(), Data: data}
}

// checkReactTarget refuses an emoji (or taking one away) on a message of a chat the bot may not use: the owner's private
// chat, the groups opted in, and the messages the owner's "@bot /ai" was taken from (aiSource, which carries its chat) are
// the only places it reacts. Where a message is is found as for a reply's parent (checkReplyParent).
func (h *Manager) checkReactTarget(ctx context.Context, api *feishubot.Client, set FeishuBotSettings, messageID string) error {
	if messageID == "" {
		return errors.New("feishu: a reaction needs a message")
	}
	if set.aiSource(messageID, h.bots.clock().UTC()) != nil {
		return nil
	}
	_, err := h.checkReplyParent(ctx, api, set, messageID)
	return err
}

// errNotBotDMMessage: a pin or a withdrawal named a message that is not the bot's own in the owner's private chat.
var errNotBotDMMessage = errors.New("feishu: only the bot's own messages in the owner's private chat can be pinned or withdrawn")

// checkOwnDMMessage verifies a message this app sent in the paired private chat. Owner-card receipts also require a live interactive message.
func checkOwnDMMessage(ctx context.Context, api *feishubot.Client, set FeishuBotSettings, messageID, msgType string) error {
	if messageID == "" || set.Owner == nil || set.Owner.ChatID == "" {
		return errNotBotDMMessage
	}
	items, err := api.Message(ctx, messageID)
	if err != nil {
		return feishuCoded(err)
	}
	if len(items) == 0 {
		return errNotBotDMMessage
	}
	it := items[0]
	if it.MessageID != messageID || it.ChatID != set.Owner.ChatID || it.Sender.SenderType != "app" || it.Sender.ID != api.AppID() {
		return errNotBotDMMessage
	}
	if msgType != "" && (it.Deleted || it.MsgType != msgType) {
		return errors.New("feishu: owner card receipt must identify a live interactive message")
	}
	return nil
}

// unpinBotOthers takes off the pins the bot made in chatID, but keep's; the owner's own pins stay.
func unpinBotOthers(ctx context.Context, api *feishubot.Client, chatID, keep string) error {
	pins, err := api.Pins(ctx, chatID)
	if err != nil {
		return err
	}
	for _, p := range pins {
		if p.MessageID == keep || !p.ByApp(api.AppID()) {
			continue
		}
		if err := api.Unpin(ctx, p.MessageID); err != nil {
			return err
		}
	}
	return nil
}

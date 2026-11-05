package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/channel"
	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/extension/host"
)

// Channels (docs/channels.md): every way of talking to Persona in one list — the apps, the Feishu bot, /agent in
// Feishu, voice, and the chat app bots plugins contribute (the channel extension point). What a channel does with its
// messages is the assistant's (internal/assistant, channels*.go), and so are its routes (/api/v1/channels…, relayed);
// the hub keeps the transport: the list as the plugins, connectors and the Feishu bot have it (channels.list), the
// switches, binding, and sending (assistant_channels.go).

// channelsHub is the plugins' channel programs as the hub reaches them.
type channelsHub struct {
	mu sync.Mutex
	// statuses: what the channels without a connector last said of themselves (channel.status)
	statuses map[string]*channel.StatusParams
	// call, when set, stands for the channels' programs (tests).
	call func(ctx context.Context, id, method string, params, out any) error
}

// pluginChannel is a channel a plugin contributes, as the registry has it now.
type extensionChannel struct {
	item      channel.Item
	extension *extension.Extension
}

// pluginChannels are the channels the installed plugins contribute (switched off ones too), by id.
func (h *Hub) extensionChannels() []extensionChannel {
	if h.extensions == nil {
		return nil
	}
	var out []extensionChannel
	for _, p := range h.extensions.reg.List() {
		for _, it := range p.Manifest.Channels() {
			out = append(out, extensionChannel{item: it, extension: p})
		}
	}
	return out
}

func (h *Hub) extensionChannel(id string) (extensionChannel, bool) {
	for _, pc := range h.extensionChannels() {
		if pc.item.ID == id {
			return pc, true
		}
	}
	return extensionChannel{}, false
}

// channelList is every channel: the apps, the hub's own, the plugins', then the markets' not installed.
func (h *Hub) channelList(ctx context.Context) []channel.View {
	out := []channel.View{appChannel(), h.feishuChannel(ctx)}
	if v, ok := h.voiceChannel(ctx); ok {
		out = append(out, v)
	}
	for _, pc := range h.extensionChannels() {
		out = append(out, h.extensionChannelView(ctx, pc))
	}
	out = append(out, h.marketChannels(out)...)
	order := func(v channel.View) int {
		switch {
		case v.Kind == channel.KindApp:
			return 0
		case v.State == channel.StateConnected:
			return 1
		case v.State == channel.StateNotInstalled:
			return 3
		}
		return 2
	}
	slices.SortStableFunc(out, func(a, b channel.View) int { return order(a) - order(b) })
	return out
}

// channelIDs are the ids of the channels there are (what a client_msg_id's prefix is matched against).
func (h *Hub) channelIDs() []string {
	ids := []string{channel.Feishu}
	for _, pc := range h.extensionChannels() {
		ids = append(ids, pc.item.ID)
	}
	return ids
}

func appChannel() channel.View {
	return channel.View{ID: channel.App, Name: "Persona", Kind: channel.KindApp, Source: channel.SourceBuiltin, Installed: true,
		Enabled: true, State: channel.StateConnected, Bind: channel.BindNone, ReadOnly: true,
		Description: "Persona 自己的各端：在哪一端说的话都进同一段对话，提问和批准在卡片上原地作答",
		Surfaces:    []string{"iPhone", "Mac", "Apple Watch", "Android", "网页", "Siri"},
		Capabilities: channel.Capabilities{ImagesIn: true, FilesIn: true, VoiceIn: true, Format: channel.FormatMarkdown, Asks: channel.AsksButtons,
			Push: true, Typing: true, Progress: true}}
}

// botAnswerPiece is how many runes one of the bot's cards holds (a card holds about 30 KB).
const botAnswerPiece = 6000

// feishuCapabilities are the Feishu bot's (docs/bots.md): cards for answers, progress and what is asked.
var feishuCapabilities = channel.Capabilities{ImagesIn: true, FilesIn: true, VoiceIn: true, Format: channel.FormatMarkdown, MaxText: botAnswerPiece,
	Asks: channel.AsksButtons, Push: true, Progress: true, Threads: true}

func (h *Hub) feishuChannel(ctx context.Context) channel.View {
	v := h.botView(ctx)
	out := channel.View{ID: channel.Feishu, Name: "飞书机器人", Kind: channel.KindIM, Source: channel.SourceBuiltin, Installed: true,
		Enabled: v.Enabled, Bind: channel.BindFields, Capabilities: feishuCapabilities,
		Description: "私聊飞书自建应用的机器人：消息进主对话，回答、进度和提问都是卡片"}
	switch {
	case !v.Enabled:
		out.State = channel.StateDisabled
	case v.Error != "":
		out.State, out.Error = channel.StateError, v.Error
	case !v.HasSecret:
		out.State, out.Error = channel.StateNotConnected, "还没填应用凭证"
	case v.Owner == nil:
		out.State, out.Error = channel.StateNotConnected, "还没配对：在 App 里拿配对码，发给机器人"
	case !v.Connected:
		out.State, out.Error = channel.StateError, "和飞书的长连接断了，正在重连"
	default:
		out.State = channel.StateConnected
	}
	if v.Owner != nil {
		out.Account = &channel.Account{Name: v.Owner.Name}
		if out.Account.Name == "" {
			out.Account.Name = "已配对"
		}
	}
	if v.LastEventAt > 0 {
		out.LastEventAt = time.Unix(v.LastEventAt, 0).UTC()
	}
	f := v.Features
	out.Features = []channel.Feature{
		{ID: "main", Title: "和 Persona 对话", Description: "消息发进主对话；/new 开新对话、/list 选最近的对话、/main 回主对话", On: f.Main},
		{ID: "cards", Title: "提问和批准", Description: "飞书在驱动的对话要回答 / 批准时发卡片", On: f.Cards},
		{ID: "all_cards", Title: "所有 Agent 的提问", Description: "各台电脑上所有 Agent 的提问和批准都发卡片", On: f.AllCards},
		{ID: "pushes", Title: "推送也发到飞书", Description: "推到手机的同时发一份；定时任务那段对话的回答也发", On: f.Pushes},
		{ID: "sync", Title: "同步主对话", Description: "主对话里不是从飞书发起的一轮结束时，把回答发到飞书", On: f.Sync},
		{ID: "agents", Title: "驱动 Agent", Description: "/agents 列出各台电脑上的 Agent，选一个之后的消息发给它", On: f.Agents},
	}
	return out
}

// voiceChannel is the first voice plugin, as a channel (it talks with its own model, so it does not use the hub's
// pipeline: listed, set up on its plugin's page).
func (h *Hub) voiceChannel(ctx context.Context) (channel.View, bool) {
	if h.extensions == nil {
		return channel.View{}, false
	}
	for _, p := range h.extensions.reg.List() {
		v := h.extensions.view(ctx, p)
		if v.Kind != "voice" {
			continue
		}
		out := channel.View{ID: "voice", Name: "语音", Kind: channel.KindVoice, Source: channel.SourcePlugin, Plugin: p.Record.ID, Installed: true,
			Enabled: p.Record.Enabled, Bind: channel.BindNone, ReadOnly: true, Description: "在 App 里和 Persona 语音对话；要做的事交给主对话",
			Capabilities: channel.Capabilities{VoiceIn: true, Format: channel.FormatPlain, Asks: channel.AsksNone}}
		switch v.Status {
		case extensionDisabled:
			out.State = channel.StateDisabled
		case host.StateRunning, host.StateStopped, host.StateStarting:
			out.State = channel.StateConnected
		default:
			out.State, out.Error = channel.StateError, v.Error
		}
		return out, true
	}
	return channel.View{}, false
}

func (h *Hub) extensionChannelView(ctx context.Context, pc extensionChannel) channel.View {
	it, p := pc.item, pc.extension
	caps := it.Capabilities.Normalized()
	out := channel.View{ID: it.ID, Name: it.Name, Kind: it.Kind, Source: channel.SourcePlugin, Plugin: p.Record.ID, Installed: true,
		Description: firstNonEmpty(it.Description, p.Manifest.Description), Capabilities: caps, Bind: channel.BindNone}
	on := p.Record.Enabled && p.Problem == ""
	var st connector.Status
	if it.Connector != "" {
		if c := h.connectors.Get(it.Connector); c != nil {
			on = on && h.connectors.Enabled(it.Connector)
			st = connector.StatusOf(ctx, c)
			if _, ok := connector.As[connector.Login](c); ok {
				out.Bind = it.Bind
				if out.Bind == "" {
					out.Bind = channel.BindFields
				}
			}
		} else {
			on = false
		}
	} else if s := h.channelStatus(it.ID); s != nil {
		st = connector.Status{State: string(s.State), Error: s.Error}
		if s.Account != nil {
			st.Account = s.Account.Name
		}
	}
	out.Enabled = on
	switch {
	case !p.Record.Enabled:
		out.State, out.Error = channel.StateDisabled, ""
	case p.Problem != "":
		out.State, out.Error = channel.StateError, p.Problem
	case !on:
		out.State = channel.StateDisabled
	case st.State == connector.StateOK || st.State == string(channel.StateConnected):
		out.State = channel.StateConnected
	case st.State == connector.StateNotSignedIn || st.State == string(channel.StateNotConnected) || st.State == "":
		out.State, out.Error = channel.StateNotConnected, st.Error
	default:
		out.State, out.Error = channel.StateError, st.Error
	}
	if st.Account != "" {
		out.Account = &channel.Account{Name: st.Account}
	}
	if !st.LastEvent.IsZero() {
		out.LastEventAt = st.LastEvent.UTC()
	}
	return out // its target and features are the assistant's
}

// marketChannels are the channels plugins in the markets would contribute, not installed (have lists the rest).
func (h *Hub) marketChannels(have []channel.View) []channel.View {
	if h.extensions == nil {
		return nil // no plugins (a test's hub): no markets either
	}
	var out []channel.View
	seen := map[string]bool{}
	for _, v := range have {
		seen[v.ID] = true
	}
	for _, it := range h.catalog("", "") {
		if it.Installed != "" || len(it.Contributes["channel"]) == 0 || it.Problem != "" {
			continue
		}
		for _, id := range it.Contributes["channel"] {
			if seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, channel.View{ID: id, Name: it.Name, Kind: channel.KindIM, Source: channel.SourceMarket, Plugin: it.ID, Market: it.Market,
				State: channel.StateNotInstalled, Bind: channel.BindNone, Description: it.Description})
		}
	}
	return out
}

// channelBindRoute is POST /api/v1/channels/{id}/bind: a step of binding (a plugin channel's connector signs in, the
// Feishu bot takes its credentials, then a pairing code). The channels' other routes are the assistant's; this one
// carries credentials and sign-in fields, which never pass through the assistant.
func (h *Hub) channelBindRoute(api *http.ServeMux) {
	api.HandleFunc("POST /api/v1/channels/{id}/bind", func(w http.ResponseWriter, r *http.Request) {
		if principalOf(r.Context()).Role != roleFull {
			writeErr(w, http.StatusForbidden, errors.New("this device can only read and answer"))
			return
		}
		id := r.PathValue("id")
		list := h.channelList(r.Context())
		i := slices.IndexFunc(list, func(v channel.View) bool { return v.ID == id })
		if i < 0 {
			writeErrCode(w, http.StatusNotFound, "not_found", errors.New("no such channel"))
			return
		}
		if v := list[i]; v.ReadOnly || v.Source == channel.SourceMarket {
			writeErrCode(w, http.StatusConflict, "read_only", fmt.Errorf("%s 不在这里设置", v.Name))
			return
		}
		var in struct {
			Step   string            `json:"step"`
			Values map[string]string `json:"values"`
		}
		if r.ContentLength != 0 {
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&in); err != nil {
				writeErr(w, http.StatusBadRequest, err)
				return
			}
		}
		p, err := h.bindChannel(r.Context(), id, in.Step, in.Values)
		if err != nil {
			writeErr(w, http.StatusBadGateway, err)
			return
		}
		if err := withQR(&p); err != nil {
			writeErr(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, p)
	})
}

// enableChannel switches a channel on or off (channels.enable): a plugin's program and its connector, the Feishu
// bot.
func (h *Hub) enableChannel(ctx context.Context, id string, on bool) error {
	if id == channel.Feishu {
		return h.updateBot(ctx, &on, nil, nil, nil, nil)
	}
	pc, ok := h.extensionChannel(id)
	if !ok {
		return fmt.Errorf("没有渠道 %s", id)
	}
	if on && !pc.extension.Record.Enabled {
		if err := h.extensions.reg.SetEnabled(ctx, pc.extension.Record.ID, true, actorOf(ctx)); err != nil {
			return err
		}
	}
	if pc.item.Connector != "" {
		return h.setConnectorEnabled(ctx, pc.item.Connector, on)
	}
	if !on {
		return h.extensions.reg.SetEnabled(ctx, pc.extension.Record.ID, false, actorOf(ctx))
	}
	return nil
}

// configureFeishu applies the assistant's changes to the bot's switches (feishu.configure): only what is given.
func (h *Hub) configureFeishu(ctx context.Context, enabled *bool, features map[string]bool) error {
	var fs *BotFeatures
	if len(features) > 0 {
		f := h.botFeatures()
		for id, on := range features {
			switch id {
			case "main":
				f.Main = on
			case "cards":
				f.Cards = on
			case "all_cards":
				f.AllCards = on
				if on {
					f.Cards = true
				}
			case "pushes":
				f.Pushes = on
			case "sync":
				f.Sync = on
			case "agents":
				f.Agents = on
			default:
				return fmt.Errorf("飞书机器人没有「%s」这个功能", id)
			}
		}
		fs = &f
	}
	return h.updateBot(ctx, enabled, nil, nil, nil, fs)
}

// actorOf is who changes a plugin from the channel's page, for the plugin's audit (the assistant relays the
// channels' routes: the device is not known here).
func actorOf(ctx context.Context) string {
	if p := principalOf(ctx); p.ID != "" {
		return "device:" + p.ID
	}
	return "hub token"
}

// PromptPairCode is a bind step that shows a code to send to the bot from the chat app (UserCode), asked again every
// Interval seconds until the answer is Done.
const PromptPairCode = "pair_code"

// bindChannel takes a step of binding: a plugin channel's connector signs in; the Feishu bot takes its credentials,
// then a pairing code.
func (h *Hub) bindChannel(ctx context.Context, id, step string, values map[string]string) (connector.Prompt, error) {
	if id == channel.Feishu {
		return h.bindFeishu(ctx, step, values)
	}
	pc, ok := h.extensionChannel(id)
	if !ok {
		return connector.Prompt{}, fmt.Errorf("没有渠道 %s", id)
	}
	v := channel.View{Name: pc.item.Name}
	if pc.item.Connector == "" {
		return connector.Prompt{}, fmt.Errorf("%s 不用绑定", v.Name)
	}
	c := h.connectors.Get(pc.item.Connector)
	if c == nil {
		return connector.Prompt{}, fmt.Errorf("%s 没在运行：先打开它", v.Name)
	}
	l, ok := connector.As[connector.Login](c)
	if !ok {
		return connector.Prompt{}, fmt.Errorf("%s 没有绑定这一步", v.Name)
	}
	if !h.connectors.Enabled(pc.item.Connector) {
		if err := h.setConnectorEnabled(ctx, pc.item.Connector, true); err != nil {
			return connector.Prompt{}, err
		}
	}
	return l.LoginStep(ctx, step, values)
}

func (h *Hub) bindFeishu(ctx context.Context, step string, values map[string]string) (connector.Prompt, error) {
	fields := []connector.Field{
		{Name: "app_id", Label: "App ID", Placeholder: "cli_…"},
		{Name: "app_secret", Label: "App Secret", Secret: true},
		{Name: "domain", Label: "域名（feishu / lark，或私有部署的地址；不填是 feishu）"},
	}
	if step == "credentials" && values != nil {
		id, secret, domain := strings.TrimSpace(values["app_id"]), strings.TrimSpace(values["app_secret"]), strings.TrimSpace(values["domain"])
		on := true
		if err := h.updateBot(ctx, &on, &id, &secret, &domain, nil); err != nil {
			return connector.Prompt{Step: "credentials", Fields: fields, Error: err.Error()}, nil
		}
		step = ""
	}
	v := h.botView(ctx)
	if !v.HasSecret {
		return connector.Prompt{Step: "credentials", Fields: fields,
			Hint: "在飞书开放平台建企业自建应用、开启机器人能力，订阅方式选「使用长连接接收事件」（docs/bots.md「在开放平台上要做的」）"}, nil
	}
	if !v.Enabled {
		on := true
		if err := h.updateBot(ctx, &on, nil, nil, nil, nil); err != nil {
			return connector.Prompt{}, err
		}
	}
	if v.Owner != nil {
		return connector.Prompt{Done: true, Account: firstNonEmpty(v.Owner.Name, "已配对")}, nil
	}
	b := &h.bots
	b.mu.Lock()
	h.loadBotsLocked(ctx)
	p := b.set.Pairing
	if p == nil || !b.clock().Before(p.Expires) || step == "" && values != nil {
		p = &botPairing{Code: botPairCode(), Expires: b.clock().Add(botPairFor)}
		b.set.Pairing = p
		h.saveBotSettingsLocked()
	}
	code := p.Code
	b.mu.Unlock()
	return connector.Prompt{Step: "pair", Kind: PromptPairCode, UserCode: code, Interval: 3,
		Hint: "在飞书里私聊机器人，把这 6 位数字发给它（10 分钟内有效）"}, nil
}

// unbindChannel undoes the binding: a plugin channel's connector signs out, the Feishu bot forgets its owner.
func (h *Hub) unbindChannel(ctx context.Context, id string) error {
	if id == channel.Feishu {
		h.unpairBot(ctx)
		return nil
	}
	pc, ok := h.extensionChannel(id)
	if !ok {
		return fmt.Errorf("没有渠道 %s", id)
	}
	v := channel.View{Name: pc.item.Name}
	if pc.item.Connector == "" {
		return fmt.Errorf("%s 没有绑定", v.Name)
	}
	c := h.connectors.Get(pc.item.Connector)
	if c == nil {
		return fmt.Errorf("%s 没在运行", v.Name)
	}
	l, ok := connector.As[connector.Login](c)
	if !ok {
		return fmt.Errorf("%s 没有绑定", v.Name)
	}
	return l.Logout(ctx) // the assistant forgets what the channel waited on
}

func firstNonEmpty(s ...string) string {
	for _, x := range s {
		if strings.TrimSpace(x) != "" {
			return x
		}
	}
	return ""
}

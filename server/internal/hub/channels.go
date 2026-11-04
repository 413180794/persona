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
	"github.com/nwverse/persona/server/internal/plugin"
	"github.com/nwverse/persona/server/internal/plugin/host"
)

// Channels (docs/channels.md): every way of talking to Persona in one list — the apps, the Feishu bot, /agent in
// Feishu, voice, and the chat app bots plugins contribute (the channel extension point). 设置 › 插件 › 渠道 lists them,
// says which are connected, and sets each up: on and off, binding, where its messages go, what it does.

// channelsKey holds each channel's settings the hub keeps for it: {"wechat": {"target": …, "features": …}}. The
// Feishu bot keeps its own (bots.feishu): this is for the channels the hub's pipeline runs.
const channelsKey = "channels"

type channelPrefs struct {
	Target   *channel.Target `json:"target,omitempty"`
	Features map[string]bool `json:"features,omitempty"`
	// Recent are the conversations its messages went to lately (newest first, a few).
	Recent []channel.Conversation `json:"recent,omitempty"`
}

const channelRecentKeep = 5

type channelsHub struct {
	mu     sync.Mutex
	loaded bool
	prefs  map[string]*channelPrefs
	// st is what the pipeline waits on (channels_runs.go), loaded apart from prefs.
	st       channelsState
	stLoaded bool
	live     map[string]*chanLive // run id -> its live progress (not persisted)
	// answered: conversation -> when its last run was answered (no "finished" push, no mirror for it)
	answered map[string]time.Time
	// statuses: what the channels without a connector last said of themselves (channel.status)
	statuses map[string]*channel.StatusParams
	now      func() time.Time // tests
	// call, when set, stands for the channels' programs (tests).
	call func(ctx context.Context, id, method string, params, out any) error
}

func (h *Hub) loadChannelsLocked(ctx context.Context) {
	c := &h.channels
	if c.loaded {
		return
	}
	c.prefs = map[string]*channelPrefs{}
	if h.store != nil {
		if _, err := h.store.Setting(ctx, channelsKey, &c.prefs); err != nil {
			h.logf("channels: load: %v", err)
			return // not marked loaded: the next look tries again
		}
	}
	if c.prefs == nil {
		c.prefs = map[string]*channelPrefs{}
	}
	c.loaded = true
}

func (h *Hub) saveChannelsLocked() {
	if h.store == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := h.store.PutSetting(ctx, channelsKey, h.channels.prefs); err != nil {
		h.logf("channels: save: %v", err)
	}
}

// channelPref is channel id's settings (created on first use; the caller holds h.channels.mu).
func (h *Hub) channelPrefLocked(id string) *channelPrefs {
	p := h.channels.prefs[id]
	if p == nil {
		p = &channelPrefs{}
		h.channels.prefs[id] = p
	}
	if p.Features == nil {
		p.Features = map[string]bool{}
		for k, v := range channel.DefaultFeatures {
			p.Features[k] = v
		}
	}
	return p
}

// pluginChannel is a channel a plugin contributes, as the registry has it now.
type pluginChannel struct {
	item   channel.Item
	plugin *plugin.Plugin
}

// pluginChannels are the channels the installed plugins contribute (switched off ones too), by id.
func (h *Hub) pluginChannels() []pluginChannel {
	if h.plugins == nil {
		return nil
	}
	var out []pluginChannel
	for _, p := range h.plugins.reg.List() {
		for _, it := range p.Manifest.Channels() {
			out = append(out, pluginChannel{item: it, plugin: p})
		}
	}
	return out
}

func (h *Hub) pluginChannel(id string) (pluginChannel, bool) {
	for _, pc := range h.pluginChannels() {
		if pc.item.ID == id {
			return pc, true
		}
	}
	return pluginChannel{}, false
}

// channelList is every channel: the apps, the hub's own, the plugins', then the markets' not installed.
func (h *Hub) channelList(ctx context.Context) []channel.View {
	out := []channel.View{appChannel(), h.feishuChannel(ctx)}
	if v, ok := h.voiceChannel(ctx); ok {
		out = append(out, v)
	}
	for _, pc := range h.pluginChannels() {
		out = append(out, h.pluginChannelView(ctx, pc))
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
	for _, pc := range h.pluginChannels() {
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
	b := &h.bots
	b.mu.Lock()
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
		t.Title = v.Target
	}
	out.Target = &t
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
	if h.plugins == nil {
		return channel.View{}, false
	}
	for _, p := range h.plugins.reg.List() {
		v := h.plugins.view(ctx, p)
		if v.Kind != "voice" {
			continue
		}
		out := channel.View{ID: "voice", Name: "语音", Kind: channel.KindVoice, Source: channel.SourcePlugin, Plugin: p.Record.ID, Installed: true,
			Enabled: p.Record.Enabled, Bind: channel.BindNone, ReadOnly: true, Description: "在 App 里和 Persona 语音对话；要做的事交给主对话",
			Capabilities: channel.Capabilities{VoiceIn: true, Format: channel.FormatPlain, Asks: channel.AsksNone}}
		switch v.Status {
		case pluginDisabled:
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

func (h *Hub) pluginChannelView(ctx context.Context, pc pluginChannel) channel.View {
	it, p := pc.item, pc.plugin
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
	c := &h.channels
	c.mu.Lock()
	h.loadChannelsLocked(ctx)
	pref := h.channelPrefLocked(it.ID)
	t := channel.Target{Kind: channel.TargetMain}
	if pref.Target != nil {
		t = *pref.Target
	}
	features := channel.PluginFeatures(caps, pref.Features)
	c.mu.Unlock()
	if t.Kind == channel.TargetConversation && t.Title == "" {
		t.Title = h.conversationTitle(ctx, t.MachineID, t.SessionID)
	}
	out.Target, out.Features = &t, features
	return out
}

// conversationTitle is how a conversation is called in the channel's settings.
func (h *Hub) conversationTitle(ctx context.Context, machine, session string) string {
	m := h.localMachine(machine)
	if m == nil {
		return ""
	}
	s, ok := m.session(session)
	if !ok {
		return ""
	}
	return conversationName(s, h.assistantConfig(ctx).Main)
}

// marketChannels are the channels plugins in the markets would contribute, not installed (have lists the rest).
func (h *Hub) marketChannels(have []channel.View) []channel.View {
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

func (h *Hub) channelView(ctx context.Context, id string) (channel.View, bool) {
	for _, v := range h.channelList(ctx) {
		if v.ID == id {
			return v, true
		}
	}
	return channel.View{}, false
}

func (h *Hub) channelDetail(ctx context.Context, id string) (channel.Detail, bool) {
	v, ok := h.channelView(ctx, id)
	if !ok {
		return channel.Detail{}, false
	}
	d := channel.Detail{View: v}
	switch id {
	case channel.Feishu:
		d.SettingsURL = "settings/connectors/feishu-bot"
		d.Waiting = h.channelWaiting(id)
		b := &h.bots
		b.mu.Lock()
		d.Waiting.Asks = len(b.cards)
		b.mu.Unlock()
	case channel.App:
	default:
		if v.Source == channel.SourcePlugin && v.Plugin != "" {
			d.SettingsURL = "settings/plugins/" + v.Plugin
		}
		d.Waiting = h.channelWaiting(id)
	}
	c := &h.channels
	c.mu.Lock()
	h.loadChannelsLocked(ctx)
	if p := c.prefs[id]; p != nil {
		d.Conversations = slices.Clone(p.Recent)
	}
	c.mu.Unlock()
	for i := range d.Conversations {
		if t := h.conversationTitle(ctx, d.Conversations[i].MachineID, d.Conversations[i].SessionID); t != "" {
			d.Conversations[i].Title = t
		}
	}
	return d, true
}

// noteChannelConversation remembers that channel id's message went to a conversation (the detail's recent list).
func (h *Hub) noteChannelConversation(id, machine, session string) {
	c := &h.channels
	c.mu.Lock()
	defer c.mu.Unlock()
	h.loadChannelsLocked(context.Background())
	p := h.channelPrefLocked(id)
	p.Recent = slices.DeleteFunc(p.Recent, func(x channel.Conversation) bool { return x.MachineID == machine && x.SessionID == session })
	p.Recent = append([]channel.Conversation{{MachineID: machine, SessionID: session, At: time.Now().UTC()}}, p.Recent...)
	if len(p.Recent) > channelRecentKeep {
		p.Recent = p.Recent[:channelRecentKeep]
	}
	h.saveChannelsLocked()
}

// channelsOfConversations are, by conversation, the channels messages came into it from (newest first): what the
// conversation list draws next to a title.
func (h *Hub) channelsOfConversations() map[[2]string][]string {
	c := &h.channels
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.loaded {
		return nil // loaded at start (startBots): not here, on every list of sessions
	}
	out := map[[2]string][]string{}
	type mark struct {
		id string
		at time.Time
	}
	marks := map[[2]string][]mark{}
	for id, p := range c.prefs {
		for _, r := range p.Recent {
			k := [2]string{r.MachineID, r.SessionID}
			marks[k] = append(marks[k], mark{id, r.At})
		}
	}
	for k, ms := range marks {
		slices.SortFunc(ms, func(a, b mark) int { return b.at.Compare(a.at) })
		for _, m := range ms {
			out[k] = append(out[k], m.id)
		}
	}
	return out
}

// --- the app's interface ---

func (h *Hub) channelRoutes(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/channels", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"channels": h.channelList(r.Context())})
	})
	api.HandleFunc("GET /api/v1/channels/{id}", func(w http.ResponseWriter, r *http.Request) {
		d, ok := h.channelDetail(r.Context(), r.PathValue("id"))
		if !ok {
			writeErrCode(w, http.StatusNotFound, "not_found", errors.New("no such channel"))
			return
		}
		writeJSON(w, d)
	})
	full := func(f func(w http.ResponseWriter, r *http.Request, v channel.View)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if principalOf(r.Context()).Role != roleFull {
				writeErr(w, http.StatusForbidden, errors.New("this device can only read and answer"))
				return
			}
			v, ok := h.channelView(r.Context(), r.PathValue("id"))
			if !ok {
				writeErrCode(w, http.StatusNotFound, "not_found", errors.New("no such channel"))
				return
			}
			if v.ReadOnly || v.Source == channel.SourceMarket {
				writeErrCode(w, http.StatusConflict, "read_only", fmt.Errorf("%s 不在这里设置", v.Name))
				return
			}
			f(w, r, v)
		}
	}
	api.HandleFunc("PUT /api/v1/channels/{id}", full(func(w http.ResponseWriter, r *http.Request, v channel.View) {
		var in struct {
			Enabled  *bool           `json:"enabled"`
			Target   *channel.Target `json:"target"`
			Features map[string]bool `json:"features"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&in); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		if err := h.updateChannel(r.Context(), v, in.Enabled, in.Target, in.Features); err != nil {
			writePluginErr(w, http.StatusBadRequest, "invalid", err)
			return
		}
		d, _ := h.channelDetail(r.Context(), v.ID)
		writeJSON(w, d)
	}))
	api.HandleFunc("POST /api/v1/channels/{id}/bind", full(func(w http.ResponseWriter, r *http.Request, v channel.View) {
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
		p, err := h.bindChannel(r.Context(), v, in.Step, in.Values)
		if err != nil {
			writeErr(w, http.StatusBadGateway, err)
			return
		}
		if err := withQR(&p); err != nil {
			writeErr(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, p)
	}))
	api.HandleFunc("DELETE /api/v1/channels/{id}/binding", full(func(w http.ResponseWriter, r *http.Request, v channel.View) {
		if err := h.unbindChannel(r.Context(), v); err != nil {
			writeErr(w, http.StatusBadGateway, err)
			return
		}
		d, _ := h.channelDetail(r.Context(), v.ID)
		writeJSON(w, d)
	}))
	api.HandleFunc("POST /api/v1/channels/{id}/test", full(func(w http.ResponseWriter, r *http.Request, v channel.View) {
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		if err := h.testChannel(ctx, v); err != nil {
			writeErr(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	}))
}

// updateChannel applies the app's changes: only what is given.
func (h *Hub) updateChannel(ctx context.Context, v channel.View, enabled *bool, target *channel.Target, features map[string]bool) error {
	if target != nil {
		if err := target.Check(); err != nil {
			return err
		}
		if target.Kind == channel.TargetConversation {
			ok := false
			if m := h.localMachine(target.MachineID); m != nil {
				s, found := m.session(target.SessionID)
				ok = found && isAssistant(s, m.assistantCwd())
			}
			if !ok {
				return errors.New("只能选助手的对话")
			}
		}
	}
	known := map[string]bool{}
	for _, f := range v.Features {
		known[f.ID] = true
	}
	for id := range features {
		if !known[id] {
			return fmt.Errorf("%s 没有「%s」这个功能", v.Name, id)
		}
	}
	if v.ID == channel.Feishu {
		return h.updateFeishuChannel(ctx, enabled, target, features)
	}
	pc, ok := h.pluginChannel(v.ID)
	if !ok {
		return fmt.Errorf("%s 不在这里设置", v.Name)
	}
	if enabled != nil {
		if *enabled && !pc.plugin.Record.Enabled {
			if err := h.plugins.reg.SetEnabled(ctx, pc.plugin.Record.ID, true, actorOf(ctx)); err != nil {
				return err
			}
		}
		if pc.item.Connector != "" {
			if err := h.setConnectorEnabled(ctx, pc.item.Connector, *enabled); err != nil {
				return err
			}
		} else if !*enabled {
			if err := h.plugins.reg.SetEnabled(ctx, pc.plugin.Record.ID, false, actorOf(ctx)); err != nil {
				return err
			}
		}
	}
	c := &h.channels
	c.mu.Lock()
	defer c.mu.Unlock()
	h.loadChannelsLocked(ctx)
	p := h.channelPrefLocked(v.ID)
	if target != nil {
		t := *target
		t.Title = ""
		p.Target = &t
	}
	for id, on := range features {
		p.Features[id] = on
		if id == channel.FeatureAllAsks && on {
			p.Features[channel.FeatureAsks] = true
		}
	}
	h.saveChannelsLocked()
	return nil
}

// updateFeishuChannel maps the channel's settings onto the Feishu bot's own.
func (h *Hub) updateFeishuChannel(ctx context.Context, enabled *bool, target *channel.Target, features map[string]bool) error {
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
			}
		}
		fs = &f
	}
	if err := h.updateBot(ctx, enabled, nil, nil, nil, fs); err != nil {
		return err
	}
	if target != nil {
		t := botTarget{Kind: targetMain}
		switch target.Kind {
		case channel.TargetNew:
			t = botTarget{Kind: targetJarvis}
		case channel.TargetConversation:
			t = botTarget{Kind: targetJarvis, MachineID: target.MachineID, SessionID: target.SessionID}
		}
		b := &h.bots
		b.mu.Lock()
		h.loadBotsLocked(ctx)
		b.st.Current = t
		h.saveBotStateLocked()
		b.mu.Unlock()
	}
	return nil
}

// actorOf is who changes a plugin from the channel's page, for the plugin's audit.
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
func (h *Hub) bindChannel(ctx context.Context, v channel.View, step string, values map[string]string) (connector.Prompt, error) {
	if v.ID == channel.Feishu {
		return h.bindFeishu(ctx, step, values)
	}
	pc, ok := h.pluginChannel(v.ID)
	if !ok || pc.item.Connector == "" {
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
func (h *Hub) unbindChannel(ctx context.Context, v channel.View) error {
	if v.ID == channel.Feishu {
		h.unpairBot(ctx)
		return nil
	}
	pc, ok := h.pluginChannel(v.ID)
	if !ok || pc.item.Connector == "" {
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
	if err := l.Logout(ctx); err != nil {
		return err
	}
	h.forgetChannelTurns(v.ID)
	return nil
}

// testChannel sends the owner (only them) a line, to see the channel reaches them.
func (h *Hub) testChannel(ctx context.Context, v channel.View) error {
	if v.State != channel.StateConnected {
		return fmt.Errorf("%s 还没连上", v.Name)
	}
	if v.ID == channel.Feishu {
		return h.testFeishuBot(ctx)
	}
	return h.channelTell(ctx, v.ID, "", "这是来自 Persona 的测试消息：这个渠道能联系到你。", channel.OutNote)
}

func firstNonEmpty(s ...string) string {
	for _, x := range s {
		if strings.TrimSpace(x) != "" {
			return x
		}
	}
	return ""
}

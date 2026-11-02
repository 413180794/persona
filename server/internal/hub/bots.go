package hub

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/chyroc/jarvis/server/internal/feishubot"
)

// The bot channel (docs/bots.md): the user talks to Jarvis through a chat app's bot — a Feishu / Lark custom app.
// A message to the bot goes into Jarvis' main conversation (or the conversation / coding agent the chat is on now) and
// the answer comes back as a card; Jarvis' questions and approvals can be answered there, its pushes can go there too,
// and agents on every computer can be driven from it. Only the paired user is answered, only in the bot's private
// chat. Everything is off until switched on in the app (设置 › 连接器 › 飞书机器人).

// BotsConfig is hub.json "bots".
type BotsConfig struct {
	Feishu FeishuBotConfig `json:"feishu,omitempty"`
}

// FeishuBotConfig: the app's credentials, for those who would rather keep them in hub.json (0600) than enter them in
// the app. What the app saves wins.
type FeishuBotConfig struct {
	AppID     string `json:"app_id,omitempty"`
	AppSecret string `json:"app_secret,omitempty"`
	Domain    string `json:"domain,omitempty"` // feishu (default) | lark | the open platform's URL
}

// FeishuBotSettings is what the app set, in settings (botSettingsKey).
type FeishuBotSettings struct {
	Enabled   bool        `json:"enabled"`
	AppID     string      `json:"app_id,omitempty"`
	SecretEnc string      `json:"secret_enc,omitempty"` // sealed with secret.key (launchers.go sealKey)
	Domain    string      `json:"domain,omitempty"`
	Features  BotFeatures `json:"features"`
	Owner     *BotOwner   `json:"owner,omitempty"`
	Pairing   *botPairing `json:"pairing,omitempty"`
}

// BotFeatures are the parts that can be switched on one by one.
type BotFeatures struct {
	Main     bool `json:"main"`      // messages go to Jarvis: the main conversation, unless the chat switched to another
	Cards    bool `json:"cards"`     // questions and approvals of what the chat drives show there as cards
	AllCards bool `json:"all_cards"` // and those of every agent and conversation
	Pushes   bool `json:"pushes"`    // notify_user and scheduled tasks' results go there too
	Sync     bool `json:"sync"`      // the main conversation's turns from elsewhere (the app, a task) are mirrored there
	Agents   bool `json:"agents"`    // coding agents on every computer can be driven from the chat
}

var defaultBotFeatures = BotFeatures{Main: true, Cards: true}

// BotOwner is the paired user: the only one the bot answers, and where pushes go.
type BotOwner struct {
	OpenID string    `json:"open_id"`
	ChatID string    `json:"chat_id"`
	Name   string    `json:"name,omitempty"`
	At     time.Time `json:"at"`
}

type botPairing struct {
	Code    string    `json:"code"`
	Expires time.Time `json:"expires"`
	Fails   int       `json:"fails,omitempty"` // wrong codes sent meanwhile; botPairTries end it
}

const (
	botSettingsKey = "bots.feishu"
	botStateKey    = "bots.feishu.state"
	botPairFor     = 10 * time.Minute
	botPairTries   = 5
)

// botTarget is where a message goes: Jarvis' main conversation (whichever it is when the message is sent), another
// Jarvis conversation, or a coding agent.
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

// botState is the chat's state, in settings (botStateKey): where messages go, the threads bound to targets, the turns
// being answered (so a restart still delivers their answers), and the messages already handled.
type botState struct {
	Current botTarget             `json:"current"`
	Threads map[string]*botThread `json:"threads,omitempty"`
	Runs    []*botRun             `json:"runs,omitempty"`
	Seen    []string              `json:"seen,omitempty"`
}

const (
	botSeenKeep    = 300
	botThreadsKeep = 100
)

type botHub struct {
	mu     sync.Mutex
	loaded bool
	set    FeishuBotSettings
	st     botState
	api    *feishubot.Client
	conn   *feishubot.Conn
	stop   context.CancelFunc
	info   feishubot.BotInfo
	base   context.Context
	cards  map[string]botCard      // interaction id -> the card asking it
	told   map[string]time.Time    // sender -> when the bot last told them it is not theirs / not paired
	names  map[string]string       // open id -> name
	live   map[string]*botProgress // run id -> its live progress (not persisted)
	// answered: conversation -> when its last run was answered (no "finished" push, no mirror for it)
	answered map[string]time.Time
	// now and post are replaced in tests.
	now func() time.Time
}

type botCard struct {
	MessageID string
	Root      string
}

func (b *botHub) clock() time.Time {
	if b.now != nil {
		return b.now()
	}
	return time.Now()
}

// loadBotsLocked reads the settings and the state once; b.mu is held.
func (h *Hub) loadBotsLocked(ctx context.Context) {
	b := &h.bots
	if b.loaded {
		return
	}
	b.loaded = true
	b.set = FeishuBotSettings{Features: defaultBotFeatures}
	if h.store != nil {
		if _, err := h.store.Setting(ctx, botSettingsKey, &b.set); err != nil {
			h.logf("bots: load settings: %v", err)
		}
		if _, err := h.store.Setting(ctx, botStateKey, &b.st); err != nil {
			h.logf("bots: load state: %v", err)
		}
	}
	if b.st.Threads == nil {
		b.st.Threads = map[string]*botThread{}
	}
	if b.st.Current.Kind == "" {
		b.st.Current = botTarget{Kind: targetMain}
	}
}

func (h *Hub) saveBotSettingsLocked() {
	if h.store == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.store.PutSetting(ctx, botSettingsKey, h.bots.set); err != nil {
		h.logf("bots: save settings: %v", err)
	}
}

func (h *Hub) saveBotStateLocked() {
	b := &h.bots
	if len(b.st.Seen) > botSeenKeep {
		b.st.Seen = b.st.Seen[len(b.st.Seen)-botSeenKeep:]
	}
	if len(b.st.Threads) > botThreadsKeep {
		var oldest string
		for len(b.st.Threads) > botThreadsKeep {
			oldest = ""
			for id, t := range b.st.Threads {
				if oldest == "" || t.At.Before(b.st.Threads[oldest].At) {
					oldest = id
				}
			}
			delete(b.st.Threads, oldest)
		}
	}
	if h.store == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.store.PutSetting(ctx, botStateKey, b.st); err != nil {
		h.logf("bots: save state: %v", err)
	}
}

// botCredentials: the app's id, secret and domain, from the app's settings, else hub.json.
func (h *Hub) botCredentialsLocked() (appID, secret, domain string, fromConfig bool, err error) {
	s := h.bots.set
	if s.AppID != "" && s.SecretEnc != "" {
		secret, err = h.openKey(s.SecretEnc)
		return s.AppID, secret, s.Domain, false, err
	}
	c := h.cfg.Bots.Feishu
	if c.AppID != "" && c.AppSecret != "" {
		return c.AppID, c.AppSecret, c.Domain, true, nil
	}
	return "", "", "", false, nil
}

// startBots connects the bot when it is switched on (at start, and after the settings change).
func (h *Hub) startBots(ctx context.Context) {
	h.bots.mu.Lock()
	h.bots.base = ctx
	h.loadBotsLocked(ctx)
	h.bots.mu.Unlock()
	h.restartBot()
	go h.botLoop(ctx)
}

// restartBot drops the connection and, if the bot is on and configured, opens a new one.
func (h *Hub) restartBot() {
	b := &h.bots
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.stop != nil {
		b.stop()
		b.stop, b.conn = nil, nil
	}
	if !b.set.Enabled || b.base == nil {
		b.api = nil
		return
	}
	appID, secret, domain, _, err := h.botCredentialsLocked()
	if err != nil {
		h.logf("bots: open the app secret: %v", err)
		return
	}
	if appID == "" {
		b.api = nil
		return
	}
	b.api = feishubot.New(appID, secret, domain)
	b.conn = feishubot.NewConn(b.api, h.botEvent, h.logf)
	ctx, cancel := context.WithCancel(b.base)
	b.stop = cancel
	conn, api := b.conn, b.api
	go conn.Run(ctx)
	go func() {
		ictx, icancel := context.WithTimeout(ctx, 15*time.Second)
		defer icancel()
		info, err := api.BotInfo(ictx)
		if err != nil {
			h.logf("bots: bot info: %v", err)
			return
		}
		b.mu.Lock()
		if b.api == api {
			b.info = info
		}
		b.mu.Unlock()
	}()
	// Turns that were being answered when the hub stopped: watch them again.
	for _, r := range b.st.Runs {
		go h.watchRun(r)
	}
}

// botAPI is the client while the bot is on and paired (nil otherwise), with the settings it runs under (a copy).
func (h *Hub) botAPI() (*feishubot.Client, FeishuBotSettings) {
	b := &h.bots
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.loaded {
		h.loadBotsLocked(context.Background())
	}
	set := b.set.copy()
	if b.api == nil || !b.set.Enabled || b.set.Owner == nil {
		return nil, set
	}
	return b.api, set
}

// copy is the settings with nothing shared: the owner is changed in place under h.bots.mu.
func (s FeishuBotSettings) copy() FeishuBotSettings {
	if s.Owner != nil {
		o := *s.Owner
		s.Owner = &o
	}
	if s.Pairing != nil {
		p := *s.Pairing
		s.Pairing = &p
	}
	return s
}

// --- the app's settings page ---

type feishuBotView struct {
	Enabled     bool           `json:"enabled"`
	AppID       string         `json:"app_id"`
	Domain      string         `json:"domain"`
	HasSecret   bool           `json:"has_secret"`
	FromConfig  bool           `json:"from_config"` // the credentials are hub.json's
	Features    BotFeatures    `json:"features"`
	Connected   bool           `json:"connected"`
	Error       string         `json:"error,omitempty"`
	LastEventAt int64          `json:"last_event_at,omitempty"`
	BotName     string         `json:"bot_name,omitempty"`
	Owner       *botOwnerView  `json:"owner,omitempty"`
	Pairing     *botPairView   `json:"pairing,omitempty"`
	Target      string         `json:"target,omitempty"` // where the chat's messages go now
	Agents      []botAgentView `json:"agents,omitempty"` // agents bound to threads of the chat
}

type botOwnerView struct {
	Name     string `json:"name"`
	PairedAt int64  `json:"paired_at"`
}

type botPairView struct {
	Code      string `json:"code"`
	ExpiresAt int64  `json:"expires_at"`
}

type botAgentView struct {
	Title     string `json:"title"`
	MachineID string `json:"machine_id"`
	SessionID string `json:"session_id"`
}

func (h *Hub) botView(ctx context.Context) feishuBotView {
	b := &h.bots
	b.mu.Lock()
	h.loadBotsLocked(ctx)
	s := b.set
	appID, _, domain, fromConfig, _ := h.botCredentialsLocked()
	v := feishuBotView{Enabled: s.Enabled, AppID: appID, Domain: feishubot.ResolveDomain(domain), HasSecret: appID != "",
		FromConfig: fromConfig, Features: s.Features, BotName: b.info.Name}
	if b.conn != nil {
		st := b.conn.State()
		v.Connected, v.Error = st.Connected, st.Error
		if !st.LastEvent.IsZero() {
			v.LastEventAt = st.LastEvent.Unix()
		}
	} else if s.Enabled && appID == "" {
		v.Error = "还没填应用凭证"
	}
	if s.Owner != nil {
		v.Owner = &botOwnerView{Name: s.Owner.Name, PairedAt: s.Owner.At.Unix()}
	}
	if p := s.Pairing; p != nil && b.clock().Before(p.Expires) {
		v.Pairing = &botPairView{Code: p.Code, ExpiresAt: p.Expires.Unix()}
	}
	cur := b.st.Current
	for _, t := range b.st.Threads {
		if t.Target.agent() {
			v.Agents = append(v.Agents, botAgentView{Title: t.Title, MachineID: t.Target.MachineID, SessionID: t.Target.SessionID})
		}
	}
	b.mu.Unlock()
	v.Target = h.targetLabel(ctx, cur)
	return v
}

func (h *Hub) botRoutes(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/bots/feishu", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, h.botView(r.Context()))
	})
	api.HandleFunc("PUT /api/v1/bots/feishu", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Enabled   *bool        `json:"enabled"`
			AppID     *string      `json:"app_id"`
			AppSecret *string      `json:"app_secret"`
			Domain    *string      `json:"domain"`
			Features  *BotFeatures `json:"features"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&in); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		if err := h.updateBot(r.Context(), in.Enabled, in.AppID, in.AppSecret, in.Domain, in.Features); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, h.botView(r.Context()))
	})
	// pair: a code the user sends the bot from Feishu within 10 minutes; whoever sends it becomes the owner.
	api.HandleFunc("POST /api/v1/bots/feishu/pair", func(w http.ResponseWriter, r *http.Request) {
		b := &h.bots
		b.mu.Lock()
		h.loadBotsLocked(r.Context())
		n, _ := rand.Int(rand.Reader, big.NewInt(900000))
		b.set.Pairing = &botPairing{Code: fmt.Sprintf("%06d", 100000+n.Int64()), Expires: b.clock().Add(botPairFor)}
		h.saveBotSettingsLocked()
		b.mu.Unlock()
		writeJSON(w, h.botView(r.Context()))
	})
	api.HandleFunc("DELETE /api/v1/bots/feishu/owner", func(w http.ResponseWriter, r *http.Request) {
		b := &h.bots
		b.mu.Lock()
		h.loadBotsLocked(r.Context())
		b.set.Owner, b.set.Pairing = nil, nil
		b.st = botState{Current: botTarget{Kind: targetMain}, Threads: map[string]*botThread{}, Seen: b.st.Seen}
		h.saveBotSettingsLocked()
		h.saveBotStateLocked()
		b.mu.Unlock()
		writeJSON(w, h.botView(r.Context()))
	})
	// test: a message to the owner, to see that the bot reaches them.
	api.HandleFunc("POST /api/v1/bots/feishu/test", func(w http.ResponseWriter, r *http.Request) {
		api, set := h.botAPI()
		if api == nil {
			writeErr(w, http.StatusConflict, errors.New("机器人没打开或还没配对"))
			return
		}
		card := feishubot.NewCard("", "", "测试消息").Markdown("这是来自 Jarvis 的测试消息：机器人能联系到你。")
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		if _, err := h.botPost(ctx, api, set.Owner.ChatID, "", card, ""); err != nil {
			writeErr(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})
}

// updateBot applies the app's changes; new credentials are tried (the bot's info) before they are saved.
func (h *Hub) updateBot(ctx context.Context, enabled *bool, appID, secret, domain *string, features *BotFeatures) error {
	b := &h.bots
	b.mu.Lock()
	h.loadBotsLocked(ctx)
	next := b.set.copy()
	b.mu.Unlock()
	reconnect := false
	creds := false
	if appID != nil || secret != nil || domain != nil {
		id, dom := next.AppID, next.Domain
		if appID != nil {
			id = strings.TrimSpace(*appID)
		}
		if domain != nil {
			dom = strings.TrimSpace(*domain)
		}
		var plain string
		switch {
		case secret != nil && strings.TrimSpace(*secret) != "":
			plain = strings.TrimSpace(*secret)
		case next.SecretEnc != "":
			var err error
			if plain, err = h.openKey(next.SecretEnc); err != nil {
				return err
			}
		}
		if id == "" {
			// Cleared: back to hub.json's, if any.
			next.AppID, next.SecretEnc, next.Domain = "", "", ""
		} else {
			if plain == "" {
				return errors.New("需要 App Secret")
			}
			vctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			_, err := feishubot.New(id, plain, dom).BotInfo(vctx)
			cancel()
			if err != nil {
				return fmt.Errorf("连不上这个应用：%w（检查 App ID / App Secret，并在开放平台给应用开启机器人能力）", err)
			}
			sealed, err := h.sealKey(plain)
			if err != nil {
				return err
			}
			next.AppID, next.SecretEnc, next.Domain = id, sealed, dom
		}
		reconnect, creds = true, true
	}
	b.mu.Lock()
	// Onto the settings as they are now: the credentials were checked over the network meanwhile, and a pairing may
	// have come in.
	cur := &b.set
	if creds {
		if next.AppID != cur.AppID {
			cur.Owner, cur.Pairing = nil, nil
		}
		cur.AppID, cur.SecretEnc, cur.Domain = next.AppID, next.SecretEnc, next.Domain
	}
	if enabled != nil && *enabled != cur.Enabled {
		cur.Enabled = *enabled
		reconnect = true
	}
	if features != nil {
		cur.Features = *features
	}
	h.saveBotSettingsLocked()
	b.mu.Unlock()
	if reconnect {
		h.restartBot()
	}
	return nil
}

// botLoop sweeps: turns whose end was missed, pairings that expired.
func (h *Hub) botLoop(ctx context.Context) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		h.sweepBotRuns(ctx)
	}
}

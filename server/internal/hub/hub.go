// Package hub serves the iPhone / Watch apps and coordinates the nodes on every machine.
package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chyroc/jarvis/server/internal/agents"
	"github.com/chyroc/jarvis/server/internal/ai"
	"github.com/chyroc/jarvis/server/internal/connector"
	"github.com/chyroc/jarvis/server/internal/db"
	"github.com/chyroc/jarvis/server/internal/lark"
	"github.com/chyroc/jarvis/server/internal/mail"
	"github.com/chyroc/jarvis/server/internal/prompts"
	"github.com/chyroc/jarvis/server/internal/proto"
	"github.com/chyroc/jarvis/server/internal/rpc"
	"github.com/chyroc/jarvis/server/internal/screen"
	"github.com/chyroc/jarvis/server/internal/update"
)

type Hub struct {
	cfg      *Config
	version  string
	build    update.Build // channel, build number, where updates come from (docs/hub-update.md)
	upd      *updater     // updating itself to the publisher's release (selfupdate.go)
	logger   *log.Logger
	machines []*Machine
	byID     map[string]*Machine
	mail     Mail
	push     *Pusher
	store    Store      // nil in unit tests: nothing is persisted
	modelMu  sync.Mutex // serializes legacy writes and test stores without a database CAS
	ai       *ai.Client
	// The inbox's own models (inbox_agent.go): 1 triages, 2 decides what the agent (3) should take on.
	triageAI, judgeAI *ai.Client
	translations      translateCache // translate.go
	quotas            quotaCache     // launcherquota.go
	lark              *lark.Client
	agents            *agents.Service
	larkLastEvent     atomic.Int64 // Unix seconds of the last realtime event from lark-connector (设置 › 连接器 › 飞书)

	terms terminals // the terminal tab's live streams (terminal.go)

	tailcat tailcatDialer // reaching machines the hub cannot ssh to (tailcat.go)

	engines engineChoices // each machine's terminal engine (engine.go)

	mutesMu sync.Mutex
	mutes   *Pins // muted spaces / agents (no pushes); loaded from settings on first use

	// sessionsSent: each machine's last session list sent to the apps, the whole message (broadcastSessions).
	sessionsMu   sync.Mutex
	sessionsSent map[string][]byte

	mu            sync.Mutex
	clients       map[*client]struct{}
	interactions  map[string]*pending
	phoneTools    []proto.Tool
	calls         map[string]chan proto.ToolResult
	seen          map[[2]string]time.Time // (machine, session) -> when the user last looked at it
	stateSince    map[[2]string]stateMark // (machine, session) -> its state and when it entered it
	userTZ        string                  // the phone's time zone (assistant settings)
	snap          *prompts.Snapshot       // the assistant's last live snapshot (assistant_snapshot.go)
	snapAt        time.Time
	snapping      chan struct{}        // closed when the snapshot being taken is in
	wokeAt        time.Time            // last silent push to wake the phone for a tool call
	notified      map[string]time.Time // assistant conversation -> when it last pushed with notify_user
	kbAgentLogged map[string]time.Time // agent -> when its result last went into a project timeline (kb.go)
	turns         *turnTracker         // "machine/session" -> the turn in progress, for the finished push (notify.go)
	live          *liveActivities      // the lock screen / Dynamic Island activity (liveactivity.go)
	usage         usageBook            // token usage of every machine (usage.go)
	metrics       metricsBook          // OS and load of every machine (metrics.go)

	// codexAnnounced: Codex card id -> when it was announced (codexcards.go)
	codexAnnounced map[string]time.Time

	sched  schedule  // the assistant's scheduled tasks (assistant_schedule.go)
	titles titleBook // assistant conversations' titles (assistant_titles.go)
	ports  portOpens // ports the user opened in the app (ports.go)
	// archived and relays: archived assistant conversations, and messages between them (assistant_conversations.go).
	archived archiveBook
	relays   relayBook
	inbox    inbox   // the unified inbox (inbox.go)
	kb       kbState // the knowledge base under ~/.jarvis/data (kb.go)
	// larkAgent: /agent requests from Feishu and what became of them (lark_agent.go).
	larkAgent larkAgents
	// launchers: 设置 › 模型 (launchers.go); launchersSeen is every machine's last ~/.zshrc list.
	launchers     launcherState
	launchersSeen map[string][]proto.Launcher
	devtok        devTokens // device token lookups (devicetokens.go)
	sends         sendLog   // messages being / just sent, by client_msg_id (send.go)
	plugins       *plugins  // processes that add to the app, relayed under /api/v1/plugins (plugins.go)

	connectors *connector.Registry // sources of the user's information (connectors.go)
}

// Mail is implemented by internal/mail; kept as an interface so the hub can run without it.
type Mail interface {
	Handler() http.Handler
	Tools() []proto.Tool
	Call(ctx context.Context, name string, args json.RawMessage, approve func(ctx context.Context, summary string, input json.RawMessage) bool) proto.ToolResult
	ListAccounts() ([]mail.MailAccount, error)
	ListMessages(ctx context.Context, opts mail.ListOptions) (*mail.ListResult, error)
	GetMessage(ctx context.Context, accountID string, uid uint32, mailbox string) (*mail.MailMessage, error)
	Send(ctx context.Context, req mail.SendRequest) error
	// AddAccount connects with the settings and saves the account when IMAP and SMTP work; DeleteAccount removes one
	// (jarvis_settings mail.accounts, self_settings.go).
	AddAccount(req mail.AddAccountRequest) (*mail.MailAccount, error)
	DeleteAccount(id string) error
}

// NewMail is set by the mail integration (see mail_glue.go).
var NewMail func(dataDir string, store Store, google GoogleConfig) (Mail, error)

// Store is the hub's persistence (internal/db on Postgres).
type Store interface {
	ClaimSend(ctx context.Context, id, machine, session string) (string, error)
	SettleSend(ctx context.Context, id, state string) error
	ReplacePhoneTools(ctx context.Context, tools []proto.Tool) error
	PhoneTools(ctx context.Context) ([]proto.Tool, error)
	UpsertDevice(ctx context.Context, d db.Device) error
	Devices(ctx context.Context) ([]db.Device, error)
	DeleteDevice(ctx context.Context, id string) error
	SetDevicePrefs(ctx context.Context, id string, prefs json.RawMessage) error
	LiveActivities(ctx context.Context) ([]db.LiveActivity, error)
	SaveLiveActivity(ctx context.Context, v db.LiveActivity) error
	InsertInteraction(ctx context.Context, ix proto.Interaction) error
	FinishInteraction(ctx context.Context, id, status string, res *proto.Resolution) error
	InteractionOutcome(ctx context.Context, id string) (status string, res *proto.Resolution, err error)
	Interactions(ctx context.Context, machine, session string, limit int) ([]db.InteractionRecord, error)
	PutNodeSession(ctx context.Context, machine, id string, s db.NodeSession) error
	NodeSessions(ctx context.Context, machine string) (map[string]db.NodeSession, error)
	MailAccounts(ctx context.Context) ([]db.MailAccountRow, error)
	ReplaceMailAccounts(ctx context.Context, rows []db.MailAccountRow) error
	MarkSeen(ctx context.Context, machine, session string, at time.Time) error
	SeenTimes(ctx context.Context) (map[[2]string]time.Time, error)
	Setting(ctx context.Context, key string, out any) (bool, error)
	PutSetting(ctx context.Context, key string, v any) error
	CreateDeviceToken(ctx context.Context, t db.DeviceToken, hash string) error
	DeviceTokenByHash(ctx context.Context, hash string) (*db.DeviceToken, error)
	DeviceTokens(ctx context.Context) ([]db.DeviceToken, error)
	RevokeDeviceToken(ctx context.Context, id string) (bool, error)
	TouchDeviceToken(ctx context.Context, id string) error
	PutInboxItem(ctx context.Context, id string, data []byte, updatedAt time.Time, doneAt *time.Time) error
	InboxItems(ctx context.Context, doneSince time.Time) ([][]byte, error)
}

type pending struct {
	ix   proto.Interaction
	done chan proto.Resolution
	// local is set for interactions the hub resolves itself (screen dialogs): it acts on the answer, and an error
	// goes back to whoever answered (prompt_changed: nothing was pressed).
	local func(proto.Resolution) error
	// orphaned is set while no hook waits on a resumable interaction (its node went away, or the hub restarted): it
	// ends the interaction unless the hook claims it again in time.
	orphaned *time.Timer
	// checkedAt / checking bound transcript reads for a plan answered outside Jarvis.
	checkedAt time.Time
	checking  bool
	// codex: a card a node listed for a Codex TUI (codexcards.go); it lasts as long as the node lists it.
	codex bool
}

// resumeGrace is how long a resumable interaction outlives its node: a little longer than hooks keep reconnecting
// (node.ResumeWait).
var resumeGrace = 3 * time.Minute

func Run(ctx context.Context, b update.Build) error {
	logger := log.New(os.Stderr, "", log.LstdFlags)
	// An update that keeps failing goes back before anything else can fail, the config included (selfupdate.go).
	updating := newUpdater(&Hub{build: b, version: b.Version, logger: logger}).boot()
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	version := b.Version
	h := &Hub{
		cfg: cfg, version: version, build: b, logger: logger,
		byID: map[string]*Machine{}, clients: map[*client]struct{}{}, interactions: map[string]*pending{},
		calls: map[string]chan proto.ToolResult{}, notified: map[string]time.Time{}, turns: newTurnTracker(),
	}
	h.live = newLiveActivities(h)
	h.upd = newUpdater(h)
	h.upd.confirmed = !updating // an update under way is confirmed by this process (its watchdog waits for that)
	database, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer database.Close()
	h.store = database
	if resumable, err := database.ExpireStaleInteractions(ctx); err != nil {
		h.logf("expire stale interactions: %v", err)
	} else {
		// Hooks waiting on these reconnect to the next node and claim them again (waitInteraction).
		h.mu.Lock()
		for _, ix := range resumable {
			h.interactions[ix.ID] = &pending{ix: ix, done: make(chan proto.Resolution, 1)}
			h.orphanLocked(h.interactions[ix.ID])
		}
		h.mu.Unlock()
	}
	h.importLegacy(ctx)
	h.loadSeen(ctx)
	h.loadPhoneTools()
	if NewMail != nil {
		if h.mail, err = NewMail(cfg.dir, h.store, cfg.Google); err != nil {
			h.logf("mail disabled: %v", err)
		}
	}
	h.push = newPusher(cfg.dir, h.store, h.logf)
	h.push.desktopOnline = h.desktopOnline
	h.push.desktopOpen = h.desktopOpen
	h.ai = ai.New(cfg.AI)
	if c := cfg.Inbox.TriageAI; c != nil {
		h.triageAI = ai.New(c.Over(cfg.AI))
	}
	if c := cfg.Inbox.JudgeAI; c != nil {
		h.judgeAI = ai.New(c.Over(cfg.AI))
	}
	h.lark = lark.New(cfg.Lark)
	h.inbox.larkAccount = h.lark.Account()
	h.initAgents()
	h.connectors = connector.NewRegistry(cfg.ConnectorGate)
	for _, mc := range cfg.Machines {
		m := newMachine(h, mc)
		h.machines = append(h.machines, m)
		h.byID[mc.ID] = m
		go m.run(ctx)
	}
	defer h.closeTailcat()
	go h.expireLoop(ctx)
	go h.terminalKeepalive(ctx)
	go h.live.run(ctx)
	go h.scheduleLoop(ctx)
	go h.larkEvents(ctx)
	go h.larkAgentLoop(ctx)
	go h.agentsLoop(ctx)
	go h.inboxLoop(ctx)
	go h.metricsLoop(ctx)
	h.connectors.Watch(ctx, h.connectorChanged, h.logf)
	h.upd.start(ctx)
	h.startKB(ctx, cfg.KB, cfg.dir)
	h.plugins = newPlugins(cfg.dir, cfg.Plugins, h.logf)
	h.startPlugins(ctx)

	srv := &http.Server{Addr: cfg.Listen, Handler: h.routes()}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	h.logf("jarvisd hub %s listening on %s (%s)", version, cfg.Listen, cfg.PublicURL)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (h *Hub) logf(format string, args ...any) { h.logger.Printf(format, args...) }

func (h *Hub) machineList() []proto.Machine {
	out := make([]proto.Machine, 0, len(h.machines))
	for _, m := range h.machines {
		out = append(out, m.snapshot())
	}
	return out
}

func (h *Hub) broadcastMachines() {
	h.broadcast(map[string]any{"type": "machines", "machines": h.machineList()})
}

// decorated returns a machine's sessions with pending interaction counts filled in.
func (h *Hub) decorated(m *Machine) []proto.Session {
	list := m.sessionList()
	h.mu.Lock()
	counts := map[string]int{}
	for _, p := range h.interactions {
		if p.ix.MachineID == m.cfg.ID {
			counts[p.ix.SessionID]++
		}
	}
	h.mu.Unlock()
	dir := m.assistantCwd()
	h.mu.Lock()
	seen := make(map[string]time.Time, len(list))
	for _, s := range list {
		if t, ok := h.seen[[2]string{m.cfg.ID, s.SessionID}]; ok {
			seen[s.SessionID] = t
		}
	}
	h.mu.Unlock()
	titles, named := h.agentConversationTitles(), h.conversationTitles()
	archived, relayed := h.archivedIDs(), h.queuedRelays()
	for i := range list {
		list[i].PendingInteractions = counts[list[i].SessionID]
		list[i].Assistant = isAssistant(list[i].Cwd, dir)
		if t := titles[list[i].SessionID]; t != "" && list[i].Assistant {
			list[i].Title = t // not the first line of the prompt the hub wrote for it
		} else if t := named[list[i].SessionID]; t != "" && list[i].Assistant {
			list[i].Title = t // 「MMDD | 类型 | 主题」 or the user's own (assistant_titles.go)
		}
		if list[i].Assistant {
			list[i].Archived = archived[list[i].SessionID]
			// Messages from other conversations wait in the hub while it runs: shown like typed-ahead input.
			if q := relayed[list[i].SessionID]; len(q) > 0 {
				list[i].Queued = append(slices.Clip(list[i].Queued), q...) // the node's slice is shared: never write into it
			}
		}
		list[i].SeenAt = seen[list[i].SessionID]
		list[i].State = agentState(list[i], list[i].SeenAt)
	}
	h.markStates(m.cfg.ID, list, time.Now())
	return list
}

func (h *Hub) sessionsChanged(m *Machine) {
	list := h.decorated(m)
	h.broadcastSessions(m, list)
	h.syncScreenInteractions(m, list)
	h.syncPlanInteractions(m)
}

// syncScreenInteractions turns blocked agents without a hook-driven interaction into "screen" interactions
// (folder trust, model pickers, any TUI dialog hooks cannot see), and clears them once the agent moves on.
func (h *Hub) syncScreenInteractions(m *Machine, list []proto.Session) {
	blocked := map[string]proto.Session{}
	for _, s := range list {
		// A session without a terminal (a source whose capabilities leave it out: Codex app-server, an agent runtime)
		// has no screen to read a dialog off: it asks through its own cards, and blocked there means it waits in its
		// own UI (docs/runtimes.md).
		if s.Status == proto.StatusBlocked && (s.SourceKind == "" || s.Capabilities != nil && s.Capabilities.Terminal) {
			blocked[s.SessionID] = s
		}
	}
	h.mu.Lock()
	var stale []string
	hasHook := map[string]bool{}
	for id, p := range h.interactions {
		if p.ix.MachineID != m.cfg.ID {
			continue
		}
		if p.ix.Kind == proto.IxScreen {
			if _, still := blocked[p.ix.SessionID]; !still {
				stale = append(stale, id)
			}
			hasHook[p.ix.SessionID] = true
		} else if coversScreen(p) {
			hasHook[p.ix.SessionID] = true
		}
	}
	h.mu.Unlock()
	for _, id := range stale {
		h.finishInteraction(id)
	}
	for sid, s := range blocked {
		if hasHook[sid] {
			continue
		}
		go h.createScreenInteraction(m, s)
	}
}

func (h *Hub) createScreenInteraction(m *Machine, s proto.Session) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Hook dialogs flip the agent to blocked a moment before the hook reaches us; give them a head start. A Codex
	// node reads its approvals off the screen once a second (codexcards.go): a little longer.
	wait := 1500 * time.Millisecond
	if s.Agent == "codex" || s.Agent == "traex" {
		wait = 3 * time.Second
	}
	time.Sleep(wait)
	if cur, ok := m.session(s.SessionID); !ok || cur.Status != proto.StatusBlocked {
		return
	}
	var scr struct {
		Text string `json:"text"`
	}
	if err := m.call(ctx, "session.screen", map[string]any{"session_id": s.SessionID, "lines": 40}, &scr); err != nil {
		return
	}
	d := screen.ParseDialog(scr.Text)
	id := "scr_" + m.cfg.ID + "_" + s.SessionID
	if d != nil {
		id += "_" + d.Rev // another dialog is another card: nothing picked on the last one carries over
	}
	ix := proto.Interaction{
		ID: id, MachineID: m.cfg.ID, SessionID: s.SessionID, SessionTitle: s.Title, Cwd: s.Cwd, Kind: proto.IxScreen,
		CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().Add(24 * time.Hour).UTC(), Screen: trimScreen(scr.Text), Dialog: d,
		Secret: screen.SecretPrompt(scr.Text),
	}
	h.mu.Lock()
	asking := false
	for _, p := range h.interactions {
		if p.ix.MachineID == m.cfg.ID && p.ix.SessionID == s.SessionID {
			if p.ix.Kind == proto.IxScreen || coversScreen(p) {
				h.mu.Unlock()
				return
			}
			asking = true
		}
	}
	if asking && d == nil { // only the async question's input box on screen: its card says it all
		h.mu.Unlock()
		return
	}
	h.interactions[id] = &pending{ix: ix, local: func(r proto.Resolution) error {
		if r.Dismiss {
			return nil
		}
		params, err := screenAnswer(s.SessionID, d, r)
		if err != nil {
			return err
		}
		kctx, kcancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer kcancel()
		err = m.call(kctx, "session.keys", params, nil)
		var re *rpc.Error
		if errors.As(err, &re) && re.Code == screen.PromptChanged {
			go h.createScreenInteraction(m, s) // the card for what is on screen now
		}
		return err
	}}
	h.mu.Unlock()
	h.record(ix)
	h.announce(ix)
}

// coversScreen: the interaction stands for what the terminal waits on (a hook's, a Codex approval or overlay
// question), so no screen card is made beside it. A Codex async question does not: Codex shows no dialog for it,
// though herdr calls the agent blocked while it is open (codexcards.go).
func coversScreen(p *pending) bool { return !(p.codex && p.ix.ToolName == "request_user_input_async") }

// screenAnswer turns an answer to a screen card into session.keys parameters. With a dialog the node checks its
// revision before pressing anything; an option's keys come from the node's fresh reading of the screen, and the
// ones worked out here are for nodes that predate that.
func screenAnswer(session string, d *screen.Dialog, r proto.Resolution) (map[string]any, error) {
	params := map[string]any{"session_id": session, "keys": r.Keys, "text": r.Text, "pre_keys": r.PreKeys}
	if d == nil {
		return params, nil
	}
	params["rev"] = d.Rev
	if picked := d.Option(r.Option); d.Multi && !(len(r.Options) == 0 && picked != nil && picked.Kind == screen.KindChat) {
		// Checked and sent by the node one key at a time (node/dialogmulti.go); "Chat about this" is pressed as it is.
		// An app from before multi-select cards names one option: that one alone (the free-text field: the text alone).
		options := r.Options
		if len(options) == 0 && r.Option != "" {
			if o := d.Option(r.Option); o != nil && o.Kind == "" {
				options = []string{o.Key}
			}
		}
		for _, k := range options {
			if o := d.Option(k); o == nil || o.Kind != "" {
				return nil, fmt.Errorf("the dialog has no option %q to check", k)
			}
		}
		if len(options) == 0 && strings.TrimSpace(r.Text) == "" {
			return nil, fmt.Errorf("至少勾一项，或者写上自己的回答")
		}
		return map[string]any{"session_id": session, "rev": d.Rev, "multi": true, "options": options, "free_text": r.Text}, nil
	}
	if r.Option == "" {
		return params, nil
	}
	o := d.Option(r.Option)
	if o == nil {
		return nil, fmt.Errorf("the dialog has no option %q", r.Option)
	}
	params["option"] = o.Key
	if o.Kind == screen.KindTypeSomething {
		params["pre_keys"], params["keys"] = o.Keys, []string{"enter"}
	} else {
		params["keys"], params["text"], params["pre_keys"] = o.Keys, "", nil
	}
	return params, nil
}

func trimScreen(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n "), "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	return strings.Join(lines, "\n")
}

func (h *Hub) announce(ix proto.Interaction) {
	h.broadcast(map[string]any{"type": "interaction", "interaction": ix})
	if m := h.byID[ix.MachineID]; m != nil {
		h.broadcastSessions(m, h.decorated(m))
	}
	if !h.muted(ix.MachineID, ix.SessionID) {
		h.push.notifyInteraction(ix, h.machineName(ix.MachineID))
	}
}

func (h *Hub) machineName(id string) string {
	if m := h.byID[id]; m != nil {
		return m.cfg.Name
	}
	return id
}

// waitInteraction registers a hook interaction and blocks until it is resolved, cancelled or expires.
// A resumable one asked again with the same id (its hook reconnected after a hub or node restart) is the same
// interaction: the wait resumes on it, or returns the answer given meanwhile.
func (h *Hub) waitInteraction(ctx context.Context, ix proto.Interaction) (proto.Resolution, error) {
	if ix.ExpiresAt.IsZero() {
		ix.ExpiresAt = time.Now().Add(time.Duration(h.cfg.HookTimeoutMinutes) * time.Minute).UTC()
	}
	if ix.Resumable {
		h.mu.Lock()
		if p := h.interactions[ix.ID]; p != nil && p.local == nil {
			if p.orphaned != nil {
				p.orphaned.Stop()
				p.orphaned = nil
			}
			h.mu.Unlock()
			return h.await(ctx, p)
		}
		h.mu.Unlock()
		if r, ended := h.outcome(ix.ID); ended {
			return r, nil
		}
	}
	p := &pending{ix: ix, done: make(chan proto.Resolution, 1)}
	h.mu.Lock()
	// A screen interaction for the same session was a premature guess; the hook knows better.
	for id, old := range h.interactions {
		if old.ix.Kind == proto.IxScreen && old.ix.MachineID == ix.MachineID && old.ix.SessionID == ix.SessionID {
			delete(h.interactions, id)
			defer h.broadcast(map[string]any{"type": "interaction_resolved", "id": id})
		}
	}
	h.interactions[ix.ID] = p
	h.mu.Unlock()
	h.record(ix)
	h.announce(ix)
	return h.await(ctx, p)
}

// await blocks on a registered interaction. When ctx ends — the node's connection is gone — a resumable
// interaction stays pending for its hook to claim again.
func (h *Hub) await(ctx context.Context, p *pending) (proto.Resolution, error) {
	id := p.ix.ID
	timer := time.NewTimer(time.Until(p.ix.ExpiresAt))
	defer timer.Stop()
	select {
	case r := <-p.done:
		h.finishInteraction(id)
		return r, nil
	case <-timer.C:
		h.recordEnd(id, "expired", nil)
		h.finishInteraction(id)
		return proto.Resolution{Dismiss: true}, nil
	case <-ctx.Done():
		if p.ix.Resumable {
			h.mu.Lock()
			kept := h.interactions[id] == p && h.orphanLocked(p)
			h.mu.Unlock()
			if kept {
				return proto.Resolution{Dismiss: true}, nil // nobody reads it: the node is gone
			}
		}
		h.recordEnd(id, "cancelled", nil)
		h.finishInteraction(id)
		return proto.Resolution{Dismiss: true}, nil
	}
}

// orphanLocked starts p's grace period: it ends (cancelled) unless its hook claims it again first. Callers hold h.mu.
func (h *Hub) orphanLocked(p *pending) bool {
	if p.orphaned != nil {
		return true
	}
	wait := min(resumeGrace, time.Until(p.ix.ExpiresAt))
	if wait <= 0 {
		return false
	}
	p.orphaned = time.AfterFunc(wait, func() {
		h.mu.Lock()
		still := h.interactions[p.ix.ID] == p && p.orphaned != nil
		h.mu.Unlock()
		if still {
			h.recordEnd(p.ix.ID, "cancelled", nil)
			h.finishInteraction(p.ix.ID)
		}
	})
	return true
}

// outcome reports how a resumable interaction no longer pending here ended: the answer given before its hook could
// read it (the hub or node restarted right then), or a dismiss when it was cancelled or expired.
func (h *Hub) outcome(id string) (proto.Resolution, bool) {
	if h.store == nil {
		return proto.Resolution{}, false
	}
	status, res, err := h.store.InteractionOutcome(context.Background(), id)
	switch {
	case err != nil || status == "" || status == "pending":
		return proto.Resolution{}, false
	case status == "resolved" && res != nil:
		return *res, true
	default:
		return proto.Resolution{Dismiss: true}, true
	}
}

// record / recordEnd keep the interaction history in the database (best effort).
func (h *Hub) record(ix proto.Interaction) {
	if h.store == nil {
		return
	}
	if err := h.store.InsertInteraction(context.Background(), ix); err != nil {
		h.logf("record interaction: %v", err)
	}
}

func (h *Hub) recordEnd(id, status string, r *proto.Resolution) {
	if h.store == nil {
		return
	}
	if err := h.store.FinishInteraction(context.Background(), id, status, r); err != nil {
		h.logf("record interaction outcome: %v", err)
	}
}

// errNotPending: the interaction was answered, withdrawn or expired already.
var errNotPending = errors.New("no longer pending")

// resolve answers an interaction from the app. A screen card acts right away and its error comes back (the
// dialog changed: nothing pressed, a new card follows).
func (h *Hub) resolve(id string, r proto.Resolution) error {
	h.mu.Lock()
	p := h.interactions[id]
	if p != nil && r.Allow != nil && *r.Allow {
		for _, f := range p.ix.Fields {
			if !f.Optional && strings.TrimSpace(r.Values[f.Key]) == "" {
				h.mu.Unlock()
				return fmt.Errorf("请先填写%s", f.Label)
			}
		}
	}
	if p != nil && p.local != nil {
		delete(h.interactions, id) // claimed: a second answer to the same card is not pending any more
	}
	h.mu.Unlock()
	if p == nil {
		return fmt.Errorf("interaction %s is %w", id, errNotPending)
	}
	status := "resolved"
	if r.Dismiss {
		status = "dismissed"
	}
	if p.local != nil {
		err := p.local(r)
		if err != nil {
			status = "failed"
		}
		// What was typed into a terminal may be a password: the history keeps that something was, not what.
		kept := redacted(p.ix, r)
		if kept.Text != "" {
			kept.Text = "<redacted>"
		}
		h.recordEnd(id, status, &kept)
		h.finished(p)
		return err
	}
	kept := redacted(p.ix, r)
	h.recordEnd(id, status, &kept)
	select {
	case p.done <- r:
	default:
	}
	h.finishInteraction(id)
	return nil
}

// redacted is the resolution as the history keeps it: secret fields say they were filled in, not what with.
func redacted(ix proto.Interaction, r proto.Resolution) proto.Resolution {
	if len(r.Values) == 0 {
		return r
	}
	secret := map[string]bool{}
	for _, f := range ix.Fields {
		secret[f.Key] = f.Secret
	}
	vals := make(map[string]string, len(r.Values))
	for k, v := range r.Values {
		if secret[k] {
			v = "<redacted>"
		}
		vals[k] = v
	}
	r.Values = vals
	return r
}

func (h *Hub) finishInteraction(id string) {
	h.mu.Lock()
	p, ok := h.interactions[id]
	delete(h.interactions, id)
	if ok && p.orphaned != nil {
		p.orphaned.Stop()
	}
	h.mu.Unlock()
	if ok {
		h.finished(p)
	}
}

// finished tells everyone an interaction, already out of the pending set, is over.
func (h *Hub) finished(p *pending) {
	id := p.ix.ID
	h.broadcast(map[string]any{"type": "interaction_resolved", "id": id})
	if m := h.byID[p.ix.MachineID]; m != nil {
		h.broadcastSessions(m, h.decorated(m))
	}
	h.push.clear(id)
}

func (h *Hub) cancelMachineInteractions(machineID string) {
	h.mu.Lock()
	var ids []string
	for id, p := range h.interactions {
		// Resumable ones wait for their hook to come back through the next node (await).
		if p.ix.MachineID == machineID && !p.ix.Resumable {
			ids = append(ids, id)
		}
	}
	h.mu.Unlock()
	for _, id := range ids {
		h.finishInteraction(id)
	}
}

func (h *Hub) pendingInteractions() []proto.Interaction {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := []proto.Interaction{}
	for _, p := range h.interactions {
		out = append(out, p.ix)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

func (h *Hub) expireLoop(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		h.mu.Lock()
		var plans []*Machine
		seen := map[string]bool{}
		for _, p := range h.interactions {
			if p.ix.Kind == proto.IxPlan && !seen[p.ix.MachineID] {
				if m := h.byID[p.ix.MachineID]; m != nil {
					plans = append(plans, m)
					seen[p.ix.MachineID] = true
				}
			}
		}
		h.mu.Unlock()
		for _, m := range plans {
			h.syncPlanInteractions(m) // also converge after a lost rev/status event
		}
		h.mu.Lock()
		var ids []string
		for id, p := range h.interactions {
			if p.local != nil && time.Now().After(p.ix.ExpiresAt) {
				ids = append(ids, id)
			}
		}
		h.mu.Unlock()
		for _, id := range ids {
			h.finishInteraction(id)
		}
	}
}

// ---- MCP tools ----

func (h *Hub) loadPhoneTools() {
	if h.store == nil {
		return
	}
	tools, err := h.store.PhoneTools(context.Background())
	if err != nil {
		h.logf("load phone tools: %v", err)
		return
	}
	h.mu.Lock()
	h.phoneTools = tools
	h.mu.Unlock()
}

func (h *Hub) setPhoneTools(tools []proto.Tool) {
	h.mu.Lock()
	h.phoneTools = tools
	h.mu.Unlock()
	if h.store != nil {
		if err := h.store.ReplacePhoneTools(context.Background(), tools); err != nil {
			h.logf("save phone tools: %v", err)
		}
	}
}

// mcpTools are the tools of an assistant conversation or an agent: the hub's own, then (the assistant's only) those
// of every connector that is ok.
func (h *Hub) mcpTools(assistant bool) []proto.Tool {
	tools := h.mcpBuiltinTools(assistant)
	if assistant {
		tools = append(tools, h.connectors.Tools(context.Background())...)
	}
	return tools
}

func (h *Hub) mcpBuiltinTools(assistant bool) []proto.Tool {
	h.mu.Lock()
	tools := append([]proto.Tool(nil), h.phoneTools...)
	h.mu.Unlock()
	if h.mail != nil {
		tools = append(tools, h.mail.Tools()...)
	}
	if assistant {
		tools = append(tools, assistantTools(h.lark != nil)...)
	}
	return tools
}

func (h *Hub) callTool(ctx context.Context, machineID, name string, args json.RawMessage) proto.ToolResult {
	switch {
	case strings.HasPrefix(name, "phone_"):
		return h.callPhone(ctx, name, args)
	case strings.HasPrefix(name, "mail_") && h.mail != nil:
		return h.mail.Call(ctx, name, args, func(ctx context.Context, summary string, input json.RawMessage) bool {
			return h.approve(ctx, machineID, name, summary, input)
		})
	}
	return proto.TextResult("unknown tool "+name, true)
}

// approve asks the user on the phone to allow a sensitive tool call (e.g. sending an email).
func (h *Hub) approve(ctx context.Context, machineID, tool, summary string, input json.RawMessage) bool {
	ix := proto.Interaction{
		ID: "ix_" + randomHex(6), MachineID: machineID, Kind: proto.IxPermission, ToolName: tool, Summary: summary,
		ToolInput: input, SessionTitle: "Jarvis", CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().Add(15 * time.Minute).UTC(),
	}
	r, _ := h.waitInteraction(ctx, ix)
	return r.Allow != nil && *r.Allow
}

func (h *Hub) callPhone(ctx context.Context, name string, args json.RawMessage) proto.ToolResult {
	c := h.phoneClient()
	if c == nil {
		c = h.wakePhone(ctx)
	}
	if c == nil {
		return proto.TextResult("The user's iPhone is not connected and did not wake up in the background: ask them to open the Jarvis app, then retry.", true)
	}
	id := randomHex(8)
	ch := make(chan proto.ToolResult, 1)
	h.mu.Lock()
	h.calls[id] = ch
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.calls, id)
		h.mu.Unlock()
	}()
	c.sendJSON(map[string]any{"type": "tool_call", "call_id": id, "name": name, "args": args})
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	select {
	case r := <-ch:
		return r
	case <-ctx.Done():
		return proto.TextResult("Timed out waiting for the iPhone (is the Jarvis app still open?).", true)
	}
}

// wakePhone asks iOS to wake the app in the background (silent push) and waits briefly for it to connect.
func (h *Hub) wakePhone(ctx context.Context) *client {
	if !h.push.ready() {
		return nil
	}
	h.mu.Lock()
	recent := time.Since(h.wokeAt) < 30*time.Second
	if !recent {
		h.wokeAt = time.Now()
	}
	h.mu.Unlock()
	if !recent {
		h.push.wake("tool")
	}
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	t := time.NewTicker(300 * time.Millisecond)
	defer t.Stop()
	for {
		if c := h.phoneClient(); c != nil {
			h.logf("phone woke up for a tool call in %d ms", time.Since(start).Milliseconds())
			return c
		}
		select {
		case <-ctx.Done():
			h.logf("phone did not wake up for a tool call within 20 s")
			return nil
		case <-t.C:
		}
	}
}

func (h *Hub) toolResult(id string, r proto.ToolResult) {
	h.mu.Lock()
	ch := h.calls[id]
	h.mu.Unlock()
	if ch != nil {
		select {
		case ch <- r:
		default:
		}
	}
}

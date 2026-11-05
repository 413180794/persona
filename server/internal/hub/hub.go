// Package hub serves the iPhone / Watch apps and coordinates the nodes on every machine.
package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nwverse/persona/server/internal/agents"
	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/mail"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
	"github.com/nwverse/persona/server/internal/screen"
	"github.com/nwverse/persona/server/internal/update"
)

type Hub struct {
	deployCtx     context.Context // canceled with the hub; accepted node updates resume from Postgres
	nodeWorkers   sync.WaitGroup
	shutdown      *shutdownBudget
	wsStopping    bool // guarded by mu; prevents new WaitGroup additions during shutdown
	wsHandlers    sync.WaitGroup
	wsConnections map[*client]struct{} // includes connections still registering their device session
	relay         relayState           // the connections relayed to the manager: Persona Link (devices_relay.go)
	linkPing      [2]time.Duration     // tests: heartbeat interval and timeout of new links
	bus           busState             // the services' connections and the roster (bus.go)
	// tests: replaces os.Hostname
	hostname func() (string, error)

	cfg         *Config
	version     string
	build       update.Build // channel, build number, where updates come from (docs/hub-update.md)
	upd         *updater     // updating itself to the publisher's release (selfupdate.go)
	logger      *log.Logger
	machines    []*Machine
	byID        map[string]*Machine
	mail        Mail
	push        *Pusher
	agentWork   agentWork  // where each agent stands in its work, as the manager reports it (agents_link.go)
	undelivered heldActs   // answers to the manager's cards given while it was away (agents_link.go)
	store       Store      // nil in unit tests: nothing is persisted
	workspaceMu sync.Mutex // serializes read-only workspace discovery and snapshot commits
	agents      *agents.Service
	lockTests   lockTestWake // wakes the lock test dispatcher when a test is scheduled (locktest.go)

	terms terminals  // the terminal tab's live streams (terminal.go)
	fulls fullRelays // the web's full herdr views (herdrclient.go)

	devices devicesState // the manager's machines as it reports them (devices_link.go)

	mutesMu sync.Mutex
	mutes   *Pins // muted spaces / agents (no pushes); loaded from settings on first use

	// sessionsSent: each machine's last session list sent to the apps, as encoded (broadcastSessions).
	sessionsMu   sync.Mutex
	sessionsSent map[string][]sentSession

	mu      sync.Mutex
	clients map[*client]struct{}
	events  *eventLog // what was broadcast lately, numbered (eventlog.go); guarded by mu
	// stopping: a planned stop has begun (beginStop, docs/hub-restart.md): what the stop itself causes (machines going
	// offline, their questions ending) is not news for the apps, which reconnect to the next hub in a moment.
	stopping atomic.Bool
	// deltas: the live text of each session's message being generated, as last relayed (machine, session -> text): a
	// client subscribing in the middle of it gets it after the backlog instead of waiting for the next token.
	deltas       map[[2]string]string
	interactions map[string]*pending
	phoneTools   map[string]db.PhoneToolRegistration
	calls        map[string]chan proto.ToolResult
	seen         map[[2]string]time.Time // (machine, session) -> when the user last looked at it
	stateSince   map[[2]string]stateMark // (machine, session) -> its state and when it entered it
	wokeAt       map[string]time.Time    // device -> last silent push to wake it for a tool call
	notified     map[string]time.Time    // assistant conversation -> when it last pushed with notify_user
	turns        *turnTracker            // "machine/session" -> the turn in progress, for the finished push (notify.go)
	live         *liveActivities         // the lock screen / Dynamic Island activity (liveactivity.go)
	usage        usageBook               // token usage of every machine (usage.go)
	metrics      metricsBook             // OS and load of every machine (metrics.go)

	// codexAnnounced: Codex card id -> when it was announced (codexcards.go)
	codexAnnounced map[string]time.Time

	ports portOpens // ports the user opened in the app (ports.go)
	// assistant: what the assistant (internal/assistant, its own process on the bus) last reported (assistant_link.go).
	assistant assistantState
	// userLocale: the user's language, an i18n.Lang (locale.go)
	userLocale atomic.Value
	deleted    deletedBook  // conversations taken out of the apps' lists (deleted_sessions.go)
	inbox      inboxSources // the inbox's sources and wake-ups; the inbox itself is the manager's (inbox.go)
	kb         kbLink       // what the hub keeps for the knowledge base, the assistant's (kb.go)
	// bots: the chat apps' bots the user talks to Persona through (bots.go).
	bots     botHub
	channels channelsHub
	// chans is what the assistant reports of the channels, and the conversations they follow (assistant_channels.go).
	chans       managerChannels
	secret      secretState // secretkey.go
	devtok      devTokens   // device token lookups (devicetokens.go)
	extTokens   extTokens   // the extensions' tokens (extension_scopes.go)
	public      publicState // TLS identity, listener and advertised addresses (public.go)
	sends       sendLog     // messages being / just sent, by client_msg_id (send.go)
	extensions  *extensions // the extension registry and what runs of it (extensions.go, docs/extensions.md)
	legacyCalls legacyCalls // calls under the old /api/v1/plugins paths (extension_compat.go)
	admin       adminState  // the admin UI (admin.go, docs/admin.md)

	apiMux *http.ServeMux // the API's routes: the one a request goes to decides what an extension's token may do

	mesh  meshState // the other hubs of the mesh group (mesh.go)
	fleet fleet     // guards machines and byID (machines.go)

	todoLinks todoLinks // what the assistant reports of its todos (assistant_todos.go)

	connectors *connector.Registry // sources of the user's information (connectors.go)
	// migrated are the hub's own connectors a plugin took over (connectors_migrate.go): id → the hub's, for when the plugin goes.
	migrated sync.Map
	media    mediaCache // connectors' media as the app's thread pages show them (connector_threads.go)
	// connectorsOff start switched off until the user turns them on (addConnectorOff).
	connectorsOff map[string]bool

	// The looks built into personad (persona.go; nil in unit tests) and the tier each tiered look shows (persona_tiers.go).
	builtinLooks fs.FS
	tiers        personaTierState

	// usageWin: subscription rate-limit windows across machines (usagewindows.go, #2432)
	usageWin usageWindowsHub
	// usageGW: the account-pool gateways that add accounts to it (usagegateways.go, #2595)
	usageGW usageGateways
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
var NewMail func(dataDir string, store Store, google GoogleConfig, microsoft MicrosoftConfig) (Mail, error)

// Store is the hub's persistence (internal/db on Postgres).
type Store interface {
	CreateLockTest(ctx context.Context, t db.LockTest) error
	LockTest(ctx context.Context, deviceID, testID string) (*db.LockTest, error)
	DueLockTests(ctx context.Context, at time.Time) ([]db.LockTest, error)
	NextLockTestDue(ctx context.Context) (*time.Time, error)
	AttemptLockTest(ctx context.Context, testID string, sessionID int64, at time.Time) (bool, error)
	ReceiveLockTest(ctx context.Context, deviceID, testID string, shown *bool, at time.Time) (*db.LockTest, error)
	RecoverLockTests(ctx context.Context, at time.Time) error
	PruneLockTests(ctx context.Context, before time.Time) error
	InsertDeviceSession(ctx context.Context, s db.DeviceSession) (int64, error)
	UpdateDeviceSessionHeartbeat(ctx context.Context, id int64, interval int) error
	FinishDeviceSession(ctx context.Context, id int64, at time.Time, reason string, interval int) error
	StopDeviceSessions(ctx context.Context, at time.Time) error
	PruneDeviceSessions(ctx context.Context, before time.Time) error
	DeviceSessions(ctx context.Context, deviceID string, since, until time.Time) (db.DeviceSessionHistory, error)
	BeginClientOp(ctx context.Context, op db.ClientOp) (*db.ClientOp, bool, error)
	ClientOp(ctx context.Context, id string) (*db.ClientOp, error)
	FinishClientOp(ctx context.Context, id, state string, result json.RawMessage) error
	RecoverClientOps(ctx context.Context) error
	CleanupClientOps(ctx context.Context, before time.Time) error
	ReplacePhoneTools(ctx context.Context, registration db.PhoneToolRegistration) error
	PhoneTools(ctx context.Context) ([]db.PhoneToolRegistration, error)
	ClaimSend(ctx context.Context, id, machine, session string) (string, error)
	SettleSend(ctx context.Context, id, state string) error
	UpsertDevice(ctx context.Context, d db.Device) error
	Devices(ctx context.Context) ([]db.Device, error)
	DeleteDevice(ctx context.Context, id string) error
	SetDevicePrefs(ctx context.Context, id, credential string, prefs json.RawMessage) error
	LiveActivities(ctx context.Context) ([]db.LiveActivity, error)
	SaveLiveActivity(ctx context.Context, v db.LiveActivity) error
	InsertInteraction(ctx context.Context, ix proto.Interaction) error
	FinishInteraction(ctx context.Context, id, status string, res *proto.Resolution) error
	InteractionOutcome(ctx context.Context, id string) (status string, res *proto.Resolution, err error)
	Interactions(ctx context.Context, machine, session string, limit int) ([]db.InteractionRecord, error)
	AllowAlways(ctx context.Context, session, tool string, keys []string) error
	AlwaysAllowed(ctx context.Context, session string, keys []string) (bool, error)
	MailAccounts(ctx context.Context) ([]db.MailAccountRow, error)
	ReplaceMailAccounts(ctx context.Context, rows []db.MailAccountRow) error
	MarkSeen(ctx context.Context, machine, session string, at time.Time) error
	SeenTimes(ctx context.Context) (map[[2]string]time.Time, error)
	Setting(ctx context.Context, key string, out any) (bool, error)
	PutSetting(ctx context.Context, key string, v any) error
	TakeSetting(ctx context.Context, key string, out any) (bool, error)
	ConnectorSecret(ctx context.Context, connector string) (string, bool, error)
	PutConnectorSecret(ctx context.Context, connector, sealed string) error
	DeleteConnectorSecret(ctx context.Context, connector string) error
	CreateDeviceToken(ctx context.Context, t db.DeviceToken, hash string) error
	DeviceTokenByHash(ctx context.Context, hash string) (*db.DeviceToken, error)
	DeviceTokens(ctx context.Context) ([]db.DeviceToken, error)
	RevokeDeviceToken(ctx context.Context, id string) (bool, error)
	TouchDeviceToken(ctx context.Context, id string) error
	CreatePairToken(ctx context.Context, hash string, expiresAt time.Time, machine string) error
	PairTokenByHash(ctx context.Context, hash string) (*db.PairToken, error)
	RedeemPairToken(ctx context.Context, pairHash string, device db.DeviceToken, tokenHash string) (bool, error)
	CreateTaskPlan(ctx context.Context, p db.TaskPlanRow) error
	TaskPlan(ctx context.Context, id string) (*db.TaskPlanRow, error)
	TaskPlans(ctx context.Context, conversation string) ([]db.TaskPlanRow, error)
	UpdateTaskPlan(ctx context.Context, id, from, to string, tasks json.RawMessage, at time.Time) (bool, error)
	AppendFeed(ctx context.Context, f db.FeedRow) error
	Feed(ctx context.Context, conversation string, since time.Time, limit int) ([]db.FeedRow, error)
	FeedItem(ctx context.Context, id string) (*db.FeedRow, error)
	MarkFeedSent(ctx context.Context, kind, ref string, at time.Time, patch json.RawMessage) ([]db.FeedRow, error)
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
	// checkedAt / checking bound transcript reads for a plan answered outside Persona.
	checkedAt time.Time
	checking  bool
	// answeredBy is who resolved it (Resolution.AnsweredBy), so the broadcast can say the assistant answered.
	answeredBy string
	// codex: a card a node listed for a Codex TUI (codexcards.go); it lasts as long as the node lists it.
	codex bool
	// claimed: an answer was taken for this hook-waited card (resolve). It stays in the map until await or
	// finishInteraction ends it, but a second answer finds it no longer pending.
	claimed bool
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
	return runConfigured(ctx, b, cfg, updating)
}

// runConfigured runs the hub until ctx ends; updating: an update is under way, this process confirms it.
func runConfigured(ctx context.Context, b update.Build, cfg *Config, updating bool) error {
	budget := newShutdownBudget(cfg)
	defer budget.close()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	return budget.run(ctx, func() error { return runHub(ctx, b, cfg, budget, updating) })
}

func runHub(ctx context.Context, b update.Build, cfg *Config, budget *shutdownBudget, updating bool) error {
	// The stop asked for (stop) begins with beginStop, before anything sees its context end (restart.go).
	stop := ctx
	ctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	defer cancel()

	h := &Hub{
		deployCtx: ctx,
		shutdown:  budget, cfg: cfg, version: b.Version, build: b, logger: log.New(os.Stderr, "", log.LstdFlags),
		byID: map[string]*Machine{}, clients: map[*client]struct{}{}, interactions: map[string]*pending{},
		calls: map[string]chan proto.ToolResult{}, notified: map[string]time.Time{}, turns: newTurnTracker(),
	}
	h.builtinLooks = builtinPersonaFS()
	h.live = newLiveActivities(h)
	h.upd = newUpdater(h)
	h.upd.confirmed = !updating // an update under way is confirmed by this process (its watchdog waits for that)
	database, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer func() {
		cancel()
		budget.step(h, "node connections", func(context.Context) error {
			h.nodeWorkers.Wait()
			return nil
		})
		if h.stopping.Load() {
			budget.step(h, "hand over the event log", func(ctx context.Context) error { h.saveEventLog(ctx); return nil })
		}
		budget.step(h, "close database", database.CloseContext)
	}()
	h.store = database
	go func() {
		select {
		case <-stop.Done():
			h.beginStop()
			cancel()
		case <-ctx.Done():
		}
	}()
	if err := h.startClientOps(ctx); err != nil {
		return err
	}
	if err := h.initHubID(ctx); err != nil {
		return err
	}
	h.loadEventLog(ctx)
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
	h.loadUserLang(ctx)
	h.loadPhoneTools()
	if NewMail != nil {
		if h.mail, err = NewMail(cfg.dir, h.store, cfg.Google, cfg.Microsoft); err != nil {
			h.logf("mail disabled: %v", err)
		}
	}
	h.push = newPusher(cfg.dir, h.store, h.logf)
	h.push.desktopOnline = h.desktopOnline
	h.push.desktopOpen = h.desktopOpen
	if cfg.Lark.lark-connectorConfigured() {
		h.logf("hub.json lark.container / token / base_url are not read any more: Feishu through lark-connector is a plugin's now; set them in its settings (docs/feishu.md)")
	}
	h.push.hubID = h.hubID
	h.push.userLang = h.userLang
	h.initAgents()
	h.connectors = connector.NewRegistry(cfg.ConnectorGate)
	// The machines are the manager's to connect (devices_link.go); until it reports them they show as before a
	// restart (what it handed over), before the assistant's process starts and takes that hand-over.
	h.startDevices(ctx)
	h.initMesh(ctx) // before the manager connects: a member leaves its computers to the center (mesh_group.go)
	go h.expireLoop(ctx)
	go h.terminalKeepalive(ctx)
	go h.fullKeepalive(ctx)
	go h.live.run(ctx)
	h.migrateWeChatState(ctx) // into the channels' state before the assistant and the WeChat program read it (channels_migrate.go)
	// The assistant, its own process on the bus (assistant_process.go); it carries the machines, so the hub's stop
	// waits for it as it waited for the node connections.
	h.nodeWorkers.Add(1)
	go func() {
		defer h.nodeWorkers.Done()
		h.superviseAssistant(ctx)
	}()
	h.startBots(ctx)
	go h.agentsLoop(ctx)
	go h.inboxLoop(ctx)
	go h.metricsLoop(ctx)
	h.usageGW.init(cfg.Usage, h.usageWin.invalidate, h.logf) // account-pool gateways' usage windows (usagegateways.go, #2595)
	go h.usageGW.run(ctx)
	go h.noLockLoop(ctx)         // 「扩展开着时不自动锁屏」 said again to its Macs (computer_nolock.go)
	go h.extensionLeaseLoop(ctx) // plugins' programs on computers keep running (extension_nodes.go)
	h.extensions = newExtensions(cfg.dir, cfg.extensionConfigs(h.logf), h.logf)
	// The hub's token, whatever extension's log it turns up in (extension_metrics.go).
	h.extensions.stats.redact = h.redactOwnSecrets
	h.loadAssistantProfile(ctx) // the user's zone and the main conversation before the assistant reports (assistant_profile.go)
	h.startConnectors(ctx)      // the plugins' connectors join here (attachPlugins)
	go h.personaTiersLoop(ctx)
	h.upd.start(ctx)
	h.startExtensions(ctx)
	// Market plugins' newer versions (extension_autoupgrade.go).
	go h.extensionUpgradeLoop(ctx)
	// How they run, to the database once an hour (extension_metrics.go).
	go h.extensionMetricsLoop(ctx)
	go h.adminStartup(ctx) // the admin UI's first-run setup code, while it has no password (admin_setupcode.go)
	if err := h.startPublic(ctx); err != nil {
		return err
	}
	h.startMesh(ctx)
	return h.serveHTTP(ctx)
}

func (h *Hub) logf(format string, args ...any) { h.logger.Printf(format, args...) }

func (h *Hub) broadcastMachines() {
	h.broadcast(map[string]any{"type": "machines", "machines": h.machineList()})
	h.meshTouch() // a member's record says which of its computers the center can reach through it
	h.assistantUpdated()
}

// decorated returns a machine's sessions with pending interaction counts filled in.
func (h *Hub) decorated(m *Machine) []proto.Session {
	list := m.sessionList()
	h.mu.Lock()
	counts := map[string]int{}
	asking := map[string]proto.Interaction{} // a session's card: its own before the manager's (agents_link.go)
	for _, p := range h.interactions {
		if p.ix.MachineID == m.cfg.ID {
			counts[p.ix.SessionID]++
			if cur, ok := asking[p.ix.SessionID]; !ok || cur.ToolName == workCardTool {
				asking[p.ix.SessionID] = p.ix
			}
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
	shown := h.assistantShown()
	via := h.channelsOfConversations()
	lang := h.userLang()
	for i := range list {
		list[i].MachineID = m.cfg.ID
		list[i].PendingInteractions = counts[list[i].SessionID]
		list[i].Assistant = isAssistant(list[i], dir)
		if list[i].Assistant {
			list[i].Space = assistantSpace(list[i].Cwd)
		}
		if t := shown[list[i].SessionID].Title; t != "" && list[i].Assistant {
			list[i].Title = t // 「MMDD | 类型 | 主题」, the user's own, 「Persona」 for the main one (assistant/titles.go)
		}
		if ch := via[[2]string{m.cfg.ID, list[i].SessionID}]; len(ch) > 0 {
			list[i].Channels = ch // where its messages came from (docs/channels.md「来源标记」)
		}
		if list[i].Assistant {
			list[i].Archived = shown[list[i].SessionID].Archived
			// Messages from other conversations wait in the assistant while it runs: shown like typed-ahead input.
			if q := shown[list[i].SessionID].Queued; len(q) > 0 {
				list[i].Queued = append(slices.Clip(list[i].Queued), q...) // the node's slice is shared: never write into it
			}
		}
		list[i].SeenAt = seen[list[i].SessionID]
		localizeSession(lang, &list[i]) // the node's Chinese activity line… (transcript_text.go)
		list[i].State, list[i].BackgroundWorking = stateWithBackground(agentState(list[i], list[i].SeenAt), list[i])
		list[i].TurnError, list[i].TurnErrorKind = turnError(list[i])
		h.decorateWork(m.cfg.ID, &list[i], asking) // the manager's work report (agents_link.go)
	}
	h.decorateTodos(m.cfg.ID, list) // the todos the assistant reported (assistant_todos.go)
	for i := range list {
		// The todos' alone, stripped before push to client (#762).
		list[i].SpawnFrom, list[i].Chat, list[i].Topic, list[i].Dispatched = "", "", "", nil
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
		h.recordEnd(id, "cancelled", nil) // answered at the computer, or gone: not pending in the history either
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
	// node reads its approvals off the screen once a second (codexcards.go): a little longer. A Claude Code without
	// Jarvis's hooks (the user's own profile) asks only on the screen: its card is made at once (#751).
	if wait := screenCardWait(s, m.hasHooks(s)); wait > 0 {
		time.Sleep(wait)
	}
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
		} else if err == nil {
			go h.recheckScreen(m, s.SessionID, ix.ID)
		}
		return err
	}}
	h.mu.Unlock()
	h.record(ix)
	h.announce(ix)
	if d == nil {
		go h.upgradeScreenCard(m, s.SessionID, id)
	}
}

// screenUpgrade: how often, and screenUpgradeFor how long, a screen card made without a dialog it could read looks
// at the screen again (upgradeScreenCard).
var screenUpgrade, screenUpgradeFor = 500 * time.Millisecond, 30 * time.Second

// upgradeScreenCard: the agent may be called blocked before it draws its dialog (Orca does so from its PreToolUse
// hook; on Windows the hooks take seconds before AskUserQuestion's dialog shows), so the card made then has no dialog
// to answer. Look at the screen again; once a dialog shows, its card replaces this one. Stop when this card is
// answered or gone, the agent no longer blocked, or the time up (a dialog the hub cannot read: the card stays).
func (h *Hub) upgradeScreenCard(m *Machine, session, id string) {
	for end := time.Now().Add(screenUpgradeFor); time.Now().Before(end); {
		time.Sleep(screenUpgrade)
		cur, ok := m.session(session)
		if !ok || cur.Status != proto.StatusBlocked {
			return
		}
		h.mu.Lock()
		_, still := h.interactions[id]
		h.mu.Unlock()
		if !still {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		var scr struct {
			Text string `json:"text"`
		}
		err := m.call(ctx, "session.screen", map[string]any{"session_id": session, "lines": 40}, &scr)
		cancel()
		if err != nil {
			return
		}
		if screen.ParseDialog(scr.Text) != nil {
			h.finishInteraction(id)
			h.createScreenInteraction(m, cur)
			return
		}
	}
}

// screenRecheck: how often, and screenRecheckFor how long, the screen is looked at again after a screen card's
// answer (recheckScreen).
var screenRecheck, screenRecheckFor = 250 * time.Millisecond, 2 * time.Second

// recheckScreen: after a screen card's answer the agent may ask again at once (the next question of an
// AskUserQuestion, the next permission) while herdr keeps calling it blocked, so no status change makes that card.
// Look again until a dialog that is not the answered one shows (its card is made), the agent works again (a new
// question then comes with its own change to blocked), or another card is up (#751).
func (h *Hub) recheckScreen(m *Machine, session, answered string) {
	for end := time.Now().Add(screenRecheckFor); time.Now().Before(end); {
		time.Sleep(screenRecheck)
		cur, ok := m.session(session)
		if !ok || cur.Status != proto.StatusBlocked {
			return
		}
		h.mu.Lock()
		asked := false
		for _, p := range h.interactions {
			asked = asked || p.ix.MachineID == m.cfg.ID && p.ix.SessionID == session
		}
		h.mu.Unlock()
		if asked {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		var scr struct {
			Text string `json:"text"`
		}
		err := m.call(ctx, "session.screen", map[string]any{"session_id": session, "lines": 40}, &scr)
		cancel()
		if err != nil {
			return
		}
		// No dialog (the screen between two) or the answered one (the keys not landed yet): look again.
		if d := screen.ParseDialog(scr.Text); d != nil && "scr_"+m.cfg.ID+"_"+session+"_"+d.Rev != answered {
			h.createScreenInteraction(m, cur)
			return
		}
	}
}

// screenCardWait is how long a blocked session's screen card waits for a card its agent asks for itself.
func screenCardWait(s proto.Session, hooks bool) time.Duration {
	switch {
	case s.Agent == "codex" || s.Agent == "traex":
		return 3 * time.Second
	case hooks:
		return 1500 * time.Millisecond
	}
	return 0
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
			return nil, i18n.Err("hub-hub.pick_or_answer")
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
	h.broadcast(map[string]any{"type": "interaction", "interaction": h.withTask(ix)})
	if m := h.localMachine(ix.MachineID); m != nil {
		h.broadcastSessions(m, h.decorated(m))
	}
	if h.quietSession(ix.MachineID, ix.SessionID) {
		// A quiet task's question is no push, bot card or channel message: the main conversation is told (#1731).
		h.quietInteraction(ix)
		return
	}
	if !h.muted(ix.MachineID, ix.SessionID) {
		h.notifyInteraction(ix, h.machineName(ix.MachineID))
	}
	h.channelsAnnounce(ix) // the Feishu bot's cards, the channels that ask in text (assistant_channels.go)
}

func (h *Hub) machineName(id string) string {
	if m := h.localMachine(id); m != nil {
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
			if ix.Kind == proto.IxElicitation && !h.hasElicitationClient() {
				_ = h.resolve(ix.ID, proto.Resolution{Dismiss: true})
			}
			return h.await(ctx, p)
		}
		h.mu.Unlock()
		if r, ended := h.outcome(ix.ID); ended {
			return r, nil
		}
	}
	if ix.Kind == proto.IxElicitation && !h.hasElicitationClient() {
		return proto.Resolution{Dismiss: true}, nil
	}
	p := &pending{ix: ix, done: make(chan proto.Resolution, 1)}
	h.mu.Lock()
	// A screen interaction for the same session was a premature guess; the hook knows better.
	// Tell the app now: the wait below can last until the hook times out.
	var dropped []*pending
	for id, old := range h.interactions {
		if old.ix.Kind == proto.IxScreen && old.ix.MachineID == ix.MachineID && old.ix.SessionID == ix.SessionID {
			delete(h.interactions, id)
			dropped = append(dropped, old)
		}
	}
	h.interactions[ix.ID] = p
	h.mu.Unlock()
	for _, old := range dropped { // as codexcards.go withdraws a screen card
		h.recordEnd(old.ix.ID, "cancelled", nil)
		h.finished(old)
	}
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

// writeEnded answers a resolve of an interaction that is over already. Answered (or handed back, and handed back
// again) it is the same outcome as the first time: 200 with already and the answer as the history keeps it, so the
// app shows the card as answered rather than an error — the first answer's event may not have reached it. Otherwise
// (expired, withdrawn, handed back, or unknown) 410, with how it ended when the history knows.
func (h *Hub) writeEnded(w http.ResponseWriter, id string, r proto.Resolution, err error) {
	var status string
	var kept *proto.Resolution
	if h.store != nil {
		status, kept, _ = h.store.InteractionOutcome(context.Background(), id)
	}
	if status == "resolved" || (status == "dismissed" && r.Dismiss) {
		out := map[string]any{"ok": true, "already": true, "status": status}
		if kept != nil {
			out["resolution"] = kept
		}
		writeJSON(w, out)
		return
	}
	body := errorBody(http.StatusGone, "", err)
	if status != "" && status != "pending" {
		body["status"] = status
	}
	writeErrorBody(w, http.StatusGone, body)
}

var errInvalidElicitation = errors.New("elicitation requires action accept, decline or cancel")

// resolve answers an interaction from the app. A screen card acts right away and its error comes back (the
// dialog changed: nothing pressed, a new card follows).
func (h *Hub) resolve(id string, r proto.Resolution) error {
	h.mu.Lock()
	p := h.interactions[id]
	if p != nil && p.ix.Kind == proto.IxElicitation && !r.Dismiss &&
		r.Action != "accept" && r.Action != "decline" && r.Action != "cancel" {
		h.mu.Unlock()
		return errInvalidElicitation
	}
	if p != nil && r.Allow != nil && *r.Allow {
		for _, f := range p.ix.Fields {
			if !f.Optional && strings.TrimSpace(r.Values[f.Key]) == "" {
				h.mu.Unlock()
				return i18n.Err("hub-hub.fill_in", i18n.Args{"field": f.Label})
			}
		}
	}
	if p != nil && p.claimed {
		p = nil // answered a moment ago: the first answer is the one the hook and the history get
	}
	if p != nil && p.local != nil {
		delete(h.interactions, id) // claimed: a second answer to the same card is not pending any more
	} else if p != nil {
		p.claimed = true
	}
	if p != nil {
		p.answeredBy = r.AnsweredBy // read by finished(), under this lock
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
			h.mu.Lock()
			back := h.interactions[id] == p
			h.mu.Unlock()
			if back { // a Codex card the node could not answer is open again (codexPending): it is not over
				go h.taskAnswered(p.ix, r, status)
				return err
			}
		} else {
			h.channelsEnded(p.ix, &r)
		}
		// What was typed into a terminal may be a password: the history keeps that something was, not what.
		kept := redacted(p.ix, r)
		if kept.Text != "" {
			kept.Text = "<redacted>"
		}
		h.recordEnd(id, status, &kept)
		h.finished(p)
		go h.taskAnswered(p.ix, r, status)
		return err
	}
	kept := redacted(p.ix, r)
	h.recordEnd(id, status, &kept)
	h.channelsEnded(p.ix, &r)
	go h.taskAnswered(p.ix, r, status)
	h.keepAlways(p.ix, r) // kept before the hook hears: its next card may follow at once (assistant_always.go)
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
	h.mu.Lock()
	answeredBy := p.answeredBy
	h.mu.Unlock()
	h.channelsEnded(p.ix, nil)
	event := map[string]any{"type": "interaction_resolved", "id": id}
	if answeredBy != "" {
		event["answered_by"] = answeredBy // the app marks a question the assistant answered (docs/assistant.md)
	}
	h.broadcast(event)
	if m := h.localMachine(p.ix.MachineID); m != nil {
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
	out := []proto.Interaction{}
	for _, p := range h.interactions {
		out = append(out, p.ix)
	}
	h.mu.Unlock()
	for i := range out { // after h.mu: the task comes from the assistant's report
		out[i] = h.withTask(out[i])
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
				if m := h.localMachine(p.ix.MachineID); m != nil {
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
	h.phoneTools = make(map[string]db.PhoneToolRegistration, len(tools))
	for _, r := range tools {
		h.phoneTools[r.DeviceID] = r
	}
	h.mu.Unlock()
}

func (h *Hub) setPhoneTools(registration db.PhoneToolRegistration) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.store != nil {
		if err := h.store.ReplacePhoneTools(context.Background(), registration); err != nil {
			h.logf("save phone tools: %v", err)
			return
		}
	}
	if h.phoneTools == nil {
		h.phoneTools = map[string]db.PhoneToolRegistration{}
	}
	registration.UpdatedAt = time.Now()
	h.phoneTools[registration.DeviceID] = registration
}

// mcpTools are the tools of an assistant conversation or an agent: the phone's, then those of the connectors that
// are ok and give their tools to agents too (mail); for the assistant also its own, then those of the connectors
// only it gets (Feishu, GitHub…).
func (h *Hub) mcpTools(assistant bool) []proto.Tool {
	h.mu.Lock()
	ids := make([]string, 0, len(h.phoneTools))
	for id := range h.phoneTools {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var tools []proto.Tool
	byName := map[string]int{}
	for _, id := range ids {
		for _, tool := range h.phoneTools[id].Tools {
			if i, ok := byName[tool.Name]; ok {
				if len([]rune(tool.Description)) > len([]rune(tools[i].Description)) ||
					(tool.Description == tools[i].Description && len(tool.InputSchema) > len(tools[i].InputSchema)) {
					tools[i] = tool
				}
			} else {
				byName[tool.Name] = len(tools)
				tools = append(tools, tool)
			}
		}
	}
	h.mu.Unlock()
	ctx := context.Background()
	tools = append(tools, h.connectors.ToolsFor(ctx, true)...)
	if assistant {
		tools = append(tools, h.asst().Tools(ctx)...)
		tools = append(tools, h.computerTools()...)
		tools = append(tools, h.browserTools()...)
		tools = append(tools, h.connectors.ToolsFor(ctx, false)...)
	}
	return tools
}

func (h *Hub) callTool(ctx context.Context, machineID, name string, args json.RawMessage) proto.ToolResult {
	switch {
	case name == runtimeCLITool: // the DEBUG app's own channel, for the user's scripts only (runtime_cli.go)
		return proto.TextResult("unknown tool "+name, true)
	case strings.HasPrefix(name, "phone_"):
		return h.callPhone(ctx, name, args)
	}
	return proto.TextResult("unknown tool "+name, true)
}

// asker is the conversation a card the hub makes itself belongs to: the clients put a card into the chat of its
// machine and session, so one with no session shows only in 需要你, not in the conversation that is waiting on it.
type asker struct{ machine, session string }

// assistantAsker is the assistant conversation sessionID (on the assistant's machine); with none, a card of machine.
func (h *Hub) assistantAsker(sessionID, machine string) asker {
	if mid := h.assistantProfile().MachineID; sessionID != "" && mid != "" {
		return asker{mid, sessionID}
	}
	return asker{machine, ""}
}

// approve asks the user on the phone to allow a sensitive tool call (e.g. sending an email).
func (h *Hub) approve(ctx context.Context, by asker, tool, summary string, input json.RawMessage) bool {
	ix := proto.Interaction{
		ID: "ix_" + randomHex(6), MachineID: by.machine, SessionID: by.session, Kind: proto.IxPermission, ToolName: tool, Summary: summary,
		ToolInput: input, SessionTitle: "Persona", CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().Add(15 * time.Minute).UTC(),
	}
	r, _ := h.waitInteraction(ctx, ix)
	return r.Allow != nil && *r.Allow
}

func (h *Hub) callPhone(ctx context.Context, name string, args json.RawMessage) proto.ToolResult {
	c := h.phoneClient(name)
	if c == nil {
		c = h.wakePhone(ctx, name)
	}
	if c == nil {
		return proto.TextResult("The user's iPhone is not connected and did not wake up in the background: ask them to open the Persona app, then retry.", true)
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
		return proto.TextResult("Timed out waiting for the iPhone (is the Persona app still open?).", true)
	}
}

// wakePhone asks iOS to wake the app in the background (silent push) and waits briefly for it to connect.
func (h *Hub) wakePhone(ctx context.Context, name string) *client {
	if !h.push.ready() {
		return nil
	}
	h.mu.Lock()
	var candidates []db.PhoneToolRegistration
	for _, r := range h.phoneTools {
		if r.Platform != "" && r.Platform != "ios" {
			continue
		}
		for _, tool := range r.Tools {
			if tool.Name == name {
				candidates = append(candidates, r)
				break
			}
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].UpdatedAt.Equal(candidates[j].UpdatedAt) {
			return candidates[i].DeviceID < candidates[j].DeviceID
		}
		return candidates[i].UpdatedAt.After(candidates[j].UpdatedAt)
	})
	if h.wokeAt == nil {
		h.wokeAt = map[string]time.Time{}
	}
	wakeIDs := map[string]bool{}
	for _, r := range candidates {
		if time.Since(h.wokeAt[r.DeviceID]) < 30*time.Second {
			break
		}
		h.wokeAt[r.DeviceID] = time.Now()
		wakeIDs[r.DeviceID] = true
		if r.CredentialID != "" {
			wakeIDs[r.CredentialID] = true
		}
		// Old push registrations have no credential binding until the app next registers.
		if r.Platform == "" {
			wakeIDs["legacy"] = true
		}
		break
	}
	h.mu.Unlock()
	if len(candidates) == 0 {
		return nil
	}
	if len(wakeIDs) > 0 {
		h.push.wakeDevices("tool", wakeIDs)
	}
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	t := time.NewTicker(300 * time.Millisecond)
	defer t.Stop()
	for {
		if c := h.phoneClient(name); c != nil {
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

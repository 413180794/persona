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
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/chyroc/jarvis/server/internal/ai"
	"github.com/chyroc/jarvis/server/internal/db"
	"github.com/chyroc/jarvis/server/internal/lark"
	"github.com/chyroc/jarvis/server/internal/mail"
	"github.com/chyroc/jarvis/server/internal/proto"
	"github.com/chyroc/jarvis/server/internal/rpc"
	"github.com/chyroc/jarvis/server/internal/screen"
)

type Hub struct {
	cfg      *Config
	version  string
	logger   *log.Logger
	machines []*Machine
	byID     map[string]*Machine
	mail     Mail
	push     *Pusher
	store    Store // nil in unit tests: nothing is persisted
	ai       *ai.Client
	// The inbox's own models (inbox_agent.go): 1 triages, 2 decides what the agent (3) should take on.
	triageAI, judgeAI *ai.Client
	lark              *lark.Client

	terms terminals // the terminal tab's live streams (terminal.go)

	mutesMu sync.Mutex
	mutes   *Pins // muted spaces / agents (no pushes); loaded from settings on first use

	mu            sync.Mutex
	clients       map[*client]struct{}
	interactions  map[string]*pending
	phoneTools    []proto.Tool
	calls         map[string]chan proto.ToolResult
	seen          map[[2]string]time.Time // (machine, session) -> when the user last looked at it
	stateSince    map[[2]string]stateMark // (machine, session) -> its state and when it entered it
	userTZ        string                  // the phone's time zone (assistant settings)
	snap          string                  // the assistant's last live snapshot (assistant_snapshot.go)
	snapAt        time.Time
	wokeAt        time.Time            // last silent push to wake the phone for a tool call
	notified      map[string]time.Time // assistant conversation -> when it last pushed with notify_user
	kbAgentLogged map[string]time.Time // agent -> when its result last went into a project timeline (kb.go)
	turns         *turnTracker         // "machine/session" -> the turn in progress, for the finished push (notify.go)
	live          *liveActivities      // the lock screen / Dynamic Island activity (liveactivity.go)
	usage         usageBook            // token usage of every machine (usage.go)
	metrics       metricsBook          // OS and load of every machine (metrics.go)

	sched  schedule  // the assistant's routines and follow-ups (assistant_schedule.go)
	inbox  inbox     // the unified inbox (inbox.go)
	kb     kbState   // the knowledge base under ~/.jarvis/data (kb.go)
	devtok devTokens // device token lookups (devicetokens.go)
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
}

// NewMail is set by the mail integration (see mail_glue.go).
var NewMail func(dataDir string, store Store, google GoogleConfig) (Mail, error)

// Store is the hub's persistence (internal/db on Postgres).
type Store interface {
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
}

// resumeGrace is how long a resumable interaction outlives its node: a little longer than hooks keep reconnecting
// (node.ResumeWait).
var resumeGrace = 3 * time.Minute

func Run(ctx context.Context, version string) error {
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	h := &Hub{
		cfg: cfg, version: version, logger: log.New(os.Stderr, "", log.LstdFlags),
		byID: map[string]*Machine{}, clients: map[*client]struct{}{}, interactions: map[string]*pending{},
		calls: map[string]chan proto.ToolResult{}, notified: map[string]time.Time{}, turns: newTurnTracker(),
	}
	h.live = newLiveActivities(h)
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
	h.ai = ai.New(cfg.AI)
	if c := cfg.Inbox.TriageAI; c != nil {
		h.triageAI = ai.New(c.Over(cfg.AI))
	}
	if c := cfg.Inbox.JudgeAI; c != nil {
		h.judgeAI = ai.New(c.Over(cfg.AI))
	}
	h.lark = lark.New(cfg.Lark)
	for _, mc := range cfg.Machines {
		m := newMachine(h, mc)
		h.machines = append(h.machines, m)
		h.byID[mc.ID] = m
		go m.run(ctx)
	}
	go h.expireLoop(ctx)
	go h.terminalKeepalive(ctx)
	go h.live.run(ctx)
	go h.scheduleLoop(ctx)
	go h.larkEvents(ctx)
	go h.inboxLoop(ctx)
	go h.metricsLoop(ctx)
	h.startKB(ctx, cfg.KB, cfg.dir)

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
	for i := range list {
		list[i].PendingInteractions = counts[list[i].SessionID]
		list[i].Assistant = isAssistant(list[i].Cwd, dir)
		list[i].SeenAt = seen[list[i].SessionID]
		list[i].State = agentState(list[i], list[i].SeenAt)
	}
	h.markStates(m.cfg.ID, list, time.Now())
	return list
}

func (h *Hub) sessionsChanged(m *Machine) {
	list := h.decorated(m)
	h.broadcast(map[string]any{"type": "sessions", "machine_id": m.cfg.ID, "sessions": list})
	h.syncScreenInteractions(m, list)
}

// syncScreenInteractions turns blocked agents without a hook-driven interaction into "screen" interactions
// (folder trust, model pickers, any TUI dialog hooks cannot see), and clears them once the agent moves on.
func (h *Hub) syncScreenInteractions(m *Machine, list []proto.Session) {
	blocked := map[string]proto.Session{}
	for _, s := range list {
		if s.Status == proto.StatusBlocked {
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
		} else {
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
	// Hook dialogs flip the agent to blocked a moment before the hook reaches us; give them a head start.
	time.Sleep(1500 * time.Millisecond)
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
	for _, p := range h.interactions {
		if p.ix.MachineID == m.cfg.ID && p.ix.SessionID == s.SessionID {
			h.mu.Unlock()
			return
		}
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

// screenAnswer turns an answer to a screen card into session.keys parameters. With a dialog the node checks its
// revision before pressing anything; an option's keys come from the node's fresh reading of the screen, and the
// ones worked out here are for nodes that predate that.
func screenAnswer(session string, d *screen.Dialog, r proto.Resolution) (map[string]any, error) {
	params := map[string]any{"session_id": session, "keys": r.Keys, "text": r.Text, "pre_keys": r.PreKeys}
	if d == nil {
		return params, nil
	}
	params["rev"] = d.Rev
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
		h.broadcast(map[string]any{"type": "sessions", "machine_id": m.cfg.ID, "sessions": h.decorated(m)})
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
		kept := r
		if kept.Text != "" {
			kept.Text = "<redacted>"
		}
		h.recordEnd(id, status, &kept)
		h.finished(p)
		return err
	}
	h.recordEnd(id, status, &r)
	select {
	case p.done <- r:
	default:
	}
	h.finishInteraction(id)
	return nil
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
		h.broadcast(map[string]any{"type": "sessions", "machine_id": m.cfg.ID, "sessions": h.decorated(m)})
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

func (h *Hub) mcpTools(assistant bool) []proto.Tool {
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

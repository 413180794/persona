// Package hub serves the iPhone / Watch apps and coordinates the nodes on every machine.
package hub

import (
	"context"
	"encoding/json"
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
	"github.com/chyroc/jarvis/server/internal/proto"
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
	lark     *lark.Client

	mutesMu sync.Mutex
	mutes   *Pins // muted spaces / agents (no pushes); loaded from settings on first use

	mu           sync.Mutex
	clients      map[*client]struct{}
	interactions map[string]*pending
	phoneTools   []proto.Tool
	calls        map[string]chan proto.ToolResult
	seen         map[[2]string]time.Time // (machine, session) -> when the user last looked at it
}

// Mail is implemented by internal/mail; kept as an interface so the hub can run without it.
type Mail interface {
	Handler() http.Handler
	Tools() []proto.Tool
	Call(ctx context.Context, name string, args json.RawMessage, approve func(ctx context.Context, summary string, input json.RawMessage) bool) proto.ToolResult
}

// NewMail is set by the mail integration (see mail_glue.go).
var NewMail func(dataDir string, store Store) (Mail, error)

// Store is the hub's persistence (internal/db on Postgres).
type Store interface {
	ReplacePhoneTools(ctx context.Context, tools []proto.Tool) error
	PhoneTools(ctx context.Context) ([]proto.Tool, error)
	UpsertDevice(ctx context.Context, d db.Device) error
	Devices(ctx context.Context) ([]db.Device, error)
	DeleteDevice(ctx context.Context, id string) error
	InsertInteraction(ctx context.Context, ix proto.Interaction) error
	FinishInteraction(ctx context.Context, id, status string, res *proto.Resolution) error
	Interactions(ctx context.Context, machine, session string, limit int) ([]db.InteractionRecord, error)
	PutNodeSession(ctx context.Context, machine, id string, s db.NodeSession) error
	NodeSessions(ctx context.Context, machine string) (map[string]db.NodeSession, error)
	MailAccounts(ctx context.Context) ([]db.MailAccountRow, error)
	ReplaceMailAccounts(ctx context.Context, rows []db.MailAccountRow) error
	MarkSeen(ctx context.Context, machine, session string, at time.Time) error
	SeenTimes(ctx context.Context) (map[[2]string]time.Time, error)
	Setting(ctx context.Context, key string, out any) (bool, error)
	PutSetting(ctx context.Context, key string, v any) error
}

type pending struct {
	ix   proto.Interaction
	done chan proto.Resolution
	// local is set for interactions the hub resolves itself (screen dialogs, mail approvals).
	local func(proto.Resolution)
}

func Run(ctx context.Context, version string) error {
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	h := &Hub{
		cfg: cfg, version: version, logger: log.New(os.Stderr, "", log.LstdFlags),
		byID: map[string]*Machine{}, clients: map[*client]struct{}{}, interactions: map[string]*pending{},
		calls: map[string]chan proto.ToolResult{},
	}
	database, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer database.Close()
	h.store = database
	if err := database.ExpireStaleInteractions(ctx); err != nil {
		h.logf("expire stale interactions: %v", err)
	}
	h.importLegacy(ctx)
	h.loadSeen(ctx)
	h.loadPhoneTools()
	if NewMail != nil {
		if h.mail, err = NewMail(cfg.dir, h.store); err != nil {
			h.logf("mail disabled: %v", err)
		}
	}
	h.push = newPusher(cfg.dir, h.store, h.logf)
	h.ai = ai.New(cfg.AI)
	h.lark = lark.New(cfg.Lark)
	for _, mc := range cfg.Machines {
		m := newMachine(h, mc)
		h.machines = append(h.machines, m)
		h.byID[mc.ID] = m
		go m.run(ctx)
	}
	go h.expireLoop(ctx)

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
	id := "scr_" + m.cfg.ID + "_" + s.SessionID
	ix := proto.Interaction{
		ID: id, MachineID: m.cfg.ID, SessionID: s.SessionID, SessionTitle: s.Title, Cwd: s.Cwd, Kind: proto.IxScreen,
		CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().Add(24 * time.Hour).UTC(), Screen: trimScreen(scr.Text),
	}
	h.mu.Lock()
	for _, p := range h.interactions {
		if p.ix.MachineID == m.cfg.ID && p.ix.SessionID == s.SessionID {
			h.mu.Unlock()
			return
		}
	}
	h.interactions[id] = &pending{ix: ix, local: func(r proto.Resolution) {
		if r.Dismiss {
			return
		}
		kctx, kcancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer kcancel()
		_ = m.call(kctx, "session.keys", map[string]any{"session_id": s.SessionID, "keys": r.Keys, "text": r.Text}, nil)
	}}
	h.mu.Unlock()
	h.record(ix)
	h.announce(ix)
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
func (h *Hub) waitInteraction(ctx context.Context, ix proto.Interaction) (proto.Resolution, error) {
	if ix.ExpiresAt.IsZero() {
		ix.ExpiresAt = time.Now().Add(time.Duration(h.cfg.HookTimeoutMinutes) * time.Minute).UTC()
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
	defer h.finishInteraction(ix.ID)
	timer := time.NewTimer(time.Until(ix.ExpiresAt))
	defer timer.Stop()
	select {
	case r := <-p.done:
		return r, nil
	case <-timer.C:
		h.recordEnd(ix.ID, "expired", nil)
		return proto.Resolution{Dismiss: true}, nil
	case <-ctx.Done():
		h.recordEnd(ix.ID, "cancelled", nil)
		return proto.Resolution{Dismiss: true}, nil
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

// resolve answers an interaction from the app.
func (h *Hub) resolve(id string, r proto.Resolution) error {
	h.mu.Lock()
	p := h.interactions[id]
	h.mu.Unlock()
	if p == nil {
		return fmt.Errorf("interaction %s is no longer pending", id)
	}
	status := "resolved"
	if r.Dismiss {
		status = "dismissed"
	}
	h.recordEnd(id, status, &r)
	if p.local != nil {
		h.finishInteraction(id)
		go p.local(r)
		return nil
	}
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
	h.mu.Unlock()
	if !ok {
		return
	}
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
		if p.ix.MachineID == machineID {
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

func (h *Hub) mcpTools() []proto.Tool {
	h.mu.Lock()
	tools := append([]proto.Tool(nil), h.phoneTools...)
	h.mu.Unlock()
	if h.mail != nil {
		tools = append(tools, h.mail.Tools()...)
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
		return proto.TextResult("The user's iPhone is not connected: ask them to open the Jarvis app, then retry.", true)
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

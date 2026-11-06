package hub

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/sessionlist"
)

const featureSessionsList = "sessions_list"

// runtimeList is a transport cache of the manager's already-decorated rows. It
// does not derive agent state or store business data. sessionsMu orders updates
// with legacy list pushes, while the index serves readers independently.
type runtimeList struct {
	once     sync.Once
	index    sessionlist.Index
	machines map[string]bool // guarded by sessionsMu
}

func (h *Hub) sessionListIndex() *sessionlist.Index {
	h.listCache.once.Do(func() {
		h.sessionsMu.Lock()
		defer h.sessionsMu.Unlock()
		h.listCache.machines = map[string]bool{}
		for _, m := range h.machinesNow() {
			list := h.shown(m.cfg.ID, h.decorated(m))
			h.indexSessionsLocked(m.cfg.ID, list, encodeSessions(list))
		}
	})
	return &h.listCache.index
}

// indexSessionsLocked consumes the same encoded whole rows used by the legacy
// push path. Only row summaries are additionally encoded, once per update.
func (h *Hub) indexSessionsLocked(mid string, list []proto.Session, encoded []sentSession) {
	if h.listCache.machines == nil {
		return // no paginated reader yet; legacy users pay no indexing overhead
	}
	full := make(map[string]json.RawMessage, len(encoded))
	for _, row := range encoded {
		full[row.id] = row.raw
	}
	items := make([]sessionlist.Item, 0, len(list))
	seen := make(map[string]bool, len(list))
	for _, row := range list {
		if row.SessionID == "" || seen[row.SessionID] {
			continue
		}
		seen[row.SessionID] = true
		project := row.RepoRoot
		if project == "" {
			project = row.WorkspaceCwd
		}
		if project == "" {
			project = row.Cwd
		}
		group := row.SpaceID
		if group == "" {
			group = row.WorkspaceID
		}
		summary, _ := json.Marshal(runtimeSummary(row))
		items = append(items, sessionlist.Item{
			ID: runtimeListID(mid, row.SessionID), Machine: mid, Project: project, Group: group,
			State: row.State, Source: row.SourceKind, Provider: row.Agent, UpdatedAt: row.UpdatedAt,
			SearchText: strings.Join([]string{row.Title, row.Cwd, row.RepoRoot, row.SpaceName, row.SlotName, row.AgentName, row.LastText}, "\n"),
			Summary:    json.RawMessage(summary), Full: full[row.SessionID],
		})
	}
	h.listCache.index.Replace(mid, items)
	h.listCache.machines[mid] = true
}

// The JSON tuple is unambiguous even if a runtime id contains slashes. Clients
// form the same key from machine_id/session_id when applying delta removals.
func runtimeListID(mid, sid string) string {
	b, _ := json.Marshal([2]string{mid, sid})
	return string(b)
}

func rowText(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

// runtimeSummary deliberately excludes transcript/task arrays and chat-only
// configuration. The normal info/items routes remain the source for chat detail.
type runtimeRowSummary struct {
	proto.Session
	Waiting          bool `json:"waiting"`
	BackgroundCount  int  `json:"background_count,omitempty"`
	RunningSubagents int  `json:"running_subagents,omitempty"`
	RunningWorkflows int  `json:"running_workflows,omitempty"`
}

func runtimeSummary(s proto.Session) runtimeRowSummary {
	out := proto.Session{
		Manager: s.Manager, MachineID: s.MachineID, SessionID: s.SessionID,
		Title: rowText(s.Title, 256), Cwd: s.Cwd, WorkspaceCwd: s.WorkspaceCwd, RepoRoot: s.RepoRoot,
		Status: s.Status, State: s.State, UpdatedAt: s.UpdatedAt, StateSince: s.StateSince, SeenAt: s.SeenAt,
		LastText: rowText(s.LastText, 180), Activity: rowText(s.Activity, 120), ActivityAt: s.ActivityAt,
		AwaitsReply: s.AwaitsReply, BackgroundWorking: s.BackgroundWorking,
		Agent: s.Agent, AgentLabel: s.AgentLabel, AgentName: s.AgentName,
		SourceKind: s.SourceKind, SourceThreadID: s.SourceThreadID, SourceInstanceID: s.SourceInstanceID,
		ParentThreadID: s.ParentThreadID, ParentSessionID: s.ParentSessionID, RootSessionID: s.RootSessionID, IsSubagent: s.IsSubagent,
		Capabilities: s.Capabilities, Attached: s.Attached, CanFork: s.CanFork,
		SpaceKind: s.SpaceKind, SpaceID: s.SpaceID, SpaceName: s.SpaceName, SpaceIndex: s.SpaceIndex,
		ParentSpaceID: s.ParentSpaceID, SlotID: s.SlotID, SlotName: s.SlotName, SlotIndex: s.SlotIndex, PaneIndex: s.PaneIndex,
		WorkspaceID: s.WorkspaceID, TabID: s.TabID, PaneID: s.PaneID,
		Assistant: s.Assistant, Archived: s.Archived, Space: s.Space,
		Starting: s.Starting, StartError: rowText(s.StartError, 180), StartedByJarvis: s.StartedByJarvis,
		ReadOnlyReason: rowText(s.ReadOnlyReason, 180), Origin: s.Origin, OriginRef: s.OriginRef,
		TurnError: rowText(s.TurnError, 180), TurnErrorKind: s.TurnErrorKind, TurnErrorCode: s.TurnErrorCode,
		PendingInteractions: s.PendingInteractions, Rev: s.Rev, Quiet: s.Quiet,
		Model: s.Model, ContextTokens: s.ContextTokens, ContextWindow: s.ContextWindow,
	}
	if s.Work != nil {
		out.Work = &proto.WorkState{Phase: s.Work.Phase, On: s.Work.On, Line: rowText(s.Work.Line, 180),
			Wait: rowText(s.Work.Wait, 180), Then: rowText(s.Work.Then, 180), WakeAt: s.Work.WakeAt,
			Attempts: s.Work.Attempts, Max: s.Work.Max, Deferred: s.Work.Deferred, Agent: s.Work.Agent}
	}
	if s.Todo != nil {
		out.Todo = &proto.TodoRef{ID: s.Todo.ID, Title: rowText(s.Todo.Title, 256)}
	}
	if s.Plan != nil {
		cp := *s.Plan
		cp.Current = rowText(cp.Current, 180)
		out.Plan = &cp
	}
	if s.Goal != nil {
		cp := *s.Goal
		cp.Objective = rowText(cp.Objective, 180)
		cp.Reason = rowText(cp.Reason, 180)
		out.Goal = &cp
	}
	if s.Loop != nil {
		cp := *s.Loop
		cp.Prompt = ""
		cp.Reason = rowText(cp.Reason, 120)
		out.Loop = &cp
	}
	row := runtimeRowSummary{Session: out, BackgroundCount: len(s.Background)}
	for _, sub := range s.Subagents {
		if sub.Running {
			row.RunningSubagents++
		}
	}
	for _, flow := range s.Workflows {
		if flow.Running {
			row.RunningWorkflows++
		}
	}
	row.Waiting = s.Loop != nil || row.BackgroundCount > 0 || row.RunningSubagents > 0 || row.RunningWorkflows > 0 ||
		(s.Work != nil && s.Work.Phase == "blocked" && s.Work.On != "user" &&
			(s.Work.WakeAt != nil || (s.Work.On == "external" && s.Work.Wait == "" && s.Work.Agent == nil)))
	return row
}

func (h *Hub) pagedSessions(w http.ResponseWriter, r *http.Request) {
	opts, err := sessionlist.Parse(r.URL.Query())
	if err != nil {
		sessionListErr(w, err)
		return
	}
	index := h.sessionListIndex()
	// Machine disappearance does not always carry a final sessions event.
	h.pruneSessionListMachines()
	if counts := r.URL.Query().Get("counts"); counts == "1" || counts == "true" {
		counts, err := index.Counts(opts)
		if err != nil {
			sessionListErr(w, err)
			return
		}
		writeJSON(w, counts)
		return
	}
	page, err := index.Query(opts)
	if err != nil {
		sessionListErr(w, err)
		return
	}
	writeJSON(w, page)
}

// pruneSessionListMachines scans only machine ids, never session rows. Called
// before machine broadcasts as well as requests, so removals wake list clients.
func (h *Hub) pruneSessionListMachines() bool {
	h.sessionsMu.Lock()
	defer h.sessionsMu.Unlock()
	if h.listCache.machines == nil {
		return false
	}
	present := map[string]bool{}
	for _, m := range h.machinesNow() {
		present[m.cfg.ID] = true
	}
	changed := false
	for mid := range h.listCache.machines {
		if !present[mid] {
			h.listCache.index.RemoveSource(mid)
			delete(h.listCache.machines, mid)
			changed = true
		}
	}
	return changed
}

func (h *Hub) broadcastSessionListNotice() {
	revision := h.listCache.index.Revision()
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		if c.listMode {
			c.sendJSON(map[string]any{"type": "sessions_list", "revision": revision})
		}
	}
}

func sessionListErr(w http.ResponseWriter, err error) {
	code, status := "invalid_query", http.StatusBadRequest
	var typed *sessionlist.Error
	if errors.As(err, &typed) {
		code = typed.Code
		if code == "cursor_expired" || code == "revision_expired" {
			status = http.StatusConflict
		}
	}
	writeErrCode(w, status, code, i18n.Err("session-list."+code))
}

// A paginated subscriber gets no whole list, including on reconnect. The HTTP
// page/revision is authoritative; this event only wakes a delta/count request.
func (h *Hub) sessionListNotice(c *client) {
	c.sendJSON(map[string]any{"type": "sessions_list", "revision": h.sessionListIndex().Revision()})
}

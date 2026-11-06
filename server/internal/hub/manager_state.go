package hub

import (
	"context"
	"slices"
	"sort"
	"sync"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/extwire"
	"github.com/nwverse/persona/server/internal/proto"
)

// What the hub keeps of each manager (docs/managers.md "What the hub keeps of each manager"): its snapshot (its
// conversations, tasks, profile, todo links), its tools, its extensions' report and its Live Activity content. The
// assistant the hub starts keeps its state where it always did (h.assistant, h.ext.report, h.reported.live); every
// other manager has one of its own here, made when it first reports and dropped when it is removed. Nothing a manager
// reports replaces another's. Where the hub needs one of a thing for itself:
//
//   - its profile (the user's time zone, where skills go): the assistant it starts; only that one is kept in the
//     database (assistant_profile.go), the others' live in memory and say which conversation is their main one;
//   - the Live Activity's content: the default manager's, else the assistant's;
//   - the extensions' report (features, prefixes, looks, each machine's extras): every manager's merged, a machine's
//     extras from its own manager, a prefix served by the first manager that reports it (the assistant's first).

// managerStates are the managers' states but the assistant's, by id.
type managerStates struct {
	mu sync.Mutex
	by map[string]*assistantState
}

// assistantOf is manager id's state ("" and the assistant's id: h.assistant).
func (h *Hub) assistantOf(id string) *assistantState {
	if id == "" || id == assistantwire.AssistantID {
		return &h.assistant
	}
	s := &h.others
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.by == nil {
		s.by = map[string]*assistantState{}
	}
	a := s.by[id]
	if a == nil {
		a = &assistantState{from: id}
		s.by[id] = a
	}
	return a
}

// managerStateIDs are the managers with a state: the assistant first, then the others by id.
func (h *Hub) managerStateIDs() []string {
	s := &h.others
	s.mu.Lock()
	out := make([]string, 0, len(s.by)+1)
	for id := range s.by {
		out = append(out, id)
	}
	s.mu.Unlock()
	sort.Strings(out)
	return append([]string{assistantwire.AssistantID}, out...)
}

// forgetManagerState drops what the hub keeps of removed manager id (its conversations, tools, report, live).
func (h *Hub) forgetManagerState(id string) {
	if id == "" || id == assistantwire.AssistantID {
		return
	}
	s := &h.others
	s.mu.Lock()
	delete(s.by, id)
	s.mu.Unlock()
	h.takeTodoReport(id, &assistantwire.TodoReport{})
	h.ext.reportsMu.Lock()
	delete(h.ext.reports, id)
	h.ext.reportsMu.Unlock()
	f := &h.reported
	f.mu.Lock()
	delete(f.lives, id)
	f.mu.Unlock()
}

// conversation is the manager whose snapshot lists conversation sid, and how it lists it; "" for none.
func (h *Hub) conversation(sid string) (string, assistantwire.SessionView) {
	if sid == "" {
		return "", assistantwire.SessionView{}
	}
	for _, id := range h.managerStateIDs() {
		a := h.assistantOf(id)
		a.mu.RLock()
		v, ok := a.sessions[sid]
		a.mu.RUnlock()
		if ok {
			return id, v
		}
	}
	return "", assistantwire.SessionView{}
}

// mainOf: session on machine is some manager's main conversation, as it reported (the assistant's as the hub
// kept it).
func (h *Hub) mainOf(machine, session string) bool {
	if p := h.assistantProfile(); p.Main != "" && p.Main == session && p.MachineID == machine {
		return true
	}
	for _, id := range h.managerStateIDs()[1:] {
		a := h.assistantOf(id)
		a.mu.RLock()
		p := a.profile
		a.mu.RUnlock()
		if p != nil && p.Main != "" && p.Main == session && p.MachineID == machine {
			return true
		}
	}
	return false
}

// managerTools are manager id's tools (not the assistant's: assistantToolsNow), asked for the first time they are
// needed after it connected and kept while it is away.
func (h *Hub) managerTools(ctx context.Context, id string) []proto.Tool {
	a := h.assistantOf(id)
	a.toolsMu.Lock()
	tools, fresh := a.tools, a.toolsFresh
	a.toolsMu.Unlock()
	if fresh || h.busConnOf(id) == nil {
		return tools
	}
	var res struct {
		Tools []proto.Tool `json:"tools"`
	}
	if err := h.callBus(ctx, id, assistantwire.MethodTools, nil, &res); err != nil {
		h.logf("assistant: [%s] tools: %v", id, err)
		return tools
	}
	a.toolsMu.Lock()
	a.tools, a.toolsFresh = res.Tools, true
	a.toolsMu.Unlock()
	return res.Tools
}

// managerReconnected: manager id (not the assistant) said its handshake: its tools are asked for again when needed.
func (h *Hub) managerReconnected(id string) {
	a := h.assistantOf(id)
	a.toolsMu.Lock()
	a.toolsFresh = false
	a.toolsMu.Unlock()
}

// ---- the extensions' reports ----

// extReportOf is manager id's extensions' report (nil: none yet).
func (h *Hub) extReportOf(id string) *extwire.Report {
	if id == assistantwire.AssistantID {
		return h.ext.report.Load()
	}
	h.ext.reportsMu.Lock()
	defer h.ext.reportsMu.Unlock()
	return h.ext.reports[id]
}

// keepExtReport keeps manager id's report; the one it replaces.
func (h *Hub) keepExtReport(id string, r *extwire.Report) *extwire.Report {
	if id == assistantwire.AssistantID {
		return h.ext.report.Swap(r)
	}
	h.ext.reportsMu.Lock()
	defer h.ext.reportsMu.Unlock()
	if h.ext.reports == nil {
		h.ext.reports = map[string]*extwire.Report{}
	}
	old := h.ext.reports[id]
	h.ext.reports[id] = r
	return old
}

// extReport is every manager's extensions' report merged (see the top of the file).
func (h *Hub) extReport() extwire.Report {
	out := extwire.Report{Machines: map[string]extwire.MachineExtras{}}
	for _, id := range h.extReporters() {
		r := h.extReportOf(id)
		if r == nil {
			continue
		}
		for _, f := range r.Features {
			if !slices.Contains(out.Features, f) {
				out.Features = append(out.Features, f)
			}
		}
		for _, a := range r.APIs {
			if !slices.ContainsFunc(out.APIs, func(b extwire.APIPrefix) bool { return b.Prefix == a.Prefix }) {
				out.APIs = append(out.APIs, a)
			}
		}
		for mid, x := range r.Machines {
			if _, has := out.Machines[mid]; !has || h.machineOwner(mid) == id {
				out.Machines[mid] = x
			}
		}
		out.ScreenKeepAwake = out.ScreenKeepAwake || r.ScreenKeepAwake
		before := slices.Clone(out.Looks) // an extension's looks come from the first manager that has it on
		for _, l := range r.Looks {
			if !slices.ContainsFunc(before, func(o extwire.LookPackage) bool { return o.Extension == l.Extension }) {
				out.Looks = append(out.Looks, l)
			}
		}
	}
	return out
}

// extReporters are the managers with a report: the assistant first, then the others by id.
func (h *Hub) extReporters() []string {
	h.ext.reportsMu.Lock()
	ids := make([]string, 0, len(h.ext.reports))
	for id := range h.ext.reports {
		ids = append(ids, id)
	}
	h.ext.reportsMu.Unlock()
	sort.Strings(ids)
	return append([]string{assistantwire.AssistantID}, ids...)
}

// extPrefixOwner is the manager whose extensions serve /api/v1/<prefix>/ ("": none).
func (h *Hub) extPrefixOwner(prefix string) string {
	for _, id := range h.extReporters() {
		if r := h.extReportOf(id); r != nil && slices.ContainsFunc(r.APIs, func(a extwire.APIPrefix) bool { return a.Prefix == prefix }) {
			return id
		}
	}
	return ""
}

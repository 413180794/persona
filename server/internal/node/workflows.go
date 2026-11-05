package node

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// Workflows: Claude Code's Workflow tool runs a script that drives many agents in the background. The transcript says
// when one was launched and when it ended (transcript.Meta.Workflows); its progress is on disk:
//
//	<session>/subagents/workflows/<run id>/journal.jsonl  {"type":"launched"}, {"type":"started","agentId","label","phase"},
//	                                                      {"type":"result","agentId","result"} — one line per event
//	<session>/subagents/workflows/<run id>/agent-<id>.jsonl / .meta.json  each agent's transcript ({description, workflowPhase})
//	<session>/workflows/<run id>.json                     written when the run ends: {status, error, durationMs, agentCount, …}
//	<session>/workflows/scripts/<name>-<run id>.js        the script: export const meta = {name, phases: [{title}, …]}
//
// docs/agent-states.md「后台 workflow」.

// workflowStall: a run that has written nothing for this long, with neither a notification nor a state file, is not
// running any more (its claude went away). Longer than a subagent's 20 minutes: one agent can sit in a long tool call.
const workflowStall = 30 * time.Minute

// workflowKeep: ended runs are listed for this long after they ended.
const workflowKeep = 24 * time.Hour

var (
	wfPhasesRe = regexp.MustCompile("(?s)phases:\\s*\\[(.*?)\\]")
	wfTitleRe  = regexp.MustCompile("title:\\s*['\"`]([^'\"`\\n]+)['\"`]")
	wfMetaRe   = regexp.MustCompile("(?s)export\\s+const\\s+meta\\s*=\\s*\\{(.*?)\\n\\}")
	wfMetaName = regexp.MustCompile("(?m)^\\s*name:\\s*['\"`]([^'\"`\\n]+)['\"`]")
)

// wfCache remembers a run's progress per run directory, re-read at most every metaMinInterval (the session list is
// rebuilt every second) and never again once the run has ended and its state file is read.
var wfCache = struct {
	sync.Mutex
	m map[string]wfMemo
}{m: map[string]wfMemo{}}

type wfMemo struct {
	readAt time.Time
	final  bool
	w      proto.Workflow
}

// setBackground fills s's background work from m: the background tasks (background subagents are listed as
// subagents) and the workflow runs. A workflow's task stays in Background only while its run is running, so a
// client that only knows Background still sees it, and one that has ended (or stalled) is not counted.
func (n *Node) setBackground(s *proto.Session, m quickMeta) {
	s.Workflows = n.workflows(s.SessionID, m.wf, m.wfScript)
	running := map[string]bool{}
	for _, w := range s.Workflows {
		running[w.TaskID] = w.Running
	}
	s.Background = nil
	for _, t := range m.bg {
		switch {
		case t.Kind == "agent":
		case t.Kind == "workflow" && !running[t.ID]:
		default:
			s.Background = append(s.Background, t)
		}
	}
}

// workflows is the session's workflow runs of the last day, newest first, with their progress read from disk.
func (n *Node) workflows(parent string, runs []proto.Workflow, scripts map[string]string) []proto.Workflow {
	if len(runs) == 0 {
		return nil
	}
	dir := n.subagentDir(parent)
	if dir == "" {
		return nil
	}
	var out []proto.Workflow
	for i := len(runs) - 1; i >= 0; i-- {
		w := readWorkflow(dir, parent, runs[i], scripts[runs[i].RunID])
		if !w.Running && time.Since(cmpTime(deref(w.EndedAt), w.UpdatedAt, w.StartedAt)) > workflowKeep {
			continue
		}
		out = append(out, w)
	}
	return out
}

func deref(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

func cmpTime(ts ...time.Time) time.Time {
	for _, t := range ts {
		if !t.IsZero() {
			return t
		}
	}
	return time.Time{}
}

// readWorkflow is run as the transcript tells it (launched, maybe ended), with its progress from disk. subagents is
// the session's subagents directory.
func readWorkflow(subagents, parent string, run proto.Workflow, script string) proto.Workflow {
	if run.RunID == "" || strings.ContainsAny(run.RunID, "/\\") || strings.Contains(run.RunID, "..") {
		return run
	}
	runDir := filepath.Join(subagents, "workflows", run.RunID)
	key := runDir + "\x00" + run.Status
	wfCache.Lock()
	memo, ok := wfCache.m[key]
	wfCache.Unlock()
	if ok && (memo.final || time.Since(memo.readAt) < metaMinInterval) {
		return memo.w
	}
	w, final := workflowProgress(runDir, filepath.Join(filepath.Dir(subagents), "workflows", run.RunID+".json"), parent, run, script)
	wfCache.Lock()
	wfCache.m[key] = wfMemo{readAt: time.Now(), final: final, w: w}
	wfCache.Unlock()
	return w
}

type wfJournalLine struct {
	Type    string `json:"type"`
	AgentID string `json:"agentId"`
	Label   string `json:"label"`
	Phase   string `json:"phase"`
}

type wfState struct {
	Status     string `json:"status"`
	Error      string `json:"error"`
	StartTime  int64  `json:"startTime"` // Unix ms
	DurationMS int64  `json:"durationMs"`
	Name       string `json:"workflowName"`
	Summary    string `json:"summary"`
	Phases     []struct {
		Title string `json:"title"`
	} `json:"phases"`
}

type wfAgentMeta struct {
	Description string `json:"description"`
	Phase       string `json:"workflowPhase"`
}

// workflowProgress reads run's progress: final when the run has ended and nothing about it will change.
func workflowProgress(runDir, stateFile, parent string, run proto.Workflow, script string) (proto.Workflow, bool) {
	w := run
	w.Agents = nil
	var state *wfState
	if b, err := os.ReadFile(stateFile); err == nil {
		var st wfState
		if json.Unmarshal(b, &st) == nil && st.Status != "" {
			state = &st
		}
	}
	if state != nil {
		w.Name, w.Summary = cmpStr(w.Name, state.Name), cmpStr(w.Summary, state.Summary)
		for _, p := range state.Phases {
			w.Phases = append(w.Phases, p.Title)
		}
		if state.StartTime > 0 && state.DurationMS > 0 && w.EndedAt == nil {
			at := time.UnixMilli(state.StartTime + state.DurationMS)
			w.EndedAt = &at
		}
	}
	if len(w.Phases) == 0 || w.Name == "" {
		name, phases := workflowScript(script)
		w.Name = cmpStr(w.Name, name)
		if len(w.Phases) == 0 {
			w.Phases = phases
		}
	}

	// The journal: which agents started, in which phase, and which returned.
	type agent struct {
		proto.WorkflowAgent
		done bool
	}
	var agents []*agent
	byID := map[string]*agent{}
	var updated time.Time
	if f, err := os.Open(filepath.Join(runDir, "journal.jsonl")); err == nil {
		if st, err := f.Stat(); err == nil {
			updated = st.ModTime()
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64<<10), 16<<20) // a result line holds the agent's whole answer
		for sc.Scan() {
			var l wfJournalLine
			if json.Unmarshal(sc.Bytes(), &l) != nil || l.AgentID == "" {
				continue
			}
			a := byID[l.AgentID]
			if a == nil {
				a = &agent{WorkflowAgent: proto.WorkflowAgent{ID: l.AgentID, SessionID: parent + subagentSep + l.AgentID}}
				byID[l.AgentID] = a
				agents = append(agents, a)
			}
			a.Label, a.Phase = cmpStr(l.Label, a.Label), cmpStr(l.Phase, a.Phase)
			if l.Type != "started" {
				a.done = true // "result", or however else it ended
			}
		}
		f.Close()
	}
	for _, a := range agents {
		if a.Label == "" || a.Phase == "" {
			if b, err := os.ReadFile(filepath.Join(runDir, "agent-"+a.ID+".meta.json")); err == nil {
				var m wfAgentMeta
				if json.Unmarshal(b, &m) == nil {
					a.Label, a.Phase = cmpStr(a.Label, m.Description), cmpStr(a.Phase, m.Phase)
				}
			}
		}
		jp := filepath.Join(runDir, "agent-"+a.ID+".jsonl")
		if st, err := os.Stat(jp); err == nil {
			a.UpdatedAt = st.ModTime()
			if a.UpdatedAt.After(updated) {
				updated = a.UpdatedAt
			}
			a.StartedAt, a.LastText = subagentEdgesCached(jp, st)
			a.LastText = snippet(a.LastText)
		}
	}
	w.UpdatedAt = updated

	// Status: the transcript's word (the notification, a TaskStop) first, then the state file, then signs of life.
	switch {
	case !run.Running:
	case state != nil && state.Status != proto.WorkflowRunning:
		w.Status = map[string]string{"failed": proto.WorkflowFailed, "killed": proto.WorkflowKilled,
			"aborted": proto.WorkflowKilled}[state.Status]
		if w.Status == "" {
			w.Status = proto.WorkflowCompleted
		}
	case time.Since(cmpTime(updated, run.StartedAt)) > workflowStall:
		w.Status = proto.WorkflowStalled
	default:
		w.Status = proto.WorkflowRunning
	}
	w.Running = w.Status == proto.WorkflowRunning
	if !w.Running && w.EndedAt == nil {
		at := cmpTime(updated, run.StartedAt)
		w.EndedAt = &at
	}
	if state != nil && w.Status != proto.WorkflowCompleted && state.Error != "" {
		w.Error = snippet(strings.SplitN(strings.TrimSpace(state.Error), "\n", 2)[0])
	}

	var latest, latestRunning string
	for _, a := range agents {
		w.AgentsStarted++
		a.Running = w.Running && !a.done
		switch {
		case a.done:
			w.AgentsDone++
		case a.Running:
			w.AgentsRunning++
		}
		if a.Phase != "" {
			latest = a.Phase
			if a.Running {
				latestRunning = a.Phase
			}
		}
	}
	w.Phase = cmpStr(latestRunning, latest) // where the latest started agent still at it is, else the latest one
	for i, p := range w.Phases {
		if p == w.Phase {
			w.PhaseIndex = i + 1
		}
	}
	if len(agents) > proto.WorkflowAgentsMax {
		agents = agents[len(agents)-proto.WorkflowAgentsMax:]
	}
	for _, a := range agents {
		w.Agents = append(w.Agents, a.WorkflowAgent)
	}
	return w, !w.Running && state != nil
}

// workflowScript reads a workflow's name and phase titles from its script's meta block.
func workflowScript(path string) (name string, phases []string) {
	if path == "" || !strings.HasSuffix(path, ".js") {
		return "", nil
	}
	f, err := os.Open(path)
	if err != nil {
		return "", nil
	}
	defer f.Close()
	head := make([]byte, 16<<10) // the meta block comes first
	k, _ := f.Read(head)
	m := wfMetaRe.FindSubmatch(head[:k])
	if m == nil {
		return "", nil
	}
	if nm := wfMetaName.FindSubmatch(m[1]); nm != nil {
		name = string(nm[1])
	}
	if p := wfPhasesRe.FindSubmatch(m[1]); p != nil {
		for _, t := range wfTitleRe.FindAllSubmatch(p[1], -1) {
			phases = append(phases, string(t[1]))
		}
	}
	return name, phases
}

func cmpStr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// workflowAgentFile finds agent's transcript among the session's workflow runs (subagents/workflows/<run>/), "" when
// it is not one of theirs.
func workflowAgentFile(subagents, agent string) string {
	if strings.ContainsAny(agent, "*?[]") {
		return ""
	}
	matches, _ := filepath.Glob(filepath.Join(subagents, "workflows", "wf_*", "agent-"+agent+".jsonl"))
	if len(matches) == 0 {
		return ""
	}
	return matches[0]
}

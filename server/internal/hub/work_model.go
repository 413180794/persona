package hub

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
)

// triageOut is what the triage model answers (prompts/agents/triage.md).
type triageOut struct {
	Phase     string           `json:"phase"`
	On        string           `json:"on"`
	Line      string           `json:"line"`
	Questions []proto.Question `json:"questions"`
	Link      *struct {
		URL     string `json:"url"`
		Minutes int    `json:"minutes"`
	} `json:"link"`
	Wait *struct {
		What         string `json:"what"`
		Then         string `json:"then"`
		CheckMinutes int    `json:"check_minutes"`
		Agent        string `json:"agent"`
		Risky        bool   `json:"risky"`
	} `json:"wait"`
	Artifacts []proto.WorkArtifact `json:"artifacts"`
}

// triageTurnModel asks the triage model where s stands after its turn. An answer it cannot read is unsure: a work state
// with no phase (nothing shown, nothing done; the turn is not triaged again).
func (h *Hub) triageTurnModel(ctx context.Context, m *Machine, s proto.Session) (*proto.WorkState, error) {
	ask, reply := lastTurnOf(ctx, m, s.SessionID)
	if reply == "" {
		reply = strings.TrimSpace(s.LastText)
	}
	if reply == "" {
		return &proto.WorkState{}, nil
	}
	others, byShort := h.triageOthers(m, s.SessionID)
	in := prompts.AgentTriageInput{
		Now: time.Now().In(h.userLocation()).Format("2006-01-02 15:04 Mon"), Agent: agentLabel(s), Machine: m.cfg.Name,
		Cwd: s.Cwd, Ask: clipMiddleRunes(ask, 500), Reply: clipMiddleRunes(reply, 1500), Others: others,
	}
	if e := h.kb.eng; e != nil {
		var names []string
		for _, p := range e.ProjectsForDir(m.cfg.ID, s.Cwd) {
			names = append(names, p.Name)
		}
		in.Project = strings.Join(first(names, 3), "、")
	}
	var out triageOut
	if err := h.purposeModel("agent.triage").JSON(ctx, prompts.RenderAgentTriage(prompts.AgentTriage{Owner: h.ownerName()}),
		prompts.RenderAgentTriageInput(in), &out); err != nil {
		return nil, err
	}
	return triageWork(out, byShort, time.Now()), nil
}

// triageWork turns the model's answer into a work state, keeping only what it may say.
func triageWork(out triageOut, others map[string]proto.WorkAgent, now time.Time) *proto.WorkState {
	w := &proto.WorkState{Line: clip(oneLine(out.Line), 60)}
	for _, a := range first(out.Artifacts, 5) {
		if k := strings.ToLower(strings.TrimSpace(a.Kind)); slices.Contains([]string{"pr", "commit", "doc", "build", "url"}, k) && strings.TrimSpace(a.Ref) != "" {
			w.Artifacts = append(w.Artifacts, proto.WorkArtifact{Kind: k, Ref: clip(strings.TrimSpace(a.Ref), 300)})
		}
	}
	switch strings.ToLower(strings.TrimSpace(out.Phase)) {
	case proto.WorkDone:
		w.Phase = proto.WorkDone
		if w.Line == "" {
			w.Line = "做完了"
		}
		return w
	case proto.WorkBlocked:
		w.Phase = proto.WorkBlocked
	default:
		return &proto.WorkState{} // unsure, or made up
	}
	w.On = strings.ToLower(strings.TrimSpace(out.On))
	if wt := out.Wait; wt != nil {
		w.Wait, w.Then, w.Every = clip(oneLine(wt.What), 80), clip(oneLine(wt.Then), 80), wt.CheckMinutes
		if a, ok := others[strings.TrimSpace(wt.Agent)]; ok && w.On == proto.WaitAgent {
			w.Agent = &a
		}
		if wt.Risky {
			w.On = proto.WaitUser // never a wait the hub ends itself (docs/agent-work.md「安全边界」)
		}
	}
	switch w.On {
	case proto.WaitAgent:
		if w.Agent == nil {
			w.On = proto.WaitExternal // it named no agent we know: check back on a timer
		}
	case proto.WaitExternal, proto.WaitUser:
	default:
		w.On = proto.WaitUser
	}
	if (w.On == proto.WaitExternal || w.On == proto.WaitAgent) && w.Wait == "" {
		w.Wait = w.Line
	}
	if w.On == proto.WaitUser {
		w.Questions = cardQuestions(out.Questions)
		if l := out.Link; l != nil && httpsLink(l.URL) {
			w.Link = &proto.WorkLink{URL: strings.TrimSpace(l.URL)}
			if l.Minutes > 0 && l.Minutes <= 24*60 {
				at := now.Add(time.Duration(l.Minutes) * time.Minute).UTC()
				w.Link.ExpiresAt = &at
			}
		}
	}
	if w.Line == "" {
		w.Line = "在等你"
	}
	return w
}

// cardQuestions are the model's questions as a card can ask them: at most 4, each with 2–4 options.
func cardQuestions(qs []proto.Question) []proto.Question {
	var out []proto.Question
	seen := map[string]bool{}
	for _, q := range qs {
		text := strings.TrimSpace(q.Question)
		if text == "" || seen[text] || len(out) == 4 {
			continue
		}
		seen[text] = true
		c := proto.Question{Question: clip(text, 500), Header: clip(oneLine(q.Header), 12), MultiSelect: q.MultiSelect}
		labels := map[string]bool{}
		for _, o := range q.Options {
			l := clip(oneLine(o.Label), 60)
			if l == "" || labels[l] || len(c.Options) == 4 {
				continue
			}
			labels[l] = true
			c.Options = append(c.Options, proto.QuestionOption{Label: l, Description: clip(oneLine(o.Description), 120)})
		}
		if len(c.Options) < 2 {
			c.Options = append(c.Options, proto.QuestionOption{Label: answerSkip})
		}
		out = append(out, c)
	}
	return out
}

// triageOthers are the agents working on m besides sid (what it may wait for), by short id.
func (h *Hub) triageOthers(m *Machine, sid string) ([]prompts.AgentTriageOther, map[string]proto.WorkAgent) {
	var out []prompts.AgentTriageOther
	by := map[string]proto.WorkAgent{}
	for _, o := range m.sessionList() {
		if o.SessionID == sid || o.Status != proto.StatusWorking || isAssistant(o, m.assistantCwd()) || len(out) == 12 {
			continue
		}
		id := short(o.SessionID)
		line := o.Activity
		if line == "" {
			line = o.LastText
		}
		out = append(out, prompts.AgentTriageOther{ID: id, Label: agentLabel(o), Line: clip(oneLine(line), 80)})
		by[id] = proto.WorkAgent{MachineID: m.cfg.ID, SessionID: o.SessionID, Label: agentLabel(o)}
	}
	return out, by
}

// lastTurnOf is a session's latest turn: what the user asked in it ("" when none) and its last reply ("" when the
// turn cannot be read).
func lastTurnOf(ctx context.Context, m *Machine, sid string) (ask, answer string) {
	var res struct {
		Items []proto.Item `json:"items"`
	}
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := m.call(cctx, "session.items", map[string]any{"session_id": sid, "turns": 1}, &res); err != nil {
		return "", ""
	}
	for _, it := range res.Items {
		if it.Kind == proto.KindText && it.Role == "user" && strings.TrimSpace(it.Text) != "" {
			ask = strings.TrimSpace(it.Text)
			break
		}
	}
	for i := len(res.Items) - 1; i >= 0; i-- {
		if it := res.Items[i]; it.Kind == proto.KindText && it.Role == "assistant" && strings.TrimSpace(it.Text) != "" {
			return ask, strings.TrimSpace(it.Text)
		}
	}
	return ask, ""
}

// clipMiddleRunes keeps a long text's first and last n runes: a last answer says what was done first and what is
// left last.
func clipMiddleRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= 2*n {
		return s
	}
	r := []rune(s)
	return string(r[:n]) + "\n…\n" + string(r[len(r)-n:])
}

func first[T any](s []T, n int) []T {
	if len(s) > n {
		return s[:n]
	}
	return s
}

type agentWake = prompts.AgentWake

func renderWake(w agentWake) string { return prompts.RenderAgentWake(w) }

func renderAnswer(items []agentAnswerItem) string {
	a := prompts.AgentAnswer{}
	for _, it := range items {
		a.Items = append(a.Items, prompts.AgentAnswerItem{Question: oneLine(it.question), Answer: it.answer})
	}
	return prompts.RenderAgentAnswer(a)
}

// workLogLine is what the knowledge base's project timeline gets for a finished turn: its conclusion when the hub
// found it done, else its last words (kbAgentDone).
func workLogLine(s proto.Session) string {
	if w := s.Work; w != nil && w.Phase == proto.WorkDone && w.Line != "" {
		line := "完成：" + w.Line
		for _, a := range w.Artifacts {
			if a.Kind == "pr" || a.Kind == "doc" {
				line += " " + filepath.ToSlash(a.Ref)
			}
		}
		return line
	}
	return s.LastText
}

// ---- stats ----

// workStats is GET /api/v1/agents/work/stats: how the triage did over the last hours (docs/agent-work.md「评估」).
type workStats struct {
	Hours    int            `json:"hours"`
	Triaged  map[string]int `json:"triaged"` // "done", "blocked/user", … "unsure", "failed"
	By       map[string]int `json:"by"`      // rule, model
	Actions  map[string]int `json:"actions"` // continued, woke, asked, answered, withdrawn
	Resumed  map[string]int `json:"resumed"` // by user / hub after a triaged turn
	Unread   sampleStats    `json:"unread"`  // 完成未看 over the hours
	NeedsYou sampleStats    `json:"needs_you"`
	// Waits: how long a triaged turn waited for the user's first look or reply (seconds), by what it was.
	Waits map[string]waitStats `json:"waits"`
}

type sampleStats struct {
	Samples int     `json:"samples"`
	Avg     float64 `json:"avg"`
	Max     int     `json:"max"`
	Last    int     `json:"last"`
}

type waitStats struct {
	N      int     `json:"n"`
	Median float64 `json:"median_seconds"`
	P90    float64 `json:"p90_seconds"`
}

func (h *Hub) workStatsRoutes(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/agents/work/stats", func(w http.ResponseWriter, r *http.Request) {
		hours, _ := strconv.Atoi(r.URL.Query().Get("hours"))
		if hours <= 0 || hours > 24*30 {
			hours = 24
		}
		st := h.workDB()
		if st == nil {
			writeErr(w, http.StatusServiceUnavailable, errors.New("no database"))
			return
		}
		events, err := st.WorkEvents(r.Context(), time.Now().Add(-time.Duration(hours)*time.Hour))
		if err != nil {
			writeErr(w, http.StatusServiceUnavailable, err)
			return
		}
		writeJSON(w, summarizeWork(hours, events))
	})
}

// summarizeWork counts the events; a turn's wait is its first look or reply (later ones are not waits).
func summarizeWork(hours int, events []db.WorkEvent) workStats {
	s := workStats{Hours: hours, Triaged: map[string]int{}, By: map[string]int{}, Actions: map[string]int{}, Resumed: map[string]int{},
		Waits: map[string]waitStats{}}
	waits := map[string][]float64{}
	firstLook := map[string]bool{} // machine/session/turn handled already
	var unread, needs []int
	for _, e := range events {
		str := func(k string) string { v, _ := e.Detail[k].(string); return v }
		num := func(k string) float64 { v, _ := e.Detail[k].(float64); return v }
		switch e.Kind {
		case "triaged":
			k := str("phase")
			if k == "" {
				k = "unsure"
			} else if on := str("on"); on != "" {
				k += "/" + on
			}
			s.Triaged[k]++
			s.By[str("by")]++
		case "failed":
			s.Triaged["failed"]++
		case "continued", "woke", "asked", "answered", "withdrawn":
			s.Actions[e.Kind]++
		case "resumed", "seen":
			if e.Kind == "resumed" {
				s.Resumed[str("by")]++
				if str("by") == "hub" {
					continue
				}
			}
			turn := e.At.Add(-time.Duration(num("after") * float64(time.Second))).Truncate(time.Second)
			key := e.MachineID + "/" + e.SessionID + "/" + turn.Format(time.RFC3339)
			if firstLook[key] {
				continue
			}
			firstLook[key] = true
			k := str("phase")
			if on := str("on"); on != "" {
				k += "/" + on
			}
			waits[k] = append(waits[k], num("after"))
		case "sample":
			u, _ := e.Detail[proto.StateUnread].(float64)
			n, _ := e.Detail[proto.StateNeedsYou].(float64)
			unread, needs = append(unread, int(u)), append(needs, int(n))
		}
	}
	s.Unread, s.NeedsYou = samples(unread), samples(needs)
	for k, v := range waits {
		slices.Sort(v)
		s.Waits[k] = waitStats{N: len(v), Median: v[len(v)/2], P90: v[min(len(v)-1, len(v)*9/10)]}
	}
	return s
}

func samples(v []int) sampleStats {
	if len(v) == 0 {
		return sampleStats{}
	}
	s := sampleStats{Samples: len(v), Last: v[len(v)-1]}
	sum := 0
	for _, n := range v {
		sum += n
		s.Max = max(s.Max, n)
	}
	s.Avg = float64(sum) / float64(len(v))
	return s
}

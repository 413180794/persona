package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
)

// Tools only the assistant (the 对话 tab's headless Claude) gets: asking on the phone (headless claude has no
// AskUserQuestion), pushing, waking itself up later, the coding agents the hub already tracks, and Feishu through
// the hub's lark-connector client. They answer from the hub's own state, so no ssh, no tokens, no scraping.

// assistantTools are the tools only assistant conversations get; their descriptions and schemas are in
// prompts/mcp (docs/prompts.md).
func assistantTools() []proto.Tool {
	names := []string{"ask_user", "notify_user", "schedule_followup", "schedule_routine", "delete_routine", "list_followups",
		"cancel_followup", "agents_list", "agent_read", "agent_send", "agent_start", "interaction_answer", "inbox_draft",
		"kb_search", "kb_list", "kb_get", "kb_update", "jarvis_settings",
		"conversations_list", "conversation_read", "conversation_rename", "conversation_archive", "conversation_unarchive",
		"conversation_new", "conversation_send", "task_plan"}
	tools := make([]proto.Tool, 0, len(names))
	for _, n := range names {
		tools = append(tools, mcpTool(n))
	}
	return tools
}

// mcpTool is one tool's definition from prompts/mcp.
func mcpTool(name string) proto.Tool {
	t := prompts.Tool(name)
	return proto.Tool{Name: t.Name, Description: t.Description, InputSchema: t.InputSchema}
}

func isAssistantTool(name string) bool {
	switch name {
	case "ask_user", "notify_user", "schedule_followup", "list_followups", "cancel_followup", "schedule_routine", "delete_routine",
		"agents_list", "agent_read", "agent_send", "agent_start", "interaction_answer", "inbox_draft", "jarvis_settings",
		"task_plan":
		return true
	}
	return strings.HasPrefix(name, "kb_") || strings.HasPrefix(name, "conversation")
}

// callAssistantTool runs one assistant tool for the conversation sessionID on machine m.
func (h *Hub) callAssistantTool(ctx context.Context, m *Machine, sessionID, name string, args json.RawMessage) proto.ToolResult {
	res, err := h.runAssistantTool(ctx, m, sessionID, name, args)
	if err != nil {
		return proto.TextResult(err.Error(), true)
	}
	return proto.TextResult(res, false)
}

func (h *Hub) runAssistantTool(ctx context.Context, m *Machine, sessionID, name string, args json.RawMessage) (string, error) {
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	switch name {
	case "ask_user":
		return h.toolAskUser(ctx, m, sessionID, args)
	case "jarvis_settings":
		return h.toolJarvisSettings(ctx, m, sessionID, args)
	case "task_plan":
		return h.toolTaskPlan(ctx, sessionID, args)
	case "notify_user":
		var a struct{ Title, Body string }
		if err := json.Unmarshal(args, &a); err != nil || strings.TrimSpace(a.Title+a.Body) == "" {
			return "", fmt.Errorf("title and body are required")
		}
		h.notifyDone("assistant", m.cfg.ID, "Persona", sessionID, a.Title, a.Body, pushWants{}) // asked for explicitly: every device
		h.botPush(m.cfg.ID, sessionID, a.Title, a.Body)
		h.channelPush(a.Title, a.Body)
		h.mu.Lock()
		h.notified[sessionID] = time.Now()
		h.mu.Unlock()
		return "Notification sent.", nil
	case "schedule_followup":
		return h.toolScheduleFollowup(ctx, m, sessionID, args)
	case "list_followups":
		return h.describeSchedule(ctx), nil
	case "schedule_routine":
		return h.toolScheduleRoutine(ctx, args)
	case "delete_routine":
		var a struct{ ID string }
		_ = json.Unmarshal(args, &a)
		ok, err := h.deleteTask(ctx, a.ID)
		if err != nil {
			return "", err
		}
		if !ok {
			return "", fmt.Errorf("no task %q", a.ID)
		}
		return "Deleted.", nil
	case "cancel_followup":
		var a struct{ ID string }
		_ = json.Unmarshal(args, &a)
		ok, err := h.deleteTask(ctx, a.ID)
		if err != nil {
			return "", err
		}
		if !ok {
			return "", fmt.Errorf("no task %q", a.ID)
		}
		return "Cancelled.", nil
	case "inbox_draft":
		return h.toolInboxDraft(ctx, sessionID, args)
	case "kb_search", "kb_list", "kb_get", "kb_update":
		return h.runKBTool(name, args)
	case "conversations_list":
		return h.toolConversationsList(ctx, sessionID, args)
	case "conversation_read":
		return h.toolConversationRead(ctx, args)
	case "conversation_rename":
		return h.toolConversationRename(ctx, sessionID, args)
	case "conversation_archive", "conversation_unarchive":
		return h.toolConversationArchive(ctx, sessionID, args, name == "conversation_archive")
	case "conversation_new":
		return h.toolConversationNew(ctx, sessionID, args)
	case "conversation_send":
		return h.toolConversationSend(ctx, sessionID, args)
	case "agents_list":
		var a struct{ State, Machine, Query string }
		_ = json.Unmarshal(args, &a)
		return h.describeAgents(a.State, a.Machine, a.Query), nil
	case "interaction_answer":
		var a struct {
			Machine       string          `json:"machine"`
			SessionID     string          `json:"session_id"`
			InteractionID string          `json:"interaction_id"`
			Answers       json.RawMessage `json:"answers"`
		}
		_ = json.Unmarshal(args, &a)
		return h.toolInteractionAnswer(ctx, m, sessionID, a.Machine, a.SessionID, a.InteractionID, a.Answers)
	case "agent_read":
		var a struct {
			Machine   string `json:"machine"`
			SessionID string `json:"session_id"`
			Turns     int    `json:"turns"`
		}
		_ = json.Unmarshal(args, &a)
		return h.toolAgentRead(ctx, a.Machine, a.SessionID, a.Turns)
	case "agent_send":
		var a struct {
			Machine   string `json:"machine"`
			SessionID string `json:"session_id"`
			Text      string `json:"text"`
		}
		_ = json.Unmarshal(args, &a)
		am, s, err := h.findAgent(a.Machine, a.SessionID)
		if err != nil {
			return "", err
		}
		if s.Assistant {
			return "", fmt.Errorf("that is an assistant conversation, not a coding agent")
		}
		if s.SourceKind != "" && (s.Capabilities == nil || !s.Capabilities.Send) {
			return "", fmt.Errorf("这个 Agent 仅支持查看：%s", s.ReadOnlyReason)
		}
		if strings.TrimSpace(a.Text) == "" {
			return "", fmt.Errorf("text is empty")
		}
		if err := am.call(ctx, "session.send", map[string]any{"session_id": s.SessionID, "text": a.Text}, nil); err != nil {
			return "", err
		}
		return fmt.Sprintf("Sent to %s on %s.", agentLabel(s), am.cfg.Name), nil
	case "agent_start":
		var a struct {
			Machine  string `json:"machine"`
			Cwd      string `json:"cwd"`
			Prompt   string `json:"prompt"`
			Launcher string `json:"launcher"`
		}
		_ = json.Unmarshal(args, &a)
		am := h.machineByName(a.Machine)
		if am == nil {
			return "", fmt.Errorf("unknown machine %q (machines: %s)", a.Machine, h.machineIDs())
		}
		if strings.HasPrefix(a.Cwd, "~/") {
			if home := am.snapshot().Home; home != "" {
				a.Cwd = path.Join(home, a.Cwd[2:])
			}
		}
		var res struct {
			Session proto.Session `json:"session"`
		}
		sctx, cancel := context.WithTimeout(ctx, 100*time.Second)
		defer cancel()
		params := map[string]any{"cwd": a.Cwd, "launcher": a.Launcher, "prompt": a.Prompt, "tab_name": h.autoTabName(sctx, a.Prompt)}
		if err := h.applyModel(sctx, "agent.new", params); err != nil {
			return "", err
		}
		if err := am.call(sctx, "session.start", params, &res); err != nil {
			return "", err
		}
		return fmt.Sprintf("Started on %s in %s: session %s. It shows up in the app's Agents tab.", am.cfg.Name, a.Cwd, res.Session.SessionID), nil
	}
	return "", fmt.Errorf("unknown tool %s", name)
}

func (h *Hub) toolAskUser(ctx context.Context, m *Machine, sessionID string, args json.RawMessage) (string, error) {
	var a struct {
		Questions []struct {
			Question    string                 `json:"question"`
			Header      string                 `json:"header"`
			MultiSelect bool                   `json:"multiSelect"`
			Options     []proto.QuestionOption `json:"options"`
		} `json:"questions"`
	}
	if err := json.Unmarshal(args, &a); err != nil || len(a.Questions) == 0 {
		return "", fmt.Errorf("questions are required")
	}
	ix := proto.Interaction{
		ID: "ix_" + randomHex(6), MachineID: m.cfg.ID, SessionID: sessionID, SessionTitle: "Persona", Kind: proto.IxQuestion,
		ToolName: "ask_user", ToolInput: args, CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().Add(30 * time.Minute).UTC(),
	}
	if s, ok := m.session(sessionID); ok {
		ix.Cwd = s.Cwd
		if s.Title != "" {
			ix.SessionTitle = s.Title
		}
	}
	for _, q := range a.Questions {
		ix.Questions = append(ix.Questions, proto.Question{Question: q.Question, Header: q.Header, MultiSelect: q.MultiSelect, Options: q.Options})
	}
	r, err := h.waitInteraction(ctx, ix)
	if err != nil {
		return "", err
	}
	if r.Dismiss || len(r.Answers) == 0 {
		return "The user did not answer (dismissed or timed out). Don't go ahead with what you asked about; say what is waiting for them.", nil
	}
	var b strings.Builder
	b.WriteString("The user answered:\n")
	for _, q := range a.Questions {
		if ans, ok := r.Answers[q.Question]; ok {
			fmt.Fprintf(&b, "- %s → %s\n", q.Question, ans)
		}
	}
	return b.String(), nil
}

// ---- agents ----

func (h *Hub) machineByName(id string) *Machine {
	if m := h.localMachine(id); m != nil {
		return m
	}
	for _, m := range h.machinesNow() {
		if strings.EqualFold(m.cfg.Name, id) || strings.EqualFold(m.cfg.SSHHost, id) {
			return m
		}
	}
	return nil
}

func (h *Hub) machineIDs() string {
	ids := make([]string, 0, len(h.machinesNow()))
	for _, m := range h.machinesNow() {
		ids = append(ids, m.cfg.ID)
	}
	return strings.Join(ids, ", ")
}

// findAgent resolves a session by full id or prefix; an empty machine searches all of them.
func (h *Hub) findAgent(machine, id string) (*Machine, proto.Session, error) {
	id = strings.TrimSpace(id)
	if len(id) < 6 {
		return nil, proto.Session{}, fmt.Errorf("session_id is required (at least its first 8 characters)")
	}
	var found *Machine
	var session proto.Session
	for _, m := range h.machinesNow() {
		if machine != "" && h.machineByName(machine) != m {
			continue
		}
		for _, s := range h.decorated(m) {
			if s.SessionID == id || strings.HasPrefix(s.SessionID, id) {
				if found != nil {
					return nil, proto.Session{}, fmt.Errorf("session_id %s is ambiguous; use the full id and machine", id)
				}
				found, session = m, s
			}
		}
	}
	if found != nil {
		return found, session, nil
	}
	return nil, proto.Session{}, fmt.Errorf("no agent %s (use agents_list)", id)
}

func agentLabel(s proto.Session) string {
	title := s.Title
	if title == "" || title == "Claude Code" {
		title = path.Base(s.Cwd)
	}
	if s.SpaceName != "" {
		slot := s.SlotName
		if slot == "" || slot == title {
			return s.SpaceName + " › " + title
		}
		return s.SpaceName + " › " + slot + "「" + title + "」"
	}
	return title
}

var stateNames = map[string]string{
	proto.StateNeedsYou: "需要你", proto.StateWorking: "工作中", proto.StateUnread: "完成未看", proto.StateIdle: "空闲",
	proto.StateOffline: "离线", proto.StateUnknown: "未知",
}

func (h *Hub) describeAgents(state, machine, query string) string {
	if state == "" {
		state = "active"
	}
	pending := map[[2]string][]string{}
	for _, ix := range h.pendingInteractions() {
		what := ix.Summary
		if ix.Kind == proto.IxQuestion && len(ix.Questions) > 0 {
			what = "提问：" + questionSummary(ix)
		} else if ix.Kind == proto.IxPlan {
			what = "计划待审批：" + firstLine(ix.Plan)
		} else if ix.Kind == proto.IxPermission {
			what = "要授权 " + ix.ToolName + "：" + ix.Summary
		} else if ix.Kind == proto.IxScreen {
			what = "终端对话框等你处理"
		}
		what += " (interaction_id=" + ix.ID + ")"
		k := [2]string{ix.MachineID, ix.SessionID}
		pending[k] = append(pending[k], clip(what, 400))
	}
	type row struct {
		m *Machine
		s proto.Session
	}
	var rows []row
	var offline []string
	for _, m := range h.machinesNow() {
		if machine != "" && h.machineByName(machine) != m {
			continue
		}
		if !m.snapshot().Online {
			offline = append(offline, m.cfg.Name)
		}
		for _, s := range h.decorated(m) {
			if s.Assistant {
				continue
			}
			switch state {
			case "all":
			case "active":
				if s.State != proto.StateNeedsYou && s.State != proto.StateWorking && s.State != proto.StateUnread {
					continue
				}
			default:
				if s.State != state {
					continue
				}
			}
			if query != "" {
				hay := strings.ToLower(s.Title + " " + s.Cwd + " " + s.SpaceName + " " + s.SlotName)
				if !strings.Contains(hay, strings.ToLower(query)) {
					continue
				}
			}
			rows = append(rows, row{m, s})
		}
	}
	order := map[string]int{proto.StateNeedsYou: 0, proto.StateWorking: 1, proto.StateUnread: 2, proto.StateIdle: 3}
	sort.SliceStable(rows, func(i, j int) bool {
		oi, oj := order[rows[i].s.State], order[rows[j].s.State]
		if oi != oj {
			return oi < oj
		}
		return rows[i].s.UpdatedAt.After(rows[j].s.UpdatedAt)
	})
	if len(rows) > 60 {
		rows = rows[:60]
	}
	var b strings.Builder
	if len(rows) == 0 {
		b.WriteString("No agents match.\n")
	}
	for _, r := range rows {
		s := r.s
		fmt.Fprintf(&b, "- [%s] %s · machine=%s session_id=%s · %s", stateNames[s.State], agentLabel(s), r.m.cfg.ID, s.SessionID, s.Cwd)
		if s.SourceKind != "" {
			fmt.Fprintf(&b, " · source=%s instance=%s", s.SourceKind, s.SourceInstanceID)
			if s.Capabilities == nil || !s.Capabilities.Send {
				fmt.Fprintf(&b, " · 仅查看：%s", s.ReadOnlyReason)
			}
		}
		if !s.UpdatedAt.IsZero() {
			fmt.Fprintf(&b, " · %s", ago(s.UpdatedAt))
		}
		if s.Loop != nil {
			b.WriteString(" · /loop")
		}
		if n := len(s.Background); n > 0 {
			fmt.Fprintf(&b, " · 后台任务 %d", n)
		}
		b.WriteString("\n")
		for _, p := range pending[[2]string{r.m.cfg.ID, s.SessionID}] {
			fmt.Fprintf(&b, "    等你：%s\n", p)
		}
		if s.LastText != "" {
			fmt.Fprintf(&b, "    最后说：%s\n", clip(oneLine(s.LastText), 200))
		}
	}
	if len(offline) > 0 {
		fmt.Fprintf(&b, "(offline machines: %s)\n", strings.Join(offline, ", "))
	}
	return b.String()
}

func (h *Hub) toolAgentRead(ctx context.Context, machine, id string, turns int) (string, error) {
	m, s, err := h.findAgent(machine, id)
	if err != nil {
		return "", err
	}
	if turns <= 0 {
		turns = 2
	}
	var res struct {
		Items []proto.Item `json:"items"`
	}
	if err := m.call(ctx, "session.items", map[string]any{"session_id": s.SessionID, "turns": min(turns, 10)}, &res); err != nil {
		return "", err
	}
	return transcriptText(fmt.Sprintf("%s on %s (%s, %s)", agentLabel(s), m.cfg.Name, stateNames[s.State], s.Cwd), res.Items), nil
}

// transcriptText is a conversation's recent items for a model to read (agent_read, conversation_read): the
// messages, tool calls in one line each, failed results, cut to the last 40 000 bytes.
func transcriptText(head string, items []proto.Item) string {
	var b strings.Builder
	b.WriteString(head + "\n\n")
	for _, it := range items {
		switch it.Kind {
		case proto.KindText:
			who := "Agent"
			if it.Role == "user" {
				who = "User"
				if it.From != "" {
					who = "Message from " + it.From
				}
			}
			fmt.Fprintf(&b, "%s: %s\n\n", who, clip(it.Text, 4000))
		case proto.KindThinking:
			if t := strings.TrimSpace(it.Text); t != "" {
				fmt.Fprintf(&b, "(agent, between tools) %s\n", clip(oneLine(t), 400))
			}
		case proto.KindToolUse:
			fmt.Fprintf(&b, "→ %s %s\n", it.ToolName, clip(oneLine(toolSummary(it.ToolName, it.ToolInput)), 200))
		case proto.KindToolResult:
			if it.IsError {
				fmt.Fprintf(&b, "  ✗ %s\n", clip(oneLine(it.Text), 300))
			}
		case proto.KindNotice, proto.KindRecap:
			if it.Text != "" {
				fmt.Fprintf(&b, "(%s)\n", clip(oneLine(it.Text), 300))
			}
		case proto.KindImage:
			b.WriteString("[image]\n")
		}
	}
	out := b.String()
	if len(out) > 40000 {
		out = "…(earlier part cut)\n" + out[len(out)-40000:]
	}
	return out
}

// toolSummary is the one-line gist of a tool call (command, file, url…).
func toolSummary(name string, input json.RawMessage) string {
	var m map[string]any
	_ = json.Unmarshal(input, &m)
	for _, k := range []string{"command", "file_path", "url", "query", "pattern", "description", "prompt", "path"} {
		if s, ok := m[k].(string); ok && s != "" {
			return s
		}
	}
	b, _ := json.Marshal(m)
	return string(b)
}

// ---- Feishu ----

// compactJSON drops media / avatar noise and cuts long answers so they fit in the model's context.
func compactJSON(raw json.RawMessage, max int) string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return clip(string(raw), max)
	}
	v = prune(v)
	b, _ := json.Marshal(v)
	if len(b) > max {
		return string(b[:max]) + "…(cut; narrow the query or page)"
	}
	return string(b)
}

func prune(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			lk := strings.ToLower(k)
			if strings.Contains(lk, "avatar") || strings.HasSuffix(lk, "thumbnail") || lk == "highlights" || lk == "emojikey" {
				delete(t, k)
				continue
			}
			if val == nil || val == "" || val == false {
				delete(t, k)
				continue
			}
			t[k] = prune(val)
		}
	case []any:
		for i := range t {
			t[i] = prune(t[i])
		}
	}
	return v
}

// toolInteractionAnswer answers another agent's question for the user (interaction_answer, docs/assistant.md). Only a
// question may be answered here: a permission, a plan and a terminal dialog are the user's to decide, and the
// assistant's own conversation is never answered by itself.
func (h *Hub) toolInteractionAnswer(ctx context.Context, m *Machine, sessionID, machine, agentID, interactionID string, answers json.RawMessage) (string, error) {
	am, s, err := h.findAgent(machine, agentID)
	if err != nil {
		return "", err
	}
	if s.Assistant || s.SessionID == sessionID {
		return "", fmt.Errorf("that interaction belongs to an assistant conversation; only the user answers it")
	}
	h.mu.Lock()
	p := h.interactions[strings.TrimSpace(interactionID)]
	var ix proto.Interaction
	if p != nil {
		ix = p.ix
	}
	h.mu.Unlock()
	if p == nil {
		return "", h.answeredInteractionError(ctx, interactionID)
	}
	if ix.Kind != proto.IxQuestion {
		return "", fmt.Errorf("interaction %s is a %s (approval), only the user can answer it in the app", ix.ID, ix.Kind)
	}
	if ix.MachineID != am.cfg.ID || ix.SessionID != s.SessionID {
		return "", fmt.Errorf("interaction %s belongs to another agent: machine=%s session_id=%s", ix.ID, ix.MachineID, ix.SessionID)
	}
	if time.Now().After(ix.ExpiresAt) {
		return "", fmt.Errorf("interaction %s expired", ix.ID)
	}
	res, err := interactionAnswers(ix, answers)
	if err != nil {
		return "", err
	}
	res.AnsweredBy = proto.AnsweredByAssistant
	if err := h.resolve(ix.ID, res); err != nil {
		if errors.Is(err, errNotPending) {
			return "", h.answeredInteractionError(ctx, ix.ID)
		}
		return "", err
	}
	var b strings.Builder
	b.WriteString("Answered for the user:\n")
	for _, q := range ix.Questions {
		fmt.Fprintf(&b, "- %s → %s\n", q.Question, res.Answers[q.Question])
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// answeredInteractionError says how an interaction that is no longer pending ended: from the history, or that there
// is none.
func (h *Hub) answeredInteractionError(ctx context.Context, id string) error {
	ix := strings.TrimSpace(id)
	if h.store != nil {
		if status, _, err := h.store.InteractionOutcome(context.Background(), ix); err == nil {
			switch status {
			case "resolved":
				return fmt.Errorf("interaction %s was already answered", ix)
			case "dismissed":
				return fmt.Errorf("interaction %s was dismissed", ix)
			case "expired":
				return fmt.Errorf("interaction %s expired", ix)
			case "cancelled":
				return fmt.Errorf("interaction %s was cancelled", ix)
			}
		}
	}
	return fmt.Errorf("no pending interaction %s (use agents_list)", ix)
}

// interactionAnswers checks a question's answers against the questions themselves and builds the resolution: every
// question answered, by the wording it was asked, with options it offers. A multi-select takes several labels and
// becomes the app's own form, "A, B". Free text is refused: the user answers that in the app, or through agent_send.
func interactionAnswers(ix proto.Interaction, args json.RawMessage) (proto.Resolution, error) {
	var raw map[string]json.RawMessage
	if len(args) == 0 || json.Unmarshal(args, &raw) != nil {
		return proto.Resolution{}, fmt.Errorf("answers is required: an object of question → option label")
	}
	asked := map[string]bool{}
	for _, q := range ix.Questions {
		asked[q.Question] = true
	}
	for k := range raw {
		if !asked[k] {
			return proto.Resolution{}, fmt.Errorf("%q is not a question this interaction asks; it asks: %s", k, askedQuestions(ix))
		}
	}
	answers := make(map[string]string, len(ix.Questions))
	for _, q := range ix.Questions {
		v, ok := raw[q.Question]
		if !ok {
			return proto.Resolution{}, fmt.Errorf("every question needs an answer, %q has none (options: %s)", q.Question, optionLabels(q))
		}
		var one string
		var many []string
		if json.Unmarshal(v, &many) != nil {
			if json.Unmarshal(v, &one) != nil {
				return proto.Resolution{}, fmt.Errorf("%q takes an option label or an array of them (options: %s)", q.Question, optionLabels(q))
			}
			if !q.MultiSelect {
				answers[q.Question] = one
			} else {
				many = []string{one}
			}
		}
		if many != nil {
			if !q.MultiSelect && len(many) > 1 {
				return proto.Resolution{}, fmt.Errorf("%q takes one option; pick one of: %s", q.Question, optionLabels(q))
			}
			answers[q.Question] = strings.Join(many, ", ")
		}
		if err := checkOptions(q, answers[q.Question]); err != nil {
			return proto.Resolution{}, err
		}
	}
	return proto.Resolution{Answers: answers}, nil
}

// checkOptions refuses an answer whose labels are not the question's options, listing them.
func checkOptions(q proto.Question, answer string) error {
	offered := map[string]bool{}
	for _, o := range q.Options {
		offered[o.Label] = true
	}
	picked := 0
	for _, label := range strings.Split(answer, ", ") {
		if !offered[label] {
			return fmt.Errorf("%q is not one of %q's options: %s", label, q.Question, optionLabels(q))
		}
		picked++
	}
	if picked == 0 {
		return fmt.Errorf("%q needs an option: %s", q.Question, optionLabels(q))
	}
	if !q.MultiSelect && picked > 1 {
		return fmt.Errorf("%q takes one option; pick one of: %s", q.Question, optionLabels(q))
	}
	return nil
}

func optionLabels(q proto.Question) string {
	labels := make([]string, len(q.Options))
	for i, o := range q.Options {
		labels[i] = o.Label
	}
	return strings.Join(labels, ", ")
}

// questionSummary is every question of an interaction with its options, so agents_list carries what answering one
// needs without a second call: 发吗？（发 / 不发）.
func questionSummary(ix proto.Interaction) string {
	parts := make([]string, 0, len(ix.Questions))
	for _, q := range ix.Questions {
		part := q.Question
		if len(q.Options) > 0 {
			part += "（" + optionLabels(q) + "）"
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, " / ")
}

func askedQuestions(ix proto.Interaction) string {
	qs := make([]string, len(ix.Questions))
	for i, q := range ix.Questions {
		qs[i] = q.Question
	}
	return strings.Join(qs, "; ")
}

// ---- formatting ----
func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n]) + "…"
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func ago(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "刚刚"
	case d < time.Hour:
		return fmt.Sprintf("%d 分钟前", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d 小时前", int(d.Hours()))
	default:
		return fmt.Sprintf("%d 天前", int(d.Hours()/24))
	}
}

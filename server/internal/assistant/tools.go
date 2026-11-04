package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
)

// Tools only the assistant (the 对话 tab's headless Claude) gets: asking on the phone (headless claude has no
// AskUserQuestion), pushing, waking itself up later, its conversations, the tasks it hands out, the coding agents the
// hub already tracks, the knowledge base, the inbox's drafts, Feishu's /agent replies and Persona's own settings.
// The hub lists them with its own and sends their calls here (Handler.Tool). Those on the hub's knowledge base,
// interaction queue and inbox call those apps' data methods (tools_apps.go); one that is still an app's to run goes to
// that app (AppTool).

// toolNames are the tools only assistant conversations get; their descriptions and schemas are in prompts/mcp
// (docs/prompts.md).
var toolNames = []string{"ask_user", "notify_user", "schedule_followup", "schedule_routine", "delete_routine", "list_followups",
	"cancel_followup", "agents_list", "agent_read", "agent_send", "agent_start", "interaction_answer", "inbox_draft",
	"kb_search", "kb_list", "kb_get", "kb_update", "jarvis_settings",
	"conversations_list", "conversation_read", "conversation_rename", "conversation_archive", "conversation_unarchive",
	"conversation_new", "conversation_send", "task_plan"}

// Tools are the assistant's tools (assistant.tools).
func (s *Service) Tools(context.Context) []proto.Tool {
	tools := make([]proto.Tool, 0, len(toolNames))
	for _, n := range toolNames {
		t := prompts.Tool(n)
		tools = append(tools, proto.Tool{Name: t.Name, Description: t.Description, InputSchema: t.InputSchema})
	}
	return tools
}

// Tool runs one of the assistant's MCP tools for the conversation call.SessionID on machine call.MachineID.
func (s *Service) Tool(ctx context.Context, call ToolCall) proto.ToolResult {
	args := call.Arguments
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	switch call.Name {
	case "jarvis_settings":
		return s.toolJarvisSettings(ctx, call, args) // settings_tool.go
	}
	res, err := s.runTool(ctx, call.MachineID, call.SessionID, call.Name, args)
	if err != nil {
		return proto.TextResult(err.Error(), true)
	}
	return proto.TextResult(res, false)
}

// appTool runs a tool that is app's (app.call app.tool).
func (s *Service) appTool(ctx context.Context, app string, call ToolCall, args json.RawMessage) proto.ToolResult {
	var res proto.ToolResult
	if err := s.appCall(ctx, app, AppTool, map[string]any{"name": call.Name, "arguments": args, "machine_id": call.MachineID,
		"session_id": call.SessionID}, &res); err != nil {
		return proto.TextResult(err.Error(), true)
	}
	return res
}

func (s *Service) runTool(ctx context.Context, machine, self, name string, args json.RawMessage) (string, error) {
	switch name {
	case "ask_user":
		return s.toolAskUser(ctx, machine, self, args)
	case "task_plan":
		return s.toolTaskPlan(ctx, self, args)
	case "kb_search", "kb_list", "kb_get", "kb_update":
		return s.toolKB(ctx, name, args)
	case "interaction_answer":
		return s.toolInteractionAnswer(ctx, self, args)
	case "inbox_draft":
		return s.toolInboxDraft(ctx, self, args)
	case "notify_user":
		var a struct{ Title, Body string }
		if err := json.Unmarshal(args, &a); err != nil || strings.TrimSpace(a.Title+a.Body) == "" {
			return "", fmt.Errorf("title and body are required")
		}
		// Asked for explicitly: every device, the Feishu bot too, and no separate push when this turn ends.
		s.push(ctx, Notification{Subtitle: "Persona", DeviceID: machine, SessionID: self, Title: a.Title, Body: a.Body, Asked: true})
		return "Notification sent.", nil
	case "conversations_list":
		return s.toolConversationsList(ctx, self, args)
	case "conversation_read":
		return s.toolConversationRead(ctx, args)
	case "conversation_rename":
		return s.toolConversationRename(ctx, self, args)
	case "conversation_archive", "conversation_unarchive":
		return s.toolConversationArchive(ctx, self, args, name == "conversation_archive")
	case "conversation_new":
		return s.toolConversationNew(ctx, self, args)
	case "conversation_send":
		return s.toolConversationSend(ctx, self, args)
	case "schedule_followup":
		return s.toolScheduleFollowup(ctx, machine, self, args)
	case "list_followups":
		return s.describeSchedule(ctx), nil
	case "schedule_routine":
		return s.toolScheduleRoutine(ctx, args)
	case "delete_routine", "cancel_followup":
		var a struct{ ID string }
		_ = json.Unmarshal(args, &a)
		ok, err := s.deleteTask(ctx, a.ID)
		if err != nil {
			return "", err
		}
		if !ok {
			return "", fmt.Errorf("no task %q", a.ID)
		}
		if name == "delete_routine" {
			return "Deleted.", nil
		}
		return "Cancelled.", nil
	case "agents_list":
		var a struct{ State, Machine, Query string }
		_ = json.Unmarshal(args, &a)
		return s.describeAgents(ctx, a.State, a.Machine, a.Query), nil
	case "agent_read":
		var a struct {
			Machine   string `json:"machine"`
			SessionID string `json:"session_id"`
			Turns     int    `json:"turns"`
		}
		_ = json.Unmarshal(args, &a)
		return s.toolAgentRead(ctx, a.Machine, a.SessionID, a.Turns)
	case "agent_send":
		var a struct {
			Machine   string `json:"machine"`
			SessionID string `json:"session_id"`
			Text      string `json:"text"`
		}
		_ = json.Unmarshal(args, &a)
		am, c, err := s.findAgent(ctx, a.Machine, a.SessionID)
		if err != nil {
			return "", err
		}
		if c.Assistant {
			return "", fmt.Errorf("that is an assistant conversation, not a coding agent")
		}
		if c.SourceKind != "" && (c.Capabilities == nil || !c.Capabilities.Send) {
			return "", fmt.Errorf("这个 Agent 仅支持查看：%s", c.ReadOnlyReason)
		}
		if strings.TrimSpace(a.Text) == "" {
			return "", fmt.Errorf("text is empty")
		}
		if c.Status != proto.StatusOffline {
			if err := s.call(ctx, am.ID, DeviceSessionSend, map[string]any{"session_id": c.SessionID, "text": a.Text}, nil); err != nil {
				return "", err
			}
			return fmt.Sprintf("Sent to %s on %s.", agentLabel(c), am.Name), nil
		}
		// Not running (a finished task's agent is closed: hub tasks_close.go): its node resumes it first, which can
		// take minutes and goes on if this call stops waiting.
		sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Minute)
		defer cancel()
		if err := s.call(sctx, am.ID, DeviceSessionSend, map[string]any{"session_id": c.SessionID, "text": a.Text}, nil); err != nil {
			return "", err
		}
		return fmt.Sprintf("Resumed %s on %s (same session, directory and launcher) and sent.", agentLabel(c), am.Name), nil
	case "agent_start":
		var a struct {
			Machine  string `json:"machine"`
			Cwd      string `json:"cwd"`
			Prompt   string `json:"prompt"`
			Launcher string `json:"launcher"`
		}
		_ = json.Unmarshal(args, &a)
		am, ok := s.machineByName(ctx, a.Machine)
		if !ok {
			return "", fmt.Errorf("unknown machine %q (machines: %s)", a.Machine, s.machineIDs(ctx))
		}
		if strings.HasPrefix(a.Cwd, "~/") {
			if home := am.Home; home != "" {
				a.Cwd = path.Join(home, a.Cwd[2:])
			}
		}
		var res struct {
			Session proto.Session `json:"session"`
		}
		sctx, cancel := context.WithTimeout(ctx, 100*time.Second)
		defer cancel()
		params := map[string]any{"cwd": a.Cwd, "launcher": a.Launcher, "prompt": a.Prompt, "tab_name": s.tabName(sctx, a.Prompt)}
		if err := s.applyModel(sctx, "agent.new", params); err != nil {
			return "", err
		}
		if err := s.call(sctx, am.ID, DeviceSessionStart, params, &res); err != nil {
			return "", err
		}
		if s.quietAgentTask(ctx, self, am, a.Cwd, a.Prompt, params["tab_name"], res.Session.SessionID) {
			return fmt.Sprintf("Started on %s in %s: session %s, a task of this conversation (its card is in the conversation; "+
				"you are told here when it is done or waits for the user, nothing else notifies the user).", am.Name, a.Cwd, res.Session.SessionID), nil
		}
		return fmt.Sprintf("Started on %s in %s: session %s. It shows up in the app's Agents tab.", am.Name, a.Cwd, res.Session.SessionID), nil
	}
	return "", fmt.Errorf("unknown tool %s", name)
}

// tabName is a herdr tab's name for an agent started with prompt ("" when the model gives none).
func (s *Service) tabName(ctx context.Context, prompt string) string {
	var res struct {
		Name string `json:"name"`
	}
	_ = s.appCall(ctx, AppModels, ModelsTabName, map[string]any{"prompt": prompt}, &res)
	return res.Name
}

// ---- ask_user ----

// question is an ask_user waiting for the user: the card the hub shows, and where the answer goes.
type question struct {
	ix     proto.Interaction
	answer chan proto.Resolution
}

// questions are the questions out to the user, by interaction id: they go to the hub in the snapshot.
type questions struct {
	mu sync.Mutex
	m  map[string]*question
}

func (s *Service) toolAskUser(ctx context.Context, machine, sessionID string, args json.RawMessage) (string, error) {
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
		ID: "ix_" + randomHex(6), MachineID: machine, SessionID: sessionID, SessionTitle: "Persona", Kind: proto.IxQuestion,
		ToolName: "ask_user", ToolInput: args, CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().Add(30 * time.Minute).UTC(),
	}
	if c, ok := s.session(ctx, machine, sessionID); ok {
		ix.Cwd = c.Cwd
		if c.Title != "" {
			ix.SessionTitle = c.Title
		}
	}
	for _, q := range a.Questions {
		ix.Questions = append(ix.Questions, proto.Question{Question: q.Question, Header: q.Header, MultiSelect: q.MultiSelect, Options: q.Options})
	}
	r, err := s.ask(ctx, ix)
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

// ask puts ix on the phone (the snapshot's interactions: the hub shows it like any agent's card, pushes it and
// expires it) and waits for the answer (Action); when ctx ends first the question is withdrawn.
func (s *Service) ask(ctx context.Context, ix proto.Interaction) (proto.Resolution, error) {
	q := &question{ix: ix, answer: make(chan proto.Resolution, 1)}
	s.questions.mu.Lock()
	if s.questions.m == nil {
		s.questions.m = map[string]*question{}
	}
	s.questions.m[ix.ID] = q
	s.questions.mu.Unlock()
	s.publish(context.WithoutCancel(ctx))
	defer func() {
		s.questions.mu.Lock()
		delete(s.questions.m, ix.ID)
		s.questions.mu.Unlock()
		s.publish(context.WithoutCancel(ctx))
	}()
	select {
	case r := <-q.answer:
		return r, nil
	case <-ctx.Done():
		return proto.Resolution{}, ctx.Err()
	}
}

// pendingQuestions are the questions out to the user, oldest first.
func (s *Service) pendingQuestions() []proto.Interaction {
	s.questions.mu.Lock()
	defer s.questions.mu.Unlock()
	out := []proto.Interaction{}
	for _, q := range s.questions.m {
		out = append(out, q.ix)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

// Action takes the user's answer to one of the questions out (op answer, the answer a proto.Resolution).
func (s *Service) Action(_ context.Context, a Action) (ActionResult, error) {
	if a.Op != "answer" {
		return ActionResult{}, fmt.Errorf("unknown op %q", a.Op)
	}
	var r proto.Resolution
	if len(a.Answer) > 0 {
		if err := json.Unmarshal(a.Answer, &r); err != nil {
			return ActionResult{}, err
		}
	}
	s.questions.mu.Lock()
	q := s.questions.m[a.InteractionID]
	s.questions.mu.Unlock()
	if q == nil {
		return ActionResult{}, fmt.Errorf("no question %s", a.InteractionID)
	}
	select {
	case q.answer <- r:
	default: // answered already
	}
	return ActionResult{OK: true}, nil
}

// ---- agents ----

// machineByName is the machine by its id, else by its name or ssh alias (any case).
func (s *Service) machineByName(ctx context.Context, id string) (Device, bool) {
	devices := s.devices(ctx)
	for _, m := range devices {
		if m.ID == id {
			return m, true
		}
	}
	for _, m := range devices {
		if strings.EqualFold(m.Name, id) || strings.EqualFold(m.SSHHost, id) {
			return m, true
		}
	}
	return Device{}, false
}

func (s *Service) machineIDs(ctx context.Context) string {
	devices := s.devices(ctx)
	ids := make([]string, 0, len(devices))
	for _, m := range devices {
		ids = append(ids, m.ID)
	}
	return strings.Join(ids, ", ")
}

// findAgent resolves a session by full id or prefix; an empty machine searches all of them.
func (s *Service) findAgent(ctx context.Context, machine, id string) (Device, proto.Session, error) {
	id = strings.TrimSpace(id)
	if len(id) < 6 {
		return Device{}, proto.Session{}, fmt.Errorf("session_id is required (at least its first 8 characters)")
	}
	var found *Device
	var session proto.Session
	for _, m := range s.devices(ctx) {
		if machine != "" {
			if want, ok := s.machineByName(ctx, machine); !ok || want.ID != m.ID {
				continue
			}
		}
		list, _ := s.sessions(ctx, m.ID)
		for _, c := range list {
			if c.SessionID == id || strings.HasPrefix(c.SessionID, id) {
				if found != nil {
					return Device{}, proto.Session{}, fmt.Errorf("session_id %s is ambiguous; use the full id and machine", id)
				}
				found, session = &m, c
			}
		}
	}
	if found != nil {
		return *found, session, nil
	}
	return Device{}, proto.Session{}, fmt.Errorf("no agent %s (use agents_list)", id)
}

func (s *Service) describeAgents(ctx context.Context, state, machine, query string) string {
	if state == "" {
		state = "active"
	}
	var res struct {
		Interactions []proto.Interaction `json:"interactions"`
	}
	_ = s.call(ctx, DeviceAll, DeviceInteractionsList, map[string]any{}, &res)
	pending := map[[2]string][]string{}
	for _, ix := range res.Interactions {
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
		k := [2]string{ix.MachineID, ix.SessionID}
		what += " (interaction_id=" + ix.ID + ")"
		pending[k] = append(pending[k], clip(what, 400))
	}
	type row struct {
		m Device
		c proto.Session
	}
	var rows []row
	var offline []string
	want, wanted := Device{}, false
	if machine != "" {
		want, wanted = s.machineByName(ctx, machine)
	}
	for _, m := range s.devices(ctx) {
		if machine != "" && (!wanted || want.ID != m.ID) {
			continue
		}
		if !m.Online {
			offline = append(offline, m.Name)
		}
		list, _ := s.sessions(ctx, m.ID)
		for _, c := range list {
			if c.Assistant {
				continue
			}
			switch state {
			case "all":
			case "active":
				if c.State != proto.StateNeedsYou && c.State != proto.StateWorking && c.State != proto.StateUnread {
					continue
				}
			default:
				if c.State != state {
					continue
				}
			}
			if query != "" {
				hay := strings.ToLower(c.Title + " " + c.Cwd + " " + c.SpaceName + " " + c.SlotName)
				if !strings.Contains(hay, strings.ToLower(query)) {
					continue
				}
			}
			rows = append(rows, row{m, c})
		}
	}
	order := map[string]int{proto.StateNeedsYou: 0, proto.StateWorking: 1, proto.StateUnread: 2, proto.StateIdle: 3}
	sort.SliceStable(rows, func(i, j int) bool {
		oi, oj := order[rows[i].c.State], order[rows[j].c.State]
		if oi != oj {
			return oi < oj
		}
		return rows[i].c.UpdatedAt.After(rows[j].c.UpdatedAt)
	})
	if len(rows) > 60 {
		rows = rows[:60]
	}
	var b strings.Builder
	if len(rows) == 0 {
		b.WriteString("No agents match.\n")
	}
	for _, r := range rows {
		c := r.c
		fmt.Fprintf(&b, "- [%s] %s · machine=%s session_id=%s · %s", stateNames[c.State], agentLabel(c), r.m.ID, c.SessionID, c.Cwd)
		if c.SourceKind != "" {
			fmt.Fprintf(&b, " · source=%s instance=%s", c.SourceKind, c.SourceInstanceID)
			if c.Capabilities == nil || !c.Capabilities.Send {
				fmt.Fprintf(&b, " · 仅查看：%s", c.ReadOnlyReason)
			}
		}
		if !c.UpdatedAt.IsZero() {
			fmt.Fprintf(&b, " · %s", ago(c.UpdatedAt))
		}
		if c.Loop != nil {
			b.WriteString(" · /loop")
		}
		if n := len(c.Background); n > 0 {
			fmt.Fprintf(&b, " · 后台任务 %d", n)
		}
		b.WriteString("\n")
		for _, p := range pending[[2]string{r.m.ID, c.SessionID}] {
			fmt.Fprintf(&b, "    等你：%s\n", p)
		}
		if c.LastText != "" {
			fmt.Fprintf(&b, "    最后说：%s\n", clip(oneLine(c.LastText), 200))
		}
	}
	if len(offline) > 0 {
		fmt.Fprintf(&b, "(offline machines: %s)\n", strings.Join(offline, ", "))
	}
	return b.String()
}

func (s *Service) toolAgentRead(ctx context.Context, machine, id string, turns int) (string, error) {
	m, c, err := s.findAgent(ctx, machine, id)
	if err != nil {
		return "", err
	}
	if turns <= 0 {
		turns = 2
	}
	var res struct {
		Items []proto.Item `json:"items"`
	}
	if err := s.call(ctx, m.ID, DeviceSessionItems, map[string]any{"session_id": c.SessionID, "turns": min(turns, 10)}, &res); err != nil {
		return "", err
	}
	return transcriptText(fmt.Sprintf("%s on %s (%s, %s)", agentLabel(c), m.Name, stateNames[c.State], c.Cwd), res.Items), nil
}

// questionSummary is every question of an interaction with its options, so agents_list carries what answering one
// (interaction_answer) needs without a second call: 发吗？（发 / 不发）.
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

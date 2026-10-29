package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/chyroc/jarvis/server/internal/lark"
	"github.com/chyroc/jarvis/server/internal/proto"
)

// Tools only the assistant (the 对话 tab's headless Claude) gets: asking on the phone (headless claude has no
// AskUserQuestion), pushing, waking itself up later, the coding agents the hub already tracks, and Feishu through
// the hub's lark-connector client. They answer from the hub's own state, so no ssh, no tokens, no scraping.

func schema(s string) json.RawMessage { return json.RawMessage(s) }

func assistantTools(withLark bool) []proto.Tool {
	tools := []proto.Tool{
		{Name: "ask_user", Description: "Ask the user one or more multiple-choice questions on their iPhone (a card in this conversation plus a push " +
			"notification) and wait for the answers. Use it to confirm anything risky (sending messages / email for them, deleting, " +
			"changing live systems) or to let them pick between options. AskUserQuestion does not work here; use this instead. " +
			"The user can also type a free-form answer. Waits up to 30 minutes.",
			InputSchema: schema(`{"type":"object","required":["questions"],"properties":{"questions":{"type":"array","minItems":1,"maxItems":4,"items":{"type":"object","required":["question","options"],"properties":{"question":{"type":"string"},"header":{"type":"string","description":"short label, ≤12 chars"},"multiSelect":{"type":"boolean"},"options":{"type":"array","minItems":2,"maxItems":4,"items":{"type":"object","required":["label"],"properties":{"label":{"type":"string"},"description":{"type":"string"}}}}}}}}}`)},
		{Name: "notify_user", Description: "Send the user a push notification right now (works when the app is closed). Tapping it opens this conversation. " +
			"Use for results of scheduled runs and anything time-sensitive; don't notify for ordinary replies (they get one automatically).",
			InputSchema: schema(`{"type":"object","required":["title","body"],"properties":{"title":{"type":"string"},"body":{"type":"string"}}}`)},
		{Name: "schedule_followup", Description: "Wake yourself up later: at the given time Jarvis sends `prompt` into this conversation (or a new one) " +
			"as if the user wrote it, you do the work, and the user gets a push with your answer. Use for \"remind me…\", \"check again in an hour\", " +
			"meeting prep 10 minutes before a meeting, watching something until it changes. `at` is RFC 3339 (2026-10-27T09:50:00+08:00) " +
			"or relative (\"30m\", \"2h\", \"1d\").",
			InputSchema: schema(`{"type":"object","required":["at","prompt"],"properties":{"at":{"type":"string"},"prompt":{"type":"string","description":"what to do then, self-contained"},"title":{"type":"string","description":"short label shown in the app"},"new_conversation":{"type":"boolean","description":"start a fresh conversation instead of continuing this one"}}}`)},
		{Name: "list_followups", Description: "List the follow-ups scheduled with schedule_followup that have not run yet, and the user's daily routines.",
			InputSchema: schema(`{"type":"object","properties":{}}`)},
		{Name: "cancel_followup", Description: "Cancel a scheduled follow-up by id (from list_followups).",
			InputSchema: schema(`{"type":"object","required":["id"],"properties":{"id":{"type":"string"}}}`)},
		{Name: "agents_list", Description: "The Claude Code / Codex agents running on the user's computers (herdr), with their state: needs_you " +
			"(waiting for an answer / approval), working, unread (finished, result not looked at), idle. Each line has machine, session id, " +
			"project, title, last update and the last thing the agent said. Much faster and more accurate than ssh + herdr.",
			InputSchema: schema(`{"type":"object","properties":{"state":{"type":"string","enum":["active","needs_you","working","unread","idle","all"],"description":"default active = needs_you + working + unread"},"machine":{"type":"string"},"query":{"type":"string","description":"filter by title / project / directory"}}}`)},
		{Name: "agent_read", Description: "Read the recent conversation of one agent (from agents_list): the user's messages, what the agent said, " +
			"tool calls in short form. `turns` = how many recent exchanges (default 2, max 10).",
			InputSchema: schema(`{"type":"object","required":["machine","session_id"],"properties":{"machine":{"type":"string"},"session_id":{"type":"string","description":"full id or its first 8 characters"},"turns":{"type":"integer"}}}`)},
		{Name: "agent_send", Description: "Send a message to an agent as the user would type it (answers its question, tells it to continue, gives " +
			"new instructions). Queued if the agent is working. Only send what the user asked for.",
			InputSchema: schema(`{"type":"object","required":["machine","session_id","text"],"properties":{"machine":{"type":"string"},"session_id":{"type":"string"},"text":{"type":"string"}}}`)},
		{Name: "agent_start", Description: "Start a new Claude Code agent in herdr on one of the user's computers, in a project directory, with a first " +
			"prompt. Use for real development work instead of doing it here. Returns the new session id.",
			InputSchema: schema(`{"type":"object","required":["machine","cwd","prompt"],"properties":{"machine":{"type":"string"},"cwd":{"type":"string","description":"absolute path or ~/…"},"prompt":{"type":"string"},"launcher":{"type":"string","description":"shell command that starts claude (default: the machine's default)"}}}`)},
		inboxDraftTool,
	}
	tools = append(tools, kbTools...)
	if withLark {
		tools = append(tools,
			proto.Tool{Name: "lark_chats", Description: "The user's Feishu (Lark) chats from their own account: unread ones by default, or the ones " +
				"that @mention them, or the latest. Each line has the chat id (for lark_messages / lark_send), name, unread count, @me, muted, " +
				"last message preview and time.",
				InputSchema: schema(`{"type":"object","properties":{"filter":{"type":"string","enum":["unread","at_me","recent"],"description":"default unread"},"limit":{"type":"integer","description":"default 30"}}}`)},
			proto.Tool{Name: "lark_messages", Description: "Read a Feishu chat (id from lark_chats or lark_search): the latest messages as text, oldest first. " +
				"Pass `before` (a message position from an earlier page) to read further back. Reading does not mark anything as read.",
				InputSchema: schema(`{"type":"object","required":["chat_id"],"properties":{"chat_id":{"type":"string"},"limit":{"type":"integer","description":"default 40, max 100"},"before":{"type":"string"}}}`)},
			proto.Tool{Name: "lark_search", Description: "Search the user's Feishu: messages, docs, people, chats, calendar events, meeting minutes, tasks, mail.",
				InputSchema: schema(`{"type":"object","required":["query"],"properties":{"query":{"type":"string"},"kind":{"type":"string","enum":["messages","docs","people","chats","calendar","minutes","todos","mail"],"description":"default messages"}}}`)},
			proto.Tool{Name: "lark_api", Description: "Read anything else from the user's Feishu through lark-connector's agent API (read-only GET, plus POST /search/page). " +
				"Useful routes: calendar/week?start=<UTC ISO>&end=<UTC ISO> (events; ≤62 days), calendar/event/<id> (attendees, meeting link, docs), " +
				"meetings?count=20, meetings/<historyId>/insights (AI notes), minutes?scope=all, minutes/<token> (summary, todos), " +
				"minutes/<token>/transcript, todos/search?q=, documents/drive?view=recent, wiki/spaces?scope=recent, search/people?q=, " +
				"profile routes; GET / lists every route. Long answers are cut at 60 KB.",
				InputSchema: schema(`{"type":"object","required":["path"],"properties":{"path":{"type":"string","description":"below /agent/v1, e.g. calendar/week"},"query":{"type":"object","additionalProperties":{"type":"string"}},"post_body":{"type":"object","description":"only for POST search/page"}}}`)},
			proto.Tool{Name: "lark_send", Description: "Send a text message to a Feishu chat as the user. The user approves it on the phone first; " +
				"say in the text exactly what will be sent.",
				InputSchema: schema(`{"type":"object","required":["chat_id","text"],"properties":{"chat_id":{"type":"string"},"text":{"type":"string"}}}`)},
		)
	}
	return tools
}

func isAssistantTool(name string) bool {
	switch name {
	case "ask_user", "notify_user", "schedule_followup", "list_followups", "cancel_followup",
		"agents_list", "agent_read", "agent_send", "agent_start", "inbox_draft":
		return true
	}
	return strings.HasPrefix(name, "lark_") || strings.HasPrefix(name, "kb_")
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
	case "notify_user":
		var a struct{ Title, Body string }
		if err := json.Unmarshal(args, &a); err != nil || strings.TrimSpace(a.Title+a.Body) == "" {
			return "", fmt.Errorf("title and body are required")
		}
		h.push.notifyDone(m.cfg.ID, "Jarvis", sessionID, a.Title, a.Body, nil) // asked for explicitly: every device
		h.mu.Lock()
		h.notified[sessionID] = time.Now()
		h.mu.Unlock()
		return "Notification sent.", nil
	case "schedule_followup":
		return h.toolScheduleFollowup(ctx, m, sessionID, args)
	case "list_followups":
		return h.describeSchedule(ctx), nil
	case "cancel_followup":
		var a struct{ ID string }
		_ = json.Unmarshal(args, &a)
		if !h.cancelFollowup(ctx, a.ID) {
			return "", fmt.Errorf("no pending follow-up %q", a.ID)
		}
		return "Cancelled.", nil
	case "inbox_draft":
		return h.toolInboxDraft(ctx, sessionID, args)
	case "kb_search", "kb_list", "kb_get", "kb_update":
		return h.runKBTool(name, args)
	case "agents_list":
		var a struct{ State, Machine, Query string }
		_ = json.Unmarshal(args, &a)
		return h.describeAgents(a.State, a.Machine, a.Query), nil
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
		if err := am.call(sctx, "session.start", map[string]any{"cwd": a.Cwd, "launcher": a.Launcher, "prompt": a.Prompt}, &res); err != nil {
			return "", err
		}
		return fmt.Sprintf("Started on %s in %s: session %s. It shows up in the app's Agents tab.", am.cfg.Name, a.Cwd, res.Session.SessionID), nil
	}
	if strings.HasPrefix(name, "lark_") {
		return h.runLarkTool(ctx, m, name, args)
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
		ID: "ix_" + randomHex(6), MachineID: m.cfg.ID, SessionID: sessionID, SessionTitle: "Jarvis", Kind: proto.IxQuestion,
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
	if m := h.byID[id]; m != nil {
		return m
	}
	for _, m := range h.machines {
		if strings.EqualFold(m.cfg.Name, id) || strings.EqualFold(m.cfg.SSHHost, id) {
			return m
		}
	}
	return nil
}

func (h *Hub) machineIDs() string {
	ids := make([]string, 0, len(h.machines))
	for _, m := range h.machines {
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
	for _, m := range h.machines {
		if machine != "" && h.machineByName(machine) != m {
			continue
		}
		for _, s := range h.decorated(m) {
			if s.SessionID == id || strings.HasPrefix(s.SessionID, id) {
				return m, s, nil
			}
		}
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
			what = "提问：" + ix.Questions[0].Question
		} else if ix.Kind == proto.IxPlan {
			what = "计划待审批：" + firstLine(ix.Plan)
		} else if ix.Kind == proto.IxPermission {
			what = "要授权 " + ix.ToolName + "：" + ix.Summary
		} else if ix.Kind == proto.IxScreen {
			what = "终端对话框等你处理"
		}
		k := [2]string{ix.MachineID, ix.SessionID}
		pending[k] = append(pending[k], clip(what, 160))
	}
	type row struct {
		m *Machine
		s proto.Session
	}
	var rows []row
	var offline []string
	for _, m := range h.machines {
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
	var b strings.Builder
	fmt.Fprintf(&b, "%s on %s (%s, %s)\n\n", agentLabel(s), m.cfg.Name, stateNames[s.State], s.Cwd)
	for _, it := range res.Items {
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
	return out, nil
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

func (h *Hub) runLarkTool(ctx context.Context, m *Machine, name string, args json.RawMessage) (string, error) {
	switch name {
	case "lark_chats":
		var a struct {
			Filter string `json:"filter"`
			Limit  int    `json:"limit"`
		}
		_ = json.Unmarshal(args, &a)
		if a.Limit <= 0 || a.Limit > 100 {
			a.Limit = 30
		}
		convs, _, err := h.lark.Conversations(ctx, "inbox", "", 100)
		if err != nil {
			return "", err
		}
		var b strings.Builder
		n := 0
		for _, c := range convs {
			switch a.Filter {
			case "recent":
			case "at_me":
				if !c.AtMe && !c.AtAll {
					continue
				}
			default:
				if c.Unread == 0 && !c.MarkedUnread {
					continue
				}
			}
			if n >= a.Limit {
				break
			}
			n++
			fmt.Fprintf(&b, "- %s (chat_id=%s", c.Name, c.ID)
			if c.IsGroup {
				b.WriteString(", 群")
			}
			if c.Unread > 0 {
				fmt.Fprintf(&b, ", 未读 %d", c.Unread)
			}
			if c.AtMe {
				b.WriteString(", @我")
			} else if c.AtAll {
				b.WriteString(", @所有人")
			}
			if c.Muted {
				b.WriteString(", 免打扰")
			}
			if len(c.Tags) > 0 {
				b.WriteString(", " + strings.Join(c.Tags, "/"))
			}
			fmt.Fprintf(&b, ") %s", ago(c.UpdatedAt))
			if c.Preview != "" {
				who := c.PreviewSender
				if c.IsMine {
					who = "我"
				}
				if who != "" {
					who += "："
				}
				fmt.Fprintf(&b, "\n    %s%s", who, clip(oneLine(c.Preview), 160))
			}
			b.WriteString("\n")
		}
		if n == 0 {
			return "No chats match.", nil
		}
		return b.String(), nil
	case "lark_messages":
		var a struct {
			ChatID string `json:"chat_id"`
			Limit  int    `json:"limit"`
			Before string `json:"before"`
		}
		_ = json.Unmarshal(args, &a)
		if a.ChatID == "" {
			return "", fmt.Errorf("chat_id is required")
		}
		if a.Limit <= 0 || a.Limit > 100 {
			a.Limit = 40
		}
		page, err := h.lark.Messages(ctx, a.ChatID, lark.Window{Before: a.Before}, a.Limit, false)
		if err != nil {
			return "", err
		}
		var b strings.Builder
		if page.HasOlder && len(page.Messages) > 0 {
			fmt.Fprintf(&b, "(older messages: before=%s)\n", page.Messages[0].Position)
		}
		for _, msg := range page.Messages {
			who := msg.SenderName
			if msg.IsMine {
				who = "我"
			}
			text := msg.Text
			if msg.Recalled {
				text = "(撤回了一条消息)"
			}
			if text == "" {
				text = "[" + strings.ToLower(msg.Type) + "]"
			}
			fmt.Fprintf(&b, "[%s] %s", msg.CreatedAt.In(h.userLocation()).Format("01-02 15:04"), who)
			if msg.AtMe {
				b.WriteString(" (@我)")
			}
			if msg.ReplyTo != nil {
				fmt.Fprintf(&b, " (回复 %s：%s)", msg.ReplyTo.SenderName, clip(oneLine(msg.ReplyTo.Text), 60))
			}
			fmt.Fprintf(&b, "：%s", clip(text, 2000))
			if msg.ThreadReplyCount > 0 {
				fmt.Fprintf(&b, " (话题 %d 条回复)", msg.ThreadReplyCount)
			}
			b.WriteString("\n")
		}
		return b.String(), nil
	case "lark_search":
		var a struct{ Query, Kind string }
		_ = json.Unmarshal(args, &a)
		if strings.TrimSpace(a.Query) == "" {
			return "", fmt.Errorf("query is required")
		}
		route := map[string]string{
			"": "search", "messages": "search", "docs": "search/documents", "people": "search/people", "calendar": "calendar/search",
			"minutes": "minutes/search", "todos": "todos/search", "mail": "mail/search",
		}[a.Kind]
		if a.Kind == "chats" {
			raw, err := h.lark.Read(ctx, "POST", "/search/page", nil, map[string]any{"tab": "groups", "query": a.Query, "limit": 20})
			if err != nil {
				return "", err
			}
			return compactJSON(raw, 30000), nil
		}
		if route == "" {
			return "", fmt.Errorf("unknown kind %q", a.Kind)
		}
		raw, err := h.lark.Read(ctx, "GET", "/"+route, url.Values{"q": {a.Query}}, nil)
		if err != nil {
			return "", err
		}
		return compactJSON(raw, 30000), nil
	case "lark_api":
		var a struct {
			Path     string            `json:"path"`
			Query    map[string]string `json:"query"`
			PostBody map[string]any    `json:"post_body"`
		}
		if err := json.Unmarshal(args, &a); err != nil {
			return "", err
		}
		p := strings.TrimPrefix(strings.TrimSpace(a.Path), "/agent/v1")
		if u, err := url.Parse(p); err == nil && u.RawQuery != "" { // query written into the path
			p = u.Path
			if a.Query == nil {
				a.Query = map[string]string{}
			}
			for k, v := range u.Query() {
				a.Query[k] = v[0]
			}
		}
		if !strings.HasPrefix(p, "/") {
			p = "/" + p
		}
		q := url.Values{}
		for k, v := range a.Query {
			q.Set(k, v)
		}
		method := "GET"
		var body any
		if a.PostBody != nil {
			method, body = "POST", a.PostBody
		}
		raw, err := h.lark.Read(ctx, method, p, q, body)
		if err != nil {
			return "", err
		}
		return compactJSON(raw, 60000), nil
	case "lark_send":
		var a struct {
			ChatID string `json:"chat_id"`
			Text   string `json:"text"`
		}
		_ = json.Unmarshal(args, &a)
		if a.ChatID == "" || strings.TrimSpace(a.Text) == "" {
			return "", fmt.Errorf("chat_id and text are required")
		}
		chat := a.ChatID
		if convs, _, err := h.lark.Conversations(ctx, "inbox", "", 100); err == nil {
			for _, c := range convs {
				if c.ID == a.ChatID {
					chat = c.Name
				}
			}
		}
		if !h.approve(ctx, m.cfg.ID, "lark_send", "发飞书消息给「"+chat+"」：\n"+a.Text, args) {
			return "The user declined; nothing was sent.", nil
		}
		if err := h.lark.Send(ctx, a.ChatID, a.Text); err != nil {
			return "", err
		}
		return "Sent.", nil
	}
	return "", fmt.Errorf("unknown tool %s", name)
}

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

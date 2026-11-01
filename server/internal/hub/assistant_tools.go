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
	"github.com/chyroc/jarvis/server/internal/prompts"
	"github.com/chyroc/jarvis/server/internal/proto"
)

// Tools only the assistant (the 对话 tab's headless Claude) gets: asking on the phone (headless claude has no
// AskUserQuestion), pushing, waking itself up later, the coding agents the hub already tracks, and Feishu through
// the hub's lark-connector client. They answer from the hub's own state, so no ssh, no tokens, no scraping.

// assistantTools are the tools only assistant conversations get; their descriptions and schemas are in
// prompts/mcp (docs/prompts.md).
func assistantTools(withLark, withMail bool) []proto.Tool {
	names := []string{"ask_user", "notify_user", "schedule_followup", "schedule_routine", "delete_routine", "list_followups",
		"cancel_followup", "agents_list", "agent_read", "agent_send", "agent_start", "inbox_draft", "lark_agent_reply",
		"kb_search", "kb_list", "kb_get", "kb_update"}
	if withMail {
		names = append(names, "mail_account_add") // Jarvis adds a mail account for the user (assistant_mail.go)
	}
	if withLark {
		names = append(names, "lark_chats", "lark_messages", "lark_search")
	}
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
		"agents_list", "agent_read", "agent_send", "agent_start", "inbox_draft", "lark_agent_reply", "mail_account_add":
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
	case "mail_account_add":
		return h.toolMailAccountAdd(ctx, m, sessionID, args)
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
	case "schedule_routine":
		return h.toolScheduleRoutine(ctx, args)
	case "delete_routine":
		var a struct{ ID string }
		_ = json.Unmarshal(args, &a)
		if !h.deleteTask(ctx, a.ID) {
			return "", fmt.Errorf("no task %q", a.ID)
		}
		return "Deleted.", nil
	case "cancel_followup":
		var a struct{ ID string }
		_ = json.Unmarshal(args, &a)
		if !h.deleteTask(ctx, a.ID) {
			return "", fmt.Errorf("no task %q", a.ID)
		}
		return "Cancelled.", nil
	case "inbox_draft":
		return h.toolInboxDraft(ctx, sessionID, args)
	case "lark_agent_reply":
		return h.toolLarkAgentReply(ctx, sessionID, args)
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
		if err := am.call(sctx, "session.start", map[string]any{"cwd": a.Cwd, "launcher": a.Launcher, "prompt": a.Prompt,
			"tab_name": h.autoTabName(sctx, a.Prompt)}, &res); err != nil {
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

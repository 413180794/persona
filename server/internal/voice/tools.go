package voice

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/chyroc/jarvis/server/internal/prompts"
)

// The function tools the voice model may call (live dialect with tools): what Jarvis knows and the agents on the
// user's machines, through the hub's API. Their descriptions are prompts (prompts/mcp/voice).
var toolNames = []string{"search_knowledge", "list_agents", "send_to_agent", "ask_jarvis"}

// toolLabels are what the app shows while a tool runs.
var toolLabels = map[string]string{
	"search_knowledge": "查知识库",
	"list_agents":      "看 Agents",
	"send_to_agent":    "发给 Agent",
	"ask_jarvis":       "交给 Jarvis",
}

func toolDecls() []toolDecl {
	out := make([]toolDecl, 0, len(toolNames))
	for _, n := range toolNames {
		t := prompts.Tool(n)
		out = append(out, toolDecl{Type: "function", Name: t.Name, Description: t.Description, Parameters: t.InputSchema})
	}
	return out
}

var stateNames = map[string]string{
	"needs_you": "需要你处理",
	"working":   "工作中",
	"unread":    "完成未看",
	"idle":      "空闲",
	"offline":   "离线",
}

// runTool runs one of the hub tools (ask_jarvis is the conversation's: it answers later) and returns what the model
// is told.
func runTool(ctx context.Context, hub *hubClient, name, args string) (string, error) {
	var in struct {
		Query     string `json:"query"`
		MachineID string `json:"machine_id"`
		SessionID string `json:"session_id"`
		Text      string `json:"text"`
	}
	if strings.TrimSpace(args) != "" {
		if err := json.Unmarshal([]byte(args), &in); err != nil {
			return "", fmt.Errorf("参数不是 JSON：%w", err)
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	switch name {
	case "search_knowledge":
		hits, err := hub.searchKnowledge(ctx, in.Query)
		if err != nil {
			return "", err
		}
		if len(hits) == 0 {
			return "没有找到。", nil
		}
		var b strings.Builder
		for i, h := range hits {
			if i == 20 {
				break
			}
			fmt.Fprintf(&b, "%s: %s\n", h.Path, clipRunes(h.Line, 300))
		}
		return strings.TrimRight(b.String(), "\n"), nil
	case "list_agents":
		agents, err := hub.agents(ctx)
		if err != nil {
			return "", err
		}
		var b strings.Builder
		for _, a := range agents {
			if a.State == "offline" {
				continue
			}
			title := a.Title
			if title == "" {
				title = a.Cwd
			}
			state := stateNames[a.State]
			if state == "" {
				state = a.State
			}
			fmt.Fprintf(&b, "- [%s/%s] %s · %s\n", a.MachineID, a.SessionID, title, state)
			if last := strings.Join(strings.Fields(a.LastText), " "); last != "" {
				fmt.Fprintf(&b, "  最后：%s\n", clipRunes(last, 240))
			}
		}
		if b.Len() == 0 {
			return "没有在运行的 Agent。", nil
		}
		return strings.TrimRight(b.String(), "\n"), nil
	case "send_to_agent":
		if in.MachineID == "" || in.SessionID == "" || strings.TrimSpace(in.Text) == "" {
			return "", fmt.Errorf("machine_id、session_id、text 都要有")
		}
		if err := hub.send(ctx, in.MachineID, in.SessionID, in.Text); err != nil {
			return "", err
		}
		return "已发送。", nil
	}
	return "", fmt.Errorf("没有这个工具：%s", name)
}

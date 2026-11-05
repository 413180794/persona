package assistant

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/proto"
)

// The nodes' MCP tools (mcp.tools / mcp.call): an assistant conversation's or an agent's Persona tools. The manager
// lists and routes them: the phone's (the hub runs them on the connected phone), the connectors', Cua's and ego's
// (the extensions', in this process: ExtensionTools, internal/extmgr), and the assistant's own (here). The skills' gated commands' settings go to
// the assistant's machine only (assistant.lark).

// runtimeCLITool is the DEBUG app's own channel, for the user's scripts only: never a node's.
const runtimeCLITool = "phone_runtime_cli"

// mcpTools are the tools of an assistant conversation or an agent: the phone's, then those of the connectors that
// are ok and give their tools to agents too (mail); for the assistant also its own, then Cua's, ego's and those of
// the connectors only it gets (Feishu, GitHub…).
func (s *Service) mcpTools(ctx context.Context, assistant bool) []proto.Tool {
	var phone struct {
		Tools []proto.Tool `json:"tools"`
	}
	if err := s.appCall(ctx, assistantwire.AppPhone, assistantwire.PhoneTools, map[string]any{}, &phone); err != nil {
		s.logf("mcp tools: the phone's: %v", err)
	}
	var agents, assistantOnly []proto.Tool
	if s.ext != nil {
		agents, assistantOnly = s.ext.AgentTools(ctx, assistant)
	}
	tools := append(phone.Tools, agents...)
	if assistant {
		tools = append(tools, s.Tools(ctx)...)
		tools = append(tools, assistantOnly...)
	}
	return tools
}

// mcpCall runs one: a connector's, Cua's or ego's at the hub (it says whether it is one of them), else the
// assistant's own (for its conversations), else the phone's.
func (s *Service) mcpCall(ctx context.Context, machine, session, name string, args json.RawMessage, assistant bool) proto.ToolResult {
	if s.ext != nil { // the connectors', Cua's, ego's: ctx is the node's call (its caller going away withdraws a card)
		if res, ok := s.ext.CallTool(ctx, machine, session, name, args, assistant); ok {
			return res
		}
	}
	if assistant && s.isOwnTool(ctx, name) {
		return s.Tool(ctx, ToolCall{Name: name, Arguments: args, MachineID: machine, SessionID: session})
	}
	switch {
	case name == runtimeCLITool:
		return proto.TextResult("unknown tool "+name, true)
	case strings.HasPrefix(name, "phone_"):
		var res proto.ToolResult
		if err := s.appCall(ctx, assistantwire.AppPhone, assistantwire.PhoneCall, map[string]any{"name": name, "args": args}, &res); err != nil {
			return proto.TextResult(err.Error(), true)
		}
		return res
	}
	return proto.TextResult("unknown tool "+name, true)
}

// isOwnTool: name is one of the assistant's own tools.
func (s *Service) isOwnTool(ctx context.Context, name string) bool {
	for _, t := range s.Tools(ctx) {
		if t.Name == name {
			return true
		}
	}
	return false
}

// assistantLark is what the assistant's machine gets for its skills' commands (proto.AssistantLark): lark-cli's
// settings and rules, and the plugins' command gates (the hub's, app.call hub). Another machine gets nothing: the
// credentials go to the assistant's machine only (its process runs them).
func (s *Service) assistantLark(ctx context.Context, machine string) (proto.AssistantLark, error) {
	if machine != s.settings(ctx).MachineID {
		return proto.AssistantLark{}, nil
	}
	if s.ext == nil {
		return proto.AssistantLark{}, nil
	}
	return s.ext.AssistantLark(ctx)
}

// assistantSummary is the text a gated command's card shows, as its plugin names what the ids are; false: none.
func (s *Service) assistantSummary(ctx context.Context, tool, summary string) (string, bool) {
	if tool == "" {
		return summary, false
	}
	if s.ext == nil {
		return summary, false
	}
	return s.ext.AssistantSummary(ctx, tool, summary)
}

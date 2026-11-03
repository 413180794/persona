// Package mcpserver is the stdio MCP server Claude Code starts (`jarvisd mcp`); it proxies tools to the local node.
package mcpserver

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"github.com/nwverse/persona/server/internal/identity"
	"io"
	"os"
	"time"

	"github.com/nwverse/persona/server/internal/node"
	"github.com/nwverse/persona/server/internal/proto"
)

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func Serve(ctx context.Context, version string, in io.Reader, out io.Writer) error {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 64*1024), 32*1024*1024)
	enc := json.NewEncoder(out)
	for sc.Scan() {
		var req request
		if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
			continue
		}
		if len(req.ID) == 0 { // notification
			continue
		}
		res, rerr := handle(ctx, version, req)
		resp := response{JSONRPC: "2.0", ID: req.ID, Result: res, Error: rerr}
		if err := enc.Encode(resp); err != nil {
			return err
		}
	}
	return sc.Err()
}

func handle(ctx context.Context, version string, req request) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		if p.ProtocolVersion == "" {
			p.ProtocolVersion = "2025-06-18"
		}
		return map[string]any{
			"protocolVersion": p.ProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": "jarvis", "version": version},
			"instructions": "Jarvis tools reach the user's iPhone (phone_* tools: contacts, calendar, reminders, location, health, photos, " +
				"clipboard, notifications) and email accounts (mail_* tools). phone_* tools need the Jarvis app to be open on the iPhone. " +
				"mail_send asks the user to approve on the phone before sending." + assistantInstructions(),
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		line, err := node.DialSocket(ctx, map[string]any{"kind": "mcp.tools", "assistant": isAssistant(), "session_id": sessionID()})
		tools := []map[string]any{}
		if err == nil {
			var res struct {
				Tools []proto.Tool `json:"tools"`
			}
			if json.Unmarshal(line, &res) == nil {
				for _, t := range res.Tools {
					schema := t.InputSchema
					if len(schema) == 0 {
						schema = json.RawMessage(`{"type":"object","properties":{}}`)
					}
					tools = append(tools, map[string]any{"name": t.Name, "description": t.Description, "inputSchema": schema})
				}
			}
		}
		return map[string]any{"tools": tools}, nil
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, &rpcError{Code: -32602, Message: err.Error()}
		}
		// Phone round trips, approvals and ask_user can take a while (the user may need to unlock the phone).
		ctx, cancel := context.WithTimeout(ctx, 40*time.Minute)
		defer cancel()
		line, err := node.DialSocket(ctx, map[string]any{"kind": "mcp.call", "name": p.Name, "args": p.Arguments,
			"session_id": sessionID(), "assistant": isAssistant()})
		if err != nil {
			return toolResult(proto.TextResult(fmt.Sprintf("Jarvis node is not reachable (%v). Is the hub connected to this machine?", err), true)), nil
		}
		var res proto.ToolResult
		if err := json.Unmarshal(line, &res); err != nil {
			return toolResult(proto.TextResult("bad response from Jarvis node", true)), nil
		}
		return toolResult(res), nil
	}
	return nil, &rpcError{Code: -32601, Message: "method not found: " + req.Method}
}

// The assistant's processes start this server with JARVIS_ASSISTANT / JARVIS_SESSION_ID (node/assistant.go):
// they get the assistant-only tools, and the hub knows which conversation a call (e.g. ask_user) belongs to.
func isAssistant() bool { return identity.Getenv("JARVIS_ASSISTANT") == "1" }

func sessionID() string {
	if id := identity.Getenv("JARVIS_SESSION_ID"); id != "" {
		return id
	}
	return os.Getenv("CLAUDE_SESSION_ID")
}

func assistantInstructions() string {
	if !isAssistant() {
		return ""
	}
	return " As the Jarvis assistant you also have: ask_user (questions shown on the phone; AskUserQuestion does not work here), " +
		"notify_user (push notification), schedule_followup / list_followups / cancel_followup (wake yourself up later), " +
		"agents_* (the coding agents on the user's computers) and lark_* (Feishu chats, calendar, docs, search)."
}

func toolResult(r proto.ToolResult) map[string]any {
	content := []map[string]any{}
	for _, c := range r.Content {
		switch c.Type {
		case "image":
			content = append(content, map[string]any{"type": "image", "data": c.Data, "mimeType": c.MimeType})
		default:
			content = append(content, map[string]any{"type": "text", "text": c.Text})
		}
	}
	return map[string]any{"content": content, "isError": r.IsError}
}

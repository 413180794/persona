// Package mcpserver is the stdio MCP server Claude Code starts (`jarvisd mcp`); it proxies tools to the local node.
package mcpserver

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/nwverse/persona/server/internal/identity"
	"io"
	"os"
	"sync"
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

// Serve answers MCP requests from in until it closes. Each request runs on its own: Claude Code calls tools in
// parallel, and one waiting for the phone (ask_user, an approval) must not hold the others back. A tool call Claude
// Code gives up on (notifications/cancelled: the user stopped the turn) is cancelled: the node hears the hang-up and
// the hub takes its card off the phone, instead of leaving a card nobody waits for until it expires.
func Serve(ctx context.Context, version string, in io.Reader, out io.Writer) error {
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 64*1024), 32*1024*1024)
	enc := json.NewEncoder(out)
	var (
		mu       sync.Mutex // enc, inflight
		inflight = map[string]context.CancelFunc{}
		wg       sync.WaitGroup
		werr     error
	)
	for sc.Scan() {
		var req request
		if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
			continue
		}
		if len(req.ID) == 0 { // notification
			if req.Method == "notifications/cancelled" {
				var p struct {
					RequestID json.RawMessage `json:"requestId"`
				}
				_ = json.Unmarshal(req.Params, &p)
				mu.Lock()
				if cancel := inflight[idKey(p.RequestID)]; cancel != nil {
					cancel()
				}
				mu.Unlock()
			}
			continue
		}
		rctx, cancel := context.WithCancel(ctx)
		key := idKey(req.ID)
		mu.Lock()
		inflight[key] = cancel
		mu.Unlock()
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, rerr := handle(rctx, version, req)
			mu.Lock()
			defer mu.Unlock()
			cancelled := rctx.Err() != nil
			if inflight[key] != nil {
				delete(inflight, key)
			}
			cancel()
			if cancelled { // the client gave up on it: no answer (MCP: a cancelled request is not responded to)
				return
			}
			if err := enc.Encode(response{JSONRPC: "2.0", ID: req.ID, Result: res, Error: rerr}); err != nil && werr == nil {
				werr = err
			}
		}()
	}
	// stdin closed: the client is gone, nobody reads the answers of what still runs.
	stop()
	wg.Wait()
	if werr != nil {
		return werr
	}
	return sc.Err()
}

// idKey names a JSON-RPC id the same however it was spaced (a number or a string).
func idKey(id json.RawMessage) string {
	var v any
	if json.Unmarshal(id, &v) != nil {
		return string(id)
	}
	b, _ := json.Marshal(v)
	return string(b)
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
			"instructions": "Persona tools reach the user's iPhone (phone_* tools: contacts, calendar, reminders, location, health, photos, " +
				"clipboard, notifications) and email accounts (mail_* tools). phone_* tools need the Persona app to be open on the iPhone. " +
				"mail_send asks the user to approve on the phone before sending." + assistantInstructions(),
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		tools, err := listTools(ctx)
		if err != nil {
			// listChanged is false, so an empty list would stick for the whole conversation: fail with the reason instead.
			return nil, &rpcError{Code: -32603, Message: "Persona tools are unavailable: " + err.Error()}
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
		// pane: an agent's MCP server has no session id of its own; the node knows the session in its pane
		// (report_to_supervisor needs to know whose report it is).
		line, err := callNode(ctx, map[string]any{"kind": "mcp.call", "name": p.Name, "args": p.Arguments,
			"session_id": sessionID(), "assistant": isAssistant(), "guest": isGuest(), "pane": node.HookPane(os.Getenv)})
		if err != nil {
			return toolResult(proto.TextResult(fmt.Sprintf("Persona node is not reachable (%v). Is the hub connected to this machine?", err), true)), nil
		}
		var res proto.ToolResult
		if err := json.Unmarshal(line, &res); err != nil {
			return toolResult(proto.TextResult("bad response from Persona node", true)), nil
		}
		return toolResult(res), nil
	}
	return nil, &rpcError{Code: -32601, Message: "method not found: " + req.Method}
}

// dialNode and retryEvery are swapped out by tests. dialNode hangs up when its ctx is cancelled, so a cancelled tool
// call ends on the node too (and with it the hub's call and its card, rpc.Peer.Cancels).
var (
	dialNode   = node.DialSocketInterruptible
	retryEvery = 500 * time.Millisecond
)

// callNode is dialNode for a tool call, tried again only while no node listens (nothing was sent): a resident node's
// socket belongs to the Node serving the hub's connection, so it is gone for a moment while a hub restart replaces
// that connection (docs/hub-restart.md "Conversations are not interrupted"). A call that reached the node is never sent twice.
func callNode(ctx context.Context, req any) ([]byte, error) {
	deadline := time.Now().Add(callRedialFor)
	for {
		line, err := dialNode(ctx, req)
		if err == nil || !errors.Is(err, node.ErrNoNode) || time.Now().After(deadline) {
			return line, err
		}
		select {
		case <-ctx.Done():
			return nil, err
		case <-time.After(retryEvery):
		}
	}
}

// callRedialFor: how long callNode waits for the node's socket to come back.
var callRedialFor = 15 * time.Second

// listTools asks the local node for the tools until ctx ends: the node may still be starting or reconnecting to the hub.
func listTools(ctx context.Context) ([]map[string]any, error) {
	for {
		tools, err := listToolsOnce(ctx)
		if err == nil {
			return tools, nil
		}
		select {
		case <-ctx.Done():
			return nil, err
		case <-time.After(retryEvery):
		}
	}
}

func listToolsOnce(ctx context.Context) ([]map[string]any, error) {
	line, err := dialNode(ctx, map[string]any{"kind": "mcp.tools", "assistant": isAssistant(), "session_id": sessionID(), "guest": isGuest()})
	if err != nil {
		return nil, fmt.Errorf("Persona node is not reachable: %w", err)
	}
	var res struct {
		Tools []proto.Tool `json:"tools"`
		Error string       `json:"error"`
	}
	if err := json.Unmarshal(line, &res); err != nil {
		return nil, fmt.Errorf("bad response from Persona node: %w", err)
	}
	if res.Error != "" { // the node could not reach the hub (node/hooks.go)
		return nil, fmt.Errorf("Persona node could not list tools: %s", res.Error)
	}
	tools := []map[string]any{}
	for _, t := range res.Tools {
		schema := t.InputSchema
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		tools = append(tools, map[string]any{"name": t.Name, "description": t.Description, "inputSchema": schema})
	}
	return tools, nil
}

// The assistant's processes start this server with JARVIS_ASSISTANT / JARVIS_SESSION_ID (node/assistant.go):
// they get the assistant-only tools, and the hub knows which conversation a call (e.g. ask_user) belongs to.
func isAssistant() bool { return identity.Getenv("JARVIS_ASSISTANT") == "1" }

// isGuest: a guest conversation's process (node/assistant_guest.go). The hub knows its guests itself; this is its word
// for one the hub cannot tell about, which then gets nothing.
func isGuest() bool { return identity.Getenv("JARVIS_GUEST") == "1" }

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
	return " As the Persona assistant you also have: ask_user (questions shown on the phone; AskUserQuestion does not work here), " +
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

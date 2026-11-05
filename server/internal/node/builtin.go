package node

// Built-in programs (#1830 steps 5 and 6, docs/plan-extensions.md「cua / ego 怎么迁」「iMessage 怎么用它」): the
// extensions cua (Cua Driver on this Mac), ego (ego lite) and imessage (this Mac's Messages, read-only) are programs on
// computers like any extension's, but they run in this process, not as a package the hub sends. A hub that sees them
// in node.info's builtin_programs asks them over plugin.node.call {id, method, params}; one from before asks by their
// old names (computer.<method>, browser.<method>, and for imessage connector.<method>), which stay. Both go through
// builtinCall: the same code answers either way.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/rpc"
)

// builtinPrograms are the built-in programs' ids (node.info's builtin_programs) and the prefix of their old method
// names that legacyBuiltinCall answers ("" for imessage: its old names, connector.*, have a case of their own in
// handle, and its methods keep those names over plugin.node.call, as the connector protocol has them).
var builtinPrograms = map[string]string{"cua": "computer.", "ego": "browser.", "imessage": ""}

// builtinProgramIDs is builtin_programs, in a fixed order.
var builtinProgramIDs = []string{"cua", "ego", "imessage"}

// errNoBuiltinMethod is a method the built-in program does not have.
var errNoBuiltinMethod = errors.New("no such method")

// builtinCall answers method of the built-in program id (cua: status, call, install, grant, cancel, clipboard, cursor,
// nolock, lock; ego: status, install, open, cancel, call, cookies.list, cookies.import, cookies.clear).
func builtinCall(ctx context.Context, id, method string, params json.RawMessage) (any, error) {
	switch id {
	case "cua":
		switch method {
		case "status": // Cua Driver on this Mac, asked only while the user has it on (computer.go)
			return computerStatus(ctx), nil
		case "call":
			return computerCall(ctx, params)
		case "install", "grant": // background jobs; their state comes back with status
			run := installCua
			if method == "grant" {
				run = grantCua
			}
			if err := startJob(method, run); err != nil {
				return nil, err
			}
			return computerStatus(ctx), nil
		case "cancel":
			cancelJob()
			return computerStatus(ctx), nil
		case "clipboard": // the live screen's 剪贴板: text RFB cannot carry (Chinese), and reading it (clipboard.go)
			return macClipboard(ctx, params)
		case "cursor": // 「显示 Agent 的光标」: Cua Driver's daemon with or without its cursor overlay (cua_extras.go)
			return setCursor(ctx, params)
		case "nolock": // 「扩展开着时不自动锁屏」, said again by the hub every few minutes (awake.go)
			var p struct {
				On bool `json:"on"`
			}
			_ = json.Unmarshal(params, &p)
			noLock.set(p.On)
			return map[string]any{"held": noLock.held()}, nil
		case "lock": // 锁上 from the app, after a one-tap unlock (awake.go)
			if err := lockScreen(ctx); err != nil {
				return nil, err
			}
			return computerStatus(ctx), nil
		}
	case "ego":
		switch method {
		case "status": // ego lite on this Mac, asked only while the user has the extension on (browser.go)
			return browserStatus(ctx), nil
		case "install", "open": // background jobs; their state comes back with status
			run := installEgo
			if method == "open" {
				run = openEgo
			}
			if err := egoJob.start(method, run, nil); err != nil {
				return nil, err
			}
			return browserStatus(ctx), nil
		case "cancel":
			egoJob.cancelInstall()
			return browserStatus(ctx), nil
		case "call":
			return browserCall(ctx, params)
		case "cookies.list", "cookies.import", "cookies.clear": // sign-ins in ego's profile (browser_cookies.go)
			return browserCookies(ctx, "browser."+method, params)
		}
	case "imessage":
		switch method {
		case "connector.list", "connector.chats", "connector.older", "connector.changes", "connector.search": // connectors.go
			return localConnector(ctx, method, params)
		}
	}
	return nil, errNoBuiltinMethod
}

// legacyBuiltinCall answers a built-in program's method by its old name (computer.status, browser.call…), for a hub
// from before builtin_programs; a name it does not have is an unknown method, as it was.
func legacyBuiltinCall(ctx context.Context, method string, params json.RawMessage) (any, error) {
	for id, prefix := range builtinPrograms {
		if prefix == "" {
			continue
		}
		if short, ok := strings.CutPrefix(method, prefix); ok {
			out, err := builtinCall(ctx, id, short, params)
			if errors.Is(err, errNoBuiltinMethod) {
				return nil, fmt.Errorf("unknown method %s", method)
			}
			return out, err
		}
	}
	return nil, fmt.Errorf("unknown method %s", method)
}

// callBuiltin is plugin.node.call for a built-in program: its answer as the JSON a program's would be.
func callBuiltin(ctx context.Context, id, method string, params json.RawMessage) (json.RawMessage, error) {
	out, err := builtinCall(ctx, id, method, params)
	if errors.Is(err, errNoBuiltinMethod) {
		return nil, &rpc.Error{Code: extension.CodeUnknownMethod, Message: "unknown method " + method + " of the built-in program " + id}
	}
	if err != nil {
		return nil, err
	}
	return json.Marshal(out)
}

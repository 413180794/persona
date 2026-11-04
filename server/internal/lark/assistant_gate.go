package lark

import (
	"fmt"

	"github.com/nwverse/persona/server/internal/proto"
)

// AssistantGate is the command gate of lark-connector's ol (the lark-connector skill) for the hub's own lark-connector, until the
// plugin that serves Feishu through lark-connector is the only one (docs/plan-plugins.md「第 3 步」): base and token go
// only into the assistant's process; reads run at once, writes others see are confirmed on the phone. user are the
// configured rules, put before the built-in ones.
func AssistantGate(base, token string, account int, user []proto.LarkRule) proto.CommandGate {
	env := map[string]string{"lark-connector_AGENT_BASE": base, "lark-connector_AGENT_TOKEN": token}
	if account > 0 {
		env["lark-connector_ACCOUNT"] = fmt.Sprint(account)
	}
	return proto.CommandGate{Command: "ol", Script: "lark-connector/scripts/ol.sh", Title: "飞书", Env: env, BaseEnv: "lark-connector_AGENT_BASE",
		Secret: []string{"lark-connector_AGENT_TOKEN"}, AllowedEnv: []string{"lark-connector_ACCOUNT", "OL_TIMEOUT"},
		Headers: []string{"content-type", "accept", "x-lark-connector-account"}, PathPrefix: "/agent/v1",
		Rules: append(append([]proto.LarkRule(nil), user...), assistantRules...), Actions: assistantActions}
}

var assistantRules = []proto.LarkRule{
	// GETs that act as the user or dump the account.
	{Method: "GET", Path: "/links/open", Action: "confirm"},
	{Method: "GET", Path: "/conversations/*/qr-code", Action: "confirm"},
	{Method: "GET", Path: "/maintenance/export", Action: "confirm"},
	{Method: "GET", Path: "**", Action: "allow"},
	{Method: "HEAD", Path: "**", Action: "allow"},
	// POSTs that only read (lark-connector's read-only token allows the same ones; references/platform.md), and translation.
	{Method: "POST", Path: "/context/harvest", Action: "allow"},
	{Method: "POST", Path: "/context/read", Action: "allow"},
	{Method: "POST", Path: "/context/jobs", Action: "allow"},
	{Method: "POST", Path: "/context/jobs/*/cancel", Action: "allow"},
	{Method: "POST", Path: "/context/jobs/*/query", Action: "allow"},
	{Method: "POST", Path: "/context/jobs/*/read", Action: "allow"},
	{Method: "POST", Path: "/search/page", Action: "allow"},
	{Method: "POST", Path: "/search/mention-history", Action: "allow"},
	{Method: "POST", Path: "/wiki/spaces/*/search", Action: "allow"},
	{Method: "POST", Path: "/documents/reader/export", Action: "allow"},
	{Method: "POST", Path: "/documents/share/export", Action: "allow"},
	{Method: "POST", Path: "/documents/base/attachments/export", Action: "allow"},
	{Method: "POST", Path: "/messages/read-states", Action: "allow"},
	{Method: "POST", Path: "/translate/text", Action: "allow"},
	{Method: "POST", Path: "/messages/*/translate", Action: "allow"},
	{Method: "POST", Path: "/messages/*/audio-text", Action: "allow"},
	// Finding the DM with someone (an existing one comes back with created:false; a new one is empty, nobody sees it).
	{Method: "POST", Path: "/conversations", Body: map[string]any{"type": "p2p"}, Action: "allow"},
	// Writes nobody else sees: the user's own read state, their own todo.
	{Method: "POST", Path: "/conversations/*/read-position", Action: "allow"},
	{Method: "POST", Path: "/conversations/*/read-thread-position", Action: "allow"},
	{Method: "POST", Path: "/conversations/*/local-read-position", Action: "allow"},
	{Method: "POST", Path: "/conversations/*/threads/*/read-position", Action: "allow"},
	{Method: "PUT", Path: "/conversations/*/unread", Action: "allow"},
	{Method: "PUT", Path: "/conversations/*/local-unread", Action: "allow"},
	{Method: "POST", Path: "/todos/*/complete", Body: map[string]any{"scope": "self"}, Action: "allow"},
	// Everything else acts as the user where others see it.
	{Method: "*", Path: "**", Action: "confirm"},
}

var assistantActions = []proto.GateAction{
	{Pattern: `^POST /conversations/[^/]+/(messages|files|images|videos|audios|stickers)`, What: "发消息"},
	{Pattern: `^POST /messages/[^/]+/replies`, What: "回复消息"},
	{Pattern: `^POST /messages/[^/]+/(group-)?recall`, What: "撤回消息"},
	{Pattern: `^(POST|DELETE) /messages/[^/]+/reactions`, What: "表情回复"},
	{Pattern: `^POST /messages/([^/]+/)?(forward|merge-forward|forward-many)`, What: "转发消息"},
	{Pattern: `^POST /messages/[^/]+/urgent`, What: "加急"},
	{Pattern: `^\S+ /calendar`, What: "建 / 改日程"},
	{Pattern: `^\S+ /(documents|docs|sheets|wiki|drive|base)`, What: "写文档"},
	{Pattern: `^\S+ /mail`, What: "发 / 改邮件"},
	{Pattern: `^\S+ /todos`, What: "改待办"},
	{Pattern: `^\S+ /approvals`, What: "处理审批"},
	{Pattern: `^\S+ /(groups|conversations)`, What: "改群 / 会话"},
}

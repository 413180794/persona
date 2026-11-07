package node

import "github.com/nwverse/persona/server/internal/proto"

// exampleGate is a command gate the way a plugin's assistant.gate makes one (docs/extensions/extensions.md "Assistant"): `ex METHOD PATH
// [BODY]` against an example API at base, its token in EXAMPLE_TOKEN. user are the configured rules, first.
func exampleGate(base string, user ...proto.LarkRule) proto.CommandGate {
	return proto.CommandGate{Command: "ex", Script: "example/scripts/ex.sh", Title: "示例",
		Env: map[string]string{"EXAMPLE_BASE": base, "EXAMPLE_TOKEN": "test-token"}, BaseEnv: "EXAMPLE_BASE",
		Secret: []string{"EXAMPLE_TOKEN"}, AllowedEnv: []string{"EXAMPLE_ACCOUNT", "EX_TIMEOUT"},
		Headers: []string{"content-type", "accept", "x-example-account"}, PathPrefix: "/api/v1",
		Rules: append(append([]proto.LarkRule(nil), user...), exampleRules...),
		Actions: []proto.GateAction{
			{Pattern: `^POST /conversations/[^/]+/(messages|files)`, What: "发消息"},
			{Pattern: `^(POST|DELETE) /messages/[^/]+/reactions`, What: "表情回复"},
			{Pattern: `^POST /messages/[^/]+/recall`, What: "撤回消息"},
		}}
}

var exampleRules = []proto.LarkRule{
	{Method: "GET", Path: "/links/open", Action: "confirm"},
	{Method: "GET", Path: "**", Action: "allow"},
	{Method: "HEAD", Path: "**", Action: "allow"},
	{Method: "POST", Path: "/context/harvest", Action: "allow"},
	{Method: "POST", Path: "/search/page", Action: "allow"},
	{Method: "POST", Path: "/conversations", Body: map[string]any{"type": "p2p"}, Action: "allow"},
	{Method: "POST", Path: "/conversations/*/read-position", Action: "allow"},
	{Method: "POST", Path: "/todos/*/complete", Body: map[string]any{"scope": "self"}, Action: "allow"},
	{Method: "*", Path: "**", Action: "confirm"},
}

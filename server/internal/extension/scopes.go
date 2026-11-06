package extension

import (
	"strings"

	"github.com/nwverse/persona/server/internal/extwire"
)

// Defined in the contract package extwire (docs/architecture.md "Contract packages"); the old names stay.
const (
	ScopeAgentsControl = extwire.ScopeAgentsControl
	ScopeAgentsRead    = extwire.ScopeAgentsRead
	ScopeAssistantAsk  = extwire.ScopeAssistantAsk
	ScopeCallPrefix    = extwire.ScopeCallPrefix
	ScopeInboxRead     = extwire.ScopeInboxRead
	ScopeInboxWrite    = extwire.ScopeInboxWrite
	ScopeKBRead        = extwire.ScopeKBRead
	ScopeKBWrite       = extwire.ScopeKBWrite
	ScopeMachinesRead  = extwire.ScopeMachinesRead
	ScopeNotify        = extwire.ScopeNotify
	ScopeTodosRead     = extwire.ScopeTodosRead
	ScopeTodosWrite    = extwire.ScopeTodosWrite
)

// HubScopes are the scopes, in the order the install screen lists them.
var HubScopes = []string{ScopeKBRead, ScopeKBWrite, ScopeInboxRead, ScopeInboxWrite, ScopeAgentsRead, ScopeAgentsControl,
	ScopeMachinesRead, ScopeNotify, ScopeAssistantAsk, ScopeTodosRead, ScopeTodosWrite}

var scopeTitles = map[string]string{
	ScopeKBRead:        "读知识库",
	ScopeKBWrite:       "改知识库里的条目",
	ScopeInboxRead:     "读收件箱",
	ScopeInboxWrite:    "处理收件箱（标为已处理、起草和发送回复、静音）",
	ScopeAgentsRead:    "看各台电脑上的 Agent 和对话",
	ScopeAgentsControl: "驱动 Agent（新建、发消息、打断、回答提问和批准）",
	ScopeMachinesRead:  "看你的各台电脑（在不在线、负载）",
	ScopeNotify:        "给你的设备发通知",
	ScopeAssistantAsk:  "给 Persona 开对话、读对话动态",
	ScopeTodosRead:     "看待办",
	ScopeTodosWrite:    "新建和修改待办、把对话挂到待办上（不能删待办）",
}

// ScopeTitle is how the install screen says scope s; ok false for what is not a scope.
func ScopeTitle(s string) (string, bool) {
	if id, ok := strings.CutPrefix(s, ScopeCallPrefix); ok {
		if !idPattern.MatchString(id) {
			return "", false
		}
		return "调用扩展「" + id + "」的接口", true
	}
	t, ok := scopeTitles[s]
	return t, ok
}

// hubLine is the line for scopes (all an extension asks for, or those an upgrade adds), in HubScopes' order.
func hubLine(title string, scopes []string) PermissionLine {
	var what []string
	for _, known := range HubScopes {
		for _, s := range scopes {
			if s == known {
				what = append(what, scopeTitles[s])
			}
		}
	}
	for _, s := range scopes {
		if strings.HasPrefix(s, ScopeCallPrefix) {
			t, _ := ScopeTitle(s)
			what = append(what, t)
		}
	}
	return PermissionLine{Key: "hub", Title: title, Detail: strings.Join(what, "；")}
}

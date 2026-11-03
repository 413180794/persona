package node

import (
	"encoding/json"
	"net/url"

	"github.com/nwverse/persona/server/internal/proto"
)

func elicitationInteraction(in hookInput, ix *proto.Interaction) bool {
	mode := in.Mode
	if mode == "" {
		mode = "form"
	}
	if ix.MCPServerName == "" {
		return false
	}
	switch mode {
	case "form":
		var schema map[string]json.RawMessage
		if json.Unmarshal(in.RequestedSchema, &schema) != nil || string(schema["type"]) != `"object"` {
			return false
		}
	case "url":
		u, err := url.Parse(in.URL)
		if err != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") || in.ElicitationID == "" {
			return false
		}
	default:
		return false
	}
	ix.Kind, ix.Summary = proto.IxElicitation, in.Message
	ix.Elicitation = &proto.Elicitation{Mode: mode, Message: in.Message, ID: in.ElicitationID}
	if mode == "form" {
		ix.Elicitation.RequestedSchema = in.RequestedSchema
	} else {
		ix.Elicitation.URL = in.URL
	}
	return true
}

func elicitationOutput(ix proto.Interaction, res proto.Resolution) any {
	if res.Dismiss || (res.Action != "accept" && res.Action != "decline" && res.Action != "cancel") {
		return nil
	}
	out := map[string]any{"hookEventName": "Elicitation", "action": res.Action}
	if res.Action == "accept" && ix.Elicitation != nil && ix.Elicitation.Mode == "form" {
		content := res.Content
		if content == nil {
			content = map[string]any{}
		}
		out["content"] = content
	}
	return map[string]any{"hookSpecificOutput": out}
}

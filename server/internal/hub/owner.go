package hub

import "strings"

// ownerName is how the hub's own prompts call the user (the Feishu bot, the channels, the knowledge base, the turn
// triage): hub.json's assistant.owner, else 「用户」.
func (h *Hub) ownerName() string {
	if o := strings.TrimSpace(h.cfg.Assistant.Owner); o != "" {
		return o
	}
	return "用户"
}

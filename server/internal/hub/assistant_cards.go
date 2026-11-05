package hub

// assistantCards is whether the assistant's card interfaces answer now: views is assistant.persona_view, bridge is
// assistant.html_views (default on) as well. The assistant's hello and hello.features read only this, so they agree.
func (h *Hub) assistantCards() (views, bridge bool) {
	if h.cfg == nil {
		return false, false
	}
	views = h.assistantPersonaView()
	return views, views && h.cfg.Assistant.HTMLViewsOn()
}

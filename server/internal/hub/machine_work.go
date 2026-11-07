package hub

// machineIsWork uses the hub's configuration for every path to a computer. Trust applies to the physical
// computer, so an unrestricted alias must not undo a work entry's restrictions.
func (h *Hub) machineIsWork(id string) bool {
	m := h.machineByID(id)
	for _, entry := range h.cfgEntries() {
		if entry.Trust == "work" && (entry.ID == id || m != nil && h.machineByID(entry.ID) == m) {
			return true
		}
	}
	if m == nil {
		return false
	}
	if !m.derived && m.cfg.Trust == "work" {
		return true
	}
	view := h.meshView()
	if view == nil {
		return false
	}
	for _, path := range h.pathInfos(m) {
		if member := view.hubs[path.HubID]; member != nil {
			for _, entry := range member.rec.Machines {
				if entry.ID == path.ID && entry.Cfg.Trust == "work" {
					return true
				}
			}
		}
	}
	return false
}

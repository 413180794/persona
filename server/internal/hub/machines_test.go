package hub

// registerMachine adds m to the machines the apps see, under h.fleet.mu: a connected assistant reads them from its
// own goroutine (FleetUp lists the fleet through machinesNow), so a test that adds a machine after connectAssistant
// must not write h.machines and h.byID bare.
func registerMachine(h *Hub, m *Machine) {
	h.fleet.mu.Lock()
	defer h.fleet.mu.Unlock()
	h.machines = append(h.machines, m)
	h.byID[m.cfg.ID] = m
}

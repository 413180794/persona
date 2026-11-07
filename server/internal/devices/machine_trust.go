package devices

import "encoding/json"

// machineTrust reads the hub's configurations, never node.info. Any work path restricts the whole computer,
// including an absorbed alias. Member records are read each time: a derived machine keeps its original cfg.
func (d *Devices) machineTrust(m *Machine) string {
	paths := d.pathsOf(m)
	if len(paths) == 0 {
		return m.cfg.Trust
	}
	trust := ""
	for _, path := range paths {
		var current string
		if path.src != nil {
			current = path.src.cfg.Trust
		} else {
			var cfg MachineConfig
			if json.Unmarshal(path.rec.Cfg, &cfg) == nil {
				current = cfg.Trust
			}
		}
		if current == "personal" {
			trust = current
		}
		if current == "work" {
			return "work"
		}
	}
	return trust
}

// IsWork resolves aliases before checking the hub's trust policy.
func (d *Devices) IsWork(id string) bool {
	m := d.machineByID(id)
	return m != nil && d.machineTrust(m) == "work"
}

package extension

import (
	"strings"
	"testing"
)

// runtime.resources: what a sandboxed program may use, within bounds; unset parts are the defaults.
func TestManifestResources(t *testing.T) {
	manifest := func(res string) string {
		return `{"manifest_version": 1, "id": "notes", "name": "Notes", "version": "1.0.0", "engines": {"plugin_protocol": 1},
			"runtime": {"command": "bin/notes", "resources": ` + res + `}, "activation_events": ["on_startup"], "contributes": {"events": [{"id": "e", "subscribe": ["agent.finished"]}]}}`
	}
	m, err := Parse([]byte(manifest(`{"memory_mb": 256}`)))
	if err != nil {
		t.Fatal(err)
	}
	if l := m.Runtime.Resources.Limits(); l.MemoryMB != 256 || l.CPUPercent != ResourcesCPUDefault {
		t.Fatalf("%+v", l)
	}
	if l := (*Resources)(nil).Limits(); l.MemoryMB != ResourcesMemoryDefault || l.CPUPercent != ResourcesCPUDefault {
		t.Fatalf("defaults %+v", l)
	}
	for _, bad := range []string{`{"memory_mb": 10}`, `{"memory_mb": 100000}`, `{"cpu_percent": 5}`, `{"cpu_percent": 900}`, `{"disk_mb": 1}`} {
		if _, err := Parse([]byte(manifest(bad))); err == nil || !strings.Contains(err.Error(), "resources") && !strings.Contains(err.Error(), "disk_mb") {
			t.Errorf("%s: %v", bad, err)
		}
	}
}

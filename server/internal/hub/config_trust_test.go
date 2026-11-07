package hub

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/i18n"
)

func TestLoadConfigMachineTrust(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PERSONA_HOME", "")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if _, err := LoadConfig(); err != nil {
		t.Fatal(err)
	}
	for _, trust := range []string{"", "personal", "work", "unknown", "WORK"} {
		t.Run(trust, func(t *testing.T) {
			body, _ := json.Marshal(map[string]any{"token": "example-token", "machines": []MachineConfig{{ID: "computer", Trust: trust}}})
			if err := os.WriteFile(filepath.Join(home, ".persona", "hub.json"), body, 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, err := LoadConfig()
			valid := trust == "" || trust == "personal" || trust == "work"
			if (err == nil) != valid {
				t.Fatalf("trust %q: %v", trust, err)
			}
			if valid && cfg.Machines[0].Trust != trust {
				t.Fatalf("trust lost: %+v", cfg.Machines)
			}
			if !valid && err.Error() != i18n.Err("machine-trust.invalid_trust", i18n.Args{"machine": "computer"}).Error() {
				t.Fatalf("unlocalized error: %v", err)
			}
		})
	}
}

func TestAssistantPlacementSkipsWorkComputers(t *testing.T) {
	for _, trust := range []string{"", "personal", "work"} {
		t.Run(trust, func(t *testing.T) {
			h := newTestHub(t)
			for _, cfg := range []MachineConfig{{ID: "local", Kind: "local", Trust: trust}, {ID: "personal", Kind: "ssh"}} {
				m := &Machine{cfg: cfg, hub: h}
				h.machines = append(h.machines, m)
				h.byID[cfg.ID] = m
				h.cfg.Machines = append(h.cfg.Machines, cfg)
			}
			hello, err := (assistantLink{h}).Hello(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			want := "local"
			if trust == "work" {
				want = "personal"
			}
			if hello.Assistant.MachineID != want || slices.Contains(hello.Assistant.WorkMachines, "local") != (trust == "work") {
				t.Fatalf("placement: %+v", hello.Assistant)
			}
			// A manager that owns only the work computer has no eligible fallback.
			h.devices.assign = map[string]string{"personal": "other"}
			placed := h.placedFor(hello, assistantwire.AssistantID)
			if trust == "work" && placed.Assistant.MachineID != "" {
				t.Fatalf("work fallback: %+v", placed.Assistant)
			}
		})
	}
}

func TestWorkTrustIncludesAbsorbedAlias(t *testing.T) {
	h := newTestHub(t)
	cfg := MachineConfig{ID: "personal", Kind: "local"}
	m := &Machine{cfg: cfg, hub: h}
	h.machines, h.byID[cfg.ID] = []*Machine{m}, m
	h.cfg.Machines = []MachineConfig{cfg, {ID: "work-alias", Trust: "work"}}
	h.devices.absorbed = map[string]string{"work-alias": cfg.ID}
	for _, id := range []string{cfg.ID, "work-alias"} {
		if !h.machineIsWork(id) {
			t.Fatalf("unrestricted alias %q", id)
		}
	}
	if got := h.defaultAssistantMachine(); got != "" {
		t.Fatalf("assistant default %q", got)
	}
}

func TestMachineTrustSurvivesManagerAndMeshHandover(t *testing.T) {
	h := newTestHub(t)
	h.cfg.Machines = []MachineConfig{{ID: "work", Kind: "ssh", Trust: "work", SSHHost: "work.example.com"}}
	cfg := h.devicesConfig()
	if len(cfg.Machines) != 1 {
		t.Fatalf("handover: %+v", cfg.Machines)
	}
	var machine MachineConfig
	if err := json.Unmarshal(cfg.Machines[0], &machine); err != nil || machine.Trust != "work" {
		t.Fatalf("trust lost in manager handover: %+v, %v", machine, err)
	}
	entries := h.recordEntries()
	if len(entries) != 1 || entries[0].Cfg.Trust != "work" || entries[0].Cfg.SSHHost != "" {
		t.Fatalf("mesh handover: %+v", entries)
	}
}

func TestWorkPlacementUsesCurrentMemberPaths(t *testing.T) {
	h := newTestHub(t)
	m := &Machine{cfg: MachineConfig{ID: "remote", Kind: "hub"}, hub: h, derived: true,
		paths: []pathInfo{{Via: viaHub, HubID: "member", ID: "computer"}}}
	h.machines, h.byID[m.cfg.ID] = []*Machine{m}, m
	for _, trust := range []string{"", "work", "personal"} {
		h.mesh.view.Store(&meshView{hubs: map[string]*viewHub{"member": {rec: &hubRecord{Machines: []recMachine{{ID: "computer", Cfg: MachineConfig{Trust: trust}}}}}}})
		if got := h.machineIsWork("remote"); got != (trust == "work") {
			t.Fatalf("trust %q: work = %v", trust, got)
		}
		want := "remote"
		if trust == "work" {
			want = ""
		}
		if got := h.defaultAssistantMachine(); got != want {
			t.Fatalf("trust %q: assistant %q, want %q", trust, got, want)
		}
	}
}

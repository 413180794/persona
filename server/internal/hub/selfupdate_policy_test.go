package hub

import (
	"context"
	"github.com/nwverse/persona/server/internal/hubedge"
	"github.com/nwverse/persona/server/internal/proto"
	"os"
	"path/filepath"
	"testing"

	"github.com/nwverse/persona/server/internal/update"
)

func TestHubOnlyUpdateNeverFetchesOrInstallsNodePackages(t *testing.T) {
	p := newPublisherWith(t, "v2-202611021700", 202611021700, func(m *update.Manifest) {
		// This advertised platform has no download endpoint. A Hub-only update must
		// succeed without ever requesting it.
		m.Files["plan9-arm"] = update.File{Name: "personad-plan9-arm.gz", Size: 1}
	})
	h, _, _, execs := updatingHub(t, p, "release")
	if h.devicesConfig().AutoUpdateNodes {
		t.Fatal("node updates enabled by default")
	}
	nodes := filepath.Join(t.TempDir(), "absent-nodes")
	t.Setenv("PERSONA_NODE_BINARIES", nodes)
	if _, err := h.upd.check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := h.upd.begin(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "Hub-only restart", func() bool { return len(execs()) == 1 })
	var pending update.Pending
	if ok, err := update.Load(update.PendingPath(h.upd.dir), &pending); !ok || err != nil {
		t.Fatalf("pending %v %v", ok, err)
	}
	if pending.Nodes != "" {
		t.Fatalf("staged nodes: %s", pending.Nodes)
	}
	h.upd.updateNodes(context.Background(), t.TempDir())
	if _, err := os.Stat(nodes); !os.IsNotExist(err) {
		t.Fatalf("node directory recreated: %v", err)
	}
	h.cfg.AutoUpdateNodes = true
	if !h.devicesConfig().AutoUpdateNodes {
		t.Fatal("explicit policy not handed to manager")
	}
}

func TestUpdaterReadsPrivateFileSource(t *testing.T) {
	h := newTestHub(t)
	dir := t.TempDir()
	manifest := `{"version":"v2","build":202611021700,"files":{"linux-amd64":{"name":"personad-linux-amd64.gz"}}}`
	if err := os.WriteFile(filepath.Join(dir, "latest.json"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	h.build.UpdateURL = "file://" + dir
	if _, err := h.upd.check(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestHubOnlySlotKeepsRemoteAgentsIndependent(t *testing.T) {
	p := newPublisher(t, "v2-202611021700", 202611021700)
	h, m, _, _ := updatingHub(t, p, "release")
	setSessions(m, proto.Session{SessionID: "long-task", Status: proto.StatusWorking, Host: proto.EngineHerdr})
	h.slot = &hubedge.Slot{}
	if busy := h.updateBusy(); len(busy) != 0 {
		t.Fatalf("remote node held Hub-only switch: %+v", busy)
	}
	setSessions(m, proto.Session{SessionID: "direct-assistant", Status: proto.StatusWorking})
	if len(h.updateBusy()) != 1 {
		t.Fatal("unknown remote assistant was not protected")
	}
	m.info.NodeUpdate = true
	if len(h.updateBusy()) != 0 {
		t.Fatal("resident node should survive transport reconnect")
	}
	m.info.NodeUpdate = false
	setSessions(m, proto.Session{SessionID: "long-task", Status: proto.StatusWorking, Host: proto.EngineHerdr})
	h.cfg.AutoUpdateNodes = true
	if len(h.updateBusy()) != 1 {
		t.Fatal("fleet update did not wait for agent")
	}
	h.cfg.AutoUpdateNodes = false
	m.cfg.Kind = "local"
	if len(h.updateBusy()) != 1 {
		t.Fatal("local agent was not protected")
	}
}

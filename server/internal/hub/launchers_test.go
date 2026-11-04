package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// A runtime's launcher (DSH) is offered in the new-chat picker under its own source, and is not listed among
// the models of 设置 › 模型 (it is not a ~/.zshrc function to probe, edit or hide).
func TestRuntimeLauncherKeepsItsSource(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	h2nR, h2nW := io.Pipe()
	n2hR, n2hW := io.Pipe()
	hubSide := rpc.New("h", h2nW)
	nodeSide := rpc.New("n", n2hW)
	nodeSide.OnRequest = func(_ context.Context, method string, _ json.RawMessage) (any, error) {
		if method != "launchers.list" {
			return nil, fmt.Errorf("unexpected %s", method)
		}
		return map[string]any{"launchers": []proto.Launcher{{Name: "cc_ds", Model: "m1"}, {Name: "dsh", Model: "DeepSeek Harness", Source: "dsh"}}}, nil
	}
	go func() { _ = hubSide.Serve(ctx, n2hR) }()
	go func() { _ = nodeSide.Serve(ctx, h2nR) }()
	t.Cleanup(func() { cancel(); _ = h2nW.Close(); _ = n2hW.Close() })
	h := &Hub{}
	m := newMachine(h, MachineConfig{ID: "m1", Name: "m1"})
	m.peer = hubSide
	h.machines = []*Machine{m}

	list, err := h.machineLaunchers(ctx, m)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Source != "zshrc" || list[1].Name != "dsh" || list[1].Source != "dsh" {
		t.Fatalf("picker %+v", list)
	}
	views := h.launcherViews(ctx)
	if len(views) != 1 || views[0].Name != "cc_ds" {
		t.Fatalf("设置 › 模型 lists %+v", views)
	}
}

func TestLauncherKeySealsAndOpens(t *testing.T) {
	h := &Hub{cfg: &Config{dir: t.TempDir()}}
	sealed, err := h.sealKey("sk-test")
	if err != nil || sealed == "" || sealed == "sk-test" {
		t.Fatalf("sealed %q %v", sealed, err)
	}
	h2 := &Hub{cfg: h.cfg} // a restart reads the same secret.key
	if plain, err := h2.openKey(sealed); err != nil || plain != "sk-test" {
		t.Fatalf("opened %q %v", plain, err)
	}
}

func TestLauncherInputCheck(t *testing.T) {
	ok := launcherInput{Name: "jv_strong", BaseURL: "https://gw.example.com", Model: "m1", Args: []string{"--dangerously-skip-permissions"}}
	if err := ok.check(); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []launcherInput{
		{Name: "claude", BaseURL: "https://gw.example.com", Model: "m"},
		{Name: "a b", BaseURL: "https://gw.example.com", Model: "m"},
		{Name: "x", BaseURL: "gw.example.com", Model: "m"},
		{Name: "x", BaseURL: "https://gw.example.com"},
		{Name: "x", BaseURL: "https://gw.example.com", Model: "m", Args: []string{"; rm -rf ~"}},
		{Name: "x", BaseURL: "https://gw.example.com", Model: "m", Kind: "api", SubagentModel: "s"},
		{Name: "x", BaseURL: "https://gw.example.com", Model: "m", Kind: "api", CustomModel: "c"},
		{Name: "x", BaseURL: "https://gw.example.com", Model: "m", CustomModelDescription: "no name"},
	} {
		if bad.check() == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
}

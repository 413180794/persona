package hub

import (
	"slices"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

func TestAdminAssistantToolsIgnoreStaleSwitch(t *testing.T) {
	h := newTestHub(t)
	switchTo := func(on bool) uint64 {
		h.admin.mu.Lock()
		defer h.admin.mu.Unlock()
		h.admin.personaView = &on
		h.admin.personaViewGeneration++
		return h.admin.personaViewGeneration
	}
	hasView := func() bool {
		return slices.ContainsFunc(h.assistantToolsNow(), func(tool proto.Tool) bool { return tool.Name == "view_show" })
	}
	old := switchTo(false)
	current := switchTo(true)
	if h.storeAssistantTools(current, nil) {
		t.Fatal("a handshake with an older Hello replaced the tool list")
	}
	if !h.storeAssistantTools(current, []proto.Tool{{Name: "view_show"}}) || !hasView() {
		t.Fatal("current refresh did not enable view tools")
	}
	if h.storeAssistantTools(old, nil) || !hasView() {
		t.Fatal("late disabled refresh replaced the current tool list")
	}
	latest := switchTo(false)
	if !h.storeAssistantTools(latest, nil) || hasView() {
		t.Fatal("current refresh did not disable view tools")
	}
	if h.storeAssistantTools(current, []proto.Tool{{Name: "view_show"}}) || hasView() {
		t.Fatal("late enabled refresh replaced the current tool list")
	}
	// Returning to the same switch value must still reject a response from its earlier generation.
	again := switchTo(true)
	if h.storeAssistantTools(current, []proto.Tool{{Name: "stale"}}) {
		t.Fatal("same-value switch accepted an earlier generation")
	}
	if h.assistantToolsGeneration() != again || !h.storeAssistantTools(again, []proto.Tool{{Name: "view_show"}}) || !hasView() {
		t.Fatal("latest generation did not become current")
	}
}

package extension

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/nwverse/persona/sdk/go/wire"
)

func TestCapsuleCardIsCompactAndNeedsConsent(t *testing.T) {
	base := mustParse(t)(with(t, func(m map[string]any) {
		m["contributes"] = map[string]any{"ui": []any{map[string]any{"id": "home", "title": "首页", "placement": "extension_page"}}}
	}))
	capsule := mustParse(t)(with(t, func(m map[string]any) {
		m["contributes"] = map[string]any{"ui": []any{
			map[string]any{"id": "home", "title": "首页", "placement": "extension_page"},
			map[string]any{"id": "task", "title": "任务状态", "placement": "capsule_card", "opens": "home"},
		}}
	}))
	slot, ok := Slot("capsule_card")
	if !ok || slot.Size != SizeCompact || slot.Since != 3 {
		t.Fatalf("capsule slot: %+v", slot)
	}
	if !slices.Contains(capsule.Surfaces(), "capsule_card") || Digest(base) == Digest(capsule) {
		t.Fatal("a capsule is a new external surface, requiring consent")
	}
	for _, permission := range []any{Explain(capsule), Added(base, capsule)} {
		if !strings.Contains(fmt.Sprint(permission), "Android 跨 App 悬浮胶囊里的卡片（在 App 外显示）") {
			t.Fatalf("missing external surface explanation: %v", permission)
		}
	}
	valid := &View{Sections: []Section{{Items: []UIElem{
		{Type: "text", Text: "正在处理"},
		{Type: "button", Title: "继续", Action: &UIAction{ID: "continue"}},
	}}}}
	if _, err := Fit(valid, slot, UIVersion); err != nil {
		t.Fatal(err)
	}
	field := &View{Sections: []Section{{Items: []UIElem{{Type: "field", Name: "answer", Label: "回答", Kind: "text"}}}}}
	if _, err := Fit(field, slot, UIVersion); err == nil {
		t.Fatal("compact capsule cards do not accept fields")
	}
}

func TestCapsulePlacementAgreesWithSchema(t *testing.T) {
	raw := with(t, func(m map[string]any) {
		m["contributes"] = map[string]any{"ui": []any{map[string]any{"id": "task", "title": "任务", "placement": "capsule_card"}}}
	})
	if _, err := Parse(raw); err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(manifest)
	if err := wire.ValidateJSON("Manifest", raw); err != nil {
		t.Fatal(err)
	}
}

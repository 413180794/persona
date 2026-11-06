package uistream

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/nwverse/persona/sdk/go/wire"
)

func TestConcurrentImmutableReaders(t *testing.T) {
	from := treeFromFixture(t, "traces/UIStreamSnapshot.native-presence.json")
	d := delta(t, `[{"op":"remove","key":"code"}]`)
	to, err := Apply(from, d)
	if err != nil {
		t.Fatal(err)
	}
	original := encoded(t, from)
	var group sync.WaitGroup
	for i := 0; i < 4; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for j := 0; j < 3; j++ {
				projection := from.Snapshot()
				projection.View.Sections[0].Items[5].Control.Action.ID = "local-projection"
				candidate, _, err := Diff(from, to)
				if err != nil {
					t.Error(err)
					return
				}
				result, err := Apply(from, candidate)
				if err != nil {
					t.Error(err)
					return
				}
				assertSame(t, result, to)
			}
		}()
	}
	group.Wait()
	if !bytes.Equal(original, encoded(t, from)) {
		t.Fatal("concurrent reader mutated canonical state")
	}
}

func TestUnicodeSourceRejection(t *testing.T) {
	for _, escape := range []string{`\ud800`, `\udfff`, `\ud800A`, `\ud800\ud800`} {
		raw := []byte(`{"protocol":1,"session":"s","epoch":"e","version":1,"root_key":"page","view":{"title":"` + escape + `","sections":[]}}`)
		if _, err := DecodeSnapshot(raw); err == nil {
			t.Fatalf("accepted silently rewritten Unicode %s", escape)
		}
	}
	snapshot(t, fmt.Sprintf(`{"title":"%cud83d%cude00","sections":[]}`, 92, 92), 1)
	snapshot(t, string(mustJSON(map[string]any{"title": string(rune(0x1f600)), "sections": []any{}})), 1)
	snapshot(t, `{"title":"\\ud800","sections":[]}`, 1)
}

func TestNestedExactNamesMediaAndActionValues(t *testing.T) {
	for _, item := range []string{
		`{"type":"row","key":"a","title":"A","control":{"kind":"switch","name":"x","on":false,"On":true,"action":{"id":"cb"}}}`,
		`{"type":"button","key":"a","title":"A","action":{"id":"cb","ID":"other"}}`,
		`{"type":"row","key":"a","title":"A","avatar":{"url":"https://user:password@example.com/avatar"}}`,
		`{"type":"row","key":"a","title":"A","control":{"kind":"slider","name":"x","min":0,"max":1,"value":0,"on":false,"action":{"id":"cb"}}}`,
		`{"type":"button","key":"a","title":"A","action":{"id":"cb","values":{"":"empty name"}}}`,
		`{"type":"button","key":"a","title":"A","action":{"id":"cb","values":{"~private":"reserved"}}}`,
		`{"type":"row","key":"a","title":"A","spark":{"values":[1e999999999,0]}}`,
	} {
		raw := []byte(`{"protocol":1,"session":"session-a","epoch":"epoch-a","version":1,"root_key":"page","view":{"sections":[{"key":"s","items":[` + item + `]}]}}`)
		if _, err := DecodeSnapshot(raw); err == nil {
			t.Fatalf("accepted unsafe native payload %s", item)
		}
	}
	// Action values are data, not media properties. Very small numbers must not
	// allocate exponent-sized integers/denominators in DTO normalization.
	snapshot(t, `{"sections":[{"key":"s","items":[{"type":"button","key":"a","title":"A","action":{"id":"cb","values":{"url":"not a media URL"}}},{"type":"progress","key":"p","label":"P","ratio":1e-999999999}]}]}`, 1)
}

func TestCompositeFrameByteBoundIsAtomic(t *testing.T) {
	// Each source snapshot/operation is under 1 MiB. Together they would exceed
	// the materialized frame bound even though logical node count is tiny.
	text := strings.Repeat("x", 190000)
	items := make([]any, 4)
	ops := make([]map[string]json.RawMessage, 4)
	for i := range items {
		items[i] = map[string]any{"type": "code", "key": fmt.Sprintf("a%d", i), "text": text}
		ops[i] = operation("insert", "parent", "s", "item", mustJSON(map[string]any{"type": "code", "key": fmt.Sprintf("b%d", i), "text": text}))
	}
	a := snapshot(t, string(mustJSON(map[string]any{"sections": []any{map[string]any{"key": "s", "items": items}}})), 1)
	original := encoded(t, a)
	d := delta(t, string(mustJSON(ops)))
	if _, err := Apply(a, d); !errors.Is(err, ErrSnapshotRequired) {
		t.Fatal("accepted oversized composite tree")
	}
	if !bytes.Equal(original, encoded(t, a)) {
		t.Fatal("oversized batch mutated source")
	}
	// Canonical escaping overhead also counts toward the serializable frame.
	raw := []byte(`{"protocol":1,"session":"s","epoch":"e","version":1,"root_key":"page","view":{"sections":[{"key":"s","items":[{"type":"code","key":"a","text":"` + strings.Repeat("<", 190000) + `"}]}]}}`)
	if _, err := DecodeSnapshot(raw); err == nil {
		t.Fatal("accepted oversized canonical serialization")
	}
}

func TestDiffOperationBudgetFallsBackToRoot(t *testing.T) {
	// 290 changed components cannot be sent as 290 operations. Full native root
	// replacement preserves every cleared field and has a snapshot recommendation.
	views := func(cleared bool) string {
		items := make([]map[string]any, 290)
		for i := range items {
			items[i] = map[string]any{"type": "code", "key": fmt.Sprintf("n%d", i), "text": ""}
			if cleared {
				items[i]["truncated"] = false
			}
		}
		return string(mustJSON(map[string]any{"sections": []any{map[string]any{"key": "s", "items": items}}}))
	}
	a, b := snapshot(t, views(false), 1), snapshot(t, views(true), 2)
	d, recommend, err := Diff(a, b)
	if err != nil {
		t.Fatal(err)
	}
	ops := d.Frame().Operations
	if !recommend || len(ops) != 1 || ops[0].View == nil {
		t.Fatal("operation budget did not trigger root fallback")
	}
	result, err := Apply(a, d)
	if err != nil {
		t.Fatal(err)
	}
	assertSame(t, result, b)
}

func TestSnapshotOnlyWhenDeltaOverheadExceedsBudget(t *testing.T) {
	items := make([]map[string]any, 6)
	for i := range items {
		items[i] = map[string]any{"type": "code", "key": fmt.Sprintf("n%d", i), "text": strings.Repeat("x", 170000)}
	}
	view := map[string]any{"title": "New", "sections": []any{map[string]any{"key": "main", "items": items}}}
	frame := map[string]any{"protocol": 1, "session": "session-a", "epoch": "epoch-a", "version": 2, "root_key": "page", "view": view}
	raw := mustJSON(frame)
	items[5]["text"] = items[5]["text"].(string) + strings.Repeat("x", FrameMost-len(raw)-1)
	raw = mustJSON(frame)
	if len(raw) != FrameMost-1 {
		t.Fatal("bad near-limit fixture")
	}
	target, err := DecodeSnapshot(raw)
	if err != nil {
		t.Fatal(err)
	}
	from := snapshot(t, `{"sections":[]}`, 1)
	delta, recommended, err := Diff(from, target)
	if err != nil || !recommended || delta != nil {
		t.Fatalf("expected snapshot-only recommendation, got %v / %v / %v", delta, recommended, err)
	}
}

func TestEventTypedBoundaryStillChecksBytes(t *testing.T) {
	a := treeFromFixture(t, "traces/UIStreamSnapshot.native-presence.json")
	event, err := DecodeEvent([]byte(`{"protocol":1,"session":"session-a","epoch":"epoch-a","version":1,"event_id":"e","key":"row","action":"cb.switch","event":"change","value":false}`))
	if err != nil {
		t.Fatal(err)
	}
	values := make([]string, 64)
	for i := range values {
		values[i] = strings.Repeat("x", 4096)
	}
	event.Value = mustJSON(values)
	if _, err := a.EventAction(event); err == nil {
		t.Fatal("typed event bypassed 64 KiB bound")
	}
	event.Value = json.RawMessage(`{"x":1,"x":2}`)
	if _, err := a.EventAction(event); err == nil {
		t.Fatal("typed raw event value bypassed duplicate-member rejection")
	}
}

func TestDisabledControlCallbacks(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		for _, tile := range []bool{false, true} {
			t.Run(fmt.Sprintf("tile=%v/disabled=%v", tile, disabled), func(t *testing.T) {
				control := map[string]any{"kind": "switch", "name": "enabled", "disabled": disabled, "action": map[string]any{"id": "cb.change"}}
				item := map[string]any{"type": "row", "key": "row", "title": "Example", "control": control, "action": map[string]any{"id": "cb.press"}}
				key := "row"
				if tile {
					key = "tile"
					item = map[string]any{"type": "grid", "key": "grid", "tiles": []any{map[string]any{"key": key, "title": "Example", "control": control, "action": map[string]any{"id": "cb.press"}}}}
				}
				tree := snapshot(t, string(mustJSON(map[string]any{"sections": []any{map[string]any{"key": "main", "items": []any{item}}}})), 1)
				event, err := DecodeEvent([]byte(fmt.Sprintf(`{"protocol":1,"session":"session-a","epoch":"epoch-a","version":1,"event_id":"e","key":%q,"action":"cb.change","event":"change","value":false}`, key)))
				if err != nil {
					t.Fatal(err)
				}
				if _, err := tree.EventAction(event); (err != nil) != disabled {
					t.Fatalf("tile=%v disabled=%v: %v", tile, disabled, err)
				}
				// Only the control is disabled, not another action belonging to the same node.
				event.Action, event.Event = "cb.press", "press"
				if _, err := tree.EventAction(event); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestNativeLineLinksRejectCredentials(t *testing.T) {
	for _, link := range []string{"https://user:password@example.com/{start}", "https://user@example.com/{start}"} {
		raw := []byte(`{"protocol":1,"session":"s","epoch":"e","version":1,"root_key":"page","view":{"sections":[{"key":"main","items":[{"type":"code","key":"code","text":"","line_url":` + string(mustJSON(link)) + `}]}]}}`)
		if _, err := DecodeSnapshot(raw); !errors.Is(err, ErrSnapshotRequired) {
			t.Fatalf("accepted credential-bearing native line link: %v", err)
		}
	}
	for _, link := range []string{"", "https://example.com/file#L{start}-L{end}"} {
		snapshot(t, `{"sections":[{"key":"main","items":[{"type":"code","key":"code","text":"","line_url":`+string(mustJSON(link))+`}]}]}`, 1)
	}
}

func TestPublicDTOsAreDetachedProjections(t *testing.T) {
	tree := snapshot(t, `{"sections":[{"key":"main","items":[{"type":"code","key":"code","text":"","truncated":false}]}]}`, 1)
	var projection wire.UIStreamSnapshot = tree.Snapshot()
	if bytes.Contains(encoded(t, projection), []byte(`"truncated":false`)) {
		t.Fatal("the legacy DTO codec unexpectedly became a lossless store")
	}
	projection.View.Sections[0].Items[0].Truncated = true
	if !bytes.Contains(encoded(t, tree), []byte(`"truncated":false`)) {
		t.Fatal("snapshot projection mutated the canonical tree")
	}
	d := delta(t, `[{"op":"replace","key":"code","item":{"type":"code","key":"code","text":"","truncated":false}}]`)
	var frame wire.UIStreamDelta = d.Frame()
	if bytes.Contains(encoded(t, frame), []byte(`"truncated":false`)) {
		t.Fatal("the legacy delta DTO codec unexpectedly became a lossless store")
	}
	frame.Operations[0].Item.Truncated = true
	if !bytes.Contains(encoded(t, d), []byte(`"truncated":false`)) {
		t.Fatal("delta projection mutated the canonical operation")
	}
	result, err := Apply(tree, d)
	if err != nil || !bytes.Equal(result.ViewJSON(), tree.ViewJSON()) {
		t.Fatalf("lossless delta serialization/application drift: %v", err)
	}
}

func TestTileKeySwapDiffFallsBackAtomically(t *testing.T) {
	view := func(a, b string) string {
		return fmt.Sprintf(`{"sections":[{"key":"main","items":[{"type":"grid","key":"grid","tiles":[{"key":%q,"title":"Grid","active":false}]},{"type":"carousel","key":"carousel","tiles":[{"key":%q,"title":"Carousel","active":false}]}]}]}`, a, b)
	}
	from, to := snapshot(t, view("a", "b"), 1), snapshot(t, view("b", "a"), 2)
	before := encoded(t, from)
	d, recommend, err := Diff(from, to)
	if err != nil {
		t.Fatal(err)
	}
	ops := d.Frame().Operations
	if !recommend || len(ops) != 1 || ops[0].View == nil {
		t.Fatal("transient duplicate tile keys should trigger root fallback")
	}
	result, err := Apply(from, d)
	if err != nil {
		t.Fatal(err)
	}
	assertSame(t, result, to)
	if !bytes.Equal(before, encoded(t, from)) {
		t.Fatal("tile-key diff mutated its input")
	}
}

func TestCallbacksStayOnTheirLogicalNode(t *testing.T) {
	tree := snapshot(t, `{"search":{"action":{"id":"cb.search"}},"sections":[{"key":"main","more":{"action":{"id":"cb.more"}},"items":[{"type":"grid","key":"grid","tiles":[{"key":"tile","title":"Example","action":{"id":"cb.tile"}}]}]}]}`, 1)
	event, err := DecodeEvent([]byte(`{"protocol":1,"session":"session-a","epoch":"epoch-a","version":1,"event_id":"e","key":"page","action":"cb.search","event":"change","value":""}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, example := range []struct{ key, action, slot string }{{"page", "cb.search", "change"}, {"main", "cb.more", "press"}, {"tile", "cb.tile", "press"}} {
		event.Key, event.Action, event.Event = example.key, example.action, example.slot
		if _, err := tree.EventAction(event); err != nil {
			t.Fatal(err)
		}
	}
	for _, key := range []string{"page", "main", "grid"} {
		event.Key = key
		if _, err := tree.EventAction(event); err == nil {
			t.Fatalf("ancestor %s resolved its tile's callback", key)
		}
	}
}

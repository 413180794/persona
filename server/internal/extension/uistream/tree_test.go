package uistream

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nwverse/persona/sdk/go/wire"
)

func fixture(t testing.TB, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "schema", "extension", "examples", name))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func snapshot(t testing.TB, view string, version int64) *Tree {
	t.Helper()
	raw := []byte(fmt.Sprintf(`{"protocol":1,"session":"session-a","epoch":"epoch-a","version":%d,"root_key":"page","view":%s}`, version, view))
	tree, err := DecodeSnapshot(raw)
	if err != nil {
		t.Fatal(err)
	}
	return tree
}
func delta(t testing.TB, ops string) *Delta {
	t.Helper()
	d, err := DecodeDelta([]byte(`{"protocol":1,"session":"session-a","epoch":"epoch-a","base_version":1,"version":2,"operations":` + ops + `}`))
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func treeFromFixture(t testing.TB, name string) *Tree {
	t.Helper()
	tree, err := DecodeSnapshot(fixture(t, name))
	if err != nil {
		t.Fatal(err)
	}
	return tree
}
func encoded(t testing.TB, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func assertSame(t testing.TB, a, b *Tree) {
	t.Helper()
	if !bytes.Equal(encoded(t, a), encoded(t, b)) {
		t.Fatalf("trees differ\n%s\n%s", encoded(t, a), encoded(t, b))
	}
}

func TestSharedTrace(t *testing.T) {
	a := treeFromFixture(t, "UIStreamSnapshot.keyed.json")
	d, err := DecodeDelta(fixture(t, "UIStreamDelta.operations.json"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := Apply(a, d)
	if err != nil {
		t.Fatal(err)
	}
	assertSame(t, b, treeFromFixture(t, "UIStreamSnapshot.after-operations.json"))
	if a.Version() != 1 || !bytes.Contains(a.ViewJSON(), []byte(`"title":"Example"`)) {
		t.Fatal("source mutated")
	}
}

func TestPresenceThroughEveryBoundary(t *testing.T) {
	for _, name := range []string{"UIStreamSnapshot.required-empty.json", "traces/UIStreamSnapshot.native-presence.json"} {
		t.Run(name, func(t *testing.T) {
			raw := fixture(t, name)
			a := treeFromFixture(t, name)
			if !bytes.Equal(encoded(t, a), canonical(raw, false)) {
				t.Fatalf("source presence lost\n%s", encoded(t, a))
			}
			again, err := DecodeSnapshot(encoded(t, a))
			if err != nil {
				t.Fatal(err)
			}
			assertSame(t, a, again)
			view := a.ViewJSON()
			replacement := operation("replace", "key", a.RootKey(), "view", view)
			d := delta(t, string(mustJSON([]any{replacement})))
			b, err := Apply(a, d)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(view, b.ViewJSON()) {
				t.Fatal("root replacement lost presence")
			}
			// Same payloads through item and section replacements, insert and move.
			for _, s := range a.root.children {
				for _, item := range s.children {
					changed := parseNode(item.json(), "item", "")
					changed.key = "inserted"
					changed.props = mapCopy(changed.props)
					changed.props["key"] = mustJSON("inserted")
					// Tiles share the same namespace: don't duplicate a grid's tile keys.
					for _, tile := range changed.children {
						tile.key = "inserted-" + tile.key
						tile.props = mapCopy(tile.props)
						tile.props["key"] = mustJSON(tile.key)
					}
					ops := []map[string]json.RawMessage{
						operation("replace", "key", item.key, "item", item.json()),
						operation("insert", "parent", s.key, "item", changed.json()),
						operation("move", "key", changed.key, "parent", s.key, "before", item.key),
						operation("remove", "key", item.key),
					}
					// Duplicate callback IDs are temporarily possible, but final unique IDs remain.
					result, err := Apply(a, delta(t, string(mustJSON(ops))))
					if err != nil {
						t.Fatal(err)
					}
					got, _, _ := result.root.find(changed.key)
					if !bytes.Equal(got.json(), changed.json()) {
						t.Fatal("insert/move changed source properties")
					}
				}
			}
			target := &Tree{header: a.header, root: a.root.clone()}
			target.header.Version = 2
			s := target.root.children[0]
			s.children = append(s.children[1:], s.children[0])
			d, _, err = Diff(a, target)
			if err != nil {
				t.Fatal(err)
			}
			b, err = Apply(a, d)
			if err != nil {
				t.Fatal(err)
			}
			assertSame(t, b, target)
			d2, err := DecodeDelta(encoded(t, d))
			if err != nil {
				t.Fatal(err)
			}
			b, err = Apply(a, d2)
			if err != nil {
				t.Fatal(err)
			}
			assertSame(t, b, target)
		})
	}
}
func mapCopy(in map[string]json.RawMessage) map[string]json.RawMessage {
	out := map[string]json.RawMessage{}
	for k, v := range in {
		out[k] = v
	}
	return out
}

func TestRawRejectsBeforeNormalization(t *testing.T) {
	bad := []string{
		`null`, `{}`, `{"protocol":1,"protocol":1}`, `{"protocol":1} {}`,
		`{"protocol":1,"session":"s","epoch":"e","version":9007199254740990.9,"root_key":"page","view":{"sections":[]}}`,
	}
	for _, raw := range bad {
		if _, err := DecodeSnapshot([]byte(raw)); !errors.Is(err, ErrSnapshotRequired) {
			t.Fatalf("accepted %s: %v", raw, err)
		}
	}
	for _, ops := range []string{
		`null`, `[null]`, `[{}]`, `[4]`, `[{"op":"remove","key":null}]`, `[{"op":"remove","key":1}]`,
		`[{"op":"move","key":"a","parent":"b","before":false}]`,
		`[{"op":"remove","key":"a","key":"b"}]`,
		`[{"op":"remove","key":"a","item":{"type":"divider","key":"a"}}]`,
		`[{"op":"replace","key":"a","item":null}]`,
		`[{"op":"replace","key":"a","item":{"type":"divider","key":"a","text":"hidden"}}]`,
		`[{"op":"replace","key":"a","item":{"type":"row","key":"a","title":"A","control":{"kind":"switch","name":"x","on":null,"action":{"id":"cb"}}}}]`,
	} {
		raw := []byte(`{"protocol":1,"session":"session-a","epoch":"epoch-a","base_version":1,"version":2,"operations":` + ops + `}`)
		if _, err := DecodeDelta(raw); !errors.Is(err, ErrSnapshotRequired) {
			t.Fatalf("accepted %s: %v", ops, err)
		}
	}
	for _, view := range []string{
		`{"sections":[{"key":"s","items":[{"key":"a","type":"html","html":"x"}]}]}`,
		`{"sections":[{"key":"s","items":[{"key":"a","type":"row","title":"A","control":{"kind":"switch","name":"x","action":{"id":"cb","script":"hidden"}}}]}]}`,
		`{"sections":[{"key":"s","items":[{"key":"a","type":"grid","tiles":[{"title":"A"}]}]}]}`,
		`{"sections":[{"key":"s","items":[{"key":"page","type":"divider"}]}]}`,
		`{"sections":[{"key":"s","items":[{"key":"s","type":"divider"}]}]}`,
		`{"sections":[{"key":"s","items":[{"key":"g","type":"grid","tiles":[{"key":"s","title":"A"}]}]}]}`,
		`{"sections":[{"key":"s","items":[{"key":"a","type":"image","alt":"A","src":{"ref":"a","path":"/api/v1/extensions/demo/-/image/a"}}]}]}`,
		`{"sections":[{"key":"s","items":[{"key":"a","type":"field","kind":"secret","name":"x","label":"A","value":"secret"}]}]}`,
		`{"sections":[{"key":"s","items":[{"key":"a","type":"field","kind":"text","name":"x","label":"A"},{"key":"b","type":"field","kind":"text","name":"x","label":"B"}]}]}`,
		`{"sections":[{"key":"s","items":[{"key":"a","type":"button","title":"A","action":{"id":"same"}},{"key":"b","type":"button","title":"B","action":{"id":"same"}}]}]}`,
		`{"sections":[{"key":"s","items":[{"key":"a","type":"table","columns":[{"title":"A"}],"rows":[[]]}]}]}`,
	} {
		raw := []byte(`{"protocol":1,"session":"session-a","epoch":"epoch-a","version":1,"root_key":"page","view":` + view + `}`)
		if _, err := DecodeSnapshot(raw); !errors.Is(err, ErrSnapshotRequired) {
			t.Fatalf("accepted %s: %v", view, err)
		}
	}
}

func TestBatchRollback(t *testing.T) {
	a := treeFromFixture(t, "traces/UIStreamSnapshot.native-presence.json")
	before := encoded(t, a)
	bad := []string{
		`{"op":"remove","key":"page"}`,
		`{"op":"remove","key":"tile"}`,
		`{"op":"move","key":"tile","parent":"other"}`,
		`{"op":"move","key":"row","parent":"carousel"}`,
		`{"op":"move","key":"row","parent":"missing"}`,
		`{"op":"move","key":"main","parent":"row"}`,
		`{"op":"move","key":"main","parent":"main"}`,
		`{"op":"move","key":"row","parent":"other","before":"diff"}`,
		`{"op":"move","key":"row","parent":"main","before":"row"}`,
		`{"op":"replace","key":"tile","item":{"type":"divider","key":"tile"}}`,
		`{"op":"replace","key":"row","section":{"key":"row","items":[]}}`,
		`{"op":"replace","key":"row","item":{"type":"divider","key":"wrong"}}`,
		`{"op":"insert","parent":"main","item":{"type":"divider","key":"diff"}}`,
		`{"op":"insert","parent":"page","item":{"type":"divider","key":"new"}}`,
		`{"op":"insert","parent":"main","section":{"key":"new","items":[]}}`,
		`{"op":"replace","key":"field","item":{"type":"field","key":"field","name":"switch-value","kind":"text","label":"Collision"}}`,
	}
	for _, op := range bad {
		t.Run(op, func(t *testing.T) {
			d := delta(t, `[{"op":"replace","key":"code","item":{"type":"code","key":"code","text":"staged"}},`+op+`]`)
			dBefore := encoded(t, d)
			if _, err := Apply(a, d); !errors.Is(err, ErrSnapshotRequired) {
				t.Fatalf("accepted %s: %v", op, err)
			}
			if !bytes.Equal(before, encoded(t, a)) || !bytes.Equal(dBefore, encoded(t, d)) {
				t.Fatal("failed batch mutated an input")
			}
		})
	}
}

func TestVersionIdentityAndNoop(t *testing.T) {
	a := treeFromFixture(t, "UIStreamSnapshot.keyed.json")
	d := delta(t, `[{"op":"replace","key":"counter","item":{"type":"progress","key":"counter","label":"Count","ratio":0}}]`)
	for _, mutation := range []func(*wire.UIStreamDelta){
		func(d *wire.UIStreamDelta) { d.Session = "other" }, func(d *wire.UIStreamDelta) { d.Epoch = "old" },
		func(d *wire.UIStreamDelta) { d.BaseVersion = 2 }, func(d *wire.UIStreamDelta) { d.Version = 1 }, func(d *wire.UIStreamDelta) { d.Version = 3 },
	} {
		frame := d.Frame()
		mutation(&frame)
		parsed, err := DecodeDelta(encoded(t, frame))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Apply(a, parsed); !errors.Is(err, ErrSnapshotRequired) {
			t.Fatal("accepted stale/gapped delta")
		}
	}
	empty, err := DecodeDelta([]byte(`{"protocol":1,"session":"session-a","epoch":"epoch-a","base_version":1,"version":1,"operations":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	same, err := Apply(a, empty)
	if err != nil {
		t.Fatal(err)
	}
	assertSame(t, a, same)
	noDiff, _, err := Diff(a, a)
	if err != nil || len(noDiff.Frame().Operations) != 0 {
		t.Fatal("identical trees should produce same-version empty delta")
	}
	b, err := Apply(a, d)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(b, d); !errors.Is(err, ErrSnapshotRequired) {
		t.Fatal("accepted replay")
	}
	for _, raw := range [][]byte{encoded(t, a), bytes.Replace(encoded(t, b), []byte(`"epoch-a"`), []byte(`"old"`), 1), bytes.Replace(encoded(t, b), []byte(`"session-a"`), []byte(`"other"`), 1)} {
		if _, err := b.AcceptSnapshot(raw); !errors.Is(err, ErrSnapshotRequired) {
			t.Fatal("accepted stale/foreign snapshot")
		}
	}
	if _, err := b.AcceptSnapshot(encoded(t, b)); err != nil {
		t.Fatal(err)
	}
	conflict := bytes.Replace(encoded(t, b), []byte(`"Count"`), []byte(`"Changed"`), 1)
	if _, err := b.AcceptSnapshot(conflict); !errors.Is(err, ErrSnapshotRequired) {
		t.Fatal("accepted same-version conflict")
	}
	maxTree := snapshot(t, `{"sections":[]}`, VersionMost)
	frame := wire.UIStreamDelta{Protocol: 1, Session: a.Session(), Epoch: a.Epoch(), BaseVersion: VersionMost, Version: VersionMost, Operations: []wire.UIStreamOperation{{Op: "replace", Key: "page", View: &wire.View{Sections: []wire.Section{}}}}}
	exhausted, err := DecodeDelta(encoded(t, frame))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(maxTree, exhausted); !errors.Is(err, ErrSnapshotRequired) {
		t.Fatal("version overflow accepted")
	}
}

func TestBoundsAndIntegralLiteralPresence(t *testing.T) {
	for _, count := range []int{298, 299} {
		items := make([]string, count)
		for i := range items {
			items[i] = fmt.Sprintf(`{"type":"divider","key":"n%d"}`, i)
		}
		raw := []byte(`{"protocol":1,"session":"s","epoch":"e","version":1,"root_key":"page","view":{"sections":[{"key":"s","items":[` + strings.Join(items, ",") + `]}]}}`)
		_, err := DecodeSnapshot(raw)
		if (err == nil) != (count == 298) {
			t.Fatalf("global node count %d: %v", count, err)
		}
	}
	raw := fixture(t, "traces/UIStreamSnapshot.native-presence.json")
	raw = bytes.Replace(raw, []byte(`"version": 1`), []byte(`"version": 1e0`), 1)
	raw = bytes.Replace(raw, []byte(`"refresh_after": 0`), []byte(`"refresh_after": 0.0`), 1)
	tree, err := DecodeSnapshot(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(tree.ViewJSON(), []byte(`"refresh_after":0.0`)) {
		t.Fatal("native integer lexical presence lost")
	}
	if _, err := DecodeSnapshot(bytes.Repeat([]byte(" "), FrameMost+1)); err == nil {
		t.Fatal("oversized frame accepted")
	}
	if _, err := DecodeRequest(bytes.Repeat([]byte(" "), RequestMost+1)); err == nil {
		t.Fatal("oversized request accepted")
	}
	if _, err := DecodeEvent(bytes.Repeat([]byte(" "), RequestMost+1)); err == nil {
		t.Fatal("oversized event accepted")
	}
	if _, err := DecodeSnapshot([]byte{0xff}); err == nil {
		t.Fatal("invalid UTF-8 accepted")
	}
	for _, depth := range []int{DepthMost, DepthMost + 1} {
		raw := []byte(strings.Repeat("[", depth) + "0" + strings.Repeat("]", depth))
		dec := json.NewDecoder(bytes.NewReader(raw))
		err := scanValue(dec, 0)
		if (err == nil) != (depth == DepthMost) {
			t.Fatalf("depth %d: %v", depth, err)
		}
	}
	for _, count := range []int{256, 257} {
		ops := make([]string, count)
		for i := range ops {
			ops[i] = `{"op":"remove","key":"n"}`
		}
		raw := []byte(`{"protocol":1,"session":"s","epoch":"e","version":2,"base_version":1,"operations":[` + strings.Join(ops, ",") + `]}`)
		_, err := DecodeDelta(raw)
		if (err == nil) != (count == 256) {
			t.Fatalf("operations %d: %v", count, err)
		}
	}
	state := strings.Repeat("界", 6000)
	request := wire.UIStreamRequest{Request: "open", Protocol: 1, RequestID: "request", Extension: "demo", Contribution: "page", Context: &wire.UIContext{}, State: state}
	if _, err := DecodeRequest(encoded(t, request)); err == nil {
		t.Fatal("state UTF-8 byte bound not checked")
	}
}

func TestStableKeyDiffAndFallback(t *testing.T) {
	a := snapshot(t, `{"sections":[{"key":"s1","items":[{"type":"button","key":"a","title":"A","action":{"id":"cb.a"}},{"type":"button","key":"b","title":"B","action":{"id":"cb.b"}}]},{"key":"s2","items":[{"type":"code","key":"code","text":""}]}]}`, 1)
	b := snapshot(t, `{"sections":[{"key":"s2","items":[{"type":"button","key":"b","title":"B","action":{"id":"cb.b"}},{"type":"code","key":"code","text":""}]},{"key":"s1","items":[{"type":"button","key":"a","title":"A","action":{"id":"cb.a"}}]}]}`, 2)
	d, _, err := Diff(a, b)
	if err != nil {
		t.Fatal(err)
	}
	for _, op := range d.Frame().Operations {
		if op.Op != "move" {
			t.Fatalf("unchanged callback-bearing payload replaced: %+v", op)
		}
	}
	result, err := Apply(a, d)
	if err != nil {
		t.Fatal(err)
	}
	assertSame(t, result, b)
	// A section metadata replacement retains source items and their callback IDs.
	meta := snapshot(t, `{"sections":[{"key":"s2","title":"Changed","items":[{"type":"button","key":"b","title":"B","action":{"id":"cb.b"}},{"type":"code","key":"code","text":""}]},{"key":"s1","items":[{"type":"button","key":"a","title":"A","action":{"id":"cb.a"}}]}]}`, 3)
	d, _, err = Diff(b, meta)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Frame().Operations) != 1 || d.Frame().Operations[0].Section == nil {
		t.Fatal("section metadata did not use section replacement")
	}
	// Root metadata clears through a complete View replacement.
	rootMeta := snapshot(t, `{"title":"New","sections":[]}`, 2)
	d, recommend, err := Diff(a, rootMeta)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Frame().Operations) != 1 || d.Frame().Operations[0].View == nil || !recommend {
		t.Fatal("root replacement/fallback recommendation missing")
	}
	gridA := snapshot(t, `{"sections":[{"key":"s","items":[{"type":"grid","key":"g","tiles":[{"key":"t","title":"A","active":true}]}]}]}`, 1)
	gridB := snapshot(t, `{"sections":[{"key":"s","items":[{"type":"grid","key":"g","tiles":[{"key":"t","title":"A","active":false}]}]}]}`, 2)
	d, _, err = Diff(gridA, gridB)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Frame().Operations) != 1 || d.Frame().Operations[0].Key != "g" {
		t.Fatal("grid change should replace parent")
	}
}

func TestAccessorsDoNotAlias(t *testing.T) {
	a := treeFromFixture(t, "traces/UIStreamSnapshot.native-presence.json")
	original := encoded(t, a)
	view := a.ViewJSON()
	view[0] = 'x'
	frame := a.Snapshot()
	frame.View.Sections[0].Items[5].Control.Action.ID = "mutated"
	d := delta(t, `[{"op":"remove","key":"code"}]`)
	before := encoded(t, d)
	projected := d.Frame()
	projected.Operations[0].Key = "mutated"
	if !bytes.Equal(original, encoded(t, a)) || !bytes.Equal(before, encoded(t, d)) {
		t.Fatal("accessor leaked mutable state")
	}
}

func TestEventSlotsAndStaleness(t *testing.T) {
	a := treeFromFixture(t, "traces/UIStreamSnapshot.native-presence.json")
	for _, example := range []struct{ key, id, slot string }{{"page", "cb.search", "change"}, {"row", "cb.switch", "change"}, {"composer", "cb.send", "submit"}, {"tile", "cb.tile", "press"}} {
		e := wire.UIStreamEvent{Protocol: 1, Session: a.Session(), Epoch: a.Epoch(), Version: a.Version(), EventID: "event", Key: example.key, Action: example.id, Event: example.slot, Value: json.RawMessage(`false`)}
		raw := encoded(t, e)
		decoded, err := DecodeEvent(raw)
		if err != nil {
			t.Fatal(err)
		}
		action, err := a.EventAction(decoded)
		if err != nil || action.ID != e.Action {
			t.Fatalf("event slot failed: %v", err)
		}
		action.ID = "mutated"
		if _, err := a.EventAction(e); err != nil {
			t.Fatal("action projection aliased")
		}
		for _, mutate := range []func(*wire.UIStreamEvent){func(e *wire.UIStreamEvent) { e.Version++ }, func(e *wire.UIStreamEvent) { e.Epoch = "old" }, func(e *wire.UIStreamEvent) { e.Session = "other" }, func(e *wire.UIStreamEvent) { e.Key = "other" }, func(e *wire.UIStreamEvent) { e.Action = "missing" }, func(e *wire.UIStreamEvent) {
			if e.Event == "press" {
				e.Event = "change"
			} else {
				e.Event = "press"
			}
		}} {
			changed := e
			mutate(&changed)
			if _, err := a.EventAction(changed); err == nil {
				t.Fatalf("accepted invalid event: %+v", changed)
			}
		}
	}
}

// Generate bounded keyed trees with adds/removes, cross-section moves, section
// reorders, payload changes and present/absent false/empty fields.
func randomTree(t testing.TB, seed int64, version int64) *Tree {
	t.Helper()
	r := rand.New(rand.NewSource(seed))
	sectionKeys := r.Perm(5)
	sections := make([]map[string]any, 1+r.Intn(4))
	for i := range sections {
		sections[i] = map[string]any{"key": fmt.Sprintf("s%d", sectionKeys[i]), "items": []any{}}
		if r.Intn(2) == 0 {
			sections[i]["title"] = ""
		}
	}
	for _, key := range r.Perm(20) {
		if r.Intn(4) == 0 {
			continue
		}
		item := map[string]any{"type": "diff", "key": fmt.Sprintf("n%d", key), "path": "example.go", "hunks": []any{}, "threads": []any{}}
		if r.Intn(2) == 0 {
			item["collapsed"] = false
		}
		if r.Intn(2) == 0 {
			item["additions"] = 0
		}
		if r.Intn(2) == 0 {
			item["old_path"] = ""
		}
		if r.Intn(2) == 0 {
			item = map[string]any{"type": "code", "key": fmt.Sprintf("n%d", key), "text": ""}
			if r.Intn(2) == 0 {
				item["truncated"] = false
			}
		}
		s := sections[r.Intn(len(sections))]
		s["items"] = append(s["items"].([]any), item)
	}
	r.Shuffle(len(sections), func(i, j int) { sections[i], sections[j] = sections[j], sections[i] })
	view := map[string]any{"sections": sections}
	if seed%5 == 0 {
		view["title"] = ""
	}
	return snapshot(t, string(mustJSON(view)), version)
}
func TestDiffPropertyDeterministic(t *testing.T) {
	for seed := int64(0); seed < 200; seed++ {
		a, b := randomTree(t, seed, 1), randomTree(t, seed+211, 2)
		d, _, err := Diff(a, b)
		if err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
		result, err := Apply(a, d)
		if err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
		assertSame(t, result, b)
		again, _, err := Diff(a, b)
		if err != nil || !bytes.Equal(encoded(t, d), encoded(t, again)) {
			t.Fatalf("nondeterministic diff seed %d", seed)
		}
	}
}
func FuzzDiffRoundTrip(f *testing.F) {
	for _, seed := range []int64{0, 1, 11, 42, 123, 9223372036854775807} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, seed int64) {
		a, b := randomTree(t, seed, 1), randomTree(t, seed^211, 2)
		if bytes.Equal(a.ViewJSON(), b.ViewJSON()) {
			b.header.Version = 1
		}
		d, _, err := Diff(a, b)
		if err != nil {
			t.Fatal(err)
		}
		result, err := Apply(a, d)
		if err != nil {
			t.Fatal(err)
		}
		assertSame(t, result, b)
	})
}
func FuzzMalformedAtomicity(f *testing.F) {
	f.Add([]byte(`[{"op":"remove","key":"code"}]`))
	f.Add([]byte(`[null]`))
	f.Add([]byte(`[{"op":"move","key":"row","parent":"other"}]`))
	f.Add([]byte(`[{"op":"replace","key":"code","item":{"type":"code","key":"code","text":"staged"}},{"op":"remove","key":"tile"}]`))
	f.Fuzz(func(t *testing.T, ops []byte) {
		if len(ops) > FrameMost {
			return
		}
		a := treeFromFixture(t, "traces/UIStreamSnapshot.native-presence.json")
		before := encoded(t, a)
		raw := append([]byte(`{"protocol":1,"session":"session-a","epoch":"epoch-a","base_version":1,"version":2,"operations":`), ops...)
		raw = append(raw, '}')
		d, err := DecodeDelta(raw)
		if err == nil {
			dBefore := encoded(t, d)
			result, err := Apply(a, d)
			if err == nil {
				if _, err := DecodeSnapshot(encoded(t, result)); err != nil {
					t.Fatal(err)
				}
			}
			if !bytes.Equal(dBefore, encoded(t, d)) {
				t.Fatal("delta mutated")
			}
		}
		if !bytes.Equal(before, encoded(t, a)) {
			t.Fatal("source mutated on malformed/valid batch")
		}
	})
}
func FuzzSnapshotPresence(f *testing.F) {
	f.Add(fixture(f, "traces/UIStreamSnapshot.native-presence.json"))
	f.Add(fixture(f, "UIStreamSnapshot.required-empty.json"))
	f.Fuzz(func(t *testing.T, raw []byte) {
		tree, err := DecodeSnapshot(raw)
		if err != nil {
			return
		}
		var obj map[string]json.RawMessage
		_ = json.Unmarshal(raw, &obj)
		if !bytes.Equal(tree.ViewJSON(), canonical(obj["view"], false)) {
			t.Fatal("native source presence lost")
		}
		again, err := DecodeSnapshot(encoded(t, tree))
		if err != nil {
			t.Fatal(err)
		}
		assertSame(t, tree, again)
	})
}

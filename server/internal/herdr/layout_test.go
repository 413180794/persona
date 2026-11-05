package herdr

import (
	"context"
	"reflect"
	"testing"
)

func pane(id string, x, y, w, h int) LayoutPane {
	return LayoutPane{PaneID: id, Rect: Rect{X: x, Y: y, Width: w, Height: h}}
}

func split(id, dir string, x, y, w, h int) LayoutSplit {
	return LayoutSplit{ID: id, Direction: dir, Ratio: 0.5, Rect: Rect{X: x, Y: y, Width: w, Height: h}}
}

func TestTabLayoutOrder(t *testing.T) {
	cases := []struct {
		name  string
		l     TabLayout
		order []string
	}{
		{"single pane", TabLayout{Panes: []LayoutPane{pane("w1:pA", 0, 0, 185, 55)}}, []string{"w1:pA"}},
		{"left / right", TabLayout{Panes: []LayoutPane{pane("w1:pR", 93, 0, 92, 55), pane("w1:pL", 0, 0, 93, 55)}},
			[]string{"w1:pL", "w1:pR"}},
		{"top / bottom", TabLayout{Panes: []LayoutPane{pane("w1:pB", 0, 28, 185, 27), pane("w1:pT", 0, 0, 185, 28)}},
			[]string{"w1:pT", "w1:pB"}},
		// Left, then the right half split down (a real snapshot's shape).
		{"nested right then down", TabLayout{Panes: []LayoutPane{
			pane("w4R:p31", 93, 28, 92, 27), pane("w4R:p2Q", 93, 0, 92, 28), pane("w4R:p2E", 0, 0, 93, 55)}},
			[]string{"w4R:p2E", "w4R:p2Q", "w4R:p31"}},
		// Split down first, then the top split right: the top row before the bottom, though the bottom starts at x 0.
		{"nested down then right", TabLayout{Panes: []LayoutPane{
			pane("w1:pB", 0, 28, 185, 27), pane("w1:pC", 93, 0, 92, 28), pane("w1:pA", 0, 0, 93, 28)}},
			[]string{"w1:pA", "w1:pC", "w1:pB"}},
		// Left half split down: the left column before the right one.
		{"left column split", TabLayout{Panes: []LayoutPane{
			pane("w1:pR", 93, 0, 92, 55), pane("w1:pLB", 0, 28, 93, 27), pane("w1:pLT", 0, 0, 93, 28)}},
			[]string{"w1:pLT", "w1:pLB", "w1:pR"}},
		// herdr keeps every pane's rect while one is zoomed.
		{"zoomed", TabLayout{Zoomed: true, Panes: []LayoutPane{pane("w1:pR", 93, 0, 92, 55), pane("w1:pL", 0, 0, 93, 55)}},
			[]string{"w1:pL", "w1:pR"}},
		// A split down (A / C), then A split right (A | B) and C split right (C | D): the split tree says the top
		// row comes first, which the rects alone cannot (both cuts are clean at x 93 and y 28).
		{"2×2 split down first", TabLayout{
			Panes: []LayoutPane{pane("w1:pD", 93, 28, 92, 27), pane("w1:pC", 0, 28, 93, 27), pane("w1:pB", 93, 0, 92, 28),
				pane("w1:pA", 0, 0, 93, 28)},
			Splits: []LayoutSplit{split("split_0_root", "down", 0, 0, 185, 55), split("split_1_0", "right", 0, 0, 185, 28),
				split("split_1_1", "right", 0, 28, 185, 27)}},
			[]string{"w1:pA", "w1:pB", "w1:pC", "w1:pD"}},
		// The same grid split right first (A | B, then each down): the left column first.
		{"2×2 split right first", TabLayout{
			Panes: []LayoutPane{pane("w1:pD", 93, 28, 92, 27), pane("w1:pC", 0, 28, 93, 27), pane("w1:pB", 93, 0, 92, 28),
				pane("w1:pA", 0, 0, 93, 28)},
			Splits: []LayoutSplit{split("split_0_root", "right", 0, 0, 185, 55), split("split_1_0", "down", 0, 0, 93, 55),
				split("split_1_1", "down", 93, 0, 92, 55)}},
			[]string{"w1:pA", "w1:pC", "w1:pB", "w1:pD"}},
		// An older herdr sends no split nodes: the rects alone, a left / right cut first.
		{"2×2 without splits", TabLayout{Panes: []LayoutPane{pane("w1:pD", 93, 28, 92, 27), pane("w1:pC", 0, 28, 93, 27),
			pane("w1:pB", 93, 0, 92, 28), pane("w1:pA", 0, 0, 93, 28)}},
			[]string{"w1:pA", "w1:pC", "w1:pB", "w1:pD"}},
		// A real snapshot's layout, split nodes and all: left, then the right half split down.
		{"real snapshot with splits", TabLayout{
			Panes:  []LayoutPane{pane("w4R:p2E", 0, 0, 93, 55), pane("w4R:p2Q", 93, 0, 92, 28), pane("w4R:p31", 93, 28, 92, 27)},
			Splits: []LayoutSplit{split("split_0_root", "right", 0, 0, 185, 55), split("split_1_1", "down", 93, 0, 92, 55)}},
			[]string{"w4R:p2E", "w4R:p2Q", "w4R:p31"}},
		// Three in a row (A | B, then B | C): two clean lines, the node's own is the one whose halves are nodes or panes.
		{"three in a row", TabLayout{
			Panes:  []LayoutPane{pane("w1:pC", 139, 0, 46, 55), pane("w1:pB", 93, 0, 46, 55), pane("w1:pA", 0, 0, 93, 55)},
			Splits: []LayoutSplit{split("split_0_root", "right", 0, 0, 185, 55), split("split_1_1", "right", 93, 0, 92, 55)}},
			[]string{"w1:pA", "w1:pB", "w1:pC"}},
		// Rects that overlap have no clean cut: herdr's own order.
		{"overlapping", TabLayout{Panes: []LayoutPane{pane("w1:pB", 0, 0, 185, 55), pane("w1:pA", 10, 10, 50, 20)}},
			[]string{"w1:pB", "w1:pA"}},
	}
	for _, c := range cases {
		if got := c.l.Order(); !reflect.DeepEqual(got, c.order) {
			t.Errorf("%s: order %v, want %v", c.name, got, c.order)
		}
	}
}

func TestPaneIndexes(t *testing.T) {
	got := PaneIndexes([]TabLayout{
		{TabID: "w1:t1", Panes: []LayoutPane{pane("w1:p2", 93, 0, 92, 55), pane("w1:p1", 0, 0, 93, 55)}},
		{TabID: "w1:t2", Panes: []LayoutPane{pane("w1:p3", 0, 0, 185, 55)}},
	})
	want := map[string]int{"w1:p1": 0, "w1:p2": 1, "w1:p3": 0}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("indexes %v, want %v", got, want)
	}
}

func TestSnapshotDecodesLayouts(t *testing.T) {
	sock := fakeServer(t, func(method string, _ map[string]any) string {
		return `"result":{"snapshot":{"agents":[],"layouts":[{"tab_id":"w1:t1","workspace_id":"w1","focused_pane_id":"w1:pW",` +
			`"zoomed":false,"panes":[{"pane_id":"w1:pW","focused":true,"rect":{"x":0,"y":0,"width":93,"height":55}},` +
			`{"pane_id":"w1:p2E","focused":false,"rect":{"x":93,"y":0,"width":92,"height":55}}],"splits":[{"direction":"right",` +
			`"id":"split_0_root","ratio":0.5,"rect":{"height":55,"width":185,"x":0,"y":0}}]}]}}`
	})
	snap, err := (&Client{Bin: "/nonexistent/herdr", Socket: sock}).Snapshot(context.Background())
	if err != nil || len(snap.Layouts) != 1 || snap.Layouts[0].TabID != "w1:t1" || len(snap.Layouts[0].Panes) != 2 ||
		snap.Layouts[0].Panes[1].Rect.X != 93 || len(snap.Layouts[0].Splits) != 1 ||
		snap.Layouts[0].Splits[0] != split("split_0_root", "right", 0, 0, 185, 55) {
		t.Fatalf("snapshot %+v, %v", snap, err)
	}
}

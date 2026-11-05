package herdr

import "sort"

// Rect is a pane's place in its tab, in terminal cells.
type Rect struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

// LayoutPane is one pane of a tab's layout.
type LayoutPane struct {
	PaneID  string `json:"pane_id"`
	Focused bool   `json:"focused"`
	Rect    Rect   `json:"rect"`
}

// LayoutSplit is one node of a tab's split tree: the region it covers and which way it was cut. herdr lists the
// nodes without their children (a real snapshot: {"id":"split_1_1","direction":"down","ratio":0.5,"rect":{…}}), so a
// node's two halves are found by rect: the panes inside its region, cut along its direction.
type LayoutSplit struct {
	ID string `json:"id"`
	// Direction is the way the region was split: "right" puts the new pane right of the old (a left / right cut),
	// "down" below it (a top / bottom cut).
	Direction string  `json:"direction"`
	Ratio     float64 `json:"ratio"`
	Rect      Rect    `json:"rect"`
}

// TabLayout is how a tab is split into panes, as the snapshot's layouts give it.
type TabLayout struct {
	TabID         string        `json:"tab_id"`
	WorkspaceID   string        `json:"workspace_id"`
	FocusedPaneID string        `json:"focused_pane_id"`
	Zoomed        bool          `json:"zoomed"`
	Panes         []LayoutPane  `json:"panes"`
	Splits        []LayoutSplit `json:"splits"`
}

// Order is the tab's panes by place in herdr's split tree, first half before second (left before right, top before
// bottom): the first pane is the one every other was split from. The tree's nodes say which way each region was cut,
// which the rects alone cannot when both cuts are clean (a 2×2 grid split down first: A, B on top, C, D below). When
// the snapshot has no split nodes (an older herdr) or a region has none, the cut is rebuilt from the rects: a line no
// pane straddles, a left / right cut tried before a top / bottom one. When no clean cut exists (rects that overlap, as
// a zoomed pane's may) the order is herdr's own list.
func (l TabLayout) Order() []string {
	panes := append([]LayoutPane(nil), l.Panes...)
	ordered, ok := order(panes, l.Splits)
	if !ok {
		ordered = panes
	}
	out := make([]string, 0, len(ordered))
	for _, p := range ordered {
		out = append(out, p.PaneID)
	}
	return out
}

// order orders panes by their split tree, falling back to the rects where the tree says nothing; false when some group
// of panes has no clean cut.
func order(panes []LayoutPane, splits []LayoutSplit) ([]LayoutPane, bool) {
	if len(panes) <= 1 {
		return panes, true
	}
	if vertical, ok := splitAt(bounds(panes), splits); ok {
		if first, second, ok := cutBy(panes, vertical, splits); ok {
			a, okA := order(first, splits)
			b, okB := order(second, splits)
			if okA && okB {
				return append(a, b...), true
			}
		}
	}
	return guillotine(panes, splits)
}

// splitAt is the direction of the split node covering exactly the region: vertical for a left / right cut.
func splitAt(region Rect, splits []LayoutSplit) (vertical, ok bool) {
	for _, s := range splits {
		if s.Rect != region {
			continue
		}
		switch s.Direction {
		case "right", "left", "vertical":
			return true, true
		case "down", "up", "horizontal":
			return false, true
		}
	}
	return false, false
}

// cutBy cuts panes along the split node's direction. Of the clean lines, the one whose halves are each a pane or another
// node's region is the node's own (a row split right twice has two clean lines); else the first.
func cutBy(panes []LayoutPane, vertical bool, splits []LayoutSplit) (first, second []LayoutPane, ok bool) {
	lines := cuts(panes, vertical)
	if len(lines) == 0 {
		return nil, nil, false
	}
	whole := func(side []LayoutPane) bool {
		if len(side) == 1 {
			return true
		}
		_, ok := splitAt(bounds(side), splits)
		return ok
	}
	for _, c := range lines {
		if whole(c[0]) && whole(c[1]) {
			return c[0], c[1], true
		}
	}
	return lines[0][0], lines[0][1], true
}

// bounds is the smallest rect holding every pane's.
func bounds(panes []LayoutPane) Rect {
	r := panes[0].Rect
	for _, p := range panes[1:] {
		x0, y0 := min(r.X, p.Rect.X), min(r.Y, p.Rect.Y)
		x1, y1 := max(r.X+r.Width, p.Rect.X+p.Rect.Width), max(r.Y+r.Height, p.Rect.Y+p.Rect.Height)
		r = Rect{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}
	}
	return r
}

// guillotine orders panes by a straight cut rebuilt from the rects (the first clean line, left / right before top /
// bottom), each side by order again; false when the panes have no clean cut.
func guillotine(panes []LayoutPane, splits []LayoutSplit) ([]LayoutPane, bool) {
	for _, vertical := range []bool{true, false} {
		lines := cuts(panes, vertical)
		if len(lines) == 0 {
			continue
		}
		a, okA := order(lines[0][0], splits)
		b, okB := order(lines[0][1], splits)
		if !okA || !okB {
			return nil, false
		}
		return append(a, b...), true
	}
	return nil, false
}

// cuts is every way to split panes along a line (vertical: an x, else a y) no pane straddles, with panes on both
// sides, first line first.
func cuts(panes []LayoutPane, vertical bool) [][2][]LayoutPane {
	start := func(r Rect) int {
		if vertical {
			return r.X
		}
		return r.Y
	}
	end := func(r Rect) int {
		if vertical {
			return r.X + r.Width
		}
		return r.Y + r.Height
	}
	var lines []int
	for _, p := range panes {
		lines = append(lines, start(p.Rect))
	}
	sort.Ints(lines)
	var out [][2][]LayoutPane
	for i, c := range lines {
		if i > 0 && c == lines[i-1] {
			continue
		}
		var first, second []LayoutPane
		clean := true
		for _, p := range panes {
			switch {
			case end(p.Rect) <= c:
				first = append(first, p)
			case start(p.Rect) >= c:
				second = append(second, p)
			default:
				clean = false
			}
			if !clean {
				break
			}
		}
		if clean && len(first) > 0 && len(second) > 0 {
			out = append(out, [2][]LayoutPane{first, second})
		}
	}
	return out
}

// PaneIndexes is every pane's 0-based place in its tab by Order, keyed by pane id.
func PaneIndexes(layouts []TabLayout) map[string]int {
	out := map[string]int{}
	for _, l := range layouts {
		for i, id := range l.Order() {
			out[id] = i
		}
	}
	return out
}

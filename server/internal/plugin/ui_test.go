package plugin

import (
	"slices"
	"testing"
)

// Fit leaves out what the asking app cannot draw (and a section left with nothing), raises refresh_after to its least,
// and holds a card to its size.
func TestFit(t *testing.T) {
	chart := UIElem{Type: "chart", Kind: "line", Series: []ChartSeries{{Points: []ChartPoint{{Label: "a", Value: 1}}}}}
	view := func() *View {
		return &View{RefreshAfter: 1, Sections: []Section{
			{Title: "图", Items: []UIElem{chart}},
			{Title: "数", Items: []UIElem{{Type: "metric", Label: "a", Value: "1"}, {Type: "hologram"}}}}}
	}
	page, _ := Slot("nav_page")
	v := view()
	dropped, err := Fit(v, page, 1)
	if err != nil || len(v.Sections) != 1 || v.Sections[0].Title != "数" || len(v.Sections[0].Items) != 1 || v.RefreshAfter != RefreshLeast ||
		!slices.Equal(dropped, []string{"chart", "hologram"}) {
		t.Fatalf("%v %+v %v", err, v, dropped)
	}
	v = view()
	if dropped, err := Fit(v, page, UIVersion); err != nil || len(v.Sections) != 2 || !slices.Equal(dropped, []string{"hologram"}) {
		t.Fatalf("%v %+v %v", err, v, dropped)
	}
	card, _ := Slot("overview_card")
	field := &View{Sections: []Section{{Items: []UIElem{{Type: "field", Name: "n", Label: "N", Kind: "text"}}}}}
	if _, err := Fit(field, card, UIVersion); err == nil {
		t.Fatal("a card has no fields")
	}
	if _, err := Fit(&View{Sections: make([]Section, CompactSections+1)}, card, UIVersion); err == nil {
		t.Fatal("a card has few sections")
	}
	if _, err := Fit(&View{RefreshAfter: 10 * RefreshMost}, page, UIVersion); err != nil {
		t.Fatal(err)
	}
}

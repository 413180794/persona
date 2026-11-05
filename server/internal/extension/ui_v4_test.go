package extension

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func codeExample(t *testing.T, name string) *View {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "schema", "extension", "examples", "showcase", name))
	if err != nil {
		t.Fatal(err)
	}
	v, err := ParseView(raw)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// An app of UI version 4 gets the code hosting components as they are.
func TestFitKeepsV4ForV4(t *testing.T) {
	v := codeExample(t, "code-review.json")
	page, _ := Slot("nav_page")
	dropped, err := Fit(v, page, 4)
	if err != nil || len(dropped) > 0 || v.Layout != "split" || v.Tabs == nil || v.Sections[1].Pane != "side" {
		t.Fatalf("err=%v dropped=%v view=%+v", err, dropped, v)
	}
}

// An older app gets the code hosting components drawn with what it knows: nothing of version 4 is left, the actions
// lose their values, and the view still holds.
func TestFitDrawsV4ForOlderApps(t *testing.T) {
	page, _ := Slot("nav_page")
	for _, name := range []string{"code-review.json", "code-browse.json", "code-overview.json"} {
		for _, version := range []int{1, 2, 3} {
			v := codeExample(t, name)
			dropped, err := Fit(v, page, version)
			if err != nil {
				t.Fatalf("%s v%d: %v", name, version, err)
			}
			if v.Tabs != nil || v.Layout != "" {
				t.Errorf("%s v%d: tabs or layout left", name, version)
			}
			for _, s := range v.Sections {
				if s.Pane != "" {
					t.Errorf("%s v%d: a pane left", name, version)
				}
				for _, e := range s.Items {
					if elemSince[e.Type] > version {
						t.Errorf("%s v%d: %s left", name, version, e.Type)
					}
				}
			}
			for _, a := range Actions(v) {
				if a.Values != nil {
					t.Errorf("%s v%d: action %s keeps its values", name, version, a.ID)
				}
			}
			if len(dropped) > 0 && version >= 2 {
				t.Errorf("%s v%d: dropped %v", name, version, dropped)
			}
		}
	}
	v := codeExample(t, "code-review.json")
	if _, err := Fit(v, page, 3); err != nil {
		t.Fatal(err)
	}
	first := v.Sections[0].Items[0]
	if first.Type != "segmented" || first.Value != "files" || len(first.Choices) != 4 || first.Choices[1].Title != "改动 3" {
		t.Errorf("tabs as a segmented: %+v", first)
	}
	var texts []string
	for _, s := range v.Sections {
		for _, e := range s.Items {
			if e.Type == "text" && e.Style == "code" {
				texts = append(texts, e.Text)
			}
		}
	}
	if len(texts) == 0 || !strings.Contains(strings.Join(texts, "\n"), "+func Retry(attempts int, f func() error) error {") {
		t.Errorf("diff as code text: %q", texts)
	}
}

// An older app gets a long view cut to ItemsMost, saying so.
func TestFitCutsALongOldView(t *testing.T) {
	var entries []TreeEntry
	for i := range 40 {
		entries = append(entries, TreeEntry{Path: strings.Repeat("d/", i%5) + "f" + string(rune('a'+i%26))})
	}
	var items []UIElem
	for range 10 {
		items = append(items, UIElem{Type: "file_tree", Entries: entries})
	}
	v := &View{Sections: []Section{{Items: items}}}
	page, _ := Slot("nav_page")
	if _, err := Fit(v, page, 3); err != nil {
		t.Fatal(err)
	}
	n := len(v.Sections[0].Items)
	if n != ItemsMost || !strings.Contains(v.Sections[0].Items[n-1].Text, "新版 App") {
		t.Fatalf("%d items, last %+v", n, v.Sections[0].Items[n-1])
	}
}

// CheckView holds the new components to what the schema cannot say.
func TestCheckViewV4(t *testing.T) {
	bad := map[string]View{
		"tab value": {Tabs: &Tabs{Name: "tab", Value: "x", Items: []Tab{{Value: "a"}, {Value: "b"}}, Action: UIAction{ID: "t"}},
			Sections: []Section{}},
		"diff prefix": {Sections: []Section{{Items: []UIElem{{Type: "diff", Path: "a", Hunks: []Hunk{{Lines: []string{"oops"}}}}}}}},
		"comment image": {Sections: []Section{{Items: []UIElem{{Type: "comment_thread",
			Comments: []Comment{{Author: "a", Body: "![x](https://example.com/x.png)"}}}}}}},
		"composer twice": {Sections: []Section{{Items: []UIElem{
			{Type: "composer", Name: "body", Buttons: []ComposerButton{{Title: "评论", Action: UIAction{ID: "c"}}}},
			{Type: "field", Name: "body", Label: "b", Kind: "text"}}}}},
		"highlight": {Sections: []Section{{Items: []UIElem{{Type: "code", Text: "a", Highlight: &LineRange{Start: 3, End: 2}}}}}},
	}
	for name, v := range bad {
		if err := CheckView(&v); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	big := strings.Repeat("x", 199000)
	var items []UIElem
	for range 22 {
		items = append(items, UIElem{Type: "code", Text: big})
	}
	if err := CheckView(&View{Sections: []Section{{Items: items}}}); err == nil || !strings.Contains(err.Error(), "bytes of code") {
		t.Errorf("too much code: %v", err)
	}
	card, _ := Slot("overview_card")
	composer := &View{Sections: []Section{{Items: []UIElem{{Type: "composer", Name: "b", Buttons: []ComposerButton{{Title: "c", Action: UIAction{ID: "c"}}}}}}}}
	if _, err := Fit(composer, card, 4); err == nil {
		t.Error("a card took a composer")
	}
	if !slices.Contains(UIIcons, "pull_request") {
		t.Error("no pull_request icon")
	}
}

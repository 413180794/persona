package extension

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nwverse/persona/sdk/go/wire"
)

// A widget is held to what its family draws: no taps, search or more; few items, rows, stats and lines; a
// complication draws the first thing it can; a view with nothing a widget draws is refused.
func TestFitWidget(t *testing.T) {
	view := func() *View {
		search, more := aSearch, aMore
		return &View{RefreshAfter: 60, Search: &search, Sections: []Section{
			{Title: "今天", More: &more, Items: []UIElem{
				{Type: "button", Title: "刷新", Action: &UIAction{ID: "r"}},
				{Type: "metric", Label: "用量", Value: "1.2M"},
				{Type: "stat_grid", Stats: []Stat{{Label: "a", Value: "1"}, {Label: "b", Value: "2"}, {Label: "c", Value: "3"}, {Label: "d", Value: "4"}, {Label: "e", Value: "5"}}},
				{Type: "markdown", Text: "一\n二\n三\n四"},
				{Type: "row", Title: "1", Action: &UIAction{ID: "o"}}, {Type: "row", Title: "2"}, {Type: "row", Title: "3"},
				{Type: "row", Title: "4"}, {Type: "row", Title: "5"}}},
			{Title: "二", Items: []UIElem{{Type: "progress", Label: "p", Ratio: &half}}},
			{Title: "三", Items: []UIElem{{Type: "text", Text: "x"}}}}}
	}
	v := view()
	dropped, err := FitWidget(v, "large", UIVersion)
	if err != nil {
		t.Fatal(err)
	}
	if v.Search != nil || v.RefreshAfter != 0 || len(v.Sections) != 2 || v.Sections[0].More != nil {
		t.Fatalf("no search, more or refresh_after; at most two sections: %+v", v)
	}
	items := v.Sections[0].Items
	if items[0].Type != "metric" || len(items[1].Stats) != 4 || items[2].Type != "text" || items[2].Text != "一\n二\n三…" {
		t.Fatalf("the button left out, the stats cut to four, the markdown three lines of text: %+v", items)
	}
	rows := 0
	for _, e := range items {
		if e.Type == "row" {
			rows++
			if e.Action != nil {
				t.Fatal("nothing in a widget is tapped")
			}
		}
	}
	if rows != 4 || !strings.Contains(strings.Join(dropped, ","), "button") || !strings.Contains(strings.Join(dropped, ","), "search") {
		t.Fatalf("four rows; it says what it left out: %d %v", rows, dropped)
	}

	v = view()
	if _, err := FitWidget(v, "small", UIVersion); err != nil || len(v.Sections) != 1 || len(v.Sections[0].Items) != 4 || len(v.Sections[0].Items[1].Stats) != 2 {
		t.Fatalf("a small widget: four items, two stats: %v %+v", err, v.Sections)
	}
	v = view()
	if _, err := FitWidget(v, "accessory_circular", UIVersion); err != nil || len(v.Sections) != 1 || v.Sections[0].Items[0].Type != "metric" {
		t.Fatalf("a complication draws the first thing it can: %v %+v", err, v.Sections)
	}
	v = &View{Sections: []Section{{Items: []UIElem{{Type: "progress", Label: "p", Ratio: &half}, {Type: "text", Text: "a\nb"}}}}}
	if _, err := FitWidget(v, "accessory_inline", UIVersion); err != nil || v.Sections[0].Items[0].Text != "a…" {
		t.Fatalf("an inline complication draws no progress, one line of text: %v %+v", err, v.Sections)
	}
	if _, err := FitWidget(&View{Sections: []Section{{Items: []UIElem{{Type: "button", Title: "x", Action: &UIAction{ID: "x"}}}}}}, "medium", UIVersion); err == nil {
		t.Fatal("a widget with nothing a widget draws")
	}
	if _, err := FitWidget(view(), "huge", UIVersion); err == nil {
		t.Fatal("a family that is not one")
	}
}

var (
	aSearch = wire.Search{Action: UIAction{ID: "q"}}
	aMore   = wire.More{Action: UIAction{ID: "m"}}
)

// A widget says its families (and maybe how often it is drawn and the page it opens); nothing else has them. A
// shortcut's params are named once, of a known kind, choices only for a choice; the page it opens is the extension's.
func TestWidgetAndShortcutManifests(t *testing.T) {
	ui := func(items ...map[string]any) []byte {
		return with(t, func(m map[string]any) {
			m["contributes"].(map[string]any)["ui"] = append([]any{map[string]any{"id": "usage", "title": "用量", "placement": "nav_page"}}, anys(items)...)
		})
	}
	for _, c := range []struct {
		item map[string]any
		ok   bool
	}{
		{map[string]any{"id": "w", "title": "小组件", "placement": "widget", "families": []any{"small", "accessory_inline"}, "refresh_minutes": 60, "opens": "usage"}, true},
		{map[string]any{"id": "w", "title": "小组件", "placement": "widget"}, false},
		{map[string]any{"id": "w", "title": "小组件", "placement": "widget", "families": []any{"huge"}}, false},
		{map[string]any{"id": "w", "title": "小组件", "placement": "widget", "families": []any{"small", "small"}}, false},
		{map[string]any{"id": "w", "title": "小组件", "placement": "widget", "families": []any{"small"}, "refresh_minutes": 5}, false},
		{map[string]any{"id": "w", "title": "小组件", "placement": "widget", "families": []any{"small"}, "opens": "nope"}, false},
		{map[string]any{"id": "w", "title": "页", "placement": "nav_page", "families": []any{"small"}}, false},
	} {
		if _, err := Parse(ui(c.item)); (err == nil) != c.ok {
			t.Errorf("%v: %v", c.item, err)
		}
	}
	sc := func(s map[string]any) []byte {
		return with(t, func(m map[string]any) {
			m["contributes"].(map[string]any)["ui"] = []any{map[string]any{"id": "usage", "title": "用量", "placement": "nav_page"}}
			m["contributes"].(map[string]any)["shortcuts"] = []any{s}
		})
	}
	for _, c := range []struct {
		item map[string]any
		ok   bool
	}{
		{map[string]any{"id": "add", "title": "记一笔", "ui": "usage", "params": []any{map[string]any{"name": "note", "title": "内容", "kind": "text", "required": true},
			map[string]any{"name": "mode", "title": "方式", "kind": "choice", "choices": []any{map[string]any{"value": "a"}}}}}, true},
		{map[string]any{"id": "add", "title": "记一笔", "ui": "nope"}, false},
		{map[string]any{"id": "add", "title": "记一笔", "params": []any{map[string]any{"name": "x", "title": "x", "kind": "date"}}}, false},
		{map[string]any{"id": "add", "title": "记一笔", "params": []any{map[string]any{"name": "x", "title": "x", "kind": "choice"}}}, false},
		{map[string]any{"id": "add", "title": "记一笔", "params": []any{map[string]any{"name": "x", "title": "x", "kind": "text"}, map[string]any{"name": "x", "title": "y", "kind": "text"}}}, false},
		{map[string]any{"id": "add", "title": "", "params": []any{}}, false},
	} {
		m, err := Parse(sc(c.item))
		if (err == nil) != c.ok {
			t.Errorf("%v: %v", c.item, err)
		}
		if err == nil && !strings.Contains(strings.Join(m.Surfaces(), ","), SurfaceShortcuts) {
			t.Errorf("shortcuts are a surface the user is shown: %v", m.Surfaces())
		}
	}
	// An upgrade adding a widget or shortcuts asks again: they are surfaces in the digest.
	plain, _ := Parse([]byte(exampleManifest))
	withWidget, err := Parse(ui(map[string]any{"id": "w", "title": "小组件", "placement": "widget", "families": []any{"small"}}))
	if err != nil {
		t.Fatal(err)
	}
	if Digest(plain) == Digest(withWidget) {
		t.Fatal("a widget changes what the user agrees to")
	}
	raw, _ := json.Marshal(withWidget.UIs())
	if !strings.Contains(string(raw), `"families":["small"]`) {
		t.Fatalf("%s", raw)
	}
}

var half = 0.5

func anys(ms []map[string]any) []any {
	out := make([]any, len(ms))
	for i, m := range ms {
		out[i] = m
	}
	return out
}

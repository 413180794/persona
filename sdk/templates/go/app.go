package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/nwverse/persona/sdk/go/ext"
	"github.com/nwverse/persona/sdk/go/wire"
)

type app struct {
	name     string
	greeting string
	apiKey   string
}

type item struct{ ID, Title, Detail string }

// items are the sample data: your service's go here (read with a.apiKey).
func (a *app) items() []item {
	out := make([]item, 0, 20)
	for i := 1; i <= 20; i++ {
		out = append(out, item{ID: fmt.Sprint(i), Title: fmt.Sprintf("示例条目 %d", i), Detail: fmt.Sprintf("第 %d 条的详情。", i)})
	}
	return out
}

func (a *app) init(ctx context.Context, in *ext.Init, hub *ext.Hub) (map[string]any, error) {
	var c struct {
		Greeting string `json:"greeting"`
	}
	if err := in.Config(&c); err != nil {
		return nil, err
	}
	if c.Greeting != "" {
		a.greeting = c.Greeting
	}
	a.apiKey = in.Secrets["api_key"]
	return nil, nil
}

// page is the home page's state: what is searched for, which item is open (a page pushed on top of the list).
type page struct {
	Q    string `json:"q,omitempty"`
	Open string `json:"open,omitempty"`
}

func (a *app) home(ctx context.Context, r *ext.UIRequest) (*wire.View, error) {
	var s page
	if err := r.DecodeState(&s); err != nil {
		return nil, err
	}
	if s.Open != "" {
		return a.detail(r, s)
	}
	rows := []wire.Component{}
	for _, it := range a.items() {
		if s.Q == "" || strings.Contains(it.Title, s.Q) {
			rows = append(rows, wire.Component{Type: wire.ComponentRow, Title: it.Title, Icon: "doc",
				Action: ext.Open("open:"+it.ID, "", page{Open: it.ID}, it.Title)})
		}
	}
	if len(rows) == 0 {
		rows = append(rows, wire.Component{Type: wire.ComponentEmpty, Title: "没有找到「" + s.Q + "」", Icon: "search"})
	}
	key := "未设置"
	if a.apiKey != "" {
		key = "已设置"
	}
	v := &wire.View{Title: a.name, State: ext.EncodeState(s), Sections: []wire.Section{
		{Items: []wire.Component{{Type: wire.ComponentText, Text: a.greeting}}},
		{Title: "条目", Footer: "API Key：" + key, Items: rows},
	}}
	if r.UIVersion >= 3 {
		v.Search = &wire.Search{Placeholder: "条目名", Value: s.Q, Action: wire.Action{ID: "search"}}
	} else { // an app before UI version 3 has no search box: a field and a button
		v.Sections[0].Items = append(v.Sections[0].Items,
			wire.Component{Type: wire.ComponentField, Name: "q", Label: "搜索", Kind: wire.FieldKindText, Value: s.Q, Placeholder: "条目名"},
			wire.Component{Type: wire.ComponentButton, Title: "搜索", Action: &wire.Action{ID: "search", Submit: true}})
	}
	return v, nil
}

func (a *app) detail(r *ext.UIRequest, s page) (*wire.View, error) {
	for _, it := range a.items() {
		if it.ID == s.Open {
			items := []wire.Component{{Type: wire.ComponentMarkdown, Text: "**" + it.Title + "**\n\n" + it.Detail}}
			if r.UIVersion < 3 { // drawn in place of the list there: no back button of the app's
				items = append(items, wire.Component{Type: wire.ComponentButton, Title: "返回", Action: &wire.Action{ID: "back"}})
			}
			return &wire.View{Title: it.Title, State: ext.EncodeState(s), Sections: []wire.Section{{Items: items}}}, nil
		}
	}
	return &wire.View{Title: "不见了", Sections: []wire.Section{{Items: []wire.Component{{Type: wire.ComponentEmpty, Title: "这一条不见了"}}}}}, nil
}

func (a *app) homeAction(ctx context.Context, r *ext.UIRequest) (*wire.ActionResult, error) {
	var s page
	_ = r.DecodeState(&s)
	switch r.Action {
	case "search":
		s.Q, s.Open = strings.TrimSpace(r.Values["q"]), ""
	case "back":
		s.Open = ""
	}
	r.State = ext.EncodeState(s)
	return nil, nil // draw the page again with the new state
}

func (a *app) today(ctx context.Context, r *ext.UIRequest) (*wire.View, error) {
	return &wire.View{Sections: []wire.Section{{Items: []wire.Component{
		{Type: wire.ComponentStatGrid, Stats: []wire.Stat{
			{Label: "条目", Value: fmt.Sprint(len(a.items()))},
			{Label: "问候", Value: a.greeting},
		}},
	}}}}, nil
}

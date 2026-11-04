package conformance

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/plugin"
	"github.com/nwverse/persona/server/internal/plugin/serve"
)

func TestServeConforms(t *testing.T) {
	m, err := plugin.Parse([]byte(`{"manifest_version": 1, "id": "example-hello", "name": "Hello", "version": "0.1.0",
		"engines": {"plugin_protocol": 1}, "runtime": {"command": "bin/hello"},
		"contributes": {"connectors": [{"id": "hello", "name": "Hello", "capabilities": ["status"]}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	prog := serve.Program{ID: "example-hello", Version: "0.1.0",
		Init: func(context.Context, plugin.InitializeParams, *serve.Hub) (map[string]any, error) {
			return map[string]any{"connectors": []plugin.ConnectorItem{{ID: "hello", Name: "Hello", Capabilities: []string{"status"}}}}, nil
		},
		Namespaces: map[string]serve.Handler{"connector": func(_ context.Context, method string, params json.RawMessage) (any, error) {
			if c := serve.Contribution(params); c != "" && c != "hello" {
				return nil, serve.NoContribution(c)
			}
			if method == "status" {
				return map[string]string{"state": "ok"}, nil
			}
			return nil, serve.UnknownMethod(method)
		}}}
	Check(t, m, Serve(t, prog), plugin.InitializeParams{})
}

// A plugin with UI draws each of its slots a View the apps can draw there, told where it is drawn.
func TestUIConforms(t *testing.T) {
	m, err := plugin.Parse([]byte(`{"manifest_version": 1, "id": "example-usage", "name": "Usage", "version": "0.1.0",
		"engines": {"plugin_protocol": 1}, "runtime": {"command": "bin/usage"},
		"contributes": {"ui": [{"id": "usage", "title": "用量", "placement": "nav_page", "icon": "chart"},
			{"id": "today", "title": "今天", "placement": "overview_card", "opens": "usage"},
			{"id": "gpu", "title": "显卡", "placement": "machine_section"}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	drawn := map[string]plugin.UIParams{}
	prog := serve.Program{ID: "example-usage", Version: "0.1.0",
		Namespaces: map[string]serve.Handler{"ui": func(_ context.Context, method string, params json.RawMessage) (any, error) {
			var p plugin.UIParams
			_ = json.Unmarshal(params, &p)
			if method != "render" {
				return nil, serve.UnknownMethod(method)
			}
			if !slices.Contains([]string{"usage", "today", "gpu"}, p.Contribution) {
				return nil, serve.NoContribution(p.Contribution)
			}
			drawn[p.Contribution] = p
			chart := plugin.UIElem{Type: "chart", Kind: "bar", Unit: "tokens", Series: []plugin.ChartSeries{{Points: []plugin.ChartPoint{{Label: "一", Value: 1}}}}}
			return plugin.View{RefreshAfter: 300, Sections: []plugin.Section{{Items: []plugin.UIElem{
				{Type: "stat_grid", Stats: []plugin.Stat{{Label: "今天", Value: "1.2M"}}}, chart}}}}, nil
		}}}
	Check(t, m, Serve(t, prog), plugin.InitializeParams{})
	if drawn["today"].Size != plugin.SizeCompact || drawn["usage"].Size != plugin.SizeFull || drawn["gpu"].Machine == nil ||
		drawn["usage"].TZ == "" || drawn["usage"].UIVersion != plugin.UIVersion {
		t.Fatalf("%+v", drawn)
	}
}

// A plugin's inbox_item is drawn for an example item of its connector.
func TestInboxItemConforms(t *testing.T) {
	m, err := plugin.Parse([]byte(`{"manifest_version": 1, "id": "example-chat", "name": "Chat", "version": "0.1.0",
		"engines": {"plugin_protocol": 1}, "runtime": {"command": "bin/chat"},
		"contributes": {"connectors": [{"id": "chat", "name": "Chat", "capabilities": ["status", "inbox"]}],
			"ui": [{"id": "event", "title": "事件", "placement": "inbox_item", "connector": "chat"}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	var drawn plugin.UIParams
	prog := serve.Program{ID: "example-chat", Version: "0.1.0",
		Init: func(context.Context, plugin.InitializeParams, *serve.Hub) (map[string]any, error) {
			return map[string]any{"connectors": []plugin.ConnectorItem{{ID: "chat", Name: "Chat", Capabilities: []string{"inbox", "status"}}}}, nil
		},
		Namespaces: map[string]serve.Handler{
			"connector": func(_ context.Context, method string, params json.RawMessage) (any, error) {
				if c := serve.Contribution(params); c != "" && c != "chat" {
					return nil, serve.NoContribution(c)
				}
				if method == "status" {
					return map[string]string{"state": "ok"}, nil
				}
				return nil, serve.UnknownMethod(method)
			},
			"ui": func(_ context.Context, method string, params json.RawMessage) (any, error) {
				if method != "render" {
					return nil, serve.UnknownMethod(method)
				}
				if c := serve.Contribution(params); c != "event" {
					return nil, serve.NoContribution(c)
				}
				_ = json.Unmarshal(params, &drawn)
				return plugin.View{Sections: []plugin.Section{{Items: []plugin.UIElem{{Type: "text", Text: drawn.Item.Title}}}}}, nil
			}}}
	Check(t, m, Serve(t, prog), plugin.InitializeParams{})
	if drawn.Placement != "inbox_item" || drawn.Item == nil || !strings.HasPrefix(drawn.Item.ID, "chat:") || drawn.Item.Title == "" {
		t.Fatalf("%+v", drawn)
	}
}

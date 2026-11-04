package conformance

import (
	"context"
	"encoding/json"
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

package serve

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/plugin"
	"github.com/nwverse/persona/server/internal/plugin/conformance"
)

// notes is a connector that reads its secret while it is built (the program must not hold its lock then).
type notes struct{ secret []byte }

func (notes) ID() string   { return "examplenotes" }
func (notes) Name() string { return "Example Notes" }
func (notes) Manifest() connector.Manifest {
	return connector.Manifest{Capabilities: []connector.Capability{connector.CapStatus}, Hosts: []string{"notes.example.com"}}
}
func (n notes) Status(context.Context) connector.Status {
	return connector.Status{State: connector.StateOK, Account: string(n.secret)}
}

// A connector program opened with initialize is a plugin program like any: plugin/conformance holds it to that.
func TestConnectorProgramIsAPlugin(t *testing.T) {
	m, err := plugin.Parse([]byte(`{"manifest_version": 1, "id": "example-notes", "name": "Example Notes", "version": "1.0.0",
		"engines": {"plugin_protocol": 1}, "runtime": {"command": "bin/notes"},
		"contributes": {"connectors": [{"id": "examplenotes", "name": "Example Notes", "capabilities": ["status"], "hosts": ["notes.example.com"]}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	progR, hubW := io.Pipe()
	hubR, progW := io.Pipe()
	exited := make(chan error, 1)
	go func() {
		exited <- Main(context.Background(), progR, progW, func(_ json.RawMessage, s connector.Secrets) (connector.Connector, error) {
			data, err := s.Load(context.Background())
			return notes{secret: data}, err
		})
		_ = progW.Close()
	}()
	res := conformance.Check(t, m, conformance.Run{ToProgram: hubW, FromProgram: hubR, Exited: exited},
		plugin.InitializeParams{Active: map[string][]string{"connectors": {"examplenotes"}}})
	var items []plugin.ConnectorItem
	_ = json.Unmarshal(res.Contributes["connectors"], &items)
	if len(items) != 1 || items[0].ID != "examplenotes" || items[0].Hosts[0] != "notes.example.com" {
		t.Fatalf("%+v", res)
	}
}

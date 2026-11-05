package serve

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"slices"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/extension/conformance"
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
	m, err := extension.Parse([]byte(`{"manifest_version": 1, "id": "example-notes", "name": "Example Notes", "version": "1.0.0",
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
		extension.InitializeParams{Active: map[string][]string{"connectors": {"examplenotes"}}})
	var items []extension.ConnectorItem
	_ = json.Unmarshal(res.Contributes["connectors"], &items)
	if len(items) != 1 || items[0].ID != "examplenotes" || items[0].Hosts[0] != "notes.example.com" {
		t.Fatalf("%+v", res)
	}
}

// hearing is a connector that listens to the hub's events.
type hearing struct {
	notes
	heard chan string
}

func (h hearing) Hear(_ context.Context, event string, params json.RawMessage) {
	h.heard <- event + " " + string(params)
}

// The hub's event.* notifications reach a connector that listens, without their namespace.
func TestEventsReachAListener(t *testing.T) {
	progR, hubW := io.Pipe()
	hubR, progW := io.Pipe()
	heard := make(chan string, 1)
	go func() {
		_ = Main(context.Background(), progR, progW, func(json.RawMessage, connector.Secrets) (connector.Connector, error) {
			return hearing{heard: heard}, nil
		})
		_ = progW.Close()
	}()
	answers := bufio.NewReader(hubR)
	write := func(s string) {
		if _, err := io.WriteString(hubW, s+"\n"); err != nil {
			t.Fatal(err)
		}
	}
	write(`{"id":"h1","method":"initialize","params":{"protocol":1,"plugin":{"id":"example-notes","version":"1.0.0"},"active":{"connectors":["examplenotes"]}}}`)
	if _, err := answers.ReadString('\n'); err != nil { // the hub sends events once the program is initialized
		t.Fatal(err)
	}
	go func() { _, _ = io.Copy(io.Discard, answers) }()
	write(`{"event":"connector.ignored","params":{}}`)
	write(`{"event":"event.agent.needs_you","params":{"session_id":"s1"}}`)
	select {
	case got := <-heard:
		if got != `agent.needs_you {"session_id":"s1"}` {
			t.Fatalf("heard %q", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the event did not reach the connector")
	}
	_ = hubW.Close()
}

// archiving is a connector that files conversations under another name too and has a command line.
type archiving struct{ notes }

func (archiving) Manifest() connector.Manifest {
	return connector.Manifest{Capabilities: []connector.Capability{connector.CapStatus}, ArchiveSources: []string{"moments"}, CLI: "ex"}
}

// The init answer and the initialize contribution carry the connector's archive_sources and cli: a program built on
// serve says what its Go manifest says.
func TestManifestKeepsArchiveSourcesAndCLI(t *testing.T) {
	m := ManifestOf(archiving{})
	if !slices.Equal(m.ArchiveSources, []string{"moments"}) || m.CLI != "ex" {
		t.Fatalf("init answer %+v", m)
	}
	if back := m.Of(); !slices.Equal(back.ArchiveSources, []string{"moments"}) || back.CLI != "ex" {
		t.Fatalf("round trip %+v", back)
	}
	progR, hubW := io.Pipe()
	hubR, progW := io.Pipe()
	go func() {
		_ = Main(context.Background(), progR, progW, func(json.RawMessage, connector.Secrets) (connector.Connector, error) {
			return archiving{}, nil
		})
		_ = progW.Close()
	}()
	defer hubW.Close()
	if _, err := io.WriteString(hubW, `{"id":"i1","method":"initialize","params":{"protocol":1,"plugin":{"id":"example-notes","version":"1.0.0"},"active":{"connectors":["examplenotes"]}}}`+"\n"); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(hubR).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	go func() { _, _ = io.Copy(io.Discard, hubR) }()
	var answer struct {
		Result extension.InitializeResult `json:"result"`
	}
	if err := json.Unmarshal([]byte(line), &answer); err != nil {
		t.Fatal(err)
	}
	var items []extension.ConnectorItem
	if err := json.Unmarshal(answer.Result.Contributes["connectors"], &items); err != nil || len(items) != 1 {
		t.Fatalf("initialize answer %s", line)
	}
	if !slices.Equal(items[0].ArchiveSources, []string{"moments"}) || items[0].CLI != "ex" {
		t.Fatalf("contribution %+v", items[0])
	}
}

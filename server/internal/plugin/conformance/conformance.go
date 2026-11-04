// Package conformance holds a plugin program to the plugin protocol (docs/plugins.md「一致性测试」): the handshake,
// the error codes, the shape of what it answers, and that it exits when the hub goes away. A program written in any
// language can be held to it with Program; one in Go with Serve. What each extension point's methods answer is
// checked by that point's own tests (connector/conformance for connectors).
package conformance

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"regexp"
	"slices"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/plugin"
	"github.com/nwverse/persona/server/internal/plugin/serve"
	"github.com/nwverse/persona/server/internal/rpc"
)

// Run is a running program: what the hub writes to and reads from it, and its exit (nil when it cannot be watched).
type Run struct {
	ToProgram   io.WriteCloser
	FromProgram io.Reader
	Exited      <-chan error
}

// Program runs the executable at path (with args) for Check.
func Program(t testing.TB, path string, args ...string) Run {
	t.Helper()
	cmd := exec.Command(path, args...)
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	return Run{ToProgram: in, FromProgram: out, Exited: exited}
}

// Serve runs prog in this process for Check.
func Serve(t testing.TB, prog serve.Program) Run {
	t.Helper()
	progR, hubW := io.Pipe()
	hubR, progW := io.Pipe()
	exited := make(chan error, 1)
	go func() {
		err := serve.Serve(context.Background(), progR, progW, prog)
		_ = progW.Close()
		if errors.Is(err, io.EOF) {
			err = nil
		}
		exited <- err
	}()
	t.Cleanup(func() { hubW.Close(); progR.Close(); hubR.Close() })
	return Run{ToProgram: hubW, FromProgram: hubR, Exited: exited}
}

var snake = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// Check holds the program to the protocol, m being its manifest; init is what to send with initialize (the plugin's
// id and version are filled in from m). It returns what initialize answered.
func Check(t testing.TB, m *plugin.Manifest, run Run, init plugin.InitializeParams) plugin.InitializeResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	hub := rpc.New("h", run.ToProgram)
	hub.OnRequest = func(context.Context, string, json.RawMessage) (any, error) { return struct{}{}, nil }
	go func() { _ = hub.Serve(ctx, run.FromProgram) }()
	code := func(err error) string {
		var re *rpc.Error
		if errors.As(err, &re) {
			return re.Code
		}
		return ""
	}

	if err := hub.Call(ctx, plugin.MPing, struct{}{}, nil); code(err) != plugin.CodeNotInitialized {
		t.Fatalf("ping before initialize: want %s, got %v", plugin.CodeNotInitialized, err)
	}
	if err := hub.Call(ctx, plugin.MInitialize, plugin.InitializeParams{Protocol: plugin.Protocol + 100}, nil); code(err) != plugin.CodeWrongProtocol {
		t.Fatalf("initialize of another protocol: want %s, got %v", plugin.CodeWrongProtocol, err)
	}

	init.Protocol = plugin.Protocol
	init.Plugin.ID, init.Plugin.Version = m.ID, m.Version
	if init.Hub.ExtensionPoints == nil {
		init.Hub.ExtensionPoints = plugin.PointVersions()
	}
	var raw json.RawMessage
	if err := hub.Call(ctx, plugin.MInitialize, init, &raw); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	snakeKeys(t, "initialize", raw, 0)
	var res plugin.InitializeResult
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if res.Protocol != plugin.Protocol || res.ID != m.ID || res.Version != m.Version {
		t.Fatalf("initialize answered %d %q %q, the manifest says %d %q %q", res.Protocol, res.ID, res.Version, plugin.Protocol, m.ID, m.Version)
	}
	declared := m.Contributions()
	for point, items := range res.Contributes {
		p, ok := plugin.Points[point]
		if !ok {
			continue
		}
		var list []json.RawMessage
		if err := json.Unmarshal(items, &list); err != nil {
			t.Fatalf("contributes.%s: a list", point)
		}
		for _, item := range list {
			id, err := p.Item(item)
			if err != nil {
				t.Fatalf("contributes.%s: %v", point, err)
			}
			if !slices.Contains(declared[point], id) {
				t.Fatalf("contributes.%s: %q is not in the manifest", point, id)
			}
		}
	}

	if err := hub.Call(ctx, plugin.MPing, struct{}{}, nil); err != nil {
		t.Fatalf("ping: %v", err)
	}
	if err := hub.Call(ctx, "nonexistent.method", map[string]any{}, nil); code(err) != plugin.CodeUnknownMethod {
		t.Fatalf("an unknown method: want %s, got %v", plugin.CodeUnknownMethod, err)
	}
	for point := range declared {
		ns := plugin.Points[point].Namespace
		if err := hub.Call(ctx, ns+".nonexistent_method", map[string]any{}, nil); code(err) != plugin.CodeUnknownMethod {
			t.Fatalf("an unknown method of %s: want %s, got %v", ns, plugin.CodeUnknownMethod, err)
		}
		if err := hub.Call(ctx, ns+".status", map[string]any{"contribution": "no-such-contribution"}, nil); code(err) != plugin.CodeNoContribution &&
			code(err) != plugin.CodeUnknownMethod {
			t.Fatalf("a contribution it does not have: want %s, got %v", plugin.CodeNoContribution, err)
		}
	}

	if run.Exited != nil {
		_ = run.ToProgram.Close()
		select {
		case <-run.Exited:
		case <-time.After(5 * time.Second):
			t.Fatal("still running 5 seconds after the hub closed its stdin")
		}
	}
	return res
}

// snakeKeys checks every object key in raw is snake_case.
func snakeKeys(t testing.TB, at string, raw json.RawMessage, depth int) {
	t.Helper()
	if depth > 6 {
		return
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) == nil {
		for k, v := range obj {
			if !snake.MatchString(k) {
				t.Fatalf("%s: key %q is not snake_case", at, k)
			}
			snakeKeys(t, at+"."+k, v, depth+1)
		}
		return
	}
	var list []json.RawMessage
	if json.Unmarshal(raw, &list) == nil {
		for _, v := range list {
			snakeKeys(t, at, v, depth+1)
		}
	}
}

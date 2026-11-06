package extmgr

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/rpc"
)

// model.complete (extension_model_calls.go, #6102): only with the models scope itself, handed to the assistant with
// the extension's id; plugins.models lists the extensions that have a purpose.
func TestExtensionModelCallNeedsItsScopeAndIsAttributed(t *testing.T) {
	h := newTestManager(t)
	granted := kbExtensionSpec(t, h, "example-models", extension.Permissions{Hub: []string{extension.ScopeModels}})
	kbOnly := kbExtensionSpec(t, h, "example-kb", extension.Permissions{Hub: []string{extension.ScopeKBRead}})
	full := kbExtensionSpec(t, h, "example-full", extension.Permissions{HubAPI: true})
	var mu sync.Mutex
	var asked []string
	var got []json.RawMessage
	h.opts.Assistant = func(_ context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
		mu.Lock()
		defer mu.Unlock()
		asked = append(asked, method)
		got = append(got, params)
		return json.RawMessage(`{"text":"ok","model":"m","stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":2,"requests":1}}`), nil
	}
	params := `{"messages":[{"role":"user","content":"hi"}],"extension":"someone-else"}`
	for _, spec := range []struct {
		name string
		call func(context.Context, string, json.RawMessage) (any, error)
	}{{"kb only", kbOnly.Handle}, {"legacy full grant", full.Handle}} {
		_, err := spec.call(context.Background(), assistantwire.MethodModelComplete, json.RawMessage(params))
		var re *rpc.Error
		if !errors.As(err, &re) || re.Code != extension.CodePermissionDenied || !strings.Contains(string(re.Data), `"models"`) {
			t.Fatalf("%s: %v", spec.name, err)
		}
	}
	if len(asked) != 0 {
		t.Fatalf("a denied call reached the assistant: %v", asked)
	}
	out, err := granted.Handle(context.Background(), assistantwire.MethodModelComplete, json.RawMessage(params))
	if err != nil {
		t.Fatal(err)
	}
	if raw, _ := json.Marshal(out); !strings.Contains(string(raw), `"text":"ok"`) {
		t.Fatalf("%s", raw)
	}
	var env assistantwire.ExtensionComplete
	if len(asked) != 1 || asked[0] != assistantwire.MethodModelsExtensionComplete || json.Unmarshal(got[0], &env) != nil ||
		env.Extension != "example-models" || string(env.Params) != params {
		t.Fatalf("the assistant got %v %s", asked, got)
	}
	// The internal method is not the extensions'.
	_, err = granted.Handle(context.Background(), assistantwire.MethodModelsExtensionComplete, json.RawMessage(`{}`))
	var re *rpc.Error
	if !errors.As(err, &re) || re.Code != extension.CodeUnknownMethod {
		t.Fatalf("internal method exposed: %v", err)
	}

	raw, err := pluginsCall(t, h, assistantwire.PluginsModels, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"extensions":[{"id":"example-models","name":"Example"}]}` {
		t.Fatalf("plugins.models: %s", raw)
	}
	h.extensions.reg.Get("example-models").Record.Enabled = false
	if raw, _ := pluginsCall(t, h, assistantwire.PluginsModels, map[string]any{}); string(raw) != `{"extensions":[]}` {
		t.Fatalf("a switched-off extension keeps its purpose: %s", raw)
	}
	if _, err := granted.Handle(context.Background(), assistantwire.MethodModelComplete, json.RawMessage(params)); !errors.As(err, &re) || re.Code != extension.CodePermissionDenied {
		t.Fatalf("switched off: %v", err)
	}
}

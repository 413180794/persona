package extension

import (
	"encoding/json"
	"testing"
)

// A built-in program on computers (node.command "builtin", #1830): no package, no consent, no permission lines, no
// machines field of its own; the digest is the one it had without a node section.
func TestBuiltinNodeIsNoPackagedProgram(t *testing.T) {
	cfg := json.RawMessage(`{"type":"object","properties":{"machines":{"type":"array","items":{"type":"string","format":"machine","x-os":["darwin"]}}}}`)
	without := &Manifest{ManifestVersion: ManifestVersion, ID: "cua", Name: "x", Version: "1.0.0", Configuration: cfg,
		Contributes: map[string]json.RawMessage{"computer": json.RawMessage(`[{"id":"cua"}]`)}}
	with := *without
	with.Node = &NodeRuntime{Command: Command{Path: NodeBuiltin}, Machines: "machines"}
	if !with.Node.Builtin() || with.NodeProgram() != nil || without.NodeProgram() != nil {
		t.Fatal("Builtin / NodeProgram")
	}
	if Digest(&with) != Digest(without) {
		t.Fatal("a built-in node section changed the consent digest")
	}
	if len(Explain(&with)) != len(Explain(without)) || NodeAdds(without, &with) || with.NodeMachines() != nil {
		t.Fatalf("permission lines or machines field: %v", Explain(&with))
	}
	b, _ := json.Marshal(with.Node)
	var back NodeRuntime
	if err := json.Unmarshal(b, &back); err != nil || !back.Builtin() || string(b) != `{"command":"builtin","machines":"machines"}` {
		t.Fatalf("round trip %s: %v", b, err)
	}
	packaged := &NodeRuntime{Command: Command{ByPlatform: map[string]string{"darwin-arm64": "node/x"}}, Machines: "machines"}
	if packaged.Builtin() {
		t.Fatal("a package is built in")
	}
}

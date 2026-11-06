package conformance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// The repository root, from this package's directory.
const repoRoot = "../../../.."

func readRepoFile(t *testing.T, rel string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot, rel))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// The schema's example connector manifest is the manifest in the wire fixtures: a change to one of them and not the
// other turns this red.
func TestExampleManifestFollowsTheWireFixtures(t *testing.T) {
	var example, wire any
	if err := json.Unmarshal(readRepoFile(t, "schema/extension/examples/Manifest.examplechat.json"), &example); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(fixture(t, "persona-plugin.json"), &wire); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(example, wire) {
		t.Error("schema/extension/examples/Manifest.examplechat.json is not the manifest in the wire fixtures")
	}
}

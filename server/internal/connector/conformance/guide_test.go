package conformance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
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

// The example manifest, the wire fixtures and the wechat-connector guide all describe the same released plugin: the example
// is that plugin's manifest, every tool the guide names is one the plugin lists, and every setting it has is in the
// guide's table. A sync that updates one of them and not the others turns this red.
func Testwechat-connectorGuideAndExampleFollowTheFixtures(t *testing.T) {
	var example, wire any
	if err := json.Unmarshal(readRepoFile(t, "schema/extension/examples/Manifest.wechat-connector.json"), &example); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(fixture(t, "persona-plugin.json"), &wire); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(example, wire) {
		t.Error("schema/extension/examples/Manifest.wechat-connector.json is not the manifest in the wire fixtures")
	}

	var listed []string
	for _, name := range []string{"tools.list.json", "actions.list.json"} {
		var list struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		}
		if err := json.Unmarshal(fixture(t, name), &list); err != nil {
			t.Fatal(err)
		}
		for _, tool := range list.Tools {
			listed = append(listed, tool.Name)
		}
	}
	guide := string(readRepoFile(t, "docs/wechat-connector.md"))
	var named []string
	for _, m := range regexp.MustCompile("`(wechat-connector_[a-z_]+)`").FindAllStringSubmatch(guide, -1) {
		named = append(named, m[1])
	}
	slices.Sort(listed)
	slices.Sort(named)
	if named = slices.Compact(named); !slices.Equal(named, listed) {
		t.Errorf("the guide names tools %v, the fixtures list %v", named, listed)
	}

	var settings struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(fixtureManifest(t).Configuration, &settings); err != nil || len(settings.Properties) == 0 {
		t.Fatalf("the manifest's settings: %v", err)
	}
	for key := range settings.Properties {
		if !strings.Contains(guide, "| `"+key+"` |") {
			t.Errorf("the setting %q is not in the guide's settings table", key)
		}
	}
}

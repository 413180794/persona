package update

import (
	"reflect"
	"strings"
	"testing"
)

// The manifest carries no min_node_build: nothing ever read it, publish.sh never wrote it, and the "a node older than
// it updates first" it was meant to express is not how a hub updates (the nodes follow once the new hub confirmed,
// docs/operations/hub-update.md, "Update nodes along with it").
func TestManifestHasNoMinNodeBuild(t *testing.T) {
	typ := reflect.TypeOf(Manifest{})
	for i := 0; i < typ.NumField(); i++ {
		if tag := typ.Field(i).Tag.Get("json"); strings.HasPrefix(tag, "min_node_build") {
			t.Fatalf("Manifest.%s is in latest.json but read by nothing", typ.Field(i).Name)
		}
	}
}

// A latest.json published before it went away still reads: the old key is ignored, not an error.
func TestParseManifestIgnoresALegacyMinNodeBuild(t *testing.T) {
	b := []byte(`{"version":"v1","build":202611021530,"protocol":1,"min_node_build":209901010000,
		"files":{"linux-amd64":{"name":"personad-linux-amd64.gz","size":1,"sha256":"a","binary_sha256":"b"}}}`)
	m, err := ParseManifest(b)
	if err != nil {
		t.Fatalf("a manifest with min_node_build does not read: %v", err)
	}
	if m.Build != 202611021530 || m.Version != "v1" || len(m.Files) != 1 {
		t.Fatalf("%+v", m)
	}
}

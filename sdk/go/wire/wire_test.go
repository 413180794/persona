package wire

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Every example in schema/extension/examples decodes into its definition's type and encodes back to the same JSON:
// the generated types hold everything the schema says, under the same names.
func TestExamplesRoundTrip(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "..", "schema", "extension", "examples", "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no examples: %v", err)
	}
	for _, f := range files {
		def := strings.SplitN(filepath.Base(f), ".", 2)[0]
		t.Run(filepath.Base(f), func(t *testing.T) {
			mk, ok := newDef[def]
			if !ok {
				t.Fatalf("no type %s", def)
			}
			raw, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			v := mk()
			if err := json.Unmarshal(raw, v); err != nil {
				t.Fatalf("decode: %v", err)
			}
			out, err := json.Marshal(v)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			var want, got any
			_ = json.Unmarshal(raw, &want)
			_ = json.Unmarshal(out, &got)
			if !reflect.DeepEqual(want, got) {
				t.Errorf("round trip differs\nwant %s\n got %s", raw, out)
			}
		})
	}
}

// The component types and the UI version that brought each are the ones the hub has drawn since UI version 2.
func TestComponentTypes(t *testing.T) {
	want := map[string]int{"text": 1, "metric": 1, "progress": 1, "row": 1, "button": 1, "link": 1, "field": 1,
		"divider": 1, "chart": 2, "stat_grid": 2, "segmented": 2}
	if !reflect.DeepEqual(ComponentSince, want) {
		t.Errorf("ComponentSince = %v", ComponentSince)
	}
	if len(ComponentTypes) != len(want) {
		t.Errorf("ComponentTypes = %v", ComponentTypes)
	}
}

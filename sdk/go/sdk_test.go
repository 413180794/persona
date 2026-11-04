package sdk

import (
	"io/fs"
	"os"
	"testing"
)

// A copy of the SDK (personad extension new) gets the go.mod this module has.
func TestGoModIsTheModules(t *testing.T) {
	b, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != GoMod {
		t.Fatalf("GoMod is %q, go.mod %q", GoMod, b)
	}
}

func TestEmbedded(t *testing.T) {
	for _, f := range []string{"ext/ext.go", "exttest/exttest.go", "wire/wire_gen.go", "wire/validate.go"} {
		if _, err := fs.Stat(Go, f); err != nil {
			t.Errorf("%s is not embedded: %v", f, err)
		}
	}
	if len(Bundle) == 0 {
		t.Fatal("no bundle")
	}
}

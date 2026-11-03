package screen

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Dialogs as the card gets them (the JSON of ParseDialog), for screens worth pinning down. Regenerate with
// UPDATE_GOLDEN=1 and read the diff.
func TestGoldenDialogs(t *testing.T) {
	for _, name := range []string{"claude-2.1.316-danger-placeholder-100"} {
		got, err := json.MarshalIndent(ParseDialog(fixture(t, name+".txt")), "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join("testdata", name+".golden.json")
		if os.Getenv("UPDATE_GOLDEN") != "" {
			if err := os.WriteFile(path, append(got, '\n'), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(want) != string(got)+"\n" {
			t.Errorf("%s:\n%s\nwant\n%s", name, got, want)
		}
	}
}

package sourcevalidate

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestProcessHasNoAmbientAuthority(t *testing.T) {
	factory := processFixture(t, "validator-test-isolation")
	private := filepath.Join(t.TempDir(), "private")
	if err := os.WriteFile(private, []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	ns, err := os.Readlink("/proc/self/ns/net")
	if err != nil {
		t.Fatal(err)
	}
	b := []byte("x")
	err = validate(context.Background(), bytes.NewReader(b), digest(b), func(ctx context.Context) (*exec.Cmd, func(), error) {
		cmd, dispose, err := factory(ctx)
		if err == nil {
			cmd.Args = append(cmd.Args, private, ns)
			cmd.Env = append(cmd.Env, "PERSONA_VALIDATOR_PRIVATE_TEST=private")
		}
		return cmd, dispose, err
	})
	if err != nil {
		t.Fatal("native namespace exposed ambient authority", err)
	}
}

func TestASTConstructionHasNoManagerEntry(t *testing.T) {
	// Check source, not the module's library code or caller-supplied fixtures.
	for _, root := range []string{"..", "../../extmgr", "."} {
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() && path != root {
				return filepath.SkipDir
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || path == "child.go" || path == "./child.go" {
				return nil
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, forbidden := range []string{"github.com/tdewolff/parse", "js.Parse(", "js.Walk(", "sourceParseMu"} {
				if bytes.Contains(b, []byte(forbidden)) {
					t.Errorf("untrusted AST construction escaped child boundary: %s", filepath.Base(path))
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

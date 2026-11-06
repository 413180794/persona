package hub

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// What the hub says to people (errors, the admin UI's messages, logs, usage, doctor) names the command they installed,
// personad (#4481). jarvisd stays a working alias and so do identities like the jarvisd systemd unit (docs/rename.md),
// so only the command form "jarvisd hub …" is caught, in string literals, not comments.
func TestUserTextNamesPersonad(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		f, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING && strings.Contains(lit.Value, "jarvisd hub") {
				t.Errorf("%s: %s tells people to run jarvisd; say personad", fset.Position(lit.Pos()), lit.Value)
			}
			return true
		})
	}
}

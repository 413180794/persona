package hub

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Commands in user-readable hub text must name the canonical server executable.
func TestUserTextNamesPersonad(t *testing.T) {
	commands := regexp.MustCompile(`\b([a-z]+d) hub (?:admin|setup|pair|run|slot|switch|update)\b`)
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
			if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				text, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatal(err)
				}
				for _, command := range commands.FindAllStringSubmatch(text, -1) {
					if command[1] != "personad" {
						t.Errorf("%s: noncanonical hub command %q", fset.Position(lit.Pos()), command[0])
					}
				}
			}
			return true
		})
	}
}

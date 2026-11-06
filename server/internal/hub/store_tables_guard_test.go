package hub

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/db"
)

// The migrations are a hub set and a manager set (docs/server.md "Migrations"): a hub's database has only the hub's
// tables, and the default manager is to get a database of its own. So what the hub asks of its store must only touch
// the hub's tables, and what the managers ask of theirs only the manager's. These read every interface a side
// declares, take the methods *db.DB serves of them, and look at the SQL those methods (and the db functions they
// call) run.

var createTableRE = regexp.MustCompile(`(?i)CREATE TABLE (?:IF NOT EXISTS )?([a-z_]+)`)

// setTables are the tables migrations/<part> makes.
func setTables(t *testing.T, part string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "db", "migrations", part, "*.sql"))
	if err != nil || len(files) == 0 {
		t.Fatalf("migrations/%s: %v", part, err)
	}
	var out []string
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range createTableRE.FindAllStringSubmatch(string(b), -1) {
			if tb := strings.ToLower(m[1]); !slices.Contains(out, tb) {
				out = append(out, tb)
			}
		}
	}
	sort.Strings(out)
	return out
}

// dbFuncs are internal/db's functions and *DB's methods by name, each with the string literals in its body and the
// names it calls.
type dbFunc struct {
	strs  []string
	calls []string
}

func dbFuncs(t *testing.T) map[string]dbFunc {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "db", "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]dbFunc{}
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			var fn dbFunc
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.BasicLit:
					if n.Kind == token.STRING {
						if s, err := strconv.Unquote(n.Value); err == nil {
							fn.strs = append(fn.strs, s)
						}
					}
				case *ast.CallExpr:
					switch c := n.Fun.(type) {
					case *ast.Ident:
						fn.calls = append(fn.calls, c.Name)
					case *ast.SelectorExpr:
						fn.calls = append(fn.calls, c.Sel.Name)
					}
				}
				return true
			})
			key := fd.Name.Name
			if fd.Recv != nil {
				key = "DB." + key
			}
			out[key] = fn
		}
	}
	return out
}

// tablesTouched are the tables of all the SQL method m of *DB runs, the db functions it calls included.
func tablesTouched(funcs map[string]dbFunc, m string, all []string) []string {
	seen := map[string]bool{}
	var out []string
	var walk func(key string)
	walk = func(key string) {
		if seen[key] {
			return
		}
		seen[key] = true
		fn, ok := funcs[key]
		if !ok {
			return
		}
		for _, s := range fn.strs {
			for _, tb := range all {
				if regexp.MustCompile(`\b`+tb+`\b`).MatchString(s) && !slices.Contains(out, tb) {
					out = append(out, tb)
				}
			}
		}
		for _, c := range fn.calls {
			walk("DB." + c)
			walk(c)
		}
	}
	walk("DB." + m)
	sort.Strings(out)
	return out
}

// storeMethods are the methods of *db.DB that the interfaces declared in the non-test files of dir ask for.
func storeMethods(t *testing.T, dir string) []string {
	t.Helper()
	served := map[string]bool{}
	typ := reflect.TypeFor[*db.DB]()
	for i := range typ.NumMethod() {
		served[typ.Method(i).Name] = true
	}
	delete(served, "Close")
	delete(served, "CloseContext")
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			it, ok := n.(*ast.InterfaceType)
			if !ok {
				return true
			}
			for _, field := range it.Methods.List {
				for _, id := range field.Names {
					if served[id.Name] && !slices.Contains(out, id.Name) {
						out = append(out, id.Name)
					}
				}
			}
			return true
		})
	}
	sort.Strings(out)
	return out
}

// The hub's store touches only the hub's tables; the managers' stores only the manager's.
func TestStoresTouchTheirOwnTables(t *testing.T) {
	hubTables, managerTables := setTables(t, "hub"), setTables(t, "manager")
	all := append(slices.Clone(hubTables), managerTables...)
	funcs := dbFuncs(t)
	for _, side := range []struct {
		dirs   []string
		tables []string
		set    string
	}{
		{[]string{"."}, hubTables, "hub"},
		{[]string{"../assistant", "../devices", "../extmgr", "../extension", "../mail", "../agents"}, managerTables, "manager"},
	} {
		for _, dir := range side.dirs {
			for _, m := range storeMethods(t, dir) {
				for _, tb := range tablesTouched(funcs, m, all) {
					if !slices.Contains(side.tables, tb) {
						t.Errorf("internal/%s asks its store for %s, which touches %s: not a table of the %s set "+
							"(internal/db/migrations/%s). Get it over the bus from the side that owns it, or move the "+
							"table; docs/server.md \"Migrations\"", strings.TrimPrefix(filepath.Clean(filepath.Join("hub", dir)), "hub/"),
							m, tb, side.set, side.set)
					}
				}
			}
		}
	}
}

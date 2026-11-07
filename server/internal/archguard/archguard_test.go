package archguard

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const (
	modulePath = "github.com/nwverse/persona/server/"
	guardDoc   = `see docs/server/architecture.md "Contract packages"`
)

// readList reads a testdata list: one entry per line, `#` starts a comment, blank lines skipped.
func readList(t *testing.T, name string) []string {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		if line = strings.Join(strings.Fields(line), " "); line != "" {
			out = append(out, line)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// goList runs go list with a template over packages (paths under the module) and returns its lines.
func goList(t *testing.T, format string, deps bool, pkgs ...string) []string {
	t.Helper()
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go tool:", err)
	}
	args := []string{"list", "-f", format}
	if deps {
		args = append(args, "-deps")
	}
	for _, p := range pkgs {
		args = append(args, importPath(p))
	}
	out, err := exec.Command(goBin, args...).Output()
	if err != nil {
		msg := ""
		if ee, ok := err.(*exec.ExitError); ok {
			msg = string(ee.Stderr)
		}
		t.Fatalf("go %s: %v %s", strings.Join(args, " "), err, msg)
	}
	return strings.Fields(string(out))
}

// importPath is a listed package's import path: a path under the module, or a full one for the SDK's contracts
// (github.com/…/sdk/go/wire, another module).
func importPath(p string) string {
	if strings.Contains(strings.SplitN(p, "/", 2)[0], ".") {
		return p
	}
	return modulePath + p
}

// under says whether rel (a path under the module) is pkg or one of its subpackages.
func under(rel, pkg string) bool { return rel == pkg || strings.HasPrefix(rel, pkg+"/") }

// Every contract package depends on nothing but the standard library and other contract packages: the parts can
// share them without importing each other's code.
func TestContractsArePure(t *testing.T) {
	contracts := readList(t, "contracts.txt")
	for _, c := range contracts {
		for _, dep := range goList(t, "{{if not .Standard}}{{.ImportPath}}{{end}}", true, c) {
			rel, ok := strings.CutPrefix(dep, modulePath)
			switch {
			case !ok && slices.Contains(contracts, dep): // the SDK's contract (checked pure as a listed package itself)
			case !ok:
				t.Errorf("contract package %s depends on %s, outside the standard library: a contract holds only types and "+
					"constants; keep the code that needs it in the part that uses it; %s", c, dep, guardDoc)
			case rel != c && !slices.Contains(contracts, rel):
				t.Errorf("contract package %s depends on %s, which is no contract: define the type or constant it borrows in "+
					"a contract package (the business package can alias it: type X = kbwire.X) and move any logic into "+
					"the part that uses it; %s", c, rel, guardDoc)
			}
		}
	}
	// Every *wire package is a contract, and is listed.
	for _, p := range goList(t, "{{.ImportPath}}", false, "internal/...") {
		rel, _ := strings.CutPrefix(p, modulePath)
		if strings.HasSuffix(rel, "wire") && !slices.Contains(contracts, rel) {
			t.Errorf("%s looks like a contract package but testdata/contracts.txt does not list it: list it (and keep it "+
				"pure) or rename it; %s", rel, guardDoc)
		}
	}
}

// The parts and the packages they are made of (their roots; everything those import counts too).
var (
	hubPart     = []string{"internal/hub", "internal/hubedge"}
	managerPart = []string{"internal/assistant", "internal/devices", "internal/extmgr", "internal/manager"}
	nodePart    = []string{"internal/node"}
)

// The parts import each other only through the contract packages: the hub imports no manager or node package, a
// manager no hub package, the node neither.
func TestPartsStayApart(t *testing.T) {
	rules := []struct {
		part      string
		roots     []string
		forbidden []string
		instead   string
	}{
		{"hub", hubPart, append(slices.Clone(managerPart), nodePart...),
			"the hub only relays: move the code into the manager and reach it over the bus (assistantwire, devicewire, extwire)"},
		{"manager", managerPart, hubPart,
			"a manager reaches the hub only over the bus: use the contract packages (assistantwire, devicewire, extwire) instead"},
		{"node", nodePart, append(slices.Clone(hubPart), managerPart...),
			"the node talks to its manager only through the node protocol (proto, nodeproto, devicewire): use those types instead"},
	}
	allowed := readList(t, "boundary_allowed.txt")
	used := map[string]bool{}
	for _, r := range rules {
		for _, root := range r.roots {
			for _, dep := range goList(t, "{{.ImportPath}}", true, root) {
				rel, ok := strings.CutPrefix(dep, modulePath)
				if !ok || slices.ContainsFunc(r.roots, func(own string) bool { return under(rel, own) }) {
					continue
				}
				for _, f := range r.forbidden {
					if !under(rel, f) {
						continue
					}
					if entry := r.part + " " + rel; slices.Contains(allowed, entry) {
						used[entry] = true
						continue
					}
					t.Errorf("%s (the %s) depends on %s: %s; %s", root, r.part, rel, r.instead, guardDoc)
				}
			}
		}
	}
	for _, a := range allowed {
		if !used[a] {
			t.Errorf("testdata/boundary_allowed.txt lists %q, which no longer happens: delete the line", a)
		}
	}
}

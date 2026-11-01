// Package tzguard holds a test only: the hub runs in UTC and its user does not, so server code reads times in the
// user's zone (hub.userLocation, the knowledge base's Store.Zone), never in the machine's own.
package tzguard

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Where the machine's own zone is right, and why.
var allowed = map[string]string{
	"internal/node/":                     "a node runs on the user's own computer, in their zone (uploads by day)",
	"internal/hub/assistant_schedule.go": "userLocation / userTZName: the hub's zone only while the user's is unknown",
	"internal/kb/zone.go":                "the archive's zone before it is set, and files written before zones were recorded",
	"internal/quota/quota.go":            "a gateway's report while the user's zone is unknown",
}

var (
	// .Local(), time.Local, and the clock read straight off time.Now() (its zone is the machine's).
	localZone = regexp.MustCompile(`\.Local\(\)|\btime\.Local\b|time\.Now\(\)\.(Format|Hour|Minute|Day|Weekday|YearDay|Month|Year|Date|Clock)\b`)
)

func TestNoMachineTimeZone(t *testing.T) {
	root, err := filepath.Abs("../..") // server/
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	err = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		for prefix := range allowed {
			if strings.HasPrefix(rel, prefix) {
				return nil
			}
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		checked++
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for n := 1; sc.Scan(); n++ {
			code, _, _ := strings.Cut(sc.Text(), "//") // comments may name it
			if localZone.MatchString(code) {
				t.Errorf("%s:%d: the machine's time zone (the hub runs in UTC): read times in the user's, "+
					"hub.userLocation() / kb Store.Zone(), or add the file to `allowed` with the reason:\n\t%s",
					rel, n, strings.TrimSpace(sc.Text()))
			}
		}
		return sc.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked < 50 {
		t.Fatalf("only %d files checked: is the walk at server/?", checked)
	}
}

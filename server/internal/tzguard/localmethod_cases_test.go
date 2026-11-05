package tzguard

import (
	"strings"
	"testing"
)

func TestMaskBooleanLocalCalls(t *testing.T) {
	cases := []struct {
		name, source string
		matches      int
	}{
		{"terminal", `package p

type Terminal struct{}
func (t Terminal) Local() bool { return true }
func (t Terminal) IsLocal() bool { return t.Local() }
`, 0},
		{"time receiver", `package p
import "time"
func local(t time.Time) time.Time { return t.Local() }
`, 1},
		{"unknown receiver", `package p
func local(t Unknown) bool { return t.Local() }
`, 1},
		{"syntax error", `package p
func local(t Unknown) { t.Local() }
!
`, 1},
		{"same line preserves time call", `package p
import "time"
type Terminal struct{}
func (t Terminal) Local() bool { return true }
func f(t Terminal) { _ = t.Local(); _ = time.Now().Local() }
`, 1},
		{"still catches machine clock", `package p
import "time"
type Terminal struct{}
func (t Terminal) Local() bool { return true }
func f(t Terminal) { _ = t.Local(); _ = time.Now().Hour(); _ = time.Local }
`, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			masked := maskBooleanLocalCalls([]byte(c.source))
			if got := len(localZone.FindAll(masked, -1)); got != c.matches {
				t.Fatalf("matches=%d want=%d:\n%s", got, c.matches, masked)
			}
			if len(masked) != len(c.source) || strings.Count(string(masked), "\n") != strings.Count(c.source, "\n") {
				t.Fatal("masking changed source positions")
			}
		})
	}
}

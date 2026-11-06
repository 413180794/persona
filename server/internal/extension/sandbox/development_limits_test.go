//go:build persona_dev_resources && linux

package sandbox

import (
	"io"
	"testing"
)

func TestDevelopmentRejectsInvalidChildBudgets(t *testing.T) {
	for _, budgets := range [][]string{
		{"0", "128", "1000"}, {"31", "128", "1000"},
		{"1", "95", "1000"}, {"1", "257", "1000"},
		{"1", "128", "999"}, {"1", "128", "300001"},
		// Multiplying this millisecond value by time.Millisecond wraps to
		// exactly one second. Reject the input before duration conversion.
		{"1", "128", "288230376151712744"},
	} {
		args := append(append([]string(nil), budgets...), "--", "/bin/true")
		if code := runDevelopmentChild(args, "/this-helper-must-not-start", io.Discard); code != 2 {
			t.Fatalf("invalid child budget %v reached process start: exit=%d", budgets, code)
		}
	}
}

package transcript

import (
	"fmt"
	"testing"
)

// A gateway's zeros on a turn's replies and its sum of the whole turn on the last one (#3300) are not the context:
// the last real number stands.
func TestContextTokensSkipImpossibleUsage(t *testing.T) {
	var meta Meta
	for i, input := range []int{84_000, 0, 0, 1_706_464} {
		line := fmt.Sprintf(`{"type":"assistant","uuid":"a%d","message":{"id":"m%d","role":"assistant","model":"claude-opus-5-5","content":[{"type":"text","text":"x"}],"usage":{"input_tokens":%d,"output_tokens":1}}}`, i, i, input)
		ParseLine(i+1, []byte(line), &meta)
	}
	if meta.ContextTokens != 84_000 {
		t.Errorf("ContextTokens = %d", meta.ContextTokens)
	}
}

func TestPlausibleContext(t *testing.T) {
	for _, c := range []struct {
		tokens int
		size   int64
		want   bool
	}{
		{999_999, 0, true}, {1_000_001, 0, false}, {1_706_464, 1_240_000, false},
		{150_000, 1_000, true}, {250_000, 1_000, false}, {900_000, 2_000_000, true},
	} {
		if got := PlausibleContext(c.tokens, c.size); got != c.want {
			t.Errorf("PlausibleContext(%d, %d) = %v", c.tokens, c.size, got)
		}
	}
}

package transcript

import (
	"encoding/json"
	"strconv"
)

// recordedMilliseconds accepts only an explicitly recorded nonnegative integer.
func recordedMilliseconds(raw json.RawMessage) (int64, bool) {
	ms, err := strconv.ParseInt(string(raw), 10, 64)
	return ms, err == nil && ms >= 0
}

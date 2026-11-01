package node

import (
	"bytes"
	"encoding/json"
	"strconv"
)

func codexRequestKey(raw json.RawMessage) string {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var id any
	if d.Decode(&id) != nil {
		return ""
	}
	switch v := id.(type) {
	case string:
		b, _ := json.Marshal(v)
		return "s:" + string(b)
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return "n:" + strconv.FormatInt(n, 10)
		}
	}
	return ""
}

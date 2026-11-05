//go:build builtinlive

package node

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

// On a real Mac with Cua Driver and ego lite (#1830 step 5): the built-in programs answer plugin.node.call as their
// old names do, against the real tools. Read-only calls only (status, list_apps, list_windows): nothing is clicked,
// typed, opened or shown, and no screenshot is taken. It prints the answers' shapes (keys, counts), never what is in
// them (apps and window titles are the user's).
//
//	PERSONA_BUILTIN_LIVE=1 ./node.test -test.run TestBuiltinLive -test.v   (built: GOOS=darwin go test -c -tags builtinlive)
func TestBuiltinLive(t *testing.T) {
	if os.Getenv("PERSONA_BUILTIN_LIVE") != "1" {
		t.Skip("PERSONA_BUILTIN_LIVE not set")
	}
	n, _ := testNode(t)
	n.dir, n.logger = filepath.Join(n.home, ".persona"), log.New(os.Stderr, "", 0)
	for _, c := range []struct {
		old, id, method string
		params          any
	}{
		{"computer.status", "cua", "status", map[string]any{}},
		{"computer.call", "cua", "call", map[string]any{"session": "builtin-live", "tool": "list_apps", "args": map[string]any{}}},
		{"computer.call", "cua", "call", map[string]any{"session": "builtin-live", "tool": "list_windows", "args": map[string]any{}}},
		{"browser.status", "ego", "status", map[string]any{}},
	} {
		oldOut, oldErr := pluginCall(t, n, c.old, c.params)
		raw, _ := json.Marshal(c.params)
		newOut, newErr := pluginCall(t, n, "plugin.node.call", proto.PluginNodeCall{ID: c.id, Method: c.method, Params: raw})
		a, _ := json.Marshal(oldOut)
		b, _ := json.Marshal(newOut)
		t.Logf("%s %s: old %s err %v | new %s err %v", c.old, c.method, shape(a), oldErr != nil, shape(b), newErr != nil)
		if (oldErr == nil) != (newErr == nil) {
			t.Errorf("%s: errors %v / %v", c.old, oldErr, newErr)
		}
		if shape(a) != shape(b) {
			t.Errorf("%s: the answers differ in shape", c.old)
		}
		if c.method == "status" && !sameIgnoringChecked(t, a, b) {
			t.Errorf("%s: the status differs", c.old)
		}
	}
}

// shape is an answer's keys and, for its output, the keys and list lengths inside: never a value.
func shape(b []byte) string {
	var m map[string]json.RawMessage
	if json.Unmarshal(b, &m) != nil {
		return "(not an object)"
	}
	keys := make([]string, 0, len(m))
	for k, v := range m {
		if k == "output" {
			var inner map[string]json.RawMessage
			if json.Unmarshal(v, &inner) == nil {
				var ik []string
				for ik2, iv := range inner {
					var list []json.RawMessage
					if json.Unmarshal(iv, &list) == nil {
						ik2 += "[" + itoa(len(list)) + "]"
					}
					ik = append(ik, ik2)
				}
				sort.Strings(ik)
				k += "{" + join(ik) + "}"
			}
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return "{" + join(keys) + "}"
}

func itoa(i int) string { b, _ := json.Marshal(i); return string(b) }

func join(s []string) string {
	out := ""
	for i, x := range s {
		if i > 0 {
			out += ","
		}
		out += x
	}
	return out
}

package hub

import (
	"context"
	"encoding/json"
	"slices"
	"strings"

	"github.com/nwverse/persona/server/internal/proto"
)

// builtinPrefixes are the extensions whose programs are built into personad (cua, ego; node/builtin.go) by the prefix of
// their old method names.
var builtinPrefixes = map[string]string{"computer.": "cua", "browser.": "ego"}

// callBuiltin asks m's built-in program for method, given by its old name (computer.status, browser.cookies.list…):
// over plugin.node.call {id, method} on a node that says it has the program (node.info's builtin_programs, #1830
// step 5), by the old name on one from before. Which way is m's own word, never a retry: an older node gets exactly
// the call it always got.
func (m *Machine) callBuiltin(ctx context.Context, method string, params any, out any) error {
	for prefix, id := range builtinPrefixes {
		short, ok := strings.CutPrefix(method, prefix)
		if !ok {
			continue
		}
		m.mu.Lock()
		has := slices.Contains(m.info.BuiltinPrograms, id)
		m.mu.Unlock()
		if !has {
			break
		}
		raw, err := json.Marshal(params)
		if err != nil {
			return err
		}
		return m.call(ctx, "plugin.node.call", proto.PluginNodeCall{ID: id, Method: short, Params: raw}, out)
	}
	return m.call(ctx, method, params, out)
}

// callBuiltinProgram asks m's built-in program id for method, a name it has kept over plugin.node.call (imessage's
// connector.*): that way to a node that says it has the program, as is to one from before.
func (m *Machine) callBuiltinProgram(ctx context.Context, id, method string, params any, out any) error {
	m.mu.Lock()
	has := slices.Contains(m.info.BuiltinPrograms, id)
	m.mu.Unlock()
	if !has {
		return m.call(ctx, method, params, out)
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return m.call(ctx, "plugin.node.call", proto.PluginNodeCall{ID: id, Method: method, Params: raw}, out)
}

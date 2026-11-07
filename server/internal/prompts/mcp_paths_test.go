package prompts

import (
	"strings"
	"testing"
)

// The MCP tools' descriptions are the same for every hub, read as they are (not templates): none may name a folder
// of the default layout, which is somewhere else once PERSONA_HOME or kb_dir moves it.
func TestMCPToolsDoNotNameTheDefaultFolders(t *testing.T) {
	for _, n := range ToolNames() {
		if d := Tool(n).Description; strings.Contains(d, "~/.persona") {
			t.Errorf("%s: the description names a default folder: %q", n, d)
		}
	}
}

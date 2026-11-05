package assistant

import (
	"testing"

	"github.com/nwverse/persona/server/internal/prompts"
)

func TestGoldenViewTools(t *testing.T) {
	for _, name := range []string{"view_show", "view_update", "view_get"} {
		tool := prompts.Tool(name)
		golden(t, name, tool.Description+"\n\n"+string(tool.InputSchema)+"\n")
	}
}

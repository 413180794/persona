package assistant

import (
	"context"
	"path"
	"slices"
	"strings"

	"github.com/nwverse/persona/server/internal/identity"
	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
)

// The health space (docs/health.md, #2691): the assistant's conversations about health run in a directory of their
// own next to the assistant's, so Claude Code's auto memory and the instructions are the space's own. Its
// conversations are listed apart (GET /api/v1/assistant?space=health), and the conversation tools of one space see
// only that space's conversations.

// SpaceHealth is the health space's name (Session.Space, StartParams.Space, ?space=).
const SpaceHealth = identity.SpaceHealth

// healthDir is the health space's directory on a machine, relative to its home (node assistant.prepare).
const healthDir = ".jarvis/health"

// healthCwd is the health space's directory on d, or "" while d has not reported its home.
func healthCwd(d Device) string {
	if d.Home == "" {
		return ""
	}
	if d.DataDir != "" {
		return path.Join(d.DataDir, SpaceHealth)
	}
	return path.Join(d.Home, healthDir)
}

// spaceCwd is the directory of space ("" the assistant's own) on d.
func spaceCwd(d Device, space string) string {
	if space == SpaceHealth {
		return healthCwd(d)
	}
	return assistantCwd(d)
}

// healthClaudeMD is the health space's CLAUDE.md for machine here (prompts/assistant/health_claude_md.md).
func (s *Service) healthClaudeMD(ctx context.Context, here string) string {
	hello := s.hello(ctx)
	d := prompts.AssistantHealthClaudeMD{Owner: strings.TrimSpace(hello.Owner), Notes: s.notes(ctx), Language: answerLanguage(hello.Locale)}
	devices := s.devices(ctx)
	if i := slices.IndexFunc(devices, func(d Device) bool { return d.ID == here }); i >= 0 {
		if cwd := assistantCwd(devices[i]); cwd != "" {
			d.StateDir = path.Dir(cwd)
		}
	}
	return prompts.RenderAssistantHealthClaudeMD(d)
}

// spaceOf is the space of an assistant conversation: its own Space when the hub set it, else its directory's.
func spaceOf(c proto.Session) string {
	if c.Space != "" {
		return c.Space
	}
	return identity.SpaceOf(c.Cwd)
}

// validSpace: "" (the assistant's own) or "health".
func validSpace(space string) bool { return space == "" || space == SpaceHealth }

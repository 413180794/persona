package node

import (
	"strings"

	"github.com/nwverse/persona/server/internal/proto"
)

// Context usage of a session for the apps' agent rows and session header (#2430). The numbers come from the
// transcript tail readQuickMetaSince already parses: the latest reply's input tokens (transcript.Meta.ContextTokens)
// and, for Codex, the window its rollout records. Nothing here reads a file.

const (
	claudeWindow   = 200_000   // every Claude model without the 1M variant
	claudeWindow1M = 1_000_000 // the [1m] variants
)

// contextWindowOf is the model's context window in tokens, 0 when it cannot be told. recorded is the window the
// transcript itself says (Codex); otherwise Claude models are known by id: a "[1m]" suffix means the 1M variant,
// and so does a reply that already saw more than 200k (the transcript records the id without the suffix). A model
// that is not a Claude one (a gateway's own) has no window we know: the apps then show the tokens only.
func contextWindowOf(model string, recorded, used int) int {
	if recorded > 0 {
		return recorded
	}
	id := strings.ToLower(strings.TrimSpace(model))
	switch {
	case strings.Contains(id, "[1m]"):
		return claudeWindow1M
	case strings.HasPrefix(id, "claude-"):
		if used > claudeWindow {
			return claudeWindow1M
		}
		return claudeWindow
	}
	return 0
}

// setContext fills a session's context fields from its transcript meta; both stay zero (and so off the wire)
// while the transcript has no reply yet.
func setContext(s *proto.Session, m quickMeta) {
	if m.contextTokens <= 0 {
		return
	}
	s.ContextTokens = m.contextTokens
	s.ContextWindow = contextWindowOf(m.model, m.contextWindow, m.contextTokens)
}

package proto

import "strings"

// IsCodexSubagent identifies delegated tasks from source metadata, even when
// their parent is not in the current session list. Titles never imply ancestry.
func (s Session) IsCodexSubagent() bool {
	if s.Agent != "codex" && !strings.HasPrefix(s.SourceKind, "codex_") {
		return false
	}
	return s.IsSubagent || s.ParentSessionID != "" || s.ParentThreadID != "" ||
		(s.RootSessionID != "" && s.RootSessionID != s.SessionID)
}

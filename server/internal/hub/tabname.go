package hub

import (
	"context"
	"strings"
	"time"
)

const tabNamePrompt = `Name a terminal tab for a coding agent after the task it is given: 1 to 3 lowercase English words joined by
hyphens, at most 20 characters, saying what the work is (duo-sidebar, inbox-agent, fix-login, release-notes). The task may
be in Chinese; the name is always English. Reply with the name only.`

// autoTabName is a short name for the herdr tab of a new agent, from its first message (the small-task model, a few
// seconds at most). "" when there is no message or the model fails: the node names it agent-<n>.
func (h *Hub) autoTabName(ctx context.Context, prompt string) string {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" || h.ai == nil {
		return ""
	}
	if r := []rune(prompt); len(r) > 1500 {
		prompt = string(r[:1500])
	}
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	out, err := h.ai.Complete(cctx, tabNamePrompt, prompt, 200) // reasoning models think first: 20 came back empty
	if err != nil {
		h.logf("tab name: %v", err)
		return ""
	}
	name := tabSlug(out)
	if name == "" {
		h.logf("tab name: nothing usable in %q", clip(oneLine(out), 80))
	}
	return name
}

// tabSlug keeps what a tab name may be: lowercase ASCII letters and digits, single hyphens, at most 24 characters.
func tabSlug(s string) string {
	if i := strings.IndexByte(strings.TrimSpace(s), '\n'); i > 0 {
		s = strings.TrimSpace(s)[:i]
	}
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(r)
		case r == '-' || r == '_' || r == ' ' || r == '/':
			dash = true
		}
		if b.Len() >= 24 {
			break
		}
	}
	return strings.Trim(b.String()[:min(b.Len(), 24)], "-")
}

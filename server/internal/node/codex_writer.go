package node

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/nwverse/persona/server/internal/i18n"
)

const codexWriterConflictCode = "codex_active_writer"

func codexWriterConflict() error {
	return refusal(codexWriterConflictCode, i18n.T(i18n.En, "rpc-errors."+codexWriterConflictCode), map[string]any{"retryable": false})
}

// Only the thread/resume caller knows that no turn has been submitted yet. Other -32600 errors can mean different things.
func codexResumeWriterConflict(err error, id string) bool {
	var rpcErr *codexError
	return errors.As(err, &rpcErr) && rpcErr.Code == -32600 && rpcErr.Message == "thread "+id+" already has an active writer"
}

// A desktop origin is stronger than a cwd heuristic, but routing still requires the exact thread and a unique
// owner through the registered transport. A subagent or program run cannot take an owner's follow-up.
func (n *Node) codexDesktopWriterRoutable(id string) bool {
	src, ok := n.codexSource(id)
	if !ok || src.thread != id || src.path == "" || !n.scopedTranscriptAllows(src.path) ||
		!pathWithin(src.path, src.home) || rolloutThreadID(src.home, src.path) != id {
		return false
	}
	o := readCodexOrigin(src.path)
	if !o.desktop || o.exec || o.sub {
		return false
	}
	_, _, err := n.codexOwner(id)
	return err == nil
}

// Use the recorded rollout's home first: a launcher can choose a different CODEX_HOME from the node's.
func (n *Node) assistantCodexWriterHeld(id string) bool {
	path := n.transcriptPath(id)
	if rolloutID(path) == id {
		dir := filepath.Dir(path)
		if filepath.Base(dir) == "archived_sessions" {
			return codexWriterHeld(filepath.Dir(dir), id)
		}
		sessions := filepath.Join(dir, "..", "..", "..")
		if filepath.Base(sessions) == "sessions" {
			return codexWriterHeld(filepath.Dir(sessions), id)
		}
	}
	if src, ok := n.codexSource(id); ok && src.thread == id {
		return codexWriterHeld(src.home, id)
	}
	home := canonicalCodexHome(os.Getenv("CODEX_HOME"), n.home)
	return codexWriterHeld(firstNonEmpty(home, n.codexHome()), id)
}

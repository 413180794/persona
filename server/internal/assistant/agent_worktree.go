package assistant

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"unicode"

	"github.com/nwverse/persona/server/internal/herdr"
	"github.com/nwverse/persona/server/internal/proto"
)

// newWorktree makes a worktree on branch of the repository cwd is in, through the machine's terminal engine ("New
// worktree…", docs/terminal.md): herdr's or Orca's own, so it is listed there and in the apps, never a bare
// `git worktree add` by the agent. The checkout goes where the engine puts them, named for the branch the way the
// dialog names it.
func (s *Service) newWorktree(ctx context.Context, mid, cwd, branch string) (proto.WorktreeCreateResult, error) {
	var prep proto.WorktreePrepare
	if err := s.call(ctx, mid, DeviceWorktreePrepare, map[string]any{"cwd": cwd}, &prep); err != nil {
		return proto.WorktreeCreateResult{}, err
	}
	var res proto.WorktreeCreateResult
	err := s.call(ctx, mid, DeviceWorktreeCreate, map[string]any{"cwd": cwd, "branch": branch,
		"path": filepath.Join(prep.Path, worktreeDirName(branch))}, &res)
	return res, err
}

// worktreeDirName is the folder of branch's checkout under the engine's worktrees directory: its slug, as the dialog
// names it. The slug drops every letter that is not ASCII, so a branch with any (a Chinese name) also gets a short hash
// of its whole name: "修复登录" would be "" (that directory itself) and "fix/登录", "fix/注册" both "fix" (#5775).
func worktreeDirName(branch string) string {
	slug := herdr.WorktreeSlug(branch)
	if !hasNonASCIIWord(branch) {
		return slug
	}
	sum := sha256.Sum256([]byte(branch))
	if slug == "" {
		slug = "worktree"
	}
	return slug + "-" + hex.EncodeToString(sum[:4])
}

// hasNonASCIIWord: s has a letter or digit the slug drops.
func hasNonASCIIWord(s string) bool {
	for _, r := range s {
		if r > unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			return true
		}
	}
	return false
}

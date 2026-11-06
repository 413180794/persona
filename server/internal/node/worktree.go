package node

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/host"
	"github.com/nwverse/persona/server/internal/proto"
)

// "New worktree…" (docs/terminal.md "New worktree"): a Git worktree of a workspace's repository, opened as a workspace of
// the terminal host. The host does it when it can (herdr: `worktree create`, which groups the checkout under the
// repository's workspace); a host that cannot gets `git worktree add` and a plain new workspace. The hub lets only a
// full device call it. The request names a workspace the host lists, a branch and a path; the checkout is never forced
// (a branch checked out elsewhere, a path git refuses, a directory with something in it are all refusals) and nothing
// is deleted.

// maxBranch bounds a branch name (git's own limit on a ref name is far above anything typed).
const maxBranch = 255

// canWorktree: the host can make worktrees: its own way, or herdr's fallback (git, then a new workspace).
func canWorktree(h host.Host) bool {
	if w, ok := h.(host.Worktreer); ok && w.CanWorktree() {
		return true
	}
	_, launcher := h.(host.Launcher)
	return launcher && h.Kind() == host.Herdr
}

// worktreeRepo is the repository a workspace is in.
type worktreeRepo struct {
	root, name, branch string
}

// repoOfWorkspace finds the repository of a workspace the host lists, and the host that makes its worktrees.
func (n *Node) repoOfWorkspace(ctx context.Context, kind, workspace string) (host.Host, worktreeRepo, error) {
	var repo worktreeRepo
	if workspace == "" {
		return nil, repo, errors.New("workspace_id is required")
	}
	h, err := n.hostOfKind(kind)
	if err != nil {
		return nil, repo, err
	}
	if !canWorktree(h) {
		return nil, repo, unsupported(h)
	}
	dir, err := n.gitDirOf(ctx, "", workspace, kind)
	if err != nil {
		return nil, repo, err
	}
	dir, err = n.gitDir(dir)
	if err != nil {
		return nil, repo, errors.New("不知道这个工作区的目录")
	}
	root, err := git(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil || strings.TrimSpace(root) == "" {
		return nil, repo, errors.New("这个工作区不在 Git 仓库里")
	}
	repo.root = strings.TrimSpace(root)
	// "" on a detached HEAD
	if b, err := git(ctx, repo.root, "branch", "--show-current"); err == nil {
		repo.branch = strings.TrimSpace(b)
	}
	repo.name = filepath.Base(repo.root)
	// Not --path-format=absolute: git before 2.31 echoes that option back as a line of output instead of failing
	// (#3442). --git-common-dir is relative to the directory it ran in unless it is absolute.
	if common, err := git(ctx, repo.root, "rev-parse", "--git-common-dir"); err == nil && strings.TrimSpace(common) != "" {
		c := strings.TrimSpace(common)
		if !filepath.IsAbs(c) {
			c = filepath.Join(repo.root, c)
		}
		if c = filepath.Clean(c); filepath.Base(c) == ".git" {
			repo.name = filepath.Base(filepath.Dir(c)) // a linked worktree is named for its repository
		} else if c != filepath.Clean(repo.root) {
			repo.name = strings.TrimSuffix(filepath.Base(c), ".git") // a bare repository
		}
	}
	return h, repo, nil
}

// worktreesDir is where the host puts the checkouts of a repository: `<its worktrees directory>/<repo>`.
func (n *Node) worktreesDir(h host.Host, repo string) string {
	base := filepath.Join(n.home, ".herdr", "worktrees")
	if w, ok := h.(host.Worktreer); ok {
		base = w.WorktreesDir(n.home)
	}
	return filepath.Join(base, repo)
}

// worktreePrepare is what the dialog starts from: the repository, the branch the workspace is on, and the directory
// checkouts of this repository go under.
func (n *Node) worktreePrepare(ctx context.Context, kind, workspace string) (proto.WorktreePrepare, error) {
	h, repo, err := n.repoOfWorkspace(ctx, kind, workspace)
	if err != nil {
		return proto.WorktreePrepare{}, err
	}
	return proto.WorktreePrepare{Root: repo.root, RepoName: repo.name, Branch: repo.branch, Path: n.worktreesDir(h, repo.name)}, nil
}

// checkBranch accepts a branch name git takes (new or existing), and nothing that git would read as an option or a
// revision expression.
func checkBranch(ctx context.Context, root, branch string) error {
	if branch == "" {
		return errors.New("分支名不能为空")
	}
	if len(branch) > maxBranch || strings.HasPrefix(branch, "-") {
		return errors.New("分支名不合法")
	}
	if out, err := git(ctx, root, "check-ref-format", "--branch", branch); err != nil || strings.TrimSpace(out) != branch {
		return errors.New("分支名不合法")
	}
	return nil
}

// checkoutPath turns what was typed into the absolute path of the checkout, and refuses one that already holds
// something (an empty directory is fine: git fills it) or lies inside the repository's own git directory.
func (n *Node) checkoutPath(root, typed string) (string, error) {
	p := strings.TrimSpace(typed)
	if p == "" {
		return "", errors.New("检出路径不能为空")
	}
	if p == "~" || strings.HasPrefix(p, "~/") {
		p = filepath.Join(n.home, p[1:])
	}
	if !filepath.IsAbs(p) {
		return "", errors.New("检出路径要写完整路径")
	}
	p = filepath.Clean(p)
	// Compared as real paths: git gives the root with its symlinks resolved (macOS's /var is /private/var), so a
	// path typed through a link would pass a check of the spelling alone (#2650).
	if within(realPath(p), realPath(filepath.Join(root, ".git"))) {
		return "", errors.New("检出路径不能放在 .git 里面")
	}
	switch fi, err := os.Stat(p); {
	case err == nil && !fi.IsDir():
		return "", errors.New("检出路径已经是一个文件")
	case err == nil:
		if entries, err := os.ReadDir(p); err != nil || len(entries) > 0 {
			return "", errors.New("检出路径已存在且不是空目录")
		}
	case !errors.Is(err, os.ErrNotExist):
		return "", fmt.Errorf("读不了检出路径：%w", err)
	}
	return p, nil
}

// within: p is dir or lies under it (both clean).
func within(p, dir string) bool {
	return p == dir || strings.HasPrefix(p, dir+string(filepath.Separator))
}

// worktreeCreate makes the checkout and opens it as a workspace of the host.
func (n *Node) worktreeCreate(ctx context.Context, kind, workspace, branch, path string) (proto.WorktreeCreateResult, error) {
	var res proto.WorktreeCreateResult
	branch = strings.TrimSpace(branch)
	h, repo, err := n.repoOfWorkspace(ctx, kind, workspace)
	if err != nil {
		return res, err
	}
	if err := checkBranch(ctx, repo.root, branch); err != nil {
		return res, err
	}
	dest, err := n.checkoutPath(repo.root, path)
	if err != nil {
		return res, err
	}
	var created *host.Created
	if w, ok := h.(host.Worktreer); ok && w.CanWorktree() {
		created, err = w.CreateWorktree(ctx, workspace, branch, dest)
	} else {
		created, err = n.worktreeByGit(ctx, h, repo, branch, dest)
	}
	if err != nil {
		return res, errors.New(gitFailure(err)) // the dialog says "Couldn't create the worktree" before it
	}
	_ = n.refreshHosts(ctx, h) // the new workspace is in the layout the terminal page lists
	res = proto.WorktreeCreateResult{WorkspaceID: created.SpaceID, TabID: created.SlotID, Path: dest, Branch: branch}
	if created.Pane.ID != "" {
		res.Pane = created.Pane.String()
		res.Terminal = panePrefix + res.Pane
	}
	return res, nil
}

// worktreeByGit is the way for a host that makes no worktrees itself: git adds the checkout (an existing local branch
// as it is, else a new branch from the repository's HEAD; no --force), then the host opens a workspace in it. When the
// workspace cannot be opened the checkout stays, and the error says so.
func (n *Node) worktreeByGit(ctx context.Context, h host.Host, repo worktreeRepo, branch, dest string) (*host.Created, error) {
	l, ok := h.(host.Launcher)
	if !ok {
		return nil, unsupported(h)
	}
	args := []string{"worktree", "add", "-b", branch, "--", dest}
	if _, err := git(ctx, repo.root, "show-ref", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
		args = []string{"worktree", "add", "--", dest, branch}
	}
	if _, err := gitTimed(ctx, repo.root, 2*time.Minute, args...); err != nil {
		return nil, err
	}
	c, err := l.CreateSpace(ctx, dest, repo.name)
	if err != nil {
		return nil, fmt.Errorf("检出已创建，但没能打开工作区：%w", err)
	}
	return c, nil
}

// gitFailure is the line git gave as the reason ("fatal: …"), else the error as it is; one short line for a toast.
func gitFailure(err error) string {
	msg := err.Error()
	for _, line := range strings.Split(msg, "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "fatal: "); ok {
			return clip(rest)
		}
	}
	return clip(msg)
}

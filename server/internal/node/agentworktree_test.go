package node

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/host"
	"github.com/nwverse/persona/server/internal/proto"
)

// A new agent's own worktree (#6115): made from its folder on a branch of its own, told apart from other folders, and
// removed only as far as nothing is lost unless the user confirmed it.
func TestAgentWorktree(t *testing.T) {
	w := worktreeNode(t)
	ctx := context.Background()
	prep, err := w.n.worktreePrepare(ctx, "", "w1", "")
	if err != nil {
		t.Fatal(err)
	}

	// made from the folder, on persona/<the agent's name>, under the repository's checkouts directory
	a, err := w.n.worktreeAdd(ctx, "", w.repo, "", "Fix the login crash!")
	if err != nil {
		t.Fatal(err)
	}
	if a.Branch != "persona/fix-the-login-crash" || a.Path != filepath.Join(prep.Path, "persona-fix-the-login-crash") || resolved(t, a.Root) != resolved(t, w.repo) {
		t.Fatalf("added %+v (checkouts under %s)", a, prep.Path)
	}
	if branchOf(t, a.Path) != a.Branch {
		t.Fatalf("checked out %q", branchOf(t, a.Path))
	}
	// the same name again: the next free one; a name with nothing to slug: "agent"
	if b, err := w.n.worktreeAdd(ctx, "", w.repo, "", "Fix the login crash"); err != nil || b.Branch != "persona/fix-the-login-crash-2" {
		t.Fatalf("again: %+v %v", b, err)
	}
	if c, err := w.n.worktreeAdd(ctx, "", w.repo, "", "修登录"); err != nil || c.Branch != "persona/agent" {
		t.Fatalf("no ASCII: %+v %v", c, err)
	}
	if _, err := w.n.worktreeAdd(ctx, "", t.TempDir(), "", "x"); codeOf(err) != "not_a_repository" {
		t.Fatalf("not a repository: %v", err)
	}

	// status: ours, clean; a change, then a commit the main branch lacks
	st, err := w.n.worktreeStatus(ctx, "", a.Path)
	if err != nil || !st.Worktree || !st.Ours || st.Branch != a.Branch || st.MainBranch != "main" || st.Checkouts != prep.Path || st.Dirty != 0 || st.Unmerged != 0 {
		t.Fatalf("status %+v %v", st, err)
	}
	if err := os.WriteFile(filepath.Join(a.Path, "app.txt"), []byte("two\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if st, _ = w.n.worktreeStatus(ctx, "", a.Path); st.Dirty != 1 {
		t.Fatalf("a changed file: %+v", st)
	}
	if _, err := w.n.worktreeRemove(ctx, "", a.Path, false, false); codeOf(err) != "worktree_dirty" {
		t.Fatalf("remove with a change: %v", err)
	}
	runPullGit(t, a.Path, "commit", "-am", "second")
	if st, _ = w.n.worktreeStatus(ctx, "", a.Path); st.Dirty != 0 || st.Unmerged != 1 {
		t.Fatalf("a commit: %+v", st)
	}
	if _, err := w.n.worktreeRemove(ctx, "", a.Path, true, false); codeOf(err) != "worktree_unmerged" {
		t.Fatalf("delete an unmerged branch: %v", err)
	}
	// an agent still in it
	w.n.mu.Lock()
	w.n.agents["live"] = host.Agent{Agent: "claude", Cwd: filepath.Join(a.Path, "sub"), AgentStatus: proto.StatusWorking}
	w.n.mu.Unlock()
	if _, err := w.n.worktreeRemove(ctx, "", a.Path, false, false); codeOf(err) != "worktree_in_use" {
		t.Fatalf("an agent in it: %v", err)
	}
	w.n.mu.Lock()
	delete(w.n.agents, "live")
	w.n.mu.Unlock()
	// removed, the branch kept (its commit is not on main)
	if r, err := w.n.worktreeRemove(ctx, "", a.Path, false, false); err != nil || !r.Removed || r.BranchDeleted {
		t.Fatalf("remove: %+v %v", r, err)
	}
	if _, err := os.Stat(a.Path); !os.IsNotExist(err) || !branchExists(ctx, w.repo, a.Branch) {
		t.Fatalf("after: %v, branch kept %v", err, branchExists(ctx, w.repo, a.Branch))
	}

	// a change, removed when the user confirmed it, the branch with it
	d, _ := w.n.worktreeAdd(ctx, "", w.repo, "", "throwaway")
	if err := os.WriteFile(filepath.Join(d.Path, "scratch.txt"), []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if r, err := w.n.worktreeRemove(ctx, "", d.Path, true, true); err != nil || !r.Removed || !r.BranchDeleted || branchExists(ctx, w.repo, d.Branch) {
		t.Fatalf("forced: %+v %v", r, err)
	}

	// not ours: the main checkout, a folder inside a worktree, a worktree somewhere else
	// the main checkout is no worktree, but says where an agent's worktree would go
	if st, _ := w.n.worktreeStatus(ctx, "", w.repo); st.Worktree || st.Path == "" || st.Checkouts != prep.Path {
		t.Fatalf("the main checkout: %+v (checkouts under %s)", st, prep.Path)
	}
	if _, err := w.n.worktreeRemove(ctx, "", w.repo, false, false); codeOf(err) != "not_our_worktree" {
		t.Fatalf("remove the main checkout: %v", err)
	}
	e, _ := w.n.worktreeAdd(ctx, "", w.repo, "", "inside")
	if err := os.MkdirAll(filepath.Join(e.Path, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := w.n.worktreeRemove(ctx, "", filepath.Join(e.Path, "pkg"), false, false); codeOf(err) != "not_our_worktree" {
		t.Fatalf("a folder inside: %v", err)
	}
	elsewhere := filepath.Join(t.TempDir(), "mine")
	runPullGit(t, w.repo, "worktree", "add", "-b", "by-hand", "--", elsewhere)
	if st, _ := w.n.worktreeStatus(ctx, "", elsewhere); !st.Worktree || st.Ours {
		t.Fatalf("made by hand: %+v", st)
	}
	if _, err := w.n.worktreeRemove(ctx, "", elsewhere, false, false); codeOf(err) != "not_our_worktree" || !strings.Contains(elsewhere, "mine") {
		t.Fatalf("remove one made by hand: %v", err)
	}
}

// A branch given with letters the slug drops (a Chinese name), or nothing to slug at all, still gets a folder of its
// own in the checkouts directory: never that directory itself, nor another branch's (fix/登录 and fix/注册 both slug
// to "fix"), as for agent_start (#5775).
func TestAgentWorktreeForABranchWithNoASCIIName(t *testing.T) {
	w := worktreeNode(t)
	ctx := context.Background()
	prep, err := w.n.worktreePrepare(ctx, "", "w1", "")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]string{}
	for _, branch := range []string{"修复登录", "需求/重构", "fix/登录", "fix/注册", "_"} {
		a, err := w.n.worktreeAdd(ctx, "", w.repo, branch, "")
		if err != nil || filepath.Dir(a.Path) != prep.Path || branchOf(t, a.Path) != branch {
			t.Fatalf("%s: added %+v %v (checkouts under %s)", branch, a, err, prep.Path)
		}
		if p := seen[a.Path]; p != "" {
			t.Fatalf("%s: same folder as %s", branch, p)
		}
		seen[a.Path] = branch
	}
}

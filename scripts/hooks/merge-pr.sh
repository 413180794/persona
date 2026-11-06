#!/bin/bash
# Merge a PR only after the privacy scan passes on what it changes (CLAUDE.md "Development workflow" 7):
#   scripts/hooks/merge-pr.sh <PR number>
# Scans the PR's commits on top of master and its title and body (the squash commit's message on master), with this
# machine's private values (docs/testing.md "Privacy scan"), then squash-merges exactly the head it scanned and deletes the
# branch. CI cannot do this: it does not have the values.
set -euo pipefail
pr="${1:?usage: scripts/hooks/merge-pr.sh <PR number>}"
case "$pr" in *[!0-9]*) echo "merge-pr: not a PR number: $pr" >&2; exit 2 ;; esac
root="$(git rev-parse --show-toplevel)"
head="$(gh pr view "$pr" --json headRefOid -q .headRefOid)"
branch="$(gh pr view "$pr" --json headRefName -q .headRefName)"
same_repo="$(gh pr view "$pr" --json isCrossRepository -q 'if .isCrossRepository then "" else "yes" end')"
base="$(gh pr view "$pr" --json baseRefName -q .baseRefName)"
git -C "$root" fetch -q origin "$base"
git -C "$root" fetch -q origin "pull/$pr/head"
[ "$(git -C "$root" rev-parse FETCH_HEAD)" = "$head" ] || { echo "merge-pr: the PR moved while fetching; run it again" >&2; exit 1; }
msg="$(mktemp "${TMPDIR:-/tmp}/merge-pr.XXXXXX")"
trap 'rm -f "$msg"' EXIT
gh pr view "$pr" --json title,body -q '.title + "\n\n" + .body' > "$msg"
python3 "$root/scripts/privacy_check.py" --range "origin/$base..$head" --message "$msg" \
  || { echo "merge-pr: #$pr not merged: fix the hits above in the PR first" >&2; exit 1; }
# From outside the repository, by name: gh then only merges on GitHub. (--delete-branch inside a worktree also checks
# out the base branch locally and fails when another worktree holds it; and it fails when GitHub already deleted the
# branch on merge.) The branch is deleted here instead, if it is still there.
repo="$(gh repo view --json nameWithOwner -q .nameWithOwner)"
(cd / && gh pr merge "$pr" --repo "$repo" --squash --match-head-commit "$head")
if [ -n "$same_repo" ] && gh api "repos/$repo/git/ref/heads/$branch" >/dev/null 2>&1; then
  gh api -X DELETE "repos/$repo/git/refs/heads/$branch" >/dev/null 2>&1 || echo "merge-pr: merged; could not delete the branch $branch" >&2
fi
echo "merge-pr: #$pr merged"

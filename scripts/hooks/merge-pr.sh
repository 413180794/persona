#!/bin/bash
# Merge a PR only after the privacy scan passes on what it changes (CLAUDE.md「开发工作流」7):
#   scripts/hooks/merge-pr.sh <PR number>
# Scans the PR's commits on top of master and its title and body (the squash commit's message on master), with this
# machine's private values (docs/testing.md「隐私扫描」), then squash-merges exactly the head it scanned and deletes the
# branch. CI cannot do this: it does not have the values.
set -euo pipefail
pr="${1:?usage: scripts/hooks/merge-pr.sh <PR number>}"
case "$pr" in *[!0-9]*) echo "merge-pr: not a PR number: $pr" >&2; exit 2 ;; esac
root="$(git rev-parse --show-toplevel)"
head="$(gh pr view "$pr" --json headRefOid -q .headRefOid)"
base="$(gh pr view "$pr" --json baseRefName -q .baseRefName)"
git -C "$root" fetch -q origin "$base"
git -C "$root" fetch -q origin "pull/$pr/head"
[ "$(git -C "$root" rev-parse FETCH_HEAD)" = "$head" ] || { echo "merge-pr: the PR moved while fetching; run it again" >&2; exit 1; }
msg="$(mktemp "${TMPDIR:-/tmp}/merge-pr.XXXXXX")"
trap 'rm -f "$msg"' EXIT
gh pr view "$pr" --json title,body -q '.title + "\n\n" + .body' > "$msg"
python3 "$root/scripts/privacy_check.py" --range "origin/$base..$head" --message "$msg" \
  || { echo "merge-pr: #$pr not merged: fix the hits above in the PR first" >&2; exit 1; }
gh pr merge "$pr" --squash --delete-branch --match-head-commit "$head"

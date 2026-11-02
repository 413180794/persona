{{if eq .Kind "review_requested"}}类型：GitHub 上有人请用户 review 一个 PR（用户在 reviewers 里），一般需要用户
{{- else if eq .Kind "mention"}}类型：GitHub 的 PR / issue 里 @ 了用户
{{- else if eq .Kind "team_mention"}}类型：GitHub 上 @ 了用户所在的团队（不是用户本人），多数知悉即可
{{- else if eq .Kind "author"}}类型：用户自己的 PR / issue 有了别人的新评论或 review；要用户改代码、回答问题时需要用户，LGTM、批准、机器人消息知悉即可
{{- else if eq .Kind "ci_failed"}}类型：用户触发的 GitHub Actions（CI）失败了
{{- else if eq .Kind "assign"}}类型：GitHub 上有 issue / PR 分配给了用户
{{- else}}类型：GitHub 通知{{end}}

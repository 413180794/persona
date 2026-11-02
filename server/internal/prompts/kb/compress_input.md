会话：{{.Name}}（{{if eq .Kind "dm"}}私聊{{else if eq .Kind "group"}}群聊{{else if eq .Kind "bot"}}机器人 / AI{{else if eq .Kind "mail"}}邮件往来{{else if eq .Kind "moment"}}removed帖子和评论（公司内公开）{{else if eq .Kind "pr"}}GitHub PR：描述、评论和 review{{else if eq .Kind "issue"}}GitHub issue：描述和评论{{end}}{{if .Automated}}，自动发出（通知 / 营销地址，不是真人写的）{{end}}）

已知项目：{{template "kb/known_projects" .Projects}}
已有标签：{{join .Tags "、"}}

# 已有的压缩上下文
{{if blank .Summary}}（还没有）{{else}}{{.Summary}}{{end}}

# 新消息
{{.News}}

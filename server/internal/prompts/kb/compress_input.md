会话：{{.Name}}（{{if eq .Kind "dm"}}私聊{{else if eq .Kind "group"}}群聊{{else if eq .Kind "bot"}}机器人 / AI{{else if eq .Kind "mail"}}邮件往来{{else if eq .Kind "moment"}}公司圈帖子和评论（公司内公开）{{else if eq .Kind "pr"}}GitHub PR：描述、评论和 review{{else if eq .Kind "issue"}}GitHub issue：描述和评论{{end}}{{if .Automated}}，自动发出（通知 / 营销地址，不是真人写的）{{end}}）

已知项目：{{template "kb/known_projects" .Projects}}
已有标签：{{join .Tags "、"}}

# 已有的压缩上下文
{{if blank .Summary}}（还没有）{{else}}{{.Summary}}{{end}}
{{with .Pinned}}
# 已确认的事实（只读，以此为准，不得改写）
{{.}}
{{end}}{{with .People}}
# 人物已确认的事实（只读，以此为准）
{{range .}}- {{.Name}}：{{.Pinned}}
{{end}}{{end}}
# 新消息
{{.News}}

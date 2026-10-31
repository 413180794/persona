# 项目：{{.Name}}
别称：{{with .Aliases}}{{join . "、"}}{{else}}无{{end}}
关键词：{{join .Keywords "、"}}
一句话：{{with .OneLine}}{{.}}{{else}}无{{end}}
目标：{{with .Goal}}{{.}}{{else}}无{{end}}

{{if blank .Summary}}（还没有档案）{{else}}{{.Summary}}{{end}}

# 帖子
{{range .Posts}}
## {{.ID}}
{{.At}} · {{.Author}} · 评论 {{.Comments}}
{{.Text}}
{{end}}

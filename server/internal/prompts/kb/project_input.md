项目：{{.Name}}（{{.Kind}}）
别称：{{join .Aliases "、"}}
已有标签：{{join .Tags "、"}}

# 已有档案
{{if blank .Summary}}（还没有）{{else}}{{.Summary}}{{end}}

# 最近的时间线
{{join .Log "\n"}}

# 相关会话
{{range .Chats}}
## {{.ID}}
{{.Summary}}
{{end}}

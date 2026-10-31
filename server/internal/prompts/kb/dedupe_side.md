# 项目 {{.Key}}：{{.Name}}
别称：{{with .Aliases}}{{join . "、"}}{{else}}无{{end}}
一句话：{{with .OneLine}}{{.}}{{else}}无{{end}}
目标：{{with .Goal}}{{.}}{{else}}无{{end}}
来自：{{with .Chats}}{{join . "、"}}{{else}}无{{end}}（{{.People}} 个人）

{{if blank .Summary}}（还没有档案）{{else}}{{.Summary}}{{end}}

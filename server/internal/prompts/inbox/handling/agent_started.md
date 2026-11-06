{{.Owner}}确认了，Agent 已经开了（{{.Machine}} 上的会话 {{.Session}}），指令{{if .Edited}}（{{.Owner}}改过）{{end}}：
{{.Text}}

它做完会告诉你，那时再交建议。现在调 inbox_suggest 只给 recap 更新总结。

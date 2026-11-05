任务：{{.Title}}

派活时的要求：
{{.Prompt}}

Agent 最后的回复：
{{.Reply}}

可用的链接：
{{range .Links}}- {{.}}
{{else}}（没有）
{{end}}

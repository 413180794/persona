{{with .Summary}}摘要：{{.}}
{{end}}{{template "inbox/item" .Item}}{{with .Tasks}}

用户手上正在做的任务（他交给各台电脑上的 Agent 的）：
{{range .}}- {{.ID}}：{{.Title}}（{{.State}}{{with .Last}}；最近：{{.}}{{end}}）
{{end}}{{end}}

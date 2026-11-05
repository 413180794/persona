任务：{{.Title}}
{{with .Prompt}}
派活时的要求：
{{.}}
{{end}}{{range .Replies}}
Agent（{{.Machine}}{{with .Cwd}} · {{.}}{{end}}）最后的回复：
{{.Text}}
{{end}}

现在：{{.Now}}
Agent：{{.Agent}}（{{.Machine}}{{with .Cwd}}，目录 {{.}}{{end}}）{{with .Project}}
项目：{{.}}{{end}}
{{with .Ask}}
这一轮用户给它的要求：
{{.}}
{{end}}
它停下时最后的回复：
{{.Reply}}
{{with .Others}}
同一台电脑上在工作的其他 Agent：
{{range .}}- id={{.ID}}：{{.Label}}{{with .Line}}（{{.}}）{{end}}
{{end}}{{end}}

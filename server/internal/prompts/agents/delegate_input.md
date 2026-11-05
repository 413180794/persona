现在：{{.Now}}
Agent：{{.Agent}}（{{.Machine}}{{with .Cwd}}，目录 {{.}}{{end}}）{{with .Project}}
项目：{{.}}{{end}}{{with .Task}}
它在做的任务：{{.}}{{end}}{{with .Decisions}}

这件事上用户拍过的板：
{{.}}{{end}}{{with .Asks}}

用户最近给它的要求（从早到晚）：
{{range .}}- {{.}}
{{end}}{{end}}{{with .Reply}}
它最近说的：
{{.}}
{{end}}
它在问：
{{range .Questions}}- {{.Question}}{{if .MultiSelect}}（可多选）{{end}}
{{range .Options}}  - {{.Label}}{{with .Description}}：{{.}}{{end}}
{{end}}{{end}}

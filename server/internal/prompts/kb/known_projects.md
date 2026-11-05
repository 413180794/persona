{{range .}}
- {{.Name}}{{with .Aliases}}（{{join . "、"}}）{{end}}{{with .OneLine}}：{{.}}{{end}}{{with .Pinned}}；已确认事实（只读，以此为准）：{{.}}{{end}}{{else}}（还没有）{{end}}

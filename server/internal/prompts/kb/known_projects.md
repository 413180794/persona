{{range .}}
- {{.Name}}{{with .Aliases}}（{{join . "、"}}）{{end}}{{with .OneLine}}：{{.}}{{end}}{{else}}（还没有）{{end}}

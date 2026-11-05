已知项目：{{template "kb/known_projects" .Projects}}
已有标签：{{join .Tags "、"}}

{{range .Items}}{{.Key}} | {{if eq .Kind "docs"}}文档{{else if eq .Kind "meetings"}}会议{{else if eq .Kind "events"}}日程{{end}} | {{.Title}} | {{if not .At.IsZero}}{{.At.Format "2006-01-02 15:04"}}{{end}} | {{.Where}}{{with .Pinned}} | 已确认事实（只读，以此为准）：{{.}}{{end}}
{{end}}

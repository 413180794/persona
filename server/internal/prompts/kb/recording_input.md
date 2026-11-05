{{with .Pinned}}已确认事实（只读，以此为准）：{{.}}
{{end}}录音：{{if .Title}}「{{.Title}}」，{{end}}{{.Start.Format "2006-01-02 15:04"}} 开始，约 {{.Minutes}} 分钟，{{if eq .Device "watch"}}手表{{else}}手机{{end}}录的{{with .Language}}，语言 {{.}}{{end}}
已知项目：{{template "kb/known_projects" .Projects}}
已有标签：{{join .Tags "、"}}

转写：
{{.Transcript}}

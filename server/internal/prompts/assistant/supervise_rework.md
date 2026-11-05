（督办：验收没通过，第 {{.Round}} 轮返工，最多 {{.Max}} 轮）
{{with .Checks}}逐条验收：
{{range .}}- {{if .Met}}✅{{else}}❌{{end}} {{.Criterion}}{{with .Evidence}}：{{.}}{{end}}
{{end}}{{end}}{{with .Note}}
要你做的：
{{.}}
{{end}}
把缺的补上。做完时逐条说明每条验收标准的证据（PR 链接、CI 结果、截图、线上验证的命令和输出），做不到的说清楚为什么、要谁决定。

任务：{{.Title}}

派活时的要求：
{{.Prompt}}

{{if eq (len .Replies) 1}}Agent 停下时最后的回复{{else}}各个 Agent 停下时最后的回复{{end}}：
{{range .Replies}}
--- {{.Machine}}
{{with .Ask}}用户后来给它的要求：
{{.}}

它的回复：
{{end}}{{.Text}}
{{end}}{{if or .MergedPR .Build}}
线索（只是提示）：回复里提到了{{if .MergedPR}}已合并的 PR{{end}}{{if and .MergedPR .Build}}、{{end}}{{if .Build}}TestFlight 构建号{{end}}。
{{end}}

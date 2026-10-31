{{with .Summary}}<jarvis-context kind="context" title="会话档案 · {{attr $.ChatName}}">
{{.}}
</jarvis-context>
{{end}}{{range .Projects}}<jarvis-context kind="context" title="项目 · {{attr .Name}}">
{{.Summary}}
</jarvis-context>
{{end}}

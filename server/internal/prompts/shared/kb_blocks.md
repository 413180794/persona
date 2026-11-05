{{with .Summary}}<jarvis-context kind="context" title="会话档案 · {{attr $.ChatName}}">
{{ctx .}}
</jarvis-context>
{{end}}{{range .Projects}}<jarvis-context kind="context" title="项目 · {{attr .Name}}">
{{ctx .Summary}}
</jarvis-context>
{{end}}

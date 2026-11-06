{{with .Summary}}<persona-context kind="context" title="会话档案 · {{attr $.ChatName}}">
{{ctx .}}
</persona-context>
{{end}}{{range .Projects}}<persona-context kind="context" title="项目 · {{attr .Name}}">
{{ctx .Summary}}
</persona-context>
{{end}}

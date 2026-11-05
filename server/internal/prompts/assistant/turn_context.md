<persona-context>
{{if .Full}}实时快照（这条消息发出时的；之后的变化用工具查）：{{else}}实时快照里和上一份比变了的（没列出的照旧）：{{end}}
{{range .Sections}}{{ctx .}}
{{end}}</persona-context>

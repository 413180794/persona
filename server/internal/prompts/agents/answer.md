{{if .ByAssistant}}关于你上面的问题，助手按我的规则先替我答了{{with .Reason}}（理由：{{.}}）{{end}}，我之后可能改答：{{else}}关于你上面的问题，我的回答：{{end}}
{{range .Items}}- {{.Question}} → {{.Answer}}
{{end}}

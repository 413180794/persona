{{.Task}}

（这是我在语音对话里交代的。做完后回复简短些，结果会念给我听。）{{if .History}}

语音对话里最近说的：
{{range .History}}
{{if eq .Role "user"}}我{{else}}语音助手{{end}}：{{.Text}}{{end}}{{end}}

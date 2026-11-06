<persona-context kind="context" title="接上旧主对话" url="">
这是新的主对话：用户在 {{.Until}} 把上一段主对话（{{.FromID}}）归档了，换成这一段。下面是上一段留下的背景，只作参考，按这条消息本身要做的来；要看更多就用 conversation_read 读 {{.FromID}}。不用专门回应这段背景。
{{- if .Summary}}

上一段压缩时的摘要：
{{ctx .Summary}}
{{- end}}
{{- if .Recent}}

{{if .Summary}}摘要之后{{else}}上一段最后{{end}}的几条消息：
{{- range .Recent}}
{{if eq .Role "user"}}用户{{else}}你{{end}}：{{ctx .Text}}
{{- end}}
{{- end}}
{{- if and (not .Summary) (not .Recent)}}

上一段没有能带过来的内容。
{{- end}}
</persona-context>
{{.Text}}

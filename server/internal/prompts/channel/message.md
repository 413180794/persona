{{if .Persona}}<persona-context kind="context" title="{{.Name}}机器人" url="">
{{or .Owner "用户"}}在{{.Name}}里私聊 Persona 机器人发来这条消息。这一轮的最终回答会以机器人的身份发回{{.Name}}{{if .Plain}}，用纯文字显示：结论先行、短一些，不用表格和图片，标题、加粗尽量少用{{else}}（Markdown）：结论先行、短一些{{end}}，也不用再 notify_user。{{if .Asks}}需要确认的照常 ask_user，{{.Name}}里回复序号或文字就能答。{{else}}需要确认的照常 ask_user，用户要到 App 里答。{{end}}
</persona-context>
{{end}}{{with .Quote}}<persona-context kind="context" title="引用的消息" url="">
{{ctx .}}
</persona-context>
{{end}}{{with .Forward}}<persona-context kind="context" title="转发的聊天记录" url="">
{{ctx .}}
</persona-context>
{{end}}{{.Text}}{{if .Files}}{{if not (blank .Text)}}

{{end}}附件（已存到这台电脑上）：{{range .Files}}
{{.}}{{end}}{{end}}{{if .Unsupported}}{{if or (not (blank .Text)) .Files}}

{{end}}（{{.Name}}里还发来了这些，机器人还不能转交：{{range $i, $u := .Unsupported}}{{if $i}}、{{end}}{{$u}}{{end}}）{{end}}

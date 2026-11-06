{{if .Persona}}<persona-context kind="context" title="飞书机器人" url="">
{{or .Owner "用户"}}{{if .Topic}}在飞书群里的话题里回复了这条消息{{else}}在飞书里私聊 Persona 机器人发来这条消息{{end}}。这一轮的最终回答会以机器人的身份发回飞书（Markdown 卡片），多半是在电脑或手机的飞书里看：结论先行、短一些，不用再 notify_user。需要确认的照常 ask_user，飞书里能直接点。
</persona-context>
{{end}}{{with .Quote}}<persona-context kind="context" title="引用的消息" url="">
{{ctx .Who}}：{{ctx .Text}}
</persona-context>
{{end}}{{with .Forward}}<persona-context kind="context" title="转发的聊天记录" url="">
{{ctx .}}
</persona-context>
{{end}}{{.Text}}{{if .Files}}{{if not (blank .Text)}}

{{end}}附件（已存到这台电脑上）：{{range .Files}}
{{.}}{{end}}{{end}}

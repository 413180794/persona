{{if eq .Status "done"}}（Jarvis 主对话处理完了你刚才交代的「{{.Task}}」。）它的回复是：
{{or .Text "（没有文字回复）"}}

用一两句话口头告诉用户结果，不要念原文。{{else if eq .Status "needs_you"}}（Jarvis 主对话在处理「{{.Task}}」时需要用户回答或批准，在手机上的 Jarvis 主对话里。）简短告诉用户去看一下。{{else if eq .Status "failed"}}（交给 Jarvis 主对话没成功：{{.Text}}）简短告诉用户。{{else}}（Jarvis 主对话处理「{{.Task}}」超过 20 分钟还没做完。）简短告诉用户还在做，做完可以在主对话里看。{{end}}

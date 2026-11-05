{{- if eq .Lang "en" -}}
{{- $to := "the Persona main conversation"}}{{if .Agent}}{{$to = "the agent"}}{{end -}}
{{- if eq .Code "unprompted" -}}
"{{.Task}}" was not sent to {{$to}}: the user had not spoken since the last thing you were told about an agent's work, or not in the last half minute, so for safety it was not sent. Tell the user in one sentence that this did not come from them, so you did not send it; if they want it done, they can say it again.
{{- else if eq .Code "empty" -}}
It was not clear what to hand to {{$to}}, so nothing was sent. Ask the user in one sentence to say it again.
{{- else if eq .Code "unknown" -}}
Not sure whether "{{.Task}}" reached {{$to}}: the connection broke off while sending. Do not hand it over again; ask the user in one sentence to look at it on the phone.
{{- else if eq .Code "unreachable" -}}
"{{.Task}}" was not sent to {{$to}}: Persona could not be reached. Tell the user in one sentence that it was not sent.
{{- else if .Reason -}}
"{{.Task}}" was not sent to {{$to}}: {{.Reason}}
Tell the user in one sentence that it was not sent, and why.
{{- else -}}
"{{.Task}}" was not sent to {{$to}}. Tell the user in one sentence that it was not sent.
{{- end -}}
{{- else if eq .Lang "ja" -}}
{{- $to := "Persona のメイン会話"}}{{if .Agent}}{{$to = "エージェント"}}{{end -}}
{{- if eq .Code "unprompted" -}}
「{{.Task}}」は{{$to}}に送っていません：エージェントの作業内容を最後に伝えてからユーザーが話していない（または最後に話してから 30 秒以上たっている）ため、安全のため送りませんでした。「それはあなたが言ったことではないので送っていません。やるならもう一度言ってください」と一文でユーザーに伝えてください。
{{- else if eq .Code "empty" -}}
{{$to}}に何を頼むのか聞き取れなかったので、何も送っていません。もう一度言ってもらうよう一文でユーザーに頼んでください。
{{- else if eq .Code "unknown" -}}
「{{.Task}}」が{{$to}}に届いたかわかりません：送信の途中で接続が切れました。もう一度頼まず、スマホで確認するよう一文でユーザーに伝えてください。
{{- else if eq .Code "unreachable" -}}
「{{.Task}}」は{{$to}}に送っていません：Persona につながりませんでした。送れなかったことを一文でユーザーに伝えてください。
{{- else if .Reason -}}
「{{.Task}}」は{{$to}}に送っていません：{{.Reason}}
送れなかったことと理由を一文でユーザーに伝えてください。
{{- else -}}
「{{.Task}}」は{{$to}}に送っていません。送れなかったことを一文でユーザーに伝えてください。
{{- end -}}
{{- else -}}
{{- $to := "Persona 主对话"}}{{if .Agent}}{{$to = "Agent"}}{{end -}}
{{- if eq .Code "unprompted" -}}
「{{.Task}}」没有发给 {{$to}}：上次把 Agent 做的事告诉你以后用户还没说过话（或者用户上一次说话已经是半分钟以前），为了安全没有发出去。用一句话告诉用户：这条不是你说的，我没发；要做的话请再说一遍。
{{- else if eq .Code "empty" -}}
没听清要交给 {{$to}}的是什么，没有发出去。用一句话请用户再说一遍。
{{- else if eq .Code "unknown" -}}
「{{.Task}}」不确定有没有发到 {{$to}}：发送的途中连接断了。不要再交代一次，用一句话请用户去手机上看一下。
{{- else if eq .Code "unreachable" -}}
「{{.Task}}」没有发给 {{$to}}：连不上 Persona。用一句话告诉用户没发出去。
{{- else if .Reason -}}
「{{.Task}}」没有发给 {{$to}}：{{.Reason}}
用一句话告诉用户没发出去，以及原因。
{{- else -}}
「{{.Task}}」没有发给 {{$to}}。用一句话告诉用户没发出去。
{{- end -}}
{{- end -}}

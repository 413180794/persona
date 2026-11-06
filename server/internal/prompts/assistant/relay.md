<persona-context kind="context" title="来自对话「{{attr .FromTitle}}」" url="">
这条消息不是用户本人发的，是 Persona 的另一段对话（{{.FromID}}）用 {{.Tool}} 发来的，第 {{.Hop}} 跳（最多 {{.MaxHops}} 跳）。
按里面说的做。要把结果告诉那段对话就用 conversation_send 发回 {{.FromID}}；只是确认收到就不要回，免得两段对话来回发。
</persona-context>
{{.Text}}

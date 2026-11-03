<jarvis-context kind="context" title="请求的位置">
位置：{{if not .Known}}会话{{else if eq .Kind "dm"}}私聊{{else if eq .Kind "group"}}群聊{{else if eq .Kind "bot"}}机器人会话{{end}}{{if .InThread}}里的一个话题{{end}}「{{.Chat}}」（feed_id {{.FeedID}}）；请求消息 id {{.MessageID}}，{{.At.Format "2006-01-02 15:04"}} 发出。{{with .KB}}
知识库里这个会话的 id：{{.ChatID}}（kb_get / kb_search 查更多）。{{end}}
</jarvis-context>
{{with .Transcript}}<jarvis-context kind="context" title="{{if $.InThread}}这个话题里的消息{{else}}前面的聊天{{end}}">
{{.}}</jarvis-context>
{{end}}{{with .Quote}}<jarvis-context kind="context" title="请求引用的消息">
{{.Sender}}：{{.Text}}
</jarvis-context>
{{end}}{{range .Docs}}{{if .Err}}<jarvis-context kind="doc" title="文档" url="{{attr .URL}}">
（读不到：{{.Err}}）
</jarvis-context>
{{else}}<jarvis-context kind="doc" title="文档 · {{attr .Title}}" url="{{attr .URL}}">
{{.Markdown}}
</jarvis-context>
{{end}}{{end}}{{with .KB}}{{template "shared/kb_blocks" .}}{{end}}

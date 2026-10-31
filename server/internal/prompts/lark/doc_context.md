<jarvis-context kind="context" title="请求的位置">
位置：{{if eq .Type 3}}电子表格{{else if eq .Type 8}}多维表格{{else if eq .Type 16}}知识库文档{{else}}文档{{end}}「{{.Title}}」里的一条评论（{{.URL}}）；评论 id {{.CommentID}}。
</jarvis-context>
{{with .Quote}}<jarvis-context kind="context" title="评论划选的原文">
{{.}}
</jarvis-context>
{{end}}<jarvis-context kind="context" title="这条评论里的讨论">
{{range $i, $l := .Lines}}{{if $i}}
{{end}}{{template "lark/line" $l}}{{end}}
</jarvis-context>
{{if .DocErr}}（文档正文读不到：{{.DocErr}}；需要的话用 lark-connector 的 documents/read 自己读，表格 / 多维表格按它们的接口读。）
{{else}}<jarvis-context kind="doc" title="文档 · {{attr .DocTitle}}" url="{{attr .URL}}">
{{.Markdown}}
</jarvis-context>
{{end}}

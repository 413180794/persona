<persona-context kind="context" title="待办 · {{attr .Title}} · 已定的决策" url="">
这件事上用户已经拍过板的决定（只列现在生效的，按时间先后）。照这些做，不要再问同样的问题；要改其中哪一条，先问用户。
{{range .Decisions}}
{{.N}}. {{.Question}}
   选了：{{.Choice}}{{if .Reason}}
   原因：{{.Reason}}{{end}}
   （{{.When}}{{with .From}}，{{.}}{{end}}）
{{end}}</persona-context>

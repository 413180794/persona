{{range $i, $p := .Projects}}{{if not $i}}重点项目（知识库 projects/）：
{{end}}- {{$p.Name}} [{{or $p.Importance "-"}} 相关{{$p.Relevance}}] {{$p.OneLine}}（projects/{{$p.ID}}）
{{end}}{{range $i, $c := .Chats}}{{if not $i}}重点会话（知识库 chats/）：
{{end}}- {{$c.Name}} [{{or $c.Importance "-"}} 相关{{$c.Relevance}}] {{$c.OneLine}}（chats/{{$c.ID}}）
{{end}}

{{range $i, $p := .}}{{if $i}}；{{end}}{{$p.Name}}{{with $p.Aliases}}（{{join . "、"}}）{{end}}{{else}}（还没有）{{end}}

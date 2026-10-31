{{range $i, $a := first 6 .}}{{if $i}}；{{end}}{{$a.Label}}（{{$a.Machine}}，session_id={{$a.Session}}）{{end}}{{if gt (len .) 6}} 等 {{len .}} 个{{end}}

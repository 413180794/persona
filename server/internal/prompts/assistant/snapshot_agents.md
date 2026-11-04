{{range $i, $a := first 6 .}}{{if $i}}；{{end}}{{$a.Label}}（{{$a.Machine}}，session_id={{$a.Session}}）{{with $a.Line}}：{{.}}{{end}}{{end}}{{if gt (len .) 6}} 等 {{len .}} 个{{end}}

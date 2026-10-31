{{.At.Format "01-02 15:04"}} {{.Who}}{{if .Mine}}（我）{{end}}：{{if .Recalled}}[已撤回]{{else}}{{.Text}}{{end}}{{if .Request}}  ← 请求{{end}}

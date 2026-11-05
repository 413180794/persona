「{{.Name}}」{{with .Phase}}阶段 {{.}}{{if $.PhaseIndex}}（{{$.PhaseIndex}}/{{$.Phases}}）{{end}}，{{end}}{{.Done}}/{{.Started}} 个 agent 完成{{with .For}}，已跑 {{.}}{{end}}

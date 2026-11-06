<persona-context kind="workspace" title="{{attr .Title}}" url="" machine="{{attr .Machine}}" cwd="{{attr .Cwd}}">
用户 @ 了这个工作区：这条消息说的事默认就在这里。派活（task_plan、agent_start）时 machine 填 {{ctx .Machine}}、cwd 填 {{ctx .Cwd}}，查代码、看 agent、回答也以这里为准；用户在消息里另说了地方时听用户的。
工作区：{{ctx .Title}}
电脑：{{ctx .MachineName}}（{{ctx .Machine}}）{{if not .Online}}，现在离线{{end}}
目录：{{ctx .Cwd}}
{{with .Branch}}分支：{{ctx .}}
{{end}}{{if .Projects}}知识库里的项目：
{{range .Projects}}- {{ctx .Name}}（{{ctx .Slug}}）{{with .OneLine}}：{{ctx .}}{{end}}
{{end}}{{end}}{{if .Sessions}}最近在这个目录的 agent（新的在前，agent_read 可看）：
{{range .Sessions}}- 「{{ctx .Title}}」{{ctx .Agent}}，{{ctx .Status}}，{{.When}}，session {{ctx .ID}}{{with .Cwd}}，在 {{ctx .}}{{end}}
{{end}}{{end}}</persona-context>

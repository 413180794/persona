The hub's health check found launchers that do not work on the machine {{.Machine}}{{if .Name}} ({{.Name}}){{end}}{{if .SSHHost}}, reachable as `ssh {{.SSHHost}}`{{end}}. Repair them with the {{.Skill}} skill (it installs and configures Claude Code and Codex and connects them to their gateway).

{{range .Launchers}}- `{{.Name}}` ({{.Agent}}), {{.Kind}}: {{.Error}}
{{end}}
What the classes usually mean: model_route: the gateway does not accept the model name the launcher sends (set the gateway's model names again); auth: the gateway key is missing, expired or rejected (configure the key again); cli_outdated: the CLI is too old (upgrade it); timeout / other: find out why first.

Work only on that machine's agent CLI setup: don't touch its projects, and don't push, deploy or message anyone. When you are done, run each launcher once there non-interactively (for Claude Code: `<launcher> -p "Reply with exactly one word: ok" --max-turns 1`) and finish with one line per launcher: fixed, or why it is still broken. The hub checks them again after you stop.

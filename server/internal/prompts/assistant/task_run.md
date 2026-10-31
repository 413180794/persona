⏰ {{.Label}}{{if .Once}}（你之前安排的跟进）{{else if .Interval}}（定时任务，每 {{.Every}}）{{else}}（定时任务）{{end}}{{if .Late}}
（本该 {{.Due.Format "01-02 15:04"}} 运行，Jarvis 当时没在运行，晚了 {{.Late}}；看看还有没有必要按原计划做。）{{end}}{{if .Interval}}
（Jarvis 会按时再运行它，不用再用 schedule_followup 安排下一次。）{{end}}

{{.Prompt}}{{if .KBHint}}

{{template "assistant/routine_kb_hint" .}}{{end}}

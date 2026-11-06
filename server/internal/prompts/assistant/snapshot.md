{{- $w := .Now.Weekday -}}
- 现在：{{.Now.Format "2006-01-02"}} 周{{if eq $w 0}}日{{else if eq $w 1}}一{{else if eq $w 2}}二{{else if eq $w 3}}三{{else if eq $w 4}}四{{else if eq $w 5}}五{{else}}六{{end}} {{.Now.Format "15:04"}}（{{.Zone}}）
- 在线的电脑：{{range $i, $m := .Up}}{{if $i}}、{{end}}{{$m.Name}}（{{$m.ID}}）{{end}}{{with .Down}}；离线：{{range $i, $m := .}}{{if $i}}、{{end}}{{$m.Name}}（{{$m.ID}}）{{end}}{{end}}
- Agents：需要你 {{len .Needs}} 个，完成未看 {{len .Unread}} 个，工作中 {{len .Working}} 个（详情用 agents_list）
{{with .Needs}}  - 需要你：{{template "assistant/snapshot_agents" .}}
{{end}}{{with .Unread}}  - 完成未看：{{template "assistant/snapshot_agents" .}}
{{end}}{{with .Waiting}}  - 在等的（hub 到时会叫醒它们，不用管）：{{template "assistant/snapshot_agents" .}}
{{end}}{{with .Done}}  - 做完的（24 小时内）：{{template "assistant/snapshot_agents" .}}
{{end}}{{with .Working}}  - 工作中：{{join (first 8 .) "；"}}{{if gt (len .) 8}} 等 {{len .}} 个{{end}}
{{end}}{{with .Workflows}}  - 后台 workflow 在跑（这些 Agent 这一轮停着是在等结果，不是做完了，结果回来会叫醒它们；别关它们）：{{range $i, $w := first 6 .}}{{if $i}}；{{end}}{{$w.Agent}}（{{$w.Machine}}，session_id={{$w.Session}}）的{{template "assistant/snapshot_workflow" $w}}{{end}}{{if gt (len .) 6}} 等 {{len .}} 个{{end}}
{{end}}{{with .Tasks}}- 从对话开的任务（task_plan；用 agent_send 对它的 Agent 说话）：
{{range .}}{{$t := .}}  - {{.Title}}（{{.ID}}{{with .Link}}，链接 {{.}}{{end}}）：{{.State}}{{with .Health}}；督办：{{.}}{{if $t.DoneWhen}}，验收标准 {{$t.DoneWhen}} 条{{else}}，没写验收标准{{end}}{{end}}{{with .Machine}}，{{.}}{{end}}{{with .Session}} session_id={{.}}{{end}}{{with .Last}}：{{.}}{{end}}{{with .Doing}}{{if $t.Last}}；{{else}}：{{end}}现在{{.}}{{with $t.DoingFor}}（{{.}}了）{{end}}{{end}}{{with .Subagents}}；子 Agent 在跑：{{join . "、"}}{{end}}{{with .Workflows}}；后台 workflow 在跑（在等它的结果，没做完）：{{range $i, $w := .}}{{if $i}}、{{end}}{{template "assistant/snapshot_workflow" $w}}{{end}}{{end}}{{with .Delivery}}；交付：{{.}}{{end}}
{{end}}{{end}}{{with .Approvals}}- 等用户点头才能上线的（{{len .}} 件；合到一起一次列给用户批，别一件件问）：{{range $i, $a := .}}{{if $i}}；{{end}}「{{$a.Title}}」（{{$a.ID}}{{with $a.Link}}，链接 {{.}}{{end}}）{{$a.Stage}}，{{$a.Gate}}{{end}}
{{end}}{{with .Unaccepted}}- 等用户验收的（已部署，飞书里发过「请验收」，还没回「通过」）：{{range $i, $a := .}}{{if $i}}；{{end}}「{{$a.Title}}」：{{$a.Changed}}{{with $a.Link}}（{{.}}）{{end}}，{{$a.Since.Format "01-02"}} 起{{end}}{{if gt $.UnacceptedTotal (len .)}} 等 {{$.UnacceptedTotal}} 个{{end}}
{{end}}{{if .PhoneOnline}}- iPhone：App 在线，phone_* 工具可用
{{else}}- iPhone：App 不在前台，phone_* 工具会先尝试在后台唤醒 App，失败就请他打开 App
{{end}}{{with .KB}}- 知识库（kb_get 看详情）：
{{indent "  " .}}{{end}}{{with .Chats}}{{if .Total}}- 飞书未读会话 {{.Total}} 个（免打扰的不算；用 lark_chats / lark_messages 看）
{{with .AtMe}}  - @我：{{range $i, $c := first 6 .}}{{if $i}}；{{end}}{{$c.Name}}（{{$c.Unread}} 条）：{{$c.Preview}}{{end}}
{{end}}{{with .Other}}  - 其他：{{range $i, $c := first 8 .}}{{if $i}}、{{end}}{{$c.Name}}（{{$c.Unread}} 条）{{end}}
{{end}}{{else}}- 飞书：没有未读（免打扰的不算）
{{end}}{{end}}{{with .Upcoming}}{{if .Events}}- 飞书日历今天接下来：{{range $i, $e := .Events}}{{if $i}}；{{end}}{{$e.Start.Format "15:04"}}–{{$e.End.Format "15:04"}} {{$e.Title}}{{end}}
{{else}}- 飞书日历：今天接下来没有会
{{end}}{{end}}- 已安排的跟进：{{with .Followups}}{{range $i, $f := .}}{{if $i}}；{{end}}{{$f.At.Format "01-02 15:04"}} {{$f.Label}}{{with $f.Every}}（每 {{.}}）{{end}}{{end}}{{else}}没有{{end}}
{{with .Routines}}- 每天的定时任务：{{range $i, $r := .}}{{if $i}}；{{end}}{{$r.Label}}（{{$r.Rule}}）{{if not $r.LastAt.IsZero}}，上次{{$r.LastAt.Format "01-02 15:04"}}{{if eq $r.LastStatus "ok"}} 成功{{else if eq $r.LastStatus "failed"}} 失败{{with $r.LastWhy}}：{{.}}{{end}}{{else if eq $r.LastStatus "skipped"}} 跳过{{with $r.LastWhy}}：{{.}}{{end}}{{end}}{{end}}{{end}}{{if gt $.RoutinesTotal (len .)}} 等 {{$.RoutinesTotal}} 个{{end}}
{{end}}{{with .Missed}}- 没成功的定时任务（昨天 18:00 以来；汇报时要说）：{{range $i, $m := .}}{{if $i}}；{{end}}{{$m.At.Format "01-02 15:04"}} {{$m.Label}}{{if $m.Skipped}}跳过了{{else}}失败了{{end}}{{with $m.Why}}：{{.}}{{end}}{{end}}{{if gt $.MissedTotal (len .)}} 等 {{$.MissedTotal}} 次{{end}}
{{end}}

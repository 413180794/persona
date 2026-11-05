{{- /* Stand-ins for what an answer holds that is not to be read out (voice/spoken.go): the voice model hears that it is
there and that the screen has it. */ -}}
{{- if eq .Lang "en" -}}
{{- if eq .Kind "code"}}(a code block of {{.N}} {{if eq .N 1}}line{{else}}lines{{end}}, on the screen)
{{- else if eq .Kind "table"}}(a table of {{.N}} {{if eq .N 1}}row{{else}}rows{{end}}, on the screen)
{{- else if eq .Kind "link"}}(a link)
{{- else if eq .Kind "image"}}(an image)
{{- else if eq .Kind "path"}}the file {{.Name}}
{{- else if eq .Kind "line"}}line {{.N}} of {{.Name}}
{{- else if eq .Kind "hash"}}(a hash)
{{- else if eq .Kind "snippet"}}(a piece of code)
{{- else}}(… the middle is left out; the whole answer is on the screen …)
{{- end -}}
{{- else if eq .Lang "ja" -}}
{{- if eq .Kind "code"}}（{{.N}} 行のコード、画面に表示）
{{- else if eq .Kind "table"}}（{{.N}} 行の表、画面に表示）
{{- else if eq .Kind "link"}}（リンク）
{{- else if eq .Kind "image"}}（画像）
{{- else if eq .Kind "path"}}ファイル {{.Name}}
{{- else if eq .Kind "line"}}{{.Name}} の {{.N}} 行目
{{- else if eq .Kind "hash"}}（ハッシュ）
{{- else if eq .Kind "snippet"}}（コード）
{{- else}}（……中略、全文は画面に……）
{{- end -}}
{{- else -}}
{{- if eq .Kind "code"}}（一段 {{.N}} 行的代码，在屏幕上）
{{- else if eq .Kind "table"}}（一张 {{.N}} 行的表，在屏幕上）
{{- else if eq .Kind "link"}}（一个链接）
{{- else if eq .Kind "image"}}（一张图）
{{- else if eq .Kind "path"}}文件 {{.Name}}
{{- else if eq .Kind "line"}}文件 {{.Name}} 第 {{.N}} 行
{{- else if eq .Kind "hash"}}（一串哈希）
{{- else if eq .Kind "snippet"}}（一段代码）
{{- else}}（……中间省略，全文在屏幕上……）
{{- end -}}
{{- end -}}

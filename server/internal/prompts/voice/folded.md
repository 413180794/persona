{{- /* Part of voice/report: the earlier tasks of a folded run (sent while it worked) were handed over on calls of
their own, which get no result of their own: nothing goes on those calls (docs/lessons/voice.md row 4: anything on an
open call has the model speak again), this answer covers them. Data: VoiceReport. */ -}}
{{- $n := len .Tasks -}}
{{- if eq .Lang "en" -}}
This result covers what was handed over before too ({{range $i, $t := .Tasks}}{{if lt (inc $i) $n}}{{if $i}} and {{end}}"{{$t}}"{{end}}{{end}}): it went to the same conversation and is taken together here; that earlier call gets no result of its own, so do not wait for one.
{{- else if eq .Lang "ja" -}}
この結果は先に渡した{{range $i, $t := .Tasks}}{{if lt (inc $i) $n}}{{if $i}}と{{end}}「{{$t}}」{{end}}{{end}}の分も兼ねています。同じ会話に渡され、ここでまとめて扱っているので、そのときの呼び出しには別の結果は返りません。待たなくて大丈夫です。
{{- else -}}
这条结果也包括之前交代的{{range $i, $t := .Tasks}}{{if lt (inc $i) $n}}{{if $i}}和{{end}}「{{$t}}」{{end}}{{end}}：交给了同一个对话，一起在这里，之前那次调用不会再单独有结果，不用再等。
{{- end -}}

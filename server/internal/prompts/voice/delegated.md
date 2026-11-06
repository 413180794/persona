{{- if eq .Lang "en" -}}
{{.Task}}

<persona-context kind="context" title="Voice conversation" url="">
I asked for this in a voice conversation and the voice assistant passed it on, so it may be misheard: ask me when something is unclear. Keep the reply short when you are done: it is read out to me.{{if .History}}

Said in the voice conversation just before:{{range .History}}
{{if eq .Role "user"}}Me{{else}}Voice assistant{{end}}: {{ctx .Text}}{{end}}{{end}}
</persona-context>
{{- else if eq .Lang "ja" -}}
{{.Task}}

<persona-context kind="context" title="音声会話" url="">
音声会話で頼んだことを音声アシスタントが伝えています。聞き間違いがあるかもしれないので、はっきりしないときは確認してください。終わったら返事は短めに。読み上げて聞きます。{{if .History}}

音声会話で直前に話したこと：{{range .History}}
{{if eq .Role "user"}}私{{else}}音声アシスタント{{end}}：{{ctx .Text}}{{end}}{{end}}
</persona-context>
{{- else -}}
{{.Task}}

<persona-context kind="context" title="语音对话" url="">
这是我在语音对话里交代的，由语音助手转述，可能有识别错误，拿不准时先问我。做完后回复简短些，结果会念给我听。{{if .History}}

语音对话里刚才说的：{{range .History}}
{{if eq .Role "user"}}我{{else}}语音助手{{end}}：{{ctx .Text}}{{end}}{{end}}
</persona-context>
{{- end -}}

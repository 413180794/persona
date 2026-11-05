package prompts

// RenderJSONOnly is a system prompt with the line asking for a bare JSON object after it (tools/json_only).
func RenderJSONOnly(system string) string { return render("tools/json_only", system) }

// RenderTabName names a new agent's herdr tab after its first message (tools/tab_name).
func RenderTabName() string { return render("tools/tab_name", nil) }

// RenderTranslateTexts and RenderTranslateMail translate into lang, the language's own name ("简体中文"): a JSON list of short
// texts (tools/translate_texts) or one mail (tools/translate_mail).
func RenderTranslateTexts(lang string) string { return render("tools/translate_texts", lang) }
func RenderTranslateMail(lang string) string  { return render("tools/translate_mail", lang) }

// RenderTranslateMailInput is the mail as the translation model reads it (tools/translate_mail_input).
func RenderTranslateMailInput(subject, text string) string {
	return render("tools/translate_mail_input", struct{ Subject, Text string }{subject, text})
}

// RenderLauncherCheck is the one-word prompt 设置 › 模型's 测试可用性 runs a launcher with (tools/launcher_check).
func RenderLauncherCheck() string { return render("tools/launcher_check", nil) }

// SessionTitle is the system prompt naming an assistant conversation after its first turn (tools/session_title):
// "类型 | 主题". The 类型 words stay Chinese whatever the language (the assistant parses them, titleKinds).
type SessionTitle struct {
	// Language is the topic's language by its own name ("English", "日本語"); "" is Chinese, as before i18n.
	Language string
	TopicMax int // the topic's characters with a Language (Chinese's 14 stays in the template)
}

func RenderSessionTitle(t SessionTitle) string { return render("tools/session_title", t) }

// SessionTitleInput is what the title model reads: the first exchange, and its last answer when that broke the format.
type SessionTitleInput struct{ First, Reply, Retry string }

func RenderSessionTitleInput(in SessionTitleInput) string {
	return render("tools/session_title_input", in)
}

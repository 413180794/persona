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

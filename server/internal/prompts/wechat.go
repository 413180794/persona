package prompts

// WeChatMessage is a message the user sent the WeChat bot, as it goes into a Persona conversation (wechat/message):
// a note that the answer goes back to WeChat as plain text, the message it quotes, and what came with it that the bot
// cannot pass on (a file, a video).
type WeChatMessage struct {
	Text        string
	Quote       string
	Unsupported []string
}

func RenderWeChatMessage(m WeChatMessage) string { return render("wechat/message", m) }

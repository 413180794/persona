package prompts

// ChannelMessage is a message the owner sent a chat app's bot (a plugin's channel, docs/channels.md), as it goes into
// a conversation (channel/message): a note that the answer goes back there and how it shows, the message it quotes, a
// forwarded chat, the files saved on the computer, and what came with it that the bot cannot pass on.
type ChannelMessage struct {
	Persona bool   // a Persona conversation (not a coding agent): add the note about answering in the chat app
	Name    string // the channel's name: 微信
	Owner   string // "" is 用户
	Plain   bool   // the chat app shows plain text
	Asks    bool   // what is asked can be answered there
	Text    string
	Quote   string
	Forward string
	Files   []string
	// Unsupported are what came that cannot be passed on (「文件「合同.pdf」」「一段视频」).
	Unsupported []string
}

func RenderChannelMessage(m ChannelMessage) string { return render("channel/message", m) }

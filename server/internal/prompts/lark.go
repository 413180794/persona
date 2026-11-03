package prompts

import (
	"strings"
	"time"
)

// LarkAgent is the first message of a Feishu /agent conversation: the request in a chat (lark/chat_agent) or in a
// document comment (lark/doc_agent).
type LarkAgent struct {
	Owner   string // "" is 我
	Title   string // the chat's / document's name
	Task    string // what was asked after /agent; "" lets the agent work it out
	Prefix  string // what starts a request ("/agent"): the message's own text when nothing else was written
	Context string // RenderLarkChatContext / RenderLarkDocContext
	Resumed bool   // the same thread asked before: the conversation goes on
}

// The instructions and every piece of context are <jarvis-context> blocks; the only text outside them, at the end,
// is what the user wrote, so the apps show just that as the message and fold the rest (docs/lark-agent.md).

func RenderLarkChatAgent(a LarkAgent) string { return render("lark/chat_agent", a) }
func RenderLarkDocAgent(a LarkAgent) string  { return render("lark/doc_agent", a) }

// LarkLine is one message in a transcript (lark/line).
type LarkLine struct {
	At       time.Time // local time
	Who      string
	Mine     bool
	Text     string
	Recalled bool
	Request  bool // the /agent request itself
}

// RenderLarkTranscript is one line per message, oldest first, ending with a newline: the newest lines that fit in
// limit bytes.
func RenderLarkTranscript(lines []LarkLine, limit int) string {
	var out []string
	n := 0
	for i := len(lines) - 1; i >= 0; i-- {
		l := render("lark/line", lines[i])
		n += len(l) + 1
		if n > limit {
			break
		}
		out = append([]string{l}, out...)
	}
	return strings.Join(out, "\n") + "\n"
}

// LarkQuote is the message a request quotes.
type LarkQuote struct {
	Sender, Text string
}

// LarkDoc is a document linked around a request: its Markdown, or why it could not be read.
type LarkDoc struct {
	URL, Title, Markdown string
	Err                  string
}

// KBProject and KBContext are what the knowledge base knows about a chat (shared/kb_blocks).
type KBProject struct {
	Name, Summary string
}

type KBContext struct {
	ChatID, ChatName string
	Summary          string // "" leaves the chat's block out
	Projects         []KBProject
}

// LarkChatContext is where a chat /agent request was made and what was around it (lark/chat_context).
type LarkChatContext struct {
	Known      bool   // the knowledge base knows the chat: Kind is its kind
	Kind       string // dm | group | bot | …
	InThread   bool
	Chat       string
	FeedID     string
	MessageID  string
	At         time.Time // local time
	Transcript string    // RenderLarkTranscript; "" when no messages were read
	Quote      *LarkQuote
	Docs       []LarkDoc
	KB         *KBContext
}

func RenderLarkChatContext(c LarkChatContext) string { return render("lark/chat_context", c) }

// LarkDocContext is the document comment a /agent request was made in (lark/doc_context).
type LarkDocContext struct {
	Type      int // drive type: 3 sheet, 8 base, 16 wiki, else a document
	Title     string
	URL       string
	CommentID string
	Quote     string // the selection the comment is on; "" for a comment on the whole document
	Lines     []LarkLine
	DocTitle  string
	Markdown  string
	DocErr    string // the document could not be read
}

func RenderLarkDocContext(c LarkDocContext) string { return render("lark/doc_context", c) }

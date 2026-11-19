package assistant

import (
	"strings"

	"github.com/nwverse/persona/server/internal/assistant/chatsearch"
	"github.com/nwverse/persona/server/internal/compat"
	"github.com/nwverse/persona/server/internal/lazyre"
)

var (
	quoteAttachment = lazyre.New(`(?is)<attached_file\b[^>]*>(?:.*?</attached_file>)?`)
	// An older brief may have been cut before the end of an attachment or context block.
	quoteOpenBlock = lazyre.New(`(?is)<(?:attached_images|persona-view-errors|` + compat.ContextPattern() + `)\b.*$`)
	// Match local absolute and home-relative paths at a token boundary, keeping web URLs and slash commands.
	quoteLocalPath = lazyre.New("(^|[^A-Za-z0-9_:/\\\\.-])(?:file://|~/|[A-Za-z]:[/\\\\]|/(?:[A-Za-z0-9_.-]+[/\\\\]|[A-Za-z0-9_-]+\\.))" +
		"[^\\s`'\"<>|()\\[\\]，。！？；、：‘’“”]*") // i18n-ignore: punctuation delimiting path tokens
)

// cleanUserQuote keeps the owner's words safe to freeze in a brief and quote in a card.
func cleanUserQuote(text string) string {
	text = chatsearch.UserText(text)
	text = quoteAttachment.Regexp().ReplaceAllString(text, "")
	text = quoteOpenBlock.Regexp().ReplaceAllString(text, "")
	return strings.TrimSpace(quoteLocalPath.Regexp().ReplaceAllString(text, "$1"))
}

package wechat

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/channel"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/wechat/ilink"
)

const (
	maxSeen     = 200
	strangerGap = 10 * time.Minute
	// typingFor: 「正在输入」 is let go when the hub has not asked for it again in this long (it asks every 5 s while a
	// turn goes on): a hub that went away does not leave it on.
	typingFor = 30 * time.Second
)

// handle takes one message the bot got: someone else's gets one line saying whose bot it is; the owner's goes to the
// hub, which knows what it is (a command, an answer to what was asked, a message for the conversation).
func (b *Bot) handle(ctx context.Context, c *ilink.Client, creds ilink.Credentials, m ilink.Message) {
	owner := m.From == creds.UserID
	b.mu.Lock()
	if slices.Contains(b.st.Seen, m.ID) {
		b.mu.Unlock()
		return
	}
	b.st.Seen = append(b.st.Seen, m.ID)
	if len(b.st.Seen) > maxSeen {
		b.st.Seen = b.st.Seen[len(b.st.Seen)-maxSeen:]
	}
	b.status.LastEvent = b.opt.Now()
	if owner {
		b.st.Context = m.ContextToken
	}
	b.saveLocked(ctx)
	b.mu.Unlock()
	if !owner {
		b.stranger(ctx, c, m)
		return
	}
	msg := b.compose(ctx, c, m)
	if msg.Empty() {
		return
	}
	if b.opt.Hub == nil {
		log.Printf("wechat: no hub to hand the message to")
		return
	}
	if err := b.opt.Hub.Receive(ctx, msg); err != nil {
		log.Printf("wechat: handing a message to the hub: %v", err)
		b.send(ctx, c, m.From, m.ContextToken, i18n.T(b.lang(), "wechat-chat.hub_error"))
	}
}

// stranger: the bot only serves the WeChat that bound it; anyone else hears so, at most once in ten minutes.
func (b *Bot) stranger(ctx context.Context, c *ilink.Client, m ilink.Message) {
	b.mu.Lock()
	last, ok := b.replied[m.From]
	now := b.opt.Now()
	if ok && now.Sub(last) < strangerGap {
		b.mu.Unlock()
		return
	}
	b.replied[m.From] = now
	for who, at := range b.replied {
		if now.Sub(at) >= strangerGap {
			delete(b.replied, who)
		}
	}
	b.mu.Unlock()
	b.send(ctx, c, m.From, m.ContextToken, i18n.T(b.lang(), "wechat-chat.stranger"))
}

// send sends text to peer as the bot: WeChat's subset of Markdown, in pieces it takes. A failure is logged (never
// the text).
func (b *Bot) send(ctx context.Context, c *ilink.Client, peer, contextToken, text string) bool {
	if c == nil || contextToken == "" {
		log.Printf("wechat: nothing to send with (no client or context token)")
		return false
	}
	for _, part := range ilink.Chunk(ilink.FilterMarkdown(strings.TrimSpace(text)), 0) {
		var err error
		for attempt := range 3 {
			if err = c.SendText(ctx, peer, contextToken, clientID(), part); err == nil || !ilink.Retryable(err) {
				break
			}
			if !sleep(ctx, time.Duration(attempt+1)*time.Second) {
				return false
			}
		}
		if err != nil {
			log.Printf("wechat: sending a reply: %v", err)
			return false
		}
	}
	return true
}

// Send is the hub's channel.send: text to the owner, in the chat of their latest message. Before the owner has
// written to the bot there is no context token, and nothing can be sent: channel.ErrCannotSend.
func (b *Bot) Send(ctx context.Context, p channel.SendParams) (channel.SendResult, error) {
	b.mu.Lock()
	c, creds, contextToken := b.client, b.st.Creds, b.st.Context
	b.mu.Unlock()
	if creds == nil || c == nil || contextToken == "" {
		return channel.SendResult{}, channel.ErrCannotSend
	}
	if !b.send(ctx, c, creds.UserID, contextToken, p.Text) {
		return channel.SendResult{}, errSendFailed
	}
	return channel.SendResult{}, nil
}

type sendError string

func (e sendError) Error() string { return string(e) }

const errSendFailed = sendError("微信没收下这条消息")

func clientID() string {
	var r [12]byte
	_, _ = rand.Read(r[:])
	return "persona-" + hex.EncodeToString(r[:])
}

// compose is the owner's message as the hub takes it: the text (a voice message's transcript), the quote, the
// pictures; files and videos are only named, the bot cannot pass them on yet.
func (b *Bot) compose(ctx context.Context, c *ilink.Client, m ilink.Message) channel.Message {
	out := channel.Message{ID: m.ID, Text: strings.TrimSpace(m.Text), Quote: m.Quote}
	for _, md := range m.Media {
		switch md.Kind {
		case ilink.MediaImage:
			if len(out.Images) >= channel.ImagesMost {
				out.Unsupported = append(out.Unsupported, "更多的图片")
				continue
			}
			data, err := c.Download(ctx, md)
			if err != nil {
				log.Printf("wechat: downloading a picture: %v", err)
				out.Unsupported = append(out.Unsupported, "一张没下载下来的图片")
				continue
			}
			out.Images = append(out.Images, channel.Blob{MIME: http.DetectContentType(data), Data: data})
		case ilink.MediaVoice:
			if out.Text == "" {
				out.Unsupported = append(out.Unsupported, "一段没有转写的语音")
			}
		case ilink.MediaFile:
			name := md.FileName
			if name == "" {
				name = "一个文件"
			} else {
				name = "文件「" + name + "」"
			}
			out.Unsupported = append(out.Unsupported, name)
		case ilink.MediaVideo:
			out.Unsupported = append(out.Unsupported, "一段视频")
		}
	}
	out.Unsupported = slices.Compact(out.Unsupported)
	return out
}

// typing is the owner's chat showing that the bot is writing.
type typing struct {
	ticket string
	at     time.Time // when the hub last asked for it
	shown  time.Time // when WeChat was last told
}

// Typing is the hub's channel.typing: on while a turn goes on (asked again every few seconds), off once answered.
func (b *Bot) Typing(ctx context.Context, on bool) error {
	b.mu.Lock()
	c, creds, contextToken, t := b.client, b.st.Creds, b.st.Context, b.typing
	now := b.opt.Now()
	if !on {
		b.typing = nil
	}
	b.mu.Unlock()
	if c == nil || creds == nil || contextToken == "" {
		return nil // presence is best effort
	}
	switch {
	case !on && t != nil:
		return c.SetTyping(ctx, creds.UserID, t.ticket, false)
	case !on:
		return nil
	case t != nil && now.Sub(t.at) < typingFor:
		b.mu.Lock()
		t.at = now
		b.mu.Unlock()
		if now.Sub(t.shown) < channel.TypingEvery {
			return nil
		}
		b.mu.Lock()
		t.shown = now
		b.mu.Unlock()
		return c.SetTyping(ctx, creds.UserID, t.ticket, true) // WeChat clears it on its own
	}
	ticket, err := c.TypingTicket(ctx, creds.UserID, contextToken)
	if err != nil {
		return nil
	}
	if err := c.SetTyping(ctx, creds.UserID, ticket, true); err != nil {
		return nil
	}
	b.mu.Lock()
	b.typing = &typing{ticket: ticket, at: now, shown: now}
	b.mu.Unlock()
	return nil
}

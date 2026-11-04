package wechat

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/wechat/ilink"
)

const (
	maxSeen     = 200
	maxImages   = 4
	strangerGap = 10 * time.Minute
)

// handle takes one message the bot got: someone else's gets one line saying whose bot it is; the owner's is a
// command, an answer to what an agent asked, or a message for the conversation it drives.
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
	text := strings.TrimSpace(m.Text)
	if strings.HasPrefix(text, "/") && b.command(ctx, c, creds, m, text) {
		return
	}
	if b.answerAsk(ctx, c, creds, m, text) {
		return
	}
	b.forward(ctx, c, creds, m)
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
	b.send(ctx, c, m.From, m.ContextToken, "这个机器人只为绑定它的人服务。")
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

// tell sends text to the owner, in the chat of their latest message.
func (b *Bot) tell(ctx context.Context, text string) bool {
	b.mu.Lock()
	c, creds, contextToken := b.client, b.st.Creds, b.st.Context
	b.mu.Unlock()
	if creds == nil {
		return false
	}
	return b.send(ctx, c, creds.UserID, contextToken, text)
}

func clientID() string {
	var r [12]byte
	_, _ = rand.Read(r[:])
	return "persona-" + hex.EncodeToString(r[:])
}

const help = `在这里说的话会发给 Persona 的主对话，回答发回这里。
/new 开一段新对话（后面可以直接跟第一句话）
/main 回到主对话
/stop 停下正在做的这一轮
/status 现在发给谁、在等几个回答
/help 这些命令
Agent 问你问题或要你批准时，直接回复序号或文字。`

// command runs one of the bot's commands; false for one it does not know (sent on as a message: it may be the
// conversation's own, /compact).
func (b *Bot) command(ctx context.Context, c *ilink.Client, creds ilink.Credentials, m ilink.Message, text string) bool {
	name, rest, _ := strings.Cut(text, " ")
	rest = strings.TrimSpace(rest)
	reply := func(s string) { b.send(ctx, c, m.From, m.ContextToken, s) }
	switch strings.ToLower(name) {
	case "/help":
		reply(help)
	case "/main":
		b.mu.Lock()
		b.st.Target = target{}
		b.saveLocked(ctx)
		b.mu.Unlock()
		reply("之后的消息发给主对话。")
	case "/new":
		b.mu.Lock()
		b.st.Target = target{Fresh: true}
		b.saveLocked(ctx)
		b.mu.Unlock()
		if rest == "" {
			reply("好，下一条消息会开一段新对话。/main 回到主对话。")
			return true
		}
		m.Text = rest
		b.forward(ctx, c, creds, m)
	case "/stop":
		machine, session, err := b.targetSession(ctx)
		switch {
		case err != nil:
			reply("没停下来：" + err.Error())
		case session == "":
			reply("现在没有在做的。")
		default:
			if err := b.opt.Hub.Interrupt(ctx, machine, session); err != nil {
				reply("没停下来：" + err.Error())
				return true
			}
			b.dropRuns(ctx, machine, session)
			reply("已停下。")
		}
	case "/status":
		reply(b.statusText(ctx))
	default:
		return false
	}
	return true
}

// targetSession is the conversation messages go to now ("" when it is still to be started).
func (b *Bot) targetSession(ctx context.Context) (machine, session string, err error) {
	b.mu.Lock()
	t := b.st.Target
	b.mu.Unlock()
	switch {
	case t.Fresh:
		return "", "", nil
	case t.Session != "":
		return t.Machine, t.Session, nil
	}
	return b.opt.Hub.Assistant(ctx)
}

func (b *Bot) statusText(ctx context.Context) string {
	b.mu.Lock()
	t, runs, asks := b.st.Target, len(b.st.Runs), len(b.st.Asks)
	b.mu.Unlock()
	where := "主对话"
	switch {
	case t.Fresh:
		where = "下一条消息开的新对话"
	case t.Session != "":
		where = "用 /new 开的对话"
		if s, err := b.opt.Hub.Session(ctx, t.Machine, t.Session); err == nil && s != nil && s.Title != "" {
			where += "「" + s.Title + "」"
		}
	}
	out := "消息发给：" + where
	if runs > 0 {
		out += fmt.Sprintf("\n在等 %d 个回答", runs)
	}
	if asks > 0 {
		out += fmt.Sprintf("\n有 %d 个问题等你回复", asks)
	}
	return out
}

// forward sends the owner's message to the conversation they talk to (starting it when there is none yet) and waits
// on its turn.
func (b *Bot) forward(ctx context.Context, c *ilink.Client, creds ilink.Credentials, m ilink.Message) {
	text, images := b.compose(ctx, c, m)
	if strings.TrimSpace(text) == "" && len(images) == 0 {
		return
	}
	reply := func(s string) { b.send(ctx, c, m.From, m.ContextToken, s) }
	b.mu.Lock()
	t := b.st.Target
	b.mu.Unlock()
	machine, session := t.Machine, t.Session
	msgID, find := "wechat-"+m.ID, probe(m.Text)
	base := 0
	var err error
	if t.main() {
		machine, session, err = b.opt.Hub.Assistant(ctx)
		if err != nil {
			reply("没发出去，hub 那边出错了：" + err.Error())
			return
		}
	}
	if session == "" {
		s, err := b.opt.Hub.Start(ctx, text, images, t.main())
		if err != nil {
			reply("没开成新对话：" + err.Error())
			return
		}
		machine, session = s.MachineID, s.SessionID
		msgID, find = "", "" // a new conversation: all of it answers this message
		if t.Fresh {
			b.mu.Lock()
			b.st.Target = target{Machine: machine, Session: session}
			b.saveLocked(ctx)
			b.mu.Unlock()
		}
	} else {
		if base, err = b.opt.Hub.LastSeq(ctx, machine, session); err != nil {
			reply("没发出去：" + err.Error())
			return
		}
		if err := b.opt.Hub.Send(ctx, machine, session, text, images, msgID); err != nil {
			reply("没发出去：" + err.Error())
			return
		}
	}
	b.startRun(ctx, machine, session, base, msgID, find)
}

// compose is what goes to the conversation for m: the text with WeChat's note (prompts/wechat/message), and its
// pictures. Files and videos are only named: the bot cannot pass them on yet.
func (b *Bot) compose(ctx context.Context, c *ilink.Client, m ilink.Message) (string, []Image) {
	var images []Image
	var unsupported []string
	for _, md := range m.Media {
		switch md.Kind {
		case ilink.MediaImage:
			if len(images) >= maxImages {
				unsupported = append(unsupported, "更多的图片")
				continue
			}
			data, err := c.Download(ctx, md)
			if err != nil {
				log.Printf("wechat: downloading a picture: %v", err)
				unsupported = append(unsupported, "一张没下载下来的图片")
				continue
			}
			images = append(images, Image{MIME: http.DetectContentType(data), Data: data})
		case ilink.MediaVoice:
			if strings.TrimSpace(m.Text) == "" {
				unsupported = append(unsupported, "一段没有转写的语音")
			}
		case ilink.MediaFile:
			name := md.FileName
			if name == "" {
				name = "一个文件"
			} else {
				name = "文件「" + name + "」"
			}
			unsupported = append(unsupported, name)
		case ilink.MediaVideo:
			unsupported = append(unsupported, "一段视频")
		}
	}
	unsupported = slices.Compact(unsupported)
	if strings.TrimSpace(m.Text) == "" && m.Quote == "" && len(images) == 0 && len(unsupported) == 0 {
		return "", nil
	}
	return prompts.RenderWeChatMessage(prompts.WeChatMessage{Text: strings.TrimSpace(m.Text), Quote: m.Quote, Unsupported: unsupported}), images
}

// probe is a piece of what the owner wrote, to find the message in the transcript when the node does not tie items
// to their send (client_msg_id).
func probe(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			r := []rune(line)
			return string(r[:min(len(r), 24)])
		}
	}
	return ""
}

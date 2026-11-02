package telegram

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gotd/td/tg"
)

// Telegram's objects in the connector's words (pure: tested on made-up objects, mapping_test.go).

// entities are the users and chats a response carries, by id.
type entities struct {
	users map[int64]*tg.User
	chats map[int64]tg.ChatClass
}

func entitiesOf(users []tg.UserClass, chats []tg.ChatClass) entities {
	e := entities{users: map[int64]*tg.User{}, chats: map[int64]tg.ChatClass{}}
	for _, u := range users {
		if u, ok := u.(*tg.User); ok {
			e.users[u.ID] = u
		}
	}
	for _, c := range chats {
		e.chats[c.GetID()] = c
	}
	return e
}

// peerOf is how to address a peer, with the access hash the entities carry.
func (e entities) peerOf(p tg.PeerClass) (peer, bool) {
	switch p := p.(type) {
	case *tg.PeerUser:
		u := e.users[p.UserID]
		if u == nil {
			return peer{Kind: 'u', ID: p.UserID}, false
		}
		return peer{Kind: 'u', ID: u.ID, Hash: u.AccessHash}, true
	case *tg.PeerChat:
		return peer{Kind: 'g', ID: p.ChatID}, true
	case *tg.PeerChannel:
		if ch, ok := e.chats[p.ChannelID].(*tg.Channel); ok {
			return peer{Kind: 'c', ID: ch.ID, Hash: ch.AccessHash}, true
		}
		return peer{Kind: 'c', ID: p.ChannelID}, false
	}
	return peer{}, false
}

// nameOf is how a peer shows: a person's name, a group's title.
func (e entities) nameOf(p tg.PeerClass) string {
	switch p := p.(type) {
	case *tg.PeerUser:
		return userName(e.users[p.UserID])
	case *tg.PeerChat:
		if c, ok := e.chats[p.ChatID].(*tg.Chat); ok {
			return c.Title
		}
	case *tg.PeerChannel:
		if c, ok := e.chats[p.ChannelID].(*tg.Channel); ok {
			return c.Title
		}
	}
	return ""
}

func userName(u *tg.User) string {
	if u == nil {
		return ""
	}
	if u.Deleted {
		return "已注销的账号"
	}
	n := strings.TrimSpace(u.FirstName + " " + u.LastName)
	if n == "" && u.Username != "" {
		n = "@" + u.Username
	}
	return n
}

func inputPeer(p peer) tg.InputPeerClass {
	switch p.Kind {
	case 'u':
		return &tg.InputPeerUser{UserID: p.ID, AccessHash: p.Hash}
	case 'g':
		return &tg.InputPeerChat{ChatID: p.ID}
	default:
		return &tg.InputPeerChannel{ChannelID: p.ID, AccessHash: p.Hash}
	}
}

// dialogsOf turns a dialogs response into the connector's, archived when it is the archive folder's.
func dialogsOf(ds []tg.DialogClass, msgs []tg.MessageClass, e entities, archived bool, now time.Time) []dialog {
	top := map[string]tg.MessageClass{}
	for _, m := range msgs {
		if p, ok := e.peerOf(peerOfMessage(m)); ok || p.ID != 0 {
			top[fmt.Sprintf("%s/%d", p.key(), m.GetID())] = m
		}
	}
	var out []dialog
	for _, dc := range ds {
		d, ok := dc.(*tg.Dialog)
		if !ok {
			continue // a folder entry
		}
		p, ok := e.peerOf(d.Peer)
		if !ok {
			continue
		}
		x := dialog{Peer: p, Name: e.nameOf(d.Peer), Top: d.TopMessage, Unread: d.UnreadCount, Mentions: d.UnreadMentionsCount,
			ReadMax: d.ReadInboxMaxID, Archived: archived || d.FolderID == 1, Muted: int64(d.NotifySettings.MuteUntil) > now.Unix(), Kind: "group"}
		switch pp := d.Peer.(type) {
		case *tg.PeerUser:
			x.Kind = "dm"
			if u := e.users[pp.UserID]; u != nil {
				x.Bot, x.Self, x.Username = u.Bot, u.Self, u.Username
			}
		case *tg.PeerChannel:
			if ch, ok := e.chats[pp.ChannelID].(*tg.Channel); ok {
				x.Broadcast, x.Username = ch.Broadcast && !ch.Megagroup, ch.Username
			}
		}
		if m, ok := top[fmt.Sprintf("%s/%d", p.key(), d.TopMessage)]; ok {
			mm := messageOf(m, e)
			x.TopAt, x.TopOut, x.TopText, x.TopFrom = mm.At, mm.Out, clip(mm.Text, 100), mm.From
		}
		out = append(out, x)
	}
	return out
}

func peerOfMessage(m tg.MessageClass) tg.PeerClass {
	switch m := m.(type) {
	case *tg.Message:
		return m.PeerID
	case *tg.MessageService:
		return m.PeerID
	}
	return nil
}

// messageOf is a message in words: a forward says from whom, media is a placeholder ([图片], [文件 名字 · 大小]),
// a group event is a line of its own.
func messageOf(mc tg.MessageClass, e entities) message {
	switch m := mc.(type) {
	case *tg.Message:
		out := message{ID: m.ID, At: time.Unix(int64(m.Date), 0), Out: m.Out, Mentioned: m.Mentioned, Type: "TEXT"}
		if m.FromID != nil {
			if p, _ := e.peerOf(m.FromID); p.ID != 0 {
				out.FromKey = p.key()
			}
			out.From = e.nameOf(m.FromID)
		} else if m.PeerID != nil { // a private chat's or a channel post's: the peer itself
			if p, _ := e.peerOf(m.PeerID); p.ID != 0 {
				out.FromKey = p.key()
			}
			out.From = e.nameOf(m.PeerID)
		}
		if h, ok := m.ReplyTo.(*tg.MessageReplyHeader); ok {
			out.ReplyTo = h.ReplyToMsgID
		}
		var parts []string
		if f := m.FwdFrom; f.FromName != "" || f.FromID != nil {
			from := f.FromName
			if from == "" {
				from = e.nameOf(f.FromID)
			}
			if from == "" {
				from = "别处"
			}
			parts = append(parts, "转发自 "+from+"：")
		}
		if m.Message != "" {
			parts = append(parts, m.Message)
		}
		if kind, say := mediaOf(m.Media); say != "" {
			out.Type = kind
			parts = append(parts, say)
		}
		out.Text = strings.Join(parts, " ")
		return out
	case *tg.MessageService:
		out := message{ID: m.ID, At: time.Unix(int64(m.Date), 0), Out: m.Out, Type: "SYSTEM", Text: actionOf(m.Action, e)}
		if m.FromID != nil {
			out.From = e.nameOf(m.FromID)
		}
		return out
	}
	return message{ID: mc.GetID(), Type: "SYSTEM", Text: "[消息]"}
}

// mediaOf is a message's media as a placeholder; a link preview says nothing (the link is in the text).
func mediaOf(m tg.MessageMediaClass) (kind, say string) {
	switch m := m.(type) {
	case nil, *tg.MessageMediaEmpty, *tg.MessageMediaWebPage:
		return "", ""
	case *tg.MessageMediaPhoto:
		return "IMAGE", "[图片]"
	case *tg.MessageMediaDocument:
		doc, ok := m.Document.(*tg.Document)
		if !ok {
			return "FILE", "[文件]"
		}
		name := ""
		for _, a := range doc.Attributes {
			switch a := a.(type) {
			case *tg.DocumentAttributeSticker:
				return "STICKER", "[贴纸]"
			case *tg.DocumentAttributeAudio:
				if a.Voice {
					return "AUDIO", "[语音]"
				}
				return "AUDIO", "[音频]"
			case *tg.DocumentAttributeVideo:
				if a.RoundMessage {
					return "VIDEO", "[视频消息]"
				}
				return "VIDEO", "[视频]"
			case *tg.DocumentAttributeAnimated:
				return "IMAGE", "[动图]"
			case *tg.DocumentAttributeFilename:
				name = a.FileName
			}
		}
		if name != "" {
			return "FILE", fmt.Sprintf("[文件 %s · %s]", name, size(doc.Size))
		}
		return "FILE", "[文件]"
	case *tg.MessageMediaGeo, *tg.MessageMediaGeoLive, *tg.MessageMediaVenue:
		return "LOCATION", "[位置]"
	case *tg.MessageMediaContact:
		return "CONTACT", "[名片]"
	case *tg.MessageMediaPoll:
		return "POLL", "[投票]"
	}
	return "OTHER", "[消息]"
}

func size(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", n>>10)
	}
	return fmt.Sprintf("%d B", n)
}

// actionOf is a group event in a line.
func actionOf(a tg.MessageActionClass, e entities) string {
	switch a := a.(type) {
	case *tg.MessageActionChatAddUser:
		var names []string
		for _, id := range a.Users {
			names = append(names, userName(e.users[id]))
		}
		return "[" + strings.Join(names, "、") + " 进群]"
	case *tg.MessageActionChatJoinedByLink, *tg.MessageActionChatJoinedByRequest:
		return "[有人进群]"
	case *tg.MessageActionChatDeleteUser:
		return "[" + userName(e.users[a.UserID]) + " 退群]"
	case *tg.MessageActionChatEditTitle:
		return "[群名改成「" + a.Title + "」]"
	case *tg.MessageActionPinMessage:
		return "[置顶了一条消息]"
	case *tg.MessageActionChatCreate, *tg.MessageActionChannelCreate:
		return "[建群]"
	}
	return "[群里的变化]"
}

// The gotd client's reads.

func (g *gotdClient) Dialogs(ctx context.Context, limit int) ([]dialog, error) {
	var out []dialog
	for _, folder := range []int{0, 1} {
		res, err := g.c.API().MessagesGetDialogs(ctx, &tg.MessagesGetDialogsRequest{OffsetPeer: &tg.InputPeerEmpty{}, Limit: limit, FolderID: folder})
		if err != nil {
			return nil, err
		}
		var ds []tg.DialogClass
		var msgs []tg.MessageClass
		var e entities
		switch r := res.(type) {
		case *tg.MessagesDialogs:
			ds, msgs, e = r.Dialogs, r.Messages, entitiesOf(r.Users, r.Chats)
		case *tg.MessagesDialogsSlice:
			ds, msgs, e = r.Dialogs, r.Messages, entitiesOf(r.Users, r.Chats)
		default:
			continue
		}
		out = append(out, dialogsOf(ds, msgs, e, folder == 1, time.Now())...)
	}
	return out, nil
}

func (g *gotdClient) History(ctx context.Context, d dialog, minID int, since time.Time, max int) ([]message, error) {
	var got []message
	offset := 0
	for len(got) < max {
		page := min(100, max-len(got))
		res, err := g.c.API().MessagesGetHistory(ctx, &tg.MessagesGetHistoryRequest{Peer: inputPeer(d.Peer), OffsetID: offset, Limit: page, MinID: minID})
		if err != nil {
			return nil, err
		}
		var msgs []tg.MessageClass
		var e entities
		switch r := res.(type) {
		case *tg.MessagesMessages:
			msgs, e = r.Messages, entitiesOf(r.Users, r.Chats)
		case *tg.MessagesMessagesSlice:
			msgs, e = r.Messages, entitiesOf(r.Users, r.Chats)
		case *tg.MessagesChannelMessages:
			msgs, e = r.Messages, entitiesOf(r.Users, r.Chats)
		}
		stop := len(msgs) < page
		for _, m := range msgs { // newest first
			mm := messageOf(m, e)
			if minID == 0 && mm.At.Before(since) {
				stop = true
				break
			}
			got = append(got, mm)
			offset = mm.ID
		}
		if stop || len(msgs) == 0 {
			break
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(readPause):
		}
	}
	for i, j := 0, len(got)-1; i < j; i, j = i+1, j-1 { // oldest first
		got[i], got[j] = got[j], got[i]
	}
	return got, nil
}

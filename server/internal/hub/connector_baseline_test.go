package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chyroc/jarvis/server/internal/connector"
	"github.com/chyroc/jarvis/server/internal/kb"
	"github.com/chyroc/jarvis/server/internal/lark"
	"github.com/chyroc/jarvis/server/internal/mail"
)

// The connector baseline (issue #522): what the hub makes today of Feishu (through lark-connector) and mail, the inbox items
// and the knowledge-base files, so moving them behind the Connector interface can prove it changed nothing. The data
// is synthetic and its times are relative to yesterday 10:00 in the user's zone (the inbox only takes the last 7
// days); the goldens name every time relative to that base, so they are the same whenever the test runs.

// baselineMe is the signed-in Feishu account of the fake.
const baselineMe = "示例用户"

// baselineMsg is one message of the fake lark-connector, At relative to the base.
type baselineMsg struct {
	ID, Pos, Sender, SenderID string
	Mine                      bool
	Type, Text                string
	At                        time.Duration
	More                      map[string]any // other wire fields (replyTo, parts, thread…)
}

func (m baselineMsg) wire(base time.Time) map[string]any {
	typ := m.Type
	if typ == "" {
		typ = "TEXT"
	}
	out := map[string]any{"id": m.ID, "position": m.Pos, "senderId": m.SenderID, "senderName": m.Sender, "isMine": m.Mine,
		"messageTypeName": typ, "textPreview": m.Text, "createdAt": base.Add(m.At).Format(time.RFC3339)}
	for k, v := range m.More {
		out[k] = v
	}
	return out
}

// baselineFeed is one chat of the fake: its feed item, its messages (oldest first) and the scopes that list it.
type baselineFeed struct {
	feed   map[string]any
	msgs   []baselineMsg
	scopes []string
}

func baselineChat(base time.Time, id, name string, scopes []string, feed map[string]any, msgs ...baselineMsg) baselineFeed {
	last := msgs[len(msgs)-1]
	preview := map[string]any{"displayAt": base.Add(last.At).Format(time.RFC3339), "isMine": last.Mine,
		"sender": map[string]any{"displayName": last.Sender},
		"digest": map[string]any{"elements": []any{map[string]any{"kind": "text", "content": last.Text}}}}
	f := map[string]any{"feedId": id, "name": name, "avatarUrl": "/api/media/avatar_" + id, "preview": preview}
	for k, v := range feed {
		f[k] = v
	}
	if _, ok := f["updatedAtMs"]; ok { // the item's time from updatedAtMs, not the preview
		delete(preview, "displayAt")
	}
	return baselineFeed{feed: f, msgs: msgs, scopes: scopes}
}

// baselineLark is the fake lark-connector agent API. It records every request; one it has no route for is reported.
func baselineLark(t *testing.T, base time.Time) (*httptest.Server, func() []string) {
	t.Helper()
	at := func(d time.Duration) string { return base.Add(d).Format(time.RFC3339) }
	ms := func(d time.Duration) int64 { return base.Add(d).UnixMilli() }
	const (
		docA = "https://example.feishu.cn/docx/DocA1"    // linked in a DM
		wiki = "https://example.feishu.cn/wiki/WikiD4"   // linked in a read group
		docB = "https://example.feishu.cn/docx/DocB2"    // @me notice
		sht  = "https://example.feishu.cn/sheets/ShtC3"  // comment notice on a sheet
		wikE = "https://example.feishu.cn/wiki/WikiE5"   // reply notice on a wiki page
		docE = "https://example.feishu.cn/docx/DocE5doc" // the page behind it
	)
	chats := []baselineFeed{
		// A DM: two unread messages after one of mine, one with a document link (a kb doc item).
		baselineChat(base, "f_dm", "张三", []string{"unread", "chats"}, map[string]any{"unreadCount": 2, "readPosition": "201"},
			baselineMsg{ID: "m201", Pos: "201", Sender: baselineMe, SenderID: "u_me", Mine: true, Text: "上次的方案我看了", At: -time.Hour},
			baselineMsg{ID: "m202", Pos: "202", Sender: "张三", SenderID: "u_zs", Text: "方案在 " + docA + " 里，帮忙看下", At: 0},
			baselineMsg{ID: "m203", Pos: "203", Sender: "张三", SenderID: "u_zs", Text: "今天能给反馈吗？", At: 5 * time.Minute}),
		// A group with @me: an image (a kb attachment), a reply, my own message among the unread ones.
		baselineChat(base, "f_grp", "示例项目群", []string{"unread", "mentions", "chats"}, map[string]any{"isGroup": true, "unreadCount": 3,
			"hasUnreadAtMe": true, "unreadMention": map[string]any{"position": "305"}, "readPosition": "302", "groupTags": []string{"内部"}},
			baselineMsg{ID: "m301", Pos: "301", Sender: "李四", SenderID: "u_ls", Text: "大家好，评审材料已经发了", At: -3 * time.Hour},
			baselineMsg{ID: "m302", Pos: "302", Sender: "王五", SenderID: "u_ww", Type: "IMAGE", Text: "[图片]", At: -30 * time.Minute,
				More: map[string]any{"parts": []any{map[string]any{"kind": "image", "imageKey": "img_k1", "url": "/api/media/img_k1"}}}},
			baselineMsg{ID: "m303", Pos: "303", Sender: baselineMe, SenderID: "u_me", Mine: true, Text: "收到", At: -20 * time.Minute},
			baselineMsg{ID: "m304", Pos: "304", Sender: "李四", SenderID: "u_ls", Text: "这张图是最新的吗", At: 10 * time.Minute,
				More: map[string]any{"replyTo": map[string]any{"id": "m302", "senderName": "王五", "textPreview": "[图片]"}}},
			baselineMsg{ID: "m305", Pos: "305", Sender: "李四", SenderID: "u_ls", Text: "@" + baselineMe + " 明天的评审你能来吗？", At: 15 * time.Minute,
				More: map[string]any{"isAtMe": true}}),
		// A muted group nobody @'d me in: archived, kept out of the inbox.
		baselineChat(base, "f_muted", "闲聊群", []string{"unread", "chats"}, map[string]any{"isGroup": true, "isMuted": true, "unreadCount": 12},
			baselineMsg{ID: "m401", Pos: "401", Sender: "孙八", SenderID: "u_sb", Text: "中午吃什么", At: -time.Hour}),
		// A muted group with an @me: in the inbox through the mentions scope.
		baselineChat(base, "f_mutedat", "兴趣小组", []string{"mentions", "chats"}, map[string]any{"isGroup": true, "isMuted": true, "unreadCount": 1,
			"hasUnreadAtMe": true, "unreadMention": map[string]any{"position": "501"}},
			baselineMsg{ID: "m501", Pos: "501", Sender: "周九", SenderID: "u_zj", Text: "@" + baselineMe + " 周末活动报名了吗", At: 45 * time.Minute,
				More: map[string]any{"isAtMe": true}}),
		// A bot chat, its time from updatedAtMs.
		baselineChat(base, "f_bot", "构建机器人", []string{"unread", "chats"}, map[string]any{"isBot": true, "unreadCount": 1,
			"updatedAtMs": float64(ms(50 * time.Minute))},
			baselineMsg{ID: "m601", Pos: "601", Sender: "构建机器人", SenderID: "b_build", Type: "CARD", Text: "构建失败：示例服务 #128", At: 50 * time.Minute,
				More: map[string]any{"senderType": "bot"}}),
		// A topic group: read as threads.
		baselineChat(base, "f_topic", "话题讨论", []string{"unread", "chats"}, map[string]any{"isGroup": true, "chatMode": "topic", "unreadCount": 1},
			baselineMsg{ID: "m701", Pos: "701", Sender: "吴十", SenderID: "u_ws", Text: "新话题：示例工具怎么用？", At: 25 * time.Minute,
				More: map[string]any{"threadId": "th701", "threadMessageType": "root", "threadReplyCount": 2}}),
		// A group with nothing unread: only the knowledge base reads it; a wiki link in it.
		baselineChat(base, "f_read", "已读的群", []string{"chats"}, map[string]any{"isGroup": true},
			baselineMsg{ID: "m801", Pos: "801", Sender: "郑一", SenderID: "u_zy", Text: "会议纪要见 " + wiki, At: -5 * time.Hour}),
	}
	byID := map[string]baselineFeed{}
	for _, c := range chats {
		byID[c.feed["feedId"].(string)] = c
	}

	reply := func(w http.ResponseWriter, v any) { _ = json.NewEncoder(w).Encode(v) }
	mux := http.NewServeMux()
	mux.HandleFunc("GET /accounts", func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]any{"accounts": []any{map[string]any{"userId": "u_me", "name": baselineMe, "primary": true, "active": true}}})
	})
	mux.HandleFunc("GET /conversations", func(w http.ResponseWriter, r *http.Request) {
		scope := r.URL.Query().Get("scope")
		items := []any{}
		for _, c := range chats {
			for _, s := range c.scopes {
				if s == scope {
					items = append(items, c.feed)
				}
			}
		}
		reply(w, map[string]any{"items": items, "hasMore": false})
	})
	// The latest page of a chat (tail): the only window one collection and one archive run ask for.
	page := func(w http.ResponseWriter, r *http.Request) {
		c, ok := byID[r.PathValue("feed")]
		q := r.URL.Query()
		limit, _ := strconv.Atoi(q.Get("limit"))
		if !ok || q.Get("tail") != "1" || limit <= 0 {
			t.Errorf("fake lark-connector: unexpected page %s", r.URL)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		msgs := c.msgs[max(0, len(c.msgs)-limit):]
		items := make([]any, 0, len(msgs))
		for _, m := range msgs {
			items = append(items, m.wire(base))
		}
		reply(w, map[string]any{"items": items, "hasOlder": len(c.msgs) > limit, "hasNewer": false})
	}
	mux.HandleFunc("GET /conversations/{feed}/messages", page)
	mux.HandleFunc("GET /conversations/{feed}/threads", page)
	mux.HandleFunc("GET /media/{key}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("fake image " + r.PathValue("key") + "\n"))
	})

	// Documents: an @me in a docx, a comment on a sheet row, a reply on a wiki page; a share stays out.
	mux.HandleFunc("GET /documents/notifications", func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]any{"items": []any{
			map[string]any{"id": "dn1", "title": "季度规划", "url": docB, "type": 22, "kind": "mention", "summary": "李四 在文档中提及了你",
				"atMe": true, "unreadCount": 1, "updatedAt": at(30 * time.Minute)},
			map[string]any{"id": "dn2", "title": "排期表", "url": sht, "type": 3, "kind": "comment", "summary": "王五 评论了你：这一行什么时候能完成？",
				"unreadCount": 1, "updatedAt": at(40 * time.Minute)},
			map[string]any{"id": "dn3", "title": "新人手册", "url": wikE, "type": 16, "kind": "reply", "summary": "周九 回复了你的评论",
				"unreadCount": 1, "updatedAt": at(55 * time.Minute)},
			map[string]any{"id": "dn4", "title": "分享给你的文档", "url": "https://example.feishu.cn/docx/DocF6", "type": 22, "kind": "share",
				"summary": "郑一 分享了文档", "updatedAt": at(5 * time.Minute)},
		}})
	})
	mux.HandleFunc("GET /documents/header", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("url") != wikE {
			t.Errorf("fake lark-connector: header of %s", r.URL)
		}
		reply(w, map[string]any{"token": "DocE5doc", "type": 22})
	})
	mux.HandleFunc("GET /documents/read", func(w http.ResponseWriter, r *http.Request) {
		u := r.URL.Query().Get("url")
		switch {
		case u == sht:
			cell := func(row, col int, text string, more ...any) map[string]any {
				c := map[string]any{"row": row, "column": col, "text": text}
				for i := 0; i+1 < len(more); i += 2 {
					c[more[i].(string)] = more[i+1]
				}
				return c
			}
			choices := []any{map[string]any{"text": "未开始"}, map[string]any{"text": "进行中", "color": "#ffaa00"}, map[string]any{"text": "已完成", "color": "#00aa00"}}
			reply(w, map[string]any{"title": "排期表", "token": "ShtC3", "url": sht, "spreadsheet": map[string]any{"revision": 7, "editable": true,
				"sheets": []any{map[string]any{"id": "s1", "name": "排期", "cells": []any{
					cell(0, 0, "任务"), cell(0, 1, "负责人"), cell(0, 2, "状态"), cell(0, 3, "截止"),
					cell(1, 0, "接入示例"), cell(1, 1, "@"+baselineMe), cell(1, 2, "进行中", "choices", choices), cell(1, 3, "周五"),
					cell(2, 0, "写文档"), cell(2, 1, "李四"), cell(2, 2, "已完成", "choices", choices),
					cell(3, 0, "联调"), cell(3, 1, "@"+baselineMe+" 李四"), cell(3, 2, "已完成", "choices", choices),
				}}}}})
		case r.URL.Query().Get("format") != "markdown":
			t.Errorf("fake lark-connector: read %s", r.URL)
		case u == docA:
			reply(w, map[string]any{"title": "接入方案", "markdown": "# 接入方案\n\n先接示例服务，再接网关。"})
		case u == wiki: // no title: the first line names it
			reply(w, map[string]any{"markdown": "# 会议纪要 · 示例项目\n\n- 结论：下周上线"})
		case u == docB:
			reply(w, map[string]any{"title": "季度规划", "markdown": "# 季度规划\n\n## 目标\n完成示例服务接入。\n\n## 分工\n@" + baselineMe +
				" 负责接入部分，请在周五前确认排期。\n李四 负责文档。"})
		case u == docE:
			reply(w, map[string]any{"title": "新人手册", "markdown": "# 新人手册\n\n1. 申请账号\n2. 装好开发环境"})
		default:
			t.Errorf("fake lark-connector: read %s", r.URL)
		}
	})
	mux.HandleFunc("GET /documents/comments", func(w http.ResponseWriter, r *http.Request) {
		author := func(id, name string) map[string]any { return map[string]any{"id": id, "name": name} }
		text := func(s string) map[string]any { return map[string]any{"kind": "text", "text": s} }
		mention := map[string]any{"kind": "mention", "id": "u_me", "name": baselineMe}
		var threads []any
		switch r.URL.Query().Get("url") {
		case docB:
			threads = []any{map[string]any{"id": "ct1", "quote": "接入部分", "createdAt": at(30 * time.Minute), "replies": []any{
				map[string]any{"id": "cr1", "author": author("u_ls", "李四"), "content": []any{mention, text(" 这里的排期能确认下吗？")}, "createdAt": at(30 * time.Minute)}}}}
		case sht:
			threads = []any{map[string]any{"id": "ct2", "quote": "接入示例", "createdAt": at(40 * time.Minute), "replies": []any{
				map[string]any{"id": "cr2", "author": author("u_ww", "王五"), "content": []any{text("@" + baselineMe + " 这一行什么时候能完成？")}, "createdAt": at(40 * time.Minute)}}}}
		case docE:
			threads = []any{
				map[string]any{"id": "ct3", "resolved": true, "quote": "申请账号", "createdAt": at(-48 * time.Hour), "replies": []any{
					map[string]any{"id": "cr3", "author": author("u_zj", "周九"), "content": []any{mention, text(" 已处理")}, "createdAt": at(-48 * time.Hour)}}},
				map[string]any{"id": "ct4", "quote": "装好开发环境", "createdAt": at(-2 * time.Hour), "replies": []any{
					map[string]any{"id": "cr4", "author": author("u_me", baselineMe), "mine": true, "content": []any{text("这里的步骤过时了")}, "createdAt": at(-2 * time.Hour)},
					map[string]any{"id": "cr5", "author": author("u_zj", "周九"), "content": []any{mention, text(" 已更新，再看看？")}, "createdAt": at(55 * time.Minute)}}},
			}
		default:
			t.Errorf("fake lark-connector: comments of %s", r.URL)
		}
		reply(w, map[string]any{"threads": threads})
	})

	// removed: a comment on my post (I answered it already), an @me in a comment on someone else's post; a new follower
	// stays out. Every commenter is on one post only: the archive reads the posts in map order.
	mux.HandleFunc("GET /moments/notifications", func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]any{"notifications": []any{
			map[string]any{"id": "mn3", "kind": "follower", "actorUserId": "u_fs", "actorName": "冯三", "isUnread": true, "createTime": ms(3 * time.Hour)},
			map[string]any{"id": "mn2", "kind": "atInComment", "postId": "p_other", "commentId": "c4", "actorUserId": "u_zl", "actorName": "赵六",
				"commentText": "@" + baselineMe + " 你怎么看", "postText": "关于示例工具的一点想法：上手快，文档少。", "isUnread": true, "createTime": ms(2 * time.Hour)},
			map[string]any{"id": "mn1", "kind": "comment", "postId": "p_mine", "commentId": "c1", "actorUserId": "u_ww2", "actorName": "钱二",
				"commentText": "写得好，下次叫上我", "postText": "周末去爬山了，风景很好。", "isUnread": false, "createTime": ms(time.Hour)},
		}})
	})
	mux.HandleFunc("GET /moments/users/me/posts", func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]any{"posts": []any{
			map[string]any{"id": "p_mine", "userId": "u_me", "userName": baselineMe, "commentCount": 2, "createTime": ms(-2 * time.Hour),
				"content": map[string]any{"text": "周末去爬山了，风景很好。"}},
			map[string]any{"id": "p_quiet", "userId": "u_me", "userName": baselineMe, "commentCount": 0, "createTime": ms(-30 * time.Hour),
				"content": map[string]any{"text": "新工位到了。"}},
		}})
	})
	mux.HandleFunc("GET /moments/posts/{id}/comments", func(w http.ResponseWriter, r *http.Request) {
		comment := func(id, uid, name, text string, d time.Duration, more ...string) map[string]any {
			c := map[string]any{"id": id, "userId": uid, "userName": name, "content": map[string]any{"text": text}, "createTime": ms(d)}
			if len(more) == 2 {
				c["replyCommentId"], c["replyUserName"] = more[0], more[1]
			}
			return c
		}
		var cs []any
		switch r.PathValue("id") {
		case "p_mine":
			cs = []any{comment("c1", "u_ww2", "钱二", "写得好，下次叫上我", time.Hour),
				comment("c2", "u_me", baselineMe, "好的", 70*time.Minute, "c1", "钱二")}
		case "p_other":
			cs = []any{comment("c3", "u_ys", "褚三", "这个工具挺好用", 100*time.Minute),
				comment("c4", "u_zl", "赵六", "@"+baselineMe+" 你怎么看", 2*time.Hour)}
		case "p_quiet":
		default:
			t.Errorf("fake lark-connector: comments of post %s", r.PathValue("id"))
		}
		reply(w, map[string]any{"comments": cs, "nextPageToken": ""})
	})

	// Meetings and the calendar (kb items): a webinar and a free slot stay out. No time falls on the day the test runs
	// (base+14h to base+38h): it could be taken for the test's own "now".
	mux.HandleFunc("GET /meetings", func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]any{"items": []any{
			map[string]any{"historyId": "mh1", "topic": "示例项目周会", "sortedAt": at(-time.Hour), "kind": "meeting"},
			map[string]any{"historyId": "mh2", "topic": "全员直播", "sortedAt": at(-26 * time.Hour), "kind": "webinar"},
		}})
	})
	mux.HandleFunc("GET /calendar/week", func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]any{"events": []any{
			map[string]any{"id": "ev1", "sourceId": "src_ev1", "title": "接入评审", "startAt": at(76 * time.Hour)},
			map[string]any{"id": "ev2", "title": "一对一", "startAt": at(2 * time.Hour)},
			map[string]any{"id": "ev3", "title": "专注时间", "startAt": at(4 * time.Hour), "isFree": true},
		}})
	})

	var mu sync.Mutex
	seen := map[string]int{}
	var unhandled []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		keys := make([]string, 0, len(q))
		for k := range q {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var parts []string
		for _, k := range keys {
			parts = append(parts, k+"="+strings.Join(q[k], ","))
		}
		line := r.Method + " " + r.URL.Path
		if len(parts) > 0 {
			line += "?" + strings.Join(parts, "&")
		}
		_, pattern := mux.Handler(r)
		mu.Lock()
		seen[line]++
		if pattern == "" {
			unhandled = append(unhandled, line)
		}
		mu.Unlock()
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(func() {
		srv.Close()
		if len(unhandled) > 0 {
			t.Errorf("fake lark-connector has no route for:\n%s", strings.Join(unhandled, "\n"))
		}
	})
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		out := make([]string, 0, len(seen))
		for l, n := range seen {
			if n > 1 {
				l += fmt.Sprintf("  ×%d", n)
			}
			out = append(out, l)
		}
		sort.Strings(out)
		return out
	}
}

// baselineMail is two accounts' inboxes, newest first as IMAP lists them.
type baselineMail struct {
	Mail
	accounts []mail.MailAccount
	msgs     map[string][]mail.MailMessage
}

func newBaselineMail(base time.Time) baselineMail {
	at := func(d time.Duration) string { return base.Add(d).UTC().Format(time.RFC3339) }
	addr := func(name, email string) mail.Address { return mail.Address{Name: name, Email: email} }
	work, home := addr("", "me@example.com"), addr("", "me.home@example.com")
	msg := func(uid uint32, from mail.Address, to []mail.Address, subject string, d time.Duration, seen bool, text, html string, cc ...mail.Address) mail.MailMessage {
		snippet := text
		if snippet == "" {
			snippet = "<style>body{font:14px sans-serif}</style>"
		}
		return mail.MailMessage{MailSummary: mail.MailSummary{Mailbox: "INBOX", UID: uid, MessageID: fmt.Sprintf("<m%d@example.com>", uid),
			Subject: subject, From: from, To: to, Date: at(d), Snippet: snippet, Seen: seen}, Cc: cc, Text: text, HTML: html}
	}
	zs := addr("张三", "zhangsan@example.com")
	return baselineMail{
		accounts: []mail.MailAccount{
			{ID: "acc1", Name: "工作", Email: work.Email, Provider: mail.ProviderIMAP},
			{ID: "acc2", Name: "个人", Email: home.Email, Provider: mail.ProviderGmail},
		},
		msgs: map[string][]mail.MailMessage{
			"acc1": {
				// A thread of two unread mails to me; the reply joins the first one's item.
				msg(13, zs, []mail.Address{work}, "Re: 请确认季度预算", 40*time.Minute, false, "补充一下：附件是最新版本。", ""),
				msg(12, zs, []mail.Address{work}, "请确认季度预算", 20*time.Minute, false, "你好，请在周五前确认季度预算。", ""),
				// A list mail, me in cc, HTML only.
				msg(11, addr("团队周报", "weekly@lists.example.com"), []mail.Address{addr("", "team@lists.example.com")}, "[团队] 本周周报",
					-30*time.Minute, false, "", "<p>本周进展：<b>示例服务</b>已上线。</p>", work),
				msg(10, addr("", "no-reply@notify.example.com"), []mail.Address{work}, "您的账单已生成", -3*time.Hour, false, "本月账单 123.45 元。", ""),
				// Read already: archived, not in the inbox.
				msg(9, addr("李四", "lisi@example.com"), []mail.Address{work}, "会议改到下午", -50*time.Hour, true, "会议改到下午三点。", ""),
			},
			"acc2": {
				msg(3, addr("Alice Example", "alice@example.com"), []mail.Address{home}, "Can we move the design review?", time.Hour, false,
					"Hi, I'm travelling on Thursday, so I won't make the review. Could we do Tuesday?", ""),
				// Mine (sent to myself), read.
				msg(2, home, []mail.Address{home}, "备忘：续签合同", -6*time.Hour, true, "记得下周续签合同。", ""),
				// Unread but past the inbox's 7 days.
				msg(1, addr("王五", "wangwu@example.com"), []mail.Address{home}, "老照片", -8*24*time.Hour, false, "翻到几张老照片。", ""),
			},
		},
	}
}

func (f baselineMail) ListAccounts() ([]mail.MailAccount, error) { return f.accounts, nil }

func (f baselineMail) ListMessages(ctx context.Context, opts mail.ListOptions) (*mail.ListResult, error) {
	res := &mail.ListResult{}
	for _, m := range f.msgs[opts.AccountID] {
		s := m.MailSummary
		s.AccountID = opts.AccountID
		res.Messages = append(res.Messages, s)
	}
	return res, nil
}

func (f baselineMail) GetMessage(ctx context.Context, accountID string, uid uint32, mailbox string) (*mail.MailMessage, error) {
	for _, m := range f.msgs[accountID] {
		if m.UID == uid {
			m.AccountID = accountID
			return &m, nil
		}
	}
	return nil, fmt.Errorf("no mail %s/%d", accountID, uid)
}

// baselineNormalizer replaces what depends on when the test runs: times become offsets from base ("<base+1h30m@Z>")
// or "<now>", days "<D0>" / "<D-1>"…, Unix milliseconds and seconds the same offsets, the temporary directories names.
type baselineNormalizer struct {
	base, start time.Time
	dirs        map[string]string // path -> placeholder
}

var (
	baselineRFC3339  = regexp.MustCompile(`\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d+)?(?:Z|[+-]\d\d:\d\d)`)
	baselineDay      = regexp.MustCompile(`\d{4}-\d\d-\d\d`)
	baselineMonthDay = regexp.MustCompile(`\b(\d\d)-(\d\d) (\d\d:\d\d)\b`) // "01-02 15:04" in contexts
	baselineMillis   = regexp.MustCompile(`\b1\d{12}\b`)
	baselineSeconds  = regexp.MustCompile(`\b1\d{9}\b`)
)

func (n baselineNormalizer) offset(t time.Time) string {
	if !t.Before(n.start.Add(-time.Minute)) && t.Before(time.Now().Add(time.Minute)) {
		return "now"
	}
	d := t.Sub(n.base)
	sign := "+"
	if d < 0 {
		sign, d = "-", -d
	}
	var b strings.Builder
	for _, u := range []struct {
		unit time.Duration
		name string
	}{{24 * time.Hour, "d"}, {time.Hour, "h"}, {time.Minute, "m"}, {time.Second, "s"}, {1, "ns"}} {
		if k := d / u.unit; k > 0 {
			fmt.Fprintf(&b, "%d%s", k, u.name)
			d -= k * u.unit
		}
	}
	if b.Len() == 0 {
		return "base"
	}
	return "base" + sign + b.String()
}

func (n baselineNormalizer) day(t time.Time) string {
	y, m, d := n.base.Date()
	b := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	y, m, d = t.Date()
	k := int(time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Sub(b) / (24 * time.Hour))
	if k == 0 {
		return "<D0>"
	}
	return fmt.Sprintf("<D%+d>", k)
}

func (n baselineNormalizer) apply(s string) string {
	for dir, name := range n.dirs {
		s = strings.ReplaceAll(s, dir, name)
	}
	s = baselineRFC3339.ReplaceAllStringFunc(s, func(v string) string {
		t, err := time.Parse(time.RFC3339Nano, v)
		if err != nil {
			return v
		}
		if t.Sub(n.base).Abs() > 366*24*time.Hour { // a zero time, a fixed date: nothing to do with when the test runs
			return v
		}
		zone := v[len(v)-6:]
		if strings.HasSuffix(v, "Z") {
			zone = "Z"
		}
		return "<" + n.offset(t) + "@" + zone + ">"
	})
	s = baselineDay.ReplaceAllStringFunc(s, func(v string) string {
		t, err := time.ParseInLocation("2006-01-02", v, n.base.Location())
		if err != nil || t.Sub(n.base).Abs() > 366*24*time.Hour {
			return v
		}
		return n.day(t)
	})
	s = baselineMonthDay.ReplaceAllStringFunc(s, func(v string) string {
		m := baselineMonthDay.FindStringSubmatch(v)
		mo, _ := strconv.Atoi(m[1])
		d, _ := strconv.Atoi(m[2])
		best := time.Time{}
		for _, y := range []int{n.base.Year() - 1, n.base.Year(), n.base.Year() + 1} { // the year closest to base
			t := time.Date(y, time.Month(mo), d, 0, 0, 0, 0, n.base.Location())
			if best.IsZero() || t.Sub(n.base).Abs() < best.Sub(n.base).Abs() {
				best = t
			}
		}
		return n.day(best) + " " + m[3]
	})
	near := func(t time.Time) bool { return t.Sub(n.base).Abs() < 60*24*time.Hour }
	s = baselineMillis.ReplaceAllStringFunc(s, func(v string) string {
		ms, _ := strconv.ParseInt(v, 10, 64)
		if t := time.UnixMilli(ms); near(t) {
			return "<ms:" + n.offset(t) + ">"
		}
		return v
	})
	return baselineSeconds.ReplaceAllStringFunc(s, func(v string) string {
		sec, _ := strconv.ParseInt(v, 10, 64)
		if t := time.Unix(sec, 0); near(t) {
			return "<unix:" + n.offset(t) + ">"
		}
		return v
	})
}

type baselineLog struct {
	mu    sync.Mutex
	lines []string
}

func (l *baselineLog) Write(p []byte) (int, error) { // log.Logger writes one line per call
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, strings.TrimSpace(string(p)))
	return len(p), nil
}

func TestConnectorBaseline(t *testing.T) {
	// Times read from Unix milliseconds are in time.Local and keep its offset when marshalled: pin it to what the hub
	// machine runs in, so the goldens do not depend on where the test runs.
	local := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = local })
	start := time.Now()
	sh, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	y, m, d := start.In(sh).AddDate(0, 0, -1).Date()
	base := time.Date(y, m, d, 10, 0, 0, 0, sh) // yesterday 10:00: always past, always inside the inbox's window

	srv, requests := baselineLark(t, base)
	h := newTestHub(t)
	logs := &baselineLog{}
	h.logger = log.New(logs, "", 0)
	h.inbox.init()
	h.userTZ = "Asia/Shanghai"
	h.lark = lark.New(lark.Config{BaseURL: srv.URL, Token: "t"})
	// The connectors as the hub registers them (Feishu's sources and items come from the registry).
	h.connectors = connector.NewRegistry(nil)
	h.registerConnectors()
	h.mail = newBaselineMail(base)
	ctx := context.Background()

	h.collectInbox(ctx, true)
	// Triage runs in the background; with no model it gives every item its rule tier (and logs why).
	h.inbox.triaging.Lock()
	h.inbox.triaging.Unlock()
	h.inbox.handing.Lock()
	h.inbox.handing.Unlock()

	st, err := kb.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h.kb.eng = &kb.Engine{Store: st, Sources: h.kbSources(), Owner: h.ownerName()}
	h.rezoneKB(st)
	if _, err := h.kb.eng.Archive(ctx); err != nil {
		t.Fatal(err)
	}
	h.kbMail(ctx)
	h.kbCollectItems(ctx)
	var unexpected []string
	for _, l := range logs.lines {
		if !strings.HasPrefix(l, "inbox: triage ") {
			unexpected = append(unexpected, l)
		}
	}
	if len(unexpected) > 0 {
		t.Errorf("the hub logged:\n%s", strings.Join(unexpected, "\n"))
	}

	norm := baselineNormalizer{base: base, start: start, dirs: map[string]string{st.Dir: "<kb>", h.cfg.dir: "<hub>"}}

	h.inbox.mu.Lock()
	ids := make([]string, 0, len(h.inbox.items))
	for id := range h.inbox.items {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var items strings.Builder
	for _, id := range ids {
		raw, err := json.MarshalIndent(h.inbox.items[id], "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		items.Write(raw)
		items.WriteString("\n")
	}
	h.inbox.mu.Unlock()
	golden(t, "connector_baseline.inbox", norm.apply(items.String()))

	var files []string
	err = filepath.WalkDir(st.Dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(st.Dir, p)
			files = append(files, filepath.ToSlash(rel))
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	var dir strings.Builder
	for _, f := range files {
		raw, err := os.ReadFile(filepath.Join(st.Dir, f))
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&dir, "=== %s\n%s", f, raw)
		if len(raw) > 0 && raw[len(raw)-1] != '\n' {
			dir.WriteString("\n")
		}
	}
	golden(t, "connector_baseline.kb", norm.apply(dir.String()))

	golden(t, "connector_baseline.requests", norm.apply(strings.Join(requests(), "\n")+"\n"))
}

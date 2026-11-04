package hub

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/connector/proc"
	"github.com/nwverse/persona/server/internal/connector/serve"
	"github.com/nwverse/persona/server/internal/kb"
)

// postsHub is a hub with a knowledge base and a program connector "feed" (an archive, also filing under "posts"),
// as the program reaches the hub through it.
func postsHub(t *testing.T) (*Hub, *kb.Store, *proc.Conn) {
	t.Helper()
	h := newTestHub(t)
	st, err := kb.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h.kb.eng = &kb.Engine{Store: st}
	h.connectors = connector.NewRegistry(nil)
	c := proc.New(proc.Config{ID: "feed", Name: "Feed", KB: h.kbFor("feed"),
		Manifest: connector.Manifest{Capabilities: []connector.Capability{connector.CapArchive}, ArchiveSources: []string{"posts"}}})
	if err := h.connectors.Add(c); err != nil {
		t.Fatal(err)
	}
	return h, st, c
}

func progCall[T any](t *testing.T, c *proc.Conn, method string, params any) (T, error) {
	t.Helper()
	var out T
	raw, _ := json.Marshal(params)
	res, err := c.Handle(context.Background(), method, raw)
	if err != nil {
		return out, err
	}
	b, _ := json.Marshal(res)
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out, nil
}

// posts.put keeps a feed's posts in the archive: new, the same, or with a moved comment count; comments when read;
// a post with no comments settled at once; another connector's post refused. posts.pending lists what to read again.
func TestConnectorPostsIntoTheKnowledgeBase(t *testing.T) {
	h, st, c := postsHub(t)
	at := time.Now().Add(-48 * time.Hour).UTC().Truncate(time.Second)
	if err := st.PutChat(&kb.Chat{ID: kb.ChatID("posts", "p2"), Source: "posts", SourceID: "p2", Kind: "moment", Name: "公司圈 · 第二帖", Cursor: "1700000000000", Messages: 3}); err != nil {
		t.Fatal(err)
	}
	if err := st.PutMomentPost(&kb.MomentPost{ID: "other", Source: "elsewhere", Text: "别人的", At: at}, nil); err != nil {
		t.Fatal(err)
	}
	put := serve.PostsPutParams{Posts: []connector.PostPut{
		{Post: connector.Post{ID: "p1", Feed: "公司圈", Author: "王五", Text: "第一帖", CommentCount: 2, At: at, Categories: []string{"研发"}}},
		{Post: connector.Post{ID: "p2", Feed: "公司圈", Author: "我", Mine: true, Text: "第二帖", CommentCount: 1, At: at.Add(time.Hour)},
			Comments: []connector.PostComment{{Author: "王五", Text: "好帖", At: at.Add(2 * time.Hour)}}},
		{Post: connector.Post{ID: "p3", Text: "没人评论", At: at}},
		{Post: connector.Post{ID: "other", Text: "抢别人的", At: at}},
		{Post: connector.Post{ID: " ", Text: "没有 id"}},
	}}
	res, err := progCall[serve.PostsPutResult](t, c, serve.MPostsPut, put)
	if err != nil || strings.Join(res.Statuses, ",") != "new,new,new,refused,refused" {
		t.Fatalf("statuses %q, %v", res.Statuses, err)
	}
	p1, _ := st.MomentPost("p1")
	if p1.Source != "feed" || p1.CommentsOf != -1 || p1.Concerns {
		t.Fatalf("p1 %+v", p1)
	}
	p2, md, _ := st.MomentArchived("p2")
	if p2.CommentsOf != 1 || !p2.Concerns || !p2.Mine {
		t.Fatalf("p2 %+v (a conversation under posts-p2 makes it concern the user)", p2)
	}
	for _, want := range []string{"# 公司圈 · 第二帖", "## 评论（1）", "王五: 好帖"} {
		if !strings.Contains(md, want) {
			t.Fatalf("p2's markdown lacks %q:\n%s", want, md)
		}
	}
	if p3, _ := st.MomentPost("p3"); p3.CommentsOf != 0 {
		t.Fatalf("p3 with no comments is settled: %+v", p3)
	}
	if o, _ := st.MomentPost("other"); o.Text != "别人的" {
		t.Fatalf("another connector's post was overwritten: %+v", o)
	}
	pending, err := progCall[serve.PostsPendingResult](t, c, serve.MPostsPending, serve.PostsPendingParams{})
	if err != nil || len(pending.Posts) != 1 || pending.Posts[0].ID != "p1" {
		t.Fatalf("pending %+v, %v", pending, err)
	}
	// Its comments read: the post stays, the comments and the count they were read at are kept; off the list.
	cm := serve.PostsCommentsParams{ID: "p1", CommentCount: 2, Comments: []connector.PostComment{{Author: "我", Text: "谢谢", At: at.Add(time.Hour)}}}
	if _, err := progCall[struct{}](t, c, serve.MPostsComments, cm); err != nil {
		t.Fatal(err)
	}
	if p1, md, _ := st.MomentArchived("p1"); p1.CommentsOf != 2 || p1.Text != "第一帖" || !strings.Contains(md, "我: 谢谢") {
		t.Fatalf("p1 after its comments %+v\n%s", p1, md)
	}
	if _, err := progCall[struct{}](t, c, serve.MPostsComments, serve.PostsCommentsParams{ID: "other"}); err == nil {
		t.Fatal("another connector's post got comments")
	}
	if pending, _ := progCall[serve.PostsPendingResult](t, c, serve.MPostsPending, serve.PostsPendingParams{}); len(pending.Posts) != 0 {
		t.Fatalf("pending after reading %+v", pending)
	}
	// Seen again: the same; then its count moved; its comments read keep the count they were read at.
	again := serve.PostsPutParams{Posts: []connector.PostPut{
		{Post: connector.Post{ID: "p2", Text: "第二帖", CommentCount: 1, At: at.Add(time.Hour)}},
		{Post: connector.Post{ID: "p1", Text: "第一帖", CommentCount: 3, At: at}},
	}}
	if res, err := progCall[serve.PostsPutResult](t, c, serve.MPostsPut, again); err != nil || strings.Join(res.Statuses, ",") != ",changed" {
		t.Fatalf("statuses %q, %v", res.Statuses, err)
	}
	if pending, _ := progCall[serve.PostsPendingResult](t, c, serve.MPostsPending, serve.PostsPendingParams{}); len(pending.Posts) != 1 || pending.Posts[0].CommentCount != 3 {
		t.Fatalf("pending after the count moved %+v", pending)
	}
	if p2, md, _ := st.MomentArchived("p2"); p2.CommentsOf != 1 || !p2.Concerns || !strings.Contains(md, "好帖") {
		t.Fatalf("seen again, p2 lost what it had: %+v\n%s", p2, md)
	}
	h.kbPostsUpkeep(false)
	if idx, _ := os.ReadFile(filepath.Join(st.Dir, "moments", "index.jsonl")); strings.Count(string(idx), "\n") != 4 {
		t.Fatalf("index:\n%s", idx)
	}
	// archive.chats: the conversations under its names, and only its.
	chats, err := progCall[serve.ArchiveChatsResult](t, c, serve.MArchiveChats, serve.ArchiveChatsParams{Source: "posts"})
	if err != nil || len(chats.Chats) != 1 || chats.Chats[0].ID != "p2" || chats.Chats[0].Cursor != "1700000000000" {
		t.Fatalf("chats %+v, %v", chats, err)
	}
	if _, err := progCall[serve.ArchiveChatsResult](t, c, serve.MArchiveChats, serve.ArchiveChatsParams{Source: "mail"}); err == nil {
		t.Fatal("another source's conversations were handed out")
	}
	// 进知识库 off: nothing goes in.
	if err := h.connectors.SetFeed("feed", connector.FeedArchive, false); err != nil {
		t.Fatal(err)
	}
	if _, err := progCall[serve.PostsPutResult](t, c, serve.MPostsPut, put); err == nil {
		t.Fatal("posts went in with 进知识库 off")
	}
}

// A connector without an archive cannot write to the knowledge base.
func TestConnectorPostsNeedAnArchive(t *testing.T) {
	h := newTestHub(t)
	st, _ := kb.NewStore(t.TempDir())
	h.kb.eng = &kb.Engine{Store: st}
	c := proc.New(proc.Config{ID: "tools", Name: "Tools", KB: h.kbFor("tools"),
		Manifest: connector.Manifest{Capabilities: []connector.Capability{connector.CapStatus}}})
	if _, err := progCall[serve.PostsPutResult](t, c, serve.MPostsPut, serve.PostsPutParams{}); err == nil || !strings.Contains(err.Error(), "archive") {
		t.Fatalf("err %v", err)
	}
}

// sourcedArchive hands one batch under the connector's other name, one under a name it never declared.
type sourcedArchive struct{}

func (sourcedArchive) Changes(_ context.Context, cursor string, _ int) ([]connector.ChatBatch, string, bool, error) {
	at := time.Now().Add(-time.Hour)
	msg := []kb.SourceMessage{{ID: "c1", SenderName: "王五", Type: "TEXT", Text: "评论", At: at, Position: "1"}}
	return []connector.ChatBatch{
		{Source: "posts", Chat: kb.SourceChat{ID: "p9", Name: "公司圈 · 帖子", Kind: "moment"}, Messages: msg},
		{Source: "mail", Chat: kb.SourceChat{ID: "x", Name: "冒名", Kind: "mail"}, Messages: msg},
		{Chat: kb.SourceChat{ID: "chat1", Name: "群", Kind: "group"}, Messages: msg},
	}, "1", false, nil
}

// A batch filed under one of the connector's archive sources becomes "<source>-<id>"; one under any other name is
// dropped.
func TestArchiveConnectorFilesUnderItsSources(t *testing.T) {
	st, _ := kb.NewStore(t.TempDir())
	e := &kb.Engine{Store: st}
	if _, err := archiveConnector(context.Background(), e, "feed", sourcedArchive{}, "posts"); err != nil {
		t.Fatal(err)
	}
	if c, ok := st.Chat(kb.ChatID("posts", "p9")); !ok || c.Source != "posts" || c.Kind != "moment" {
		t.Fatalf("posts-p9 %+v", c)
	}
	if _, ok := st.Chat(kb.ChatID("mail", "x")); ok {
		t.Fatal("a batch under an undeclared name went in")
	}
	if _, ok := st.Chat(kb.ChatID("feed", "chat1")); !ok {
		t.Fatal("its own chat is missing")
	}
}

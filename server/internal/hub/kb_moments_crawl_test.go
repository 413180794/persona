package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chyroc/jarvis/server/internal/kb"
	"github.com/chyroc/jarvis/server/internal/lark"
)

// A fake lark-connector with two tabs: the crawler archives every post once, reads comments only where the count moved,
// stops a tab at a page with nothing new, and marks my post and the posts that are chats.
func TestCrawlMoments(t *testing.T) {
	now := time.Now()
	ms := func(d time.Duration) int64 { return now.Add(-d).UnixMilli() }
	post := func(id, user, text string, comments int, age time.Duration) map[string]any {
		return map[string]any{"id": id, "userId": user, "userName": "U" + user, "commentCount": comments, "reactionCount": 1,
			"createTime": ms(age), "categoryIds": []string{"c1"}, "content": map[string]any{"text": text}}
	}
	feeds := map[string][][]map[string]any{
		"1":  {{post("p1", "u1", "第一帖", 2, time.Hour), post("p2", "me", "我发的", 0, 2*time.Hour)}, {post("p3", "u3", "第三帖", 1, 3*time.Hour)}},
		"c1": {{post("p1", "u1", "第一帖", 2, time.Hour), post("old", "u9", "两年前", 0, 800*24*time.Hour)}},
	}
	var commentCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/agent/v1")
		switch {
		case p == "/accounts":
			_ = json.NewEncoder(w).Encode(map[string]any{"accounts": []any{map[string]any{"userId": "me", "name": "我", "primary": true}}})
		case p == "/moments/tabs":
			_ = json.NewEncoder(w).Encode(map[string]any{"tabs": []any{map[string]any{"id": "1", "name": "推荐", "type": 1},
				map[string]any{"id": "c1", "name": "字节研发", "type": 2}}})
		case p == "/moments/feed":
			pages := feeds[r.URL.Query().Get("tabId")]
			i := 0
			if tok := r.URL.Query().Get("pageToken"); tok != "" {
				i = int(tok[0] - '0')
			}
			next := ""
			if i+1 < len(pages) {
				next = string(rune('0' + i + 1))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"posts": pages[i], "nextPageToken": next})
		case strings.HasSuffix(p, "/comments"):
			commentCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"comments": []any{
				map[string]any{"id": "c1", "userId": "u5", "userName": "王五", "createTime": ms(50 * time.Minute), "content": map[string]any{"text": "好帖"}},
				map[string]any{"id": "c2", "userId": "me", "userName": "我的名字", "replyUserName": "王五", "createTime": ms(40 * time.Minute), "content": map[string]any{"text": "谢谢"}},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	h := newTestHub(t)
	h.lark = lark.New(lark.Config{BaseURL: srv.URL + "/agent/v1", Token: "t"})
	st, err := kb.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// p3 concerns me already (a chat of its own).
	if err := st.PutChat(&kb.Chat{ID: kb.ChatID("moments", "p3"), Source: "moments", Kind: "moment"}); err != nil {
		t.Fatal(err)
	}
	cfg := MomentsCrawl{Enabled: true, DelayMS: 1}
	r := h.crawlMoments(context.Background(), st, cfg, 365)
	if r.Error != "" || r.New != 3 || r.Seen != 3 || r.Comments != 2 || r.Total != 3 {
		t.Fatalf("round 1 = %+v", r)
	}
	md, _ := os.ReadFile(filepath.Join(st.Dir, "moments", "posts", "p1.md"))
	for _, want := range []string{"# removed · 第一帖", "Uu1", "字节研发", "评论 2", "## 评论（2）", "王五: 好帖", "我: （回复 王五）谢谢"} {
		if !strings.Contains(string(md), want) {
			t.Fatalf("p1.md lacks %q:\n%s", want, md)
		}
	}
	if p, _ := st.MomentPost("p2"); !p.Mine || p.CommentsOf != 0 {
		t.Fatalf("p2 = %+v", p)
	}
	if p, _ := st.MomentPost("p3"); !p.Concerns {
		t.Fatalf("p3 = %+v", p)
	}
	if _, ok := st.MomentPost("old"); ok {
		t.Fatal("a post past the retention was archived")
	}
	idx, _ := os.ReadFile(filepath.Join(st.Dir, "moments", "index.jsonl"))
	if strings.Count(string(idx), "\n") != 3 {
		t.Fatalf("index:\n%s", idx)
	}
	// Nothing moved: no comments read again, and each tab stops at its first page.
	calls := commentCalls.Load()
	r = h.crawlMoments(context.Background(), st, cfg, 365)
	if r.New != 0 || r.Changed != 0 || r.Comments != 0 || commentCalls.Load() != calls || r.Pages != 2 {
		t.Fatalf("round 2 = %+v (comment calls %d → %d)", r, calls, commentCalls.Load())
	}
	// A new comment on p1: only p1 is read again.
	feeds["1"][0][0]["commentCount"] = 3
	r = h.crawlMoments(context.Background(), st, cfg, 365)
	if r.Changed != 1 || r.Comments != 1 {
		t.Fatalf("round 3 = %+v", r)
	}
	// Search finds archived posts.
	e := &kb.Engine{Store: st}
	if hits := e.Search("第三帖", false, 5); len(hits) == 0 || !strings.Contains(hits[0].Path, "moments") {
		t.Fatalf("search = %+v", hits)
	}
}

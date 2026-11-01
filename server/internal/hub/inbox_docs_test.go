package hub

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chyroc/jarvis/server/internal/lark"
)

var testMe = lark.Me{UserID: "u-me", Name: "我自己"}

func TestDocNoticesBecomeItems(t *testing.T) {
	b := newInbox()
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	notices := []lark.DocNotice{
		{ID: "n1", Title: "排期表", URL: "https://example.com/sheets/S1", Type: 3, Kind: "mention", Summary: "张三提及了你", AtMe: true, UpdatedAt: now.Add(-time.Hour)},
		{ID: "n2", Title: "分享的", URL: "https://example.com/docx/D1", Type: 22, Kind: "share", Summary: "李四分享了文档", UpdatedAt: now},
		{ID: "n3", Title: "静音的", Type: 22, Kind: "comment", AtMe: true, Muted: true, UpdatedAt: now},
		{ID: "n4", Title: "太旧", Type: 22, Kind: "comment", AtMe: true, UpdatedAt: now.Add(-10 * 24 * time.Hour)},
	}
	changed := b.applyDocs(notices, now)
	if len(changed) != 1 {
		t.Fatalf("want only the @me card, got %d", len(changed))
	}
	it := changed[0]
	if it.Kind != KindDoc || it.Doc.Type != "sheet" || it.Doc.Notice != "mention" || it.Sender != "张三" || it.Source != "lark" {
		t.Fatalf("item %+v doc %+v", it, it.Doc)
	}
	if again := b.applyDocs(notices, now); len(again) != 0 {
		t.Fatalf("the same card again changed %d items", len(again))
	}
	// Done, then new activity on the same document: a new item.
	it.done(now, "user")
	if again := b.applyDocs(notices, now); len(again) != 0 {
		t.Fatal("a done card came back without new activity")
	}
	notices[0].UpdatedAt = now
	notices[0].Summary = "王五评论了你"
	next := b.applyDocs(notices, now)
	if len(next) != 1 || next[0].ID == it.ID || next[0].Sender != "王五" {
		t.Fatalf("new activity: %+v", next)
	}
}

func sheetCell(row, col int, text string, choices ...string) lark.SheetCell {
	c := lark.SheetCell{Row: row, Column: col, Text: text}
	for _, ch := range choices {
		c.Choices = append(c.Choices, lark.SheetChoice{Text: ch, Color: "#bacefd"})
	}
	return c
}

func TestSheetRowsAboutMe(t *testing.T) {
	states := []string{"待处理", "进行中", "已完成"}
	tab := lark.SheetTab{ID: "t1", Name: "任务", Cells: []lark.SheetCell{
		sheetCell(0, 0, "任务"), sheetCell(0, 1, "负责人"), sheetCell(0, 2, "状态"), sheetCell(0, 3, "备注"),
		sheetCell(1, 0, "写文档"), sheetCell(1, 1, "@我自己"), sheetCell(1, 2, "进行中", states...), sheetCell(1, 3, "周五前"),
		sheetCell(2, 0, "别人的"), sheetCell(2, 1, "@张三"), sheetCell(2, 2, "待处理", states...),
		sheetCell(3, 0, "做完的"), sheetCell(3, 1, "@我自己 @张三"), sheetCell(3, 2, "已完成", states...),
		sheetCell(4, 1, "@我自己"), sheetCell(4, 2, "", states...),
	}}
	rows, cell := sheetRows(tab, testMe)
	if len(rows) != 3 {
		t.Fatalf("want 3 rows about me, got %+v", rows)
	}
	r := rows[0]
	if r.Ref != "t1!1" || r.Title != "写文档" || len(r.Status) != 1 || r.Status[0].Value != "进行中" || r.Status[0].Field != "2" || len(r.Status[0].Options) != 3 {
		t.Fatalf("row %+v", r)
	}
	if len(r.Fields) != 2 || r.Fields[0].Name != "负责人" || r.Fields[1].Value != "周五前" {
		t.Fatalf("fields %+v", r.Fields)
	}
	if rows[2].Title != "@我自己" { // no first column: the first cell there is
		t.Fatalf("untitled row: %q", rows[2].Title)
	}
	if cell == nil || cell.Kind != "cell" || cell.Row != 1 || cell.Column != 1 {
		t.Fatalf("reply cell %+v", cell)
	}
	// Unfinished first.
	picked := pickRows(rows)
	if picked[len(picked)-1].Title != "做完的" {
		t.Fatalf("order %v", picked)
	}
}

func TestSheetStatusWithoutDropdown(t *testing.T) {
	tab := lark.SheetTab{ID: "t", Cells: []lark.SheetCell{
		sheetCell(0, 0, "事项"), sheetCell(0, 1, "进度"),
		sheetCell(1, 0, "a @我自己"), sheetCell(1, 1, "开发中"),
		sheetCell(2, 0, "b"), sheetCell(2, 1, "已上线"),
	}}
	rows, _ := sheetRows(tab, testMe)
	if len(rows) != 1 || len(rows[0].Status) != 1 {
		t.Fatalf("%+v", rows)
	}
	opts := rows[0].Status[0].Options
	if len(opts) != 2 || opts[0].Name != "开发中" || opts[1].Name != "已上线" {
		t.Fatalf("options from the column: %+v", opts)
	}
}

func TestBaseRowsAboutMe(t *testing.T) {
	var tbl lark.BaseTable
	raw := `{"id":"tbl1","name":"缺陷","fields":[
	  {"id":"f1","name":"标题","type":1,"primary":true},
	  {"id":"f2","name":"状态","type":3,"options":[{"id":"o1","name":"未处理","color":0},{"id":"o2","name":"处理中","color":1},{"id":"o3","name":"已解决","color":2}]},
	  {"id":"f3","name":"跟进人","type":1},
	  {"id":"f4","name":"环境","type":3,"options":[{"id":"e1","name":"PROD","color":0}]},
	  {"id":"f5","name":"负责人","type":11}],
	 "records":[
	  {"id":"r1","cells":{"f1":{"type":"text","runs":[{"text":"挂了"}]},"f2":{"type":"option","ids":["o2"]},"f3":{"type":"text","runs":[{"text":"@我自己","href":"https://img"}]},"f4":{"type":"option","ids":["e1"]}}},
	  {"id":"r2","cells":{"f1":{"type":"text","runs":[{"text":"别人的"}]},"f3":{"type":"text","runs":[{"text":"@张三"}]}}},
	  {"id":"r3","cells":{"f1":{"type":"text","runs":[{"text":"派给我"}]},"f5":{"type":"user","ids":["u-me"]}}}]}`
	if err := json.Unmarshal([]byte(raw), &tbl); err != nil {
		t.Fatal(err)
	}
	rows := baseRows(tbl, testMe, testUserZone)
	if len(rows) != 2 {
		t.Fatalf("want 2, got %+v", rows)
	}
	r := rows[0]
	if r.Ref != "tbl1/r1" || r.Title != "挂了" || r.Table != "缺陷" || len(r.Status) != 1 || r.Status[0].Value != "处理中" || len(r.Status[0].Options) != 3 {
		t.Fatalf("row %+v", r)
	}
	// 环境 is a single select but not a status; it shows as a field.
	if len(r.Fields) != 2 || r.Fields[1].Name != "环境" || r.Fields[1].Value != "PROD" {
		t.Fatalf("fields %+v", r.Fields)
	}
	if rows[1].Title != "派给我" || rows[1].Status[0].Value != "" {
		t.Fatalf("user field row %+v", rows[1])
	}
	if fp := tbl.Records[0].Cells["f2"].Fingerprint(); fp != `o:["o2"]` {
		t.Fatalf("fingerprint %s", fp)
	}
	if fp := tbl.Records[0].Cells["f3"].Fingerprint(); fp != `t:[["@我自己","https://img"]]` {
		t.Fatalf("text fingerprint %s", fp)
	}
	var none *lark.BaseCell
	if none.Fingerprint() != "" {
		t.Fatal("an empty cell's fingerprint is empty")
	}
}

func TestDocReplyTargetAndOffers(t *testing.T) {
	d := InboxDoc{Type: "base", Rows: []DocRow{{Ref: "tbl1/r1", Status: []DocStatus{{Field: "f2", Name: "状态", Options: []DocOption{{Name: "未处理"}, {Name: "已解决"}}}}}}}
	rd := docRead{me: testMe, rows: d.Rows}
	if r := rd.reply(d); r == nil || r.Kind != "record" || r.Table != "tbl1" || r.Record != "r1" {
		t.Fatalf("reply %+v", r)
	}
	rd.threads = []lark.CommentThread{{ID: "c9"}}
	if r := rd.reply(d); r.Kind != "thread" || r.Thread != "c9" {
		t.Fatalf("a thread about me wins: %+v", r)
	}
	if !docOffers(d, DocChange{Ref: "tbl1/r1", Field: "f2", Value: "已解决"}) {
		t.Fatal("offered change refused")
	}
	for _, ch := range []DocChange{{Ref: "tbl1/r1", Field: "f2", Value: "瞎写"}, {Ref: "tbl1/r2", Field: "f2", Value: "已解决"}, {Ref: "tbl1/r1", Field: "f9", Value: "已解决"}} {
		if docOffers(d, ch) {
			t.Fatalf("%+v accepted", ch)
		}
	}
}

func TestThreadsAboutMe(t *testing.T) {
	reply := func(segs ...lark.CommentSegment) lark.CommentReply { return lark.CommentReply{Content: segs} }
	threads := []lark.CommentThread{
		{ID: "a", Replies: []lark.CommentReply{reply(lark.CommentSegment{Kind: "text", Text: "没提我"})}},
		{ID: "b", Replies: []lark.CommentReply{reply(lark.CommentSegment{Kind: "mention", ID: "u-me", Name: "我自己"}, lark.CommentSegment{Kind: "text", Text: " 看下"})}},
		{ID: "c", Resolved: true, Replies: []lark.CommentReply{reply(lark.CommentSegment{Kind: "text", Text: "@我自己 已解决的"})}},
		{ID: "d", Replies: []lark.CommentReply{reply(lark.CommentSegment{Kind: "text", Text: "@我自己 文字里提的"})}},
	}
	got := threadsAboutMe(threads, testMe)
	if len(got) != 2 || got[0].ID != "b" || got[1].ID != "d" {
		t.Fatalf("%+v", got)
	}
	if s := commentText(got[0].Replies[0]); s != "@我自己 看下" {
		t.Fatalf("text %q", s)
	}
}

func TestAroundMention(t *testing.T) {
	md := strings.Repeat("前", 50) + "@我自己 请确认" + strings.Repeat("后", 50)
	s := aroundMention(md, "@我自己", 10)
	if !strings.HasPrefix(s, "…") || !strings.HasSuffix(s, "…") || !strings.Contains(s, "@我自己 请确认") {
		t.Fatalf("%q", s)
	}
}

// TestDocsLive reads and moves real demo documents through lark-connector, on the hub machine:
//
//	JARVIS_LIVE_DOCS='{"lark":{"base_url":"http://127.0.0.1:<port>/agent/v1","container":"<lark-connector>"},"sheet":"<url>","base":"<url>","docx":"<url>"}' \
//	  GOCMD='go test -count=1 -run TestDocsLive -v' ./scripts/test-server.sh ./internal/hub
//
// The documents are the tester's own: the test moves one status there and back, and with "comment":true leaves a comment.
func TestDocsLive(t *testing.T) {
	raw := os.Getenv("JARVIS_LIVE_DOCS")
	if raw == "" {
		t.Skip("JARVIS_LIVE_DOCS not set")
	}
	var cfg struct {
		Lark              lark.Config
		Sheet, Base, Docx string
		Comment           bool
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatal(err)
	}
	h := newTestHub(t)
	h.lark = lark.New(cfg.Lark)
	h.inbox.init()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	me, err := h.larkMe(ctx)
	if err != nil || me.Name == "" {
		t.Fatalf("me: %+v %v", me, err)
	}
	for _, c := range []struct {
		name, link string
		typ        int
	}{{"sheet", cfg.Sheet, 3}, {"base", cfg.Base, 8}, {"docx", cfg.Docx, 22}} {
		if c.link == "" {
			continue
		}
		t.Run(c.name, func(t *testing.T) {
			n := lark.DocNotice{ID: "live-" + c.name, Title: "demo " + c.name, URL: c.link, Type: c.typ, Kind: "mention", AtMe: true,
				Summary: "测试提及了你", UpdatedAt: time.Now()}
			items := h.inbox.applyDocs([]lark.DocNotice{n}, time.Now())
			if len(items) != 1 {
				t.Fatal("no item")
			}
			it := items[0]
			h.readDoc(ctx, it, me)
			t.Logf("context:\n%s", it.Context)
			if it.Doc.Error != "" {
				t.Fatalf("read: %s", it.Doc.Error)
			}
			if it.Doc.Reply == nil {
				t.Fatal("nowhere to reply")
			}
			if c.name != "docx" {
				if len(it.Doc.Rows) == 0 || len(it.Doc.Rows[0].Status) == 0 {
					t.Fatalf("no rows / status: %+v", it.Doc.Rows)
				}
				row, st := it.Doc.Rows[0], it.Doc.Rows[0].Status[0]
				was := st.Value
				to := st.Options[0].Name
				if to == was {
					to = st.Options[len(st.Options)-1].Name
				}
				got, err := h.setDocStatus(ctx, it.ID, DocChange{Ref: row.Ref, Field: st.Field, Value: to})
				if err != nil {
					t.Fatalf("move to %s: %v", to, err)
				}
				var now string
				for _, r := range got.Doc.Rows {
					if r.Ref == row.Ref {
						now = r.Status[0].Value
					}
				}
				if now != to {
					t.Fatalf("after the move the row says %q, want %q", now, to)
				}
				if was != "" {
					if _, err := h.setDocStatus(ctx, it.ID, DocChange{Ref: row.Ref, Field: st.Field, Value: was}); err != nil {
						t.Fatalf("move back: %v", err)
					}
				}
			}
			if cfg.Comment {
				if err := h.replyDoc(ctx, *it.Doc, "Jarvis 收件箱测试评论（"+time.Now().Format("15:04:05")+"）"); err != nil {
					t.Fatalf("comment (%s): %v", it.Doc.Reply.Kind, err)
				}
			}
		})
	}
}

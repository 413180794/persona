package hub

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/lark"
)

// Feishu documents in the inbox (docs/inbox.md「云文档」): the docs notification center's @me / comment / reply
// cards become items. For a sheet or a Base the hub finds the rows that mention me and their status columns (a
// single select / a dropdown named 状态, 进度…), so the phone can move a row's status in one tap and the AI 分身 can
// propose the move with its draft; 发送 posts the reply as a comment where the mention was.

const KindDoc = "doc"

const (
	docRowsMax   = 5  // rows of a sheet / Base shown with an item
	docTablesMax = 6  // Base tables / sheet tabs looked through
	docPagesMax  = 5  // record pages per Base table (200 each)
	docFieldsMax = 6  // other cells of a row shown
	docOptions   = 12 // status values offered
)

// InboxDoc is a document item: which document, what happened, the rows about me and where a reply goes.
type InboxDoc struct {
	URL      string     `json:"url"`            // as the notification links it (a wiki link stays one)
	Link     string     `json:"link,omitempty"` // the document behind it (/sheets/…, /base/…, /docx/…)
	Type     string     `json:"type"`           // docx | doc | sheet | base | other
	Notice   string     `json:"notice"`         // mention | comment | reply | share
	NoticeID string     `json:"notice_id"`
	Rows     []DocRow   `json:"rows,omitempty"`
	Reply    *DocReply  `json:"reply,omitempty"` // where 发送 posts the reply; nil: nowhere
	Error    string     `json:"error,omitempty"` // the document could not be read
	ReadAt   *time.Time `json:"read_at,omitempty"`
}

// DocRow is a sheet row / Base record that mentions me.
type DocRow struct {
	Ref    string      `json:"ref"` // Base: tbl…/rec…; sheet: <tab id>!<row from 0>
	Table  string      `json:"table,omitempty"`
	Title  string      `json:"title"`
	Fields []DocField  `json:"fields,omitempty"`
	Status []DocStatus `json:"status,omitempty"`
}

type DocField struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// DocStatus is one status column of a row: its value and what it can be moved to.
type DocStatus struct {
	Field   string      `json:"field"` // Base field id / sheet column from 0
	Name    string      `json:"name"`
	Value   string      `json:"value"`
	Options []DocOption `json:"options"`
}

type DocOption struct {
	Name  string `json:"name"`
	Color string `json:"color,omitempty"` // #rrggbb
}

// DocReply is where the reply to a document item goes.
type DocReply struct {
	Kind   string `json:"kind"` // thread | record | cell | doc
	Label  string `json:"label"`
	Thread string `json:"thread,omitempty"`
	Table  string `json:"table,omitempty"`
	Record string `json:"record,omitempty"`
	Sheet  string `json:"sheet,omitempty"`
	Row    int    `json:"row,omitempty"`
	Column int    `json:"column,omitempty"`
}

// DocChange moves one row's status (the user's tap, or the AI 分身's proposal).
type DocChange struct {
	Ref   string `json:"ref"`
	Field string `json:"field"`
	Value string `json:"value"`
}

var (
	statusName  = regexp.MustCompile(`(?i)状态|进度|进展|阶段|status|state|stage|progress`)
	finalStatus = regexp.MustCompile(`(?i)完成|解决|关闭|结束|取消|上线|发布|done|closed|resolved|fixed|cancel`)
	noticeActor = regexp.MustCompile(`^(.{1,30}?)\s*(提及|评论|回复|分享|邀请|@|在)`)
)

func docType(t int) string {
	switch t {
	case 2:
		return "doc"
	case 3:
		return "sheet"
	case 8:
		return "base"
	case 22:
		return "docx"
	case 16:
		return "wiki"
	}
	return "other"
}

// concernsMe: the cards of the notification center the inbox takes (shares and likes are not things to do).
func concernsMe(n lark.DocNotice) bool {
	if n.Muted {
		return false
	}
	return n.AtMe || n.Kind == "mention" || n.Kind == "comment" || n.Kind == "reply"
}

func (h *Hub) collectDocs(ctx context.Context) ([]*InboxItem, error) {
	notices, err := h.lark.DocNotices(ctx)
	if err != nil {
		return nil, err
	}
	changed := h.inbox.applyDocs(notices, time.Now())
	if len(changed) == 0 {
		return nil, nil
	}
	me, err := h.larkMe(ctx)
	if err != nil {
		return changed, err
	}
	for _, it := range changed {
		h.readDoc(ctx, it, me)
	}
	return changed, nil
}

// larkMe is the signed-in account, read once.
func (h *Hub) larkMe(ctx context.Context) (lark.Me, error) {
	h.inbox.mu.Lock()
	me := h.inbox.me
	h.inbox.mu.Unlock()
	if me.Name != "" {
		return me, nil
	}
	me, err := h.lark.Me(ctx)
	if err != nil {
		return me, err
	}
	h.inbox.mu.Lock()
	h.inbox.me = me
	h.inbox.mu.Unlock()
	return me, nil
}

// applyDocs turns the notification cards into items: one open item per card (a document) until it is done; newer
// activity on it after that starts a new one. The returned items still need their document read (readDoc).
func (b *inbox) applyDocs(notices []lark.DocNotice, now time.Time) []*InboxItem {
	b.mu.Lock()
	defer b.mu.Unlock()
	var changed []*InboxItem
	for _, n := range notices {
		if !concernsMe(n) || n.ID == "" || now.Sub(n.UpdatedAt) > inboxWindow {
			continue
		}
		last := strconv.FormatInt(n.UpdatedAt.UnixMilli(), 10)
		open, done := b.openFor(func(it *InboxItem) bool { return it.Doc != nil && it.Doc.NoticeID == n.ID })
		if open != nil && open.Last == last || open == nil && done != nil && !n.UpdatedAt.After(done.UpdatedAt) {
			continue
		}
		it := open
		if it == nil {
			it = &InboxItem{ID: "doc:" + n.ID + ":" + last, Source: "lark", Kind: KindDoc, Account: "feishu", AccountName: "飞书",
				StartedAt: n.UpdatedAt, Doc: &InboxDoc{NoticeID: n.ID}}
			b.items[it.ID] = it
		}
		it.Title = n.Title
		if it.Title == "" {
			it.Title = "（无标题文档）"
		}
		it.Doc.URL, it.Doc.Type, it.Doc.Notice = n.URL, docType(n.Type), n.Kind
		if n.AtMe && it.Doc.Notice != "comment" && it.Doc.Notice != "reply" {
			it.Doc.Notice = "mention"
		}
		it.Sender = ""
		if m := noticeActor.FindStringSubmatch(n.Summary); m != nil {
			it.Sender = strings.TrimSpace(m[1])
		}
		it.Preview = clip(oneLine(n.Summary), 200)
		it.Count = 1
		it.UpdatedAt = n.UpdatedAt
		it.Read = false
		it.Last = last
		changed = append(changed, it)
	}
	return changed
}

// readDoc reads the document behind an item: the rows that mention me with their status columns (sheet, Base), the
// comment threads that mention me, the text around the mention (docx), and builds the item's context from them.
func (h *Hub) readDoc(ctx context.Context, it *InboxItem, me lark.Me) {
	h.inbox.mu.Lock()
	d := *it.Doc
	title, summary := it.Title, it.Preview
	h.inbox.mu.Unlock()
	rd := docRead{me: me, loc: h.userLocation()}
	err := h.readDocInto(ctx, &d, &rd)
	now := time.Now().UTC()
	h.inbox.mu.Lock()
	defer h.inbox.mu.Unlock()
	if it.Doc == nil || it.Doc.NoticeID != d.NoticeID {
		return
	}
	it.Doc.Link, it.Doc.Type, it.Doc.ReadAt = d.Link, d.Type, &now
	it.Doc.Rows, it.Doc.Reply, it.Doc.Error = rd.rows, rd.reply(d), ""
	if err != nil {
		it.Doc.Error = clip(oneLine(err.Error()), 200)
	}
	it.Context = docContext(title, summary, d, rd, err)
}

// docRead is what readDocInto found.
type docRead struct {
	me      lark.Me
	loc     *time.Location // the user's time zone: a 多维表格 date is the day there
	rows    []DocRow
	threads []lark.CommentThread // unresolved, mentioning me, newest last
	text    string               // docx / doc: the text around the mention
	// Where the mention is in a sheet (the reply goes on that cell's comment).
	cell *DocReply
}

func (h *Hub) readDocInto(ctx context.Context, d *InboxDoc, rd *docRead) error {
	d.Link = d.URL
	if d.Type == "wiki" || d.Type == "other" {
		hd, err := h.lark.DocHeader(ctx, d.URL)
		if err != nil {
			return err
		}
		d.Type = docType(hd.Type)
		if u, err := url.Parse(d.URL); err == nil && hd.Token != "" {
			path := map[string]string{"sheet": "/sheets/", "base": "/base/", "docx": "/docx/", "doc": "/docs/"}[d.Type]
			if path != "" {
				u.Path, u.RawQuery, u.Fragment = path+hd.Token, "", ""
				d.Link = u.String()
			}
		}
	}
	var err error
	switch d.Type {
	case "sheet":
		err = h.readSheetRows(ctx, d.Link, rd)
	case "base":
		err = h.readBaseRows(ctx, d.Link, rd)
	case "docx", "doc":
		if _, md, e := h.lark.DocMarkdown(ctx, d.Link); e == nil {
			rd.text = aroundMention(md, "@"+rd.me.Name, 700)
		} else {
			err = e
		}
	}
	if d.Type != "other" {
		if threads, e := h.lark.Comments(ctx, d.Link, d.Type == "base"); e == nil {
			rd.threads = threadsAboutMe(threads, rd.me)
		} else if err == nil && d.Type != "base" {
			err = e
		}
	}
	return err
}

// reply picks where 发送 goes: the newest comment thread that mentions me, else a comment on the row / cell that
// does, else a comment on the document.
func (rd docRead) reply(d InboxDoc) *DocReply {
	if n := len(rd.threads); n > 0 {
		return &DocReply{Kind: "thread", Label: "回复评论", Thread: rd.threads[n-1].ID}
	}
	switch d.Type {
	case "base":
		if len(rd.rows) > 0 {
			tbl, rec, _ := strings.Cut(rd.rows[0].Ref, "/")
			return &DocReply{Kind: "record", Label: "评论这条记录", Table: tbl, Record: rec}
		}
	case "sheet":
		if rd.cell != nil {
			return rd.cell
		}
	case "docx", "doc":
		return &DocReply{Kind: "doc", Label: "评论文档"}
	}
	return nil
}

func mentionsMe(text string, me lark.Me) bool {
	return me.Name != "" && strings.Contains(text, "@"+me.Name)
}

func threadsAboutMe(threads []lark.CommentThread, me lark.Me) []lark.CommentThread {
	var out []lark.CommentThread
	for _, t := range threads {
		if t.Resolved {
			continue
		}
		about := false
		for _, r := range t.Replies {
			for _, s := range r.Content {
				if s.Kind == "mention" && (s.ID == me.UserID && me.UserID != "" || s.Name == me.Name && me.Name != "") || mentionsMe(s.Text, me) {
					about = true
				}
			}
		}
		if about {
			out = append(out, t)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return threadTime(out[i]).Before(threadTime(out[j])) })
	if len(out) > 3 {
		out = out[len(out)-3:]
	}
	return out
}

func threadTime(t lark.CommentThread) time.Time {
	if n := len(t.Replies); n > 0 && !t.Replies[n-1].CreatedAt.IsZero() {
		return t.Replies[n-1].CreatedAt
	}
	return t.CreatedAt
}

func commentText(r lark.CommentReply) string {
	var b strings.Builder
	for _, s := range r.Content {
		switch {
		case s.Kind == "mention" && s.Name != "":
			b.WriteString("@" + s.Name)
		default:
			b.WriteString(s.Text)
		}
	}
	return strings.TrimSpace(b.String())
}

// aroundMention is the part of a document around the first mention (or its start).
func aroundMention(md, mention string, radius int) string {
	r := []rune(md)
	i := strings.Index(md, mention)
	if i < 0 || mention == "@" {
		return clip(md, radius*2)
	}
	at := len([]rune(md[:i]))
	from, to := max(at-radius, 0), min(at+radius, len(r))
	s := string(r[from:to])
	if from > 0 {
		s = "…" + s
	}
	if to < len(r) {
		s += "…"
	}
	return s
}

// ---- sheets ----

func (h *Hub) readSheetRows(ctx context.Context, link string, rd *docRead) error {
	sp, err := h.lark.Sheet(ctx, link, "")
	if err != nil {
		return err
	}
	for i, tab := range sp.Sheets {
		if i >= docTablesMax {
			break
		}
		if tab.Unloaded {
			full, err := h.lark.Sheet(ctx, link, tab.ID)
			if err != nil {
				continue
			}
			for _, t := range full.Sheets {
				if t.ID == tab.ID {
					tab = t
				}
			}
		}
		rows, cell := sheetRows(tab, rd.me)
		rd.rows = append(rd.rows, rows...)
		if rd.cell == nil {
			rd.cell = cell
		}
	}
	rd.rows = pickRows(rd.rows)
	return nil
}

// sheetRows finds a tab's rows that mention me. The header is the first row with cells; status columns are the
// header cells named like a status, their values the dropdown's items (or what the column already holds).
func sheetRows(tab lark.SheetTab, me lark.Me) ([]DocRow, *DocReply) {
	byRow := map[int]map[int]lark.SheetCell{}
	header := -1
	for _, c := range tab.Cells {
		if strings.TrimSpace(c.Text) == "" && len(c.Choices) == 0 {
			continue
		}
		if byRow[c.Row] == nil {
			byRow[c.Row] = map[int]lark.SheetCell{}
		}
		byRow[c.Row][c.Column] = c
		if strings.TrimSpace(c.Text) != "" && (header < 0 || c.Row < header) {
			header = c.Row
		}
	}
	if header < 0 {
		return nil, nil
	}
	heads := map[int]string{}
	var cols []int
	for col, c := range byRow[header] {
		if t := strings.TrimSpace(c.Text); t != "" {
			heads[col] = t
			cols = append(cols, col)
		}
	}
	sort.Ints(cols)
	var statusCols []int
	for _, col := range cols {
		if statusName.MatchString(heads[col]) {
			statusCols = append(statusCols, col)
		}
	}
	isStatus := func(col int) bool {
		for _, s := range statusCols {
			if s == col {
				return true
			}
		}
		return false
	}
	var rowNums []int
	for r := range byRow {
		if r > header {
			rowNums = append(rowNums, r)
		}
	}
	sort.Ints(rowNums)
	var out []DocRow
	var cell *DocReply
	for _, r := range rowNums {
		cells := byRow[r]
		mentionCol := -1
		for col, c := range cells {
			if mentionsMe(c.Text, me) && (mentionCol < 0 || col < mentionCol) {
				mentionCol = col
			}
		}
		if mentionCol < 0 {
			continue
		}
		if cell == nil {
			cell = &DocReply{Kind: "cell", Label: "评论这一行", Sheet: tab.ID, Row: r, Column: mentionCol}
		}
		row := DocRow{Ref: fmt.Sprintf("%s!%d", tab.ID, r), Table: tab.Name}
		for _, col := range cols {
			c, ok := cells[col]
			text := strings.TrimSpace(c.Text)
			if isStatus(col) {
				st := DocStatus{Field: strconv.Itoa(col), Name: heads[col], Value: text}
				for _, ch := range c.Choices {
					st.Options = append(st.Options, DocOption{Name: ch.Text, Color: ch.Color})
				}
				if len(st.Options) == 0 { // no dropdown: what the column holds
					seen := map[string]bool{}
					for _, rn := range rowNums {
						if v := strings.TrimSpace(byRow[rn][col].Text); v != "" && !seen[v] && len(st.Options) < docOptions {
							seen[v] = true
							st.Options = append(st.Options, DocOption{Name: v})
						}
					}
				}
				row.Status = append(row.Status, st)
				continue
			}
			if !ok || text == "" {
				continue
			}
			if row.Title == "" {
				row.Title = clip(oneLine(text), 80)
				continue
			}
			if len(row.Fields) < docFieldsMax {
				row.Fields = append(row.Fields, DocField{Name: heads[col], Value: clip(oneLine(text), 120)})
			}
		}
		if row.Title == "" {
			row.Title = fmt.Sprintf("第 %d 行", r+1)
		}
		out = append(out, row)
	}
	return out, cell
}

// pickRows keeps the rows to show: unfinished ones first, in document order, at most docRowsMax.
func pickRows(rows []DocRow) []DocRow {
	final := func(r DocRow) bool {
		for _, s := range r.Status {
			if s.Value != "" && finalStatus.MatchString(s.Value) {
				return true
			}
		}
		return false
	}
	sort.SliceStable(rows, func(i, j int) bool { return !final(rows[i]) && final(rows[j]) })
	if len(rows) > docRowsMax {
		rows = rows[:docRowsMax]
	}
	return rows
}

// ---- Base ----

func (h *Hub) readBaseRows(ctx context.Context, link string, rd *docRead) error {
	first, err := h.lark.BaseTable(ctx, link, "")
	if err != nil {
		return err
	}
	tables := []string{first.Table.ID}
	for _, t := range first.Tables {
		if t.ID != first.Table.ID && len(tables) < docTablesMax {
			tables = append(tables, t.ID)
		}
	}
	for _, id := range tables {
		tbl := first.Table
		if id != first.Table.ID {
			b, err := h.lark.BaseTable(ctx, link, id)
			if err != nil {
				continue
			}
			tbl = b.Table
		}
		records := tbl.Records
		next := tbl.Next
		for page := 1; next != nil && page < docPagesMax; page++ {
			more, n, err := h.lark.BaseRecords(ctx, link, tbl.ID, next)
			if err != nil {
				break
			}
			records, next = append(records, more...), n
		}
		tbl.Records = records
		rd.rows = append(rd.rows, baseRows(tbl, rd.me, rd.loc)...)
	}
	rd.rows = pickRows(rd.rows)
	return nil
}

// Base option colors (the palette index the field stores) as the app shows them.
var baseColors = []string{"#bacefd", "#fed4a4", "#b1e8fc", "#c5f2bb", "#f9c8de", "#d7c8f8", "#fbe39a", "#c8e4c2", "#f7b8b0", "#dee0e3"}

// baseRows finds a table's records that mention me (an @me in a text cell, or me in a person field).
func baseRows(tbl lark.BaseTable, me lark.Me, loc *time.Location) []DocRow {
	var out []DocRow
	for _, rec := range tbl.Records {
		mine := false
		for _, c := range rec.Cells {
			if c == nil {
				continue
			}
			for _, r := range c.Runs {
				if mentionsMe(r.Text, me) {
					mine = true
				}
			}
			if mentionsMe(c.Text, me) {
				mine = true
			}
			if c.Type == "user" {
				for _, id := range c.IDs {
					if id == me.UserID && id != "" {
						mine = true
					}
				}
			}
		}
		if !mine {
			continue
		}
		row := DocRow{Ref: tbl.ID + "/" + rec.ID, Table: tbl.Name}
		for _, f := range tbl.Fields {
			c := rec.Cells[f.ID]
			if f.Type == 3 && len(f.Options) > 0 && statusName.MatchString(f.Name) {
				st := DocStatus{Field: f.ID, Name: f.Name}
				for _, o := range f.Options {
					col := ""
					if o.Color >= 0 {
						col = baseColors[o.Color%len(baseColors)]
					}
					st.Options = append(st.Options, DocOption{Name: o.Name, Color: col})
					if c != nil && len(c.IDs) > 0 && c.IDs[0] == o.ID {
						st.Value = o.Name
					}
				}
				if len(st.Options) > docOptions {
					st.Options = st.Options[:docOptions]
				}
				row.Status = append(row.Status, st)
				continue
			}
			v := baseCellText(f, c, loc)
			if v == "" {
				continue
			}
			if f.Primary && row.Title == "" {
				row.Title = clip(oneLine(v), 80)
			} else if len(row.Fields) < docFieldsMax {
				row.Fields = append(row.Fields, DocField{Name: f.Name, Value: clip(oneLine(v), 120)})
			}
		}
		if row.Title == "" {
			if len(row.Fields) > 0 {
				row.Title, row.Fields = row.Fields[0].Value, row.Fields[1:]
			} else {
				row.Title = "（空记录）"
			}
		}
		out = append(out, row)
	}
	return out
}

func baseCellText(f lark.BaseField, c *lark.BaseCell, loc *time.Location) string {
	if c == nil {
		return ""
	}
	switch c.Type {
	case "text", "url", "email", "phone":
		var b strings.Builder
		for _, r := range c.Runs {
			b.WriteString(r.Text)
		}
		if b.Len() == 0 {
			b.WriteString(c.Text)
		}
		return strings.TrimSpace(b.String())
	case "option":
		var names []string
		for _, id := range c.IDs {
			for _, o := range f.Options {
				if o.ID == id {
					names = append(names, o.Name)
				}
			}
		}
		return strings.Join(names, "、")
	case "user", "group":
		var names []string
		for _, u := range c.Users {
			names = append(names, u.Name)
		}
		return strings.Join(names, "、")
	case "number":
		if c.Number != nil {
			return strconv.FormatFloat(*c.Number, 'f', -1, 64)
		}
	case "date":
		if ms, err := strconv.ParseInt(strings.Trim(string(c.Value), `"`), 10, 64); err == nil && ms > 0 {
			return time.UnixMilli(ms).In(loc).Format("2006-01-02")
		}
	case "checkbox":
		if c.Checked != nil && *c.Checked {
			return "✓"
		}
	}
	return ""
}

// ---- context ----

func docContext(title, summary string, d InboxDoc, rd docRead, err error) string {
	kind := map[string]string{"sheet": "电子表格", "base": "多维表格", "docx": "文档", "doc": "文档"}[d.Type]
	if kind == "" {
		kind = "云文档"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "飞书%s「%s」的通知：%s\n链接：%s\n", kind, title, summary, d.URL)
	if err != nil {
		fmt.Fprintf(&b, "（读文档失败：%s）\n", clip(oneLine(err.Error()), 120))
	}
	if len(rd.rows) > 0 {
		b.WriteString("\n提到用户的行（ref 用来改状态；状态列 field=列的 id，可选值在括号里）：\n")
		for i, r := range rd.rows {
			fmt.Fprintf(&b, "%d. ", i+1)
			if r.Table != "" {
				fmt.Fprintf(&b, "[%s] ", r.Table)
			}
			b.WriteString(r.Title)
			for _, f := range r.Fields {
				fmt.Fprintf(&b, "；%s=%s", f.Name, f.Value)
			}
			for _, s := range r.Status {
				var opts []string
				for _, o := range s.Options {
					opts = append(opts, o.Name)
				}
				v := s.Value
				if v == "" {
					v = "（空）"
				}
				fmt.Fprintf(&b, "；%s=%s（field=%s，可选：%s）", s.Name, v, s.Field, strings.Join(opts, " / "))
			}
			fmt.Fprintf(&b, "（ref=%s）\n", r.Ref)
		}
	}
	if len(rd.threads) > 0 {
		b.WriteString("\n提到用户的评论：\n")
		for _, t := range rd.threads {
			if q := strings.TrimSpace(t.Quote); q != "" {
				fmt.Fprintf(&b, "- 评论「%s」：\n", clip(oneLine(q), 80))
			} else {
				b.WriteString("- 评论：\n")
			}
			replies := t.Replies
			if len(replies) > 6 {
				replies = replies[len(replies)-6:]
			}
			for _, r := range replies {
				fmt.Fprintf(&b, "  %s：%s\n", r.Author.Name, clip(oneLine(commentText(r)), inboxText))
			}
		}
	}
	if rd.text != "" {
		fmt.Fprintf(&b, "\n文档里 @用户 附近的内容：\n%s\n", rd.text)
	}
	return strings.TrimSpace(b.String())
}

// ---- writes ----

// docOffers: the change names a row and status column of the item and one of the column's values.
func docOffers(d InboxDoc, ch DocChange) bool {
	for _, r := range d.Rows {
		for _, st := range r.Status {
			if r.Ref == ch.Ref && st.Field == ch.Field {
				for _, o := range st.Options {
					if o.Name == ch.Value {
						return true
					}
				}
			}
		}
	}
	return false
}

var errNoDocRow = errors.New("this row or status column is not in the item")

// setDocStatus moves one row's status in the document (re-read first, so a change made meanwhile is not
// overwritten), then reads the document again for the item.
func (h *Hub) setDocStatus(ctx context.Context, id string, ch DocChange) (InboxItem, error) {
	h.inbox.mu.Lock()
	it, ok := h.inbox.items[id]
	var d InboxDoc
	if ok && it.Doc != nil {
		d = *it.Doc
	}
	h.inbox.mu.Unlock()
	if !ok || d.NoticeID == "" {
		return InboxItem{}, errNoInboxItem
	}
	if err := h.writeDocStatus(ctx, d, ch); err != nil {
		return InboxItem{}, err
	}
	if me, err := h.larkMe(ctx); err == nil {
		h.readDoc(ctx, it, me)
	}
	h.inbox.mu.Lock()
	out := it.listed()
	h.inbox.mu.Unlock()
	h.saveInbox(ctx, []*InboxItem{it})
	return out, nil
}

func (h *Hub) writeDocStatus(ctx context.Context, d InboxDoc, ch DocChange) error {
	var st *DocStatus
	for _, r := range d.Rows {
		if r.Ref != ch.Ref {
			continue
		}
		for i := range r.Status {
			if r.Status[i].Field == ch.Field {
				st = &r.Status[i]
			}
		}
	}
	if st == nil {
		return errNoDocRow
	}
	allowed := false
	for _, o := range st.Options {
		allowed = allowed || o.Name == ch.Value
	}
	if !allowed {
		return fmt.Errorf("「%s」不是「%s」的可选值", ch.Value, st.Name)
	}
	link := d.Link
	if link == "" {
		link = d.URL
	}
	switch d.Type {
	case "base":
		tbl, rec, _ := strings.Cut(ch.Ref, "/")
		b, err := h.lark.BaseTable(ctx, link, tbl)
		if err != nil {
			return err
		}
		var field *lark.BaseField
		for i := range b.Table.Fields {
			if b.Table.Fields[i].ID == ch.Field {
				field = &b.Table.Fields[i]
			}
		}
		if field == nil {
			return errNoDocRow
		}
		optID := ""
		for _, o := range field.Options {
			if o.Name == ch.Value {
				optID = o.ID
			}
		}
		if optID == "" {
			return fmt.Errorf("「%s」不是「%s」的可选值", ch.Value, st.Name)
		}
		records, next := b.Table.Records, b.Table.Next
		var found *lark.BaseRecord
		for page := 0; found == nil; page++ {
			for i := range records {
				if records[i].ID == rec {
					found = &records[i]
				}
			}
			if found != nil || next == nil || page >= docPagesMax {
				break
			}
			if records, next, err = h.lark.BaseRecords(ctx, link, tbl, next); err != nil {
				return err
			}
		}
		if found == nil {
			return errNoDocRow
		}
		return h.lark.SetBaseOption(ctx, link, tbl, rec, ch.Field, optID, found.Cells[ch.Field].Fingerprint())
	case "sheet":
		tab, rowS, _ := strings.Cut(ch.Ref, "!")
		row, err1 := strconv.Atoi(rowS)
		col, err2 := strconv.Atoi(ch.Field)
		if err1 != nil || err2 != nil {
			return errNoDocRow
		}
		sp, err := h.lark.Sheet(ctx, link, tab)
		if err != nil {
			return err
		}
		var cell *lark.SheetCell
		for _, t := range sp.Sheets {
			if t.ID != tab {
				continue
			}
			for i := range t.Cells {
				if t.Cells[i].Row == row && t.Cells[i].Column == col {
					cell = &t.Cells[i]
				}
			}
		}
		if cell != nil && len(cell.Choices) > 0 {
			return h.lark.ChooseSheetCell(ctx, link, tab, sp.Revision, row, col, ch.Value)
		}
		base := ""
		if cell != nil {
			base = cell.Base()
		}
		return h.lark.SetSheetCell(ctx, link, tab, sp.Revision, row, col, ch.Value, base)
	}
	return errors.New("this document has no status to change")
}

// replyDoc posts the reply where DocReply says (a comment).
func (h *Hub) replyDoc(ctx context.Context, d InboxDoc, text string) error {
	link := d.Link
	if link == "" {
		link = d.URL
	}
	r := d.Reply
	if r == nil {
		return errors.New("这份文档没有能回复的地方")
	}
	switch r.Kind {
	case "thread":
		return h.lark.ReplyComment(ctx, link, r.Thread, text)
	case "record":
		return h.lark.CommentRecord(ctx, link, r.Table, r.Record, text)
	case "cell":
		return h.lark.CommentCell(ctx, link, r.Sheet, r.Row, r.Column, text)
	case "doc":
		return h.lark.CommentDoc(ctx, link, text)
	}
	return errors.New("这份文档没有能回复的地方")
}

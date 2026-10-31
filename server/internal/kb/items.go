package kb

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ItemSource lists documents, meetings and calendar events (Feishu through lark-connector).
type ItemSource interface {
	Items(ctx context.Context) ([]Item, error)
	// DocTitle looks up a document's title by its link ("" when it cannot).
	DocTitle(ctx context.Context, url string) string
}

// CollectItems stores new meetings and events and fills in titles of documents seen in chats.
func (e *Engine) CollectItems(ctx context.Context, src ItemSource) error {
	items, err := src.Items(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, it := range items {
		old, ok := e.Store.Item(it.Kind, it.ID)
		if ok {
			if old.Title == it.Title && old.At.Equal(it.At) {
				continue
			}
			it.Tagging, it.Projects, it.Note, it.Seen = old.Tagging, old.Projects, old.Note, old.Seen
			it.Tagging.TaggedAt = time.Time{} // changed: tag again
		} else {
			it.Seen = now
		}
		it := it
		if err := e.Store.PutItem(&it); err != nil {
			return err
		}
	}
	n := 0
	for _, it := range e.Store.Items(KindDoc) {
		if it.Title != "" || n >= 10 || ctx.Err() != nil {
			continue
		}
		n++
		if t := src.DocTitle(ctx, it.URL); t != "" {
			it.Title = t
		} else {
			it.Title = it.URL
		}
		_ = e.Store.PutItem(it)
	}
	return nil
}

type itemTags struct {
	Items []struct {
		ID         string   `json:"id"`
		Importance string   `json:"importance"`
		Relevance  int      `json:"relevance"`
		Tags       []string `json:"tags"`
		Project    string   `json:"project"`
		Note       string   `json:"note"`
	} `json:"items"`
}

const itemPrompt = `你在给 {{owner}} 的文档、会议、日程打标。「我」就是 {{owner}}。每行一条：id | 类型 | 标题 | 时间 | 来源。
JSON：{"items":[{"id","importance","relevance","tags","project","note"}]}，每条都要有：
importance P0–P3（对我的重要级，P3 是例行 / 无关），relevance 0–100（与我相关性），tags 1–4 个短标签（优先复用已有标签），
project 是「已知项目」里对应的项目原名（没有就空），note 一句话（20 字内，可空）。不知道的不要编。`

// TagItems tags documents, meetings and events that are new or changed, 40 per call.
func (e *Engine) TagItems(ctx context.Context) error {
	if e.Model == nil {
		return nil
	}
	var todo []*Item
	for _, k := range []string{KindEvent, KindMeeting, KindDoc} {
		for _, it := range e.Store.Items(k) {
			if it.TaggedAt.IsZero() && it.Title != "" && !it.Locked {
				todo = append(todo, it)
			}
		}
	}
	for len(todo) > 0 && ctx.Err() == nil {
		batch := todo[:min(40, len(todo))]
		todo = todo[len(batch):]
		byID := map[string]*Item{}
		var in strings.Builder
		fmt.Fprintf(&in, "已知项目：%s\n已有标签：%s\n\n", e.projectNames(), strings.Join(e.TopTags(40), "、"))
		for _, it := range batch {
			key := it.Kind + "/" + it.ID
			byID[key] = it
			at := ""
			if !it.At.IsZero() {
				at = it.At.Local().Format("2006-01-02 15:04")
			}
			where := it.Where
			if c, ok := e.Store.Chat(it.Where); ok {
				where = c.Name
			}
			fmt.Fprintf(&in, "%s | %s | %s | %s | %s\n", key, map[string]string{KindDoc: "文档", KindMeeting: "会议", KindEvent: "日程"}[it.Kind], it.Title, at, where)
		}
		var out itemTags
		cctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		err := e.Model.JSONMax(cctx, strings.ReplaceAll(itemPrompt, "{{owner}}", e.Owner), in.String(), 6000, &out)
		cancel()
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		for _, t := range out.Items {
			it := byID[t.ID]
			if it == nil {
				continue
			}
			applyTags(&it.Tagging, t.Importance, t.Relevance, t.Tags, now)
			it.Note = oneLine(t.Note)
			if p := e.FindProject(t.Project); p != nil {
				it.Projects = addUnique(it.Projects, p.Slug)
				key := it.Kind + "/" + SafeID(it.ID)
				n := len(p.Items)
				if p.Items = addUnique(p.Items, key); len(p.Items) != n {
					_ = e.Store.PutProject(p)
				}
			}
			_ = e.Store.PutItem(it)
			e.index(it.Kind, it.ID, it.Title, it.Tagging, it.Note, it.At)
		}
		// Ones the model skipped: mark tagged anyway so they do not come back every run.
		for _, it := range batch {
			if it.TaggedAt.IsZero() {
				it.TaggedAt = now
				_ = e.Store.PutItem(it)
			}
		}
	}
	return nil
}

// --- reading ---

// Hit is one search result.
type Hit struct {
	Path string `json:"path"` // relative to the data dir; absolute for a file in one of the ExtraDirs
	Line string `json:"line"`
}

// Search finds lines containing every word of the query (case-insensitive) in summaries, metadata, timelines, the
// Markdown of the ExtraDirs and (when messages is set) the message archive; newest message days first. At most limit
// hits.
func (e *Engine) Search(query string, messages bool, limit int) []Hit {
	words := strings.Fields(strings.ToLower(query))
	if len(words) == 0 {
		return nil
	}
	match := func(l string) bool {
		l = strings.ToLower(l)
		for _, w := range words {
			if !strings.Contains(l, w) {
				return false
			}
		}
		return true
	}
	var hits []Hit
	scan := func(p string) {
		raw, err := os.ReadFile(p)
		if err != nil {
			return
		}
		rel, err := filepath.Rel(e.Store.Dir, p)
		if err != nil || strings.HasPrefix(rel, "..") {
			rel = p
		}
		for _, l := range strings.Split(string(raw), "\n") {
			if len(hits) >= limit {
				return
			}
			if match(l) {
				hits = append(hits, Hit{Path: rel, Line: clip(strings.TrimSpace(l), 300)})
			}
		}
	}
	var files, days, moments []string
	sep := string(filepath.Separator)
	_ = filepath.WalkDir(e.Store.Dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		switch {
		case strings.Contains(p, sep+"moments"+sep+"posts"+sep):
			// The removed archive (moments.go): thousands of posts, read after everything else so they never crowd it out.
			if strings.HasSuffix(p, ".md") {
				moments = append(moments, p)
			}
		case strings.Contains(p, string(filepath.Separator)+"attachments"+string(filepath.Separator)):
		case strings.Contains(p, string(filepath.Separator)+"messages"+string(filepath.Separator)):
			if messages {
				days = append(days, p)
			}
		case strings.HasSuffix(p, ".md") || strings.HasSuffix(p, ".json"):
			files = append(files, p)
		}
		return nil
	})
	for _, dir := range e.ExtraDirs {
		_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() && p != dir && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir // .git and the like
			}
			if !d.IsDir() && strings.HasSuffix(p, ".md") {
				files = append(files, p)
			}
			return nil
		})
	}
	for _, p := range files {
		scan(p)
	}
	sort.Slice(days, func(i, j int) bool { return filepath.Base(days[i]) > filepath.Base(days[j]) })
	for _, p := range days {
		if len(hits) >= limit {
			break
		}
		scan(p)
	}
	sort.Slice(moments, func(i, j int) bool { return moments[i] > moments[j] }) // newer post ids first
	for _, p := range moments {
		if len(hits) >= limit {
			break
		}
		scan(p)
	}
	return hits
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// Entry is a row of a ranked list.
type Entry struct {
	Kind       string    `json:"kind"`
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	OneLine    string    `json:"one_line,omitempty"`
	Importance string    `json:"importance,omitempty"`
	Relevance  int       `json:"relevance"`
	Tags       []string  `json:"tags,omitempty"`
	Updated    time.Time `json:"updated"`
	Extra      string    `json:"extra,omitempty"` // projects: short / long · status; chats: dm / group…
}

// Rank orders by importance, then relevance, then recency.
func Rank(list []Entry) {
	imp := func(s string) int {
		if s == "" {
			return 4
		}
		return int(s[1] - '0')
	}
	sort.SliceStable(list, func(i, j int) bool {
		a, b := list[i], list[j]
		if imp(a.Importance) != imp(b.Importance) {
			return imp(a.Importance) < imp(b.Importance)
		}
		if a.Relevance != b.Relevance {
			return a.Relevance > b.Relevance
		}
		return a.Updated.After(b.Updated)
	})
}

// List returns the entities of a kind as ranked entries; tag filters to those carrying it.
func (e *Engine) List(kind, tag string) []Entry {
	var out []Entry
	has := func(t []string) bool {
		if tag == "" {
			return true
		}
		for _, x := range t {
			if x == tag {
				return true
			}
		}
		return false
	}
	switch kind {
	case KindProject:
		for _, p := range e.Store.Projects() {
			if has(p.Tags) {
				out = append(out, Entry{Kind: kind, ID: p.Slug, Name: p.Name, OneLine: p.OneLine, Importance: p.Importance,
					Relevance: p.Relevance, Tags: p.Tags, Updated: p.Updated, Extra: p.Kind + " · " + p.Status})
			}
		}
	case KindChat:
		for _, c := range e.Store.Chats() {
			if has(c.Tags) {
				out = append(out, Entry{Kind: kind, ID: c.ID, Name: c.Name, OneLine: c.OneLine, Importance: c.Importance,
					Relevance: c.Relevance, Tags: c.Tags, Updated: c.LastAt, Extra: c.Kind})
			}
		}
	case KindPerson:
		for _, p := range e.Store.People() {
			if has(p.Tags) {
				out = append(out, Entry{Kind: kind, ID: p.ID, Name: p.Name, OneLine: p.Note, Importance: p.Importance,
					Relevance: p.Relevance, Tags: p.Tags, Updated: p.LastSeen})
			}
		}
	case KindDoc, KindMeeting, KindEvent:
		for _, it := range e.Store.Items(kind) {
			if has(it.Tags) && it.Title != "" {
				out = append(out, Entry{Kind: kind, ID: it.ID, Name: it.Title, OneLine: it.Note, Importance: it.Importance,
					Relevance: it.Relevance, Tags: it.Tags, Updated: it.At, Extra: it.URL})
			}
		}
	}
	Rank(out)
	return out
}

// Prompt is a short Markdown digest of the most important projects and chats, for the assistant's live snapshot.
func (e *Engine) Prompt(projects, chats int) string {
	var b strings.Builder
	ps := e.List(KindProject, "")
	n := 0
	for _, p := range ps {
		if n >= projects || strings.HasSuffix(p.Extra, "done") || strings.HasSuffix(p.Extra, "ignored") {
			continue
		}
		if n == 0 {
			b.WriteString("重点项目（知识库 projects/）：\n")
		}
		fmt.Fprintf(&b, "- %s [%s 相关%d] %s（projects/%s）\n", p.Name, orDash(p.Importance), p.Relevance, p.OneLine, p.ID)
		n++
	}
	cs := e.List(KindChat, "")
	n = 0
	for _, c := range cs {
		if n >= chats || c.Importance == "P3" {
			continue
		}
		if n == 0 {
			b.WriteString("重点会话（知识库 chats/）：\n")
		}
		fmt.Fprintf(&b, "- %s [%s 相关%d] %s（chats/%s）\n", c.Name, orDash(c.Importance), c.Relevance, c.OneLine, c.ID)
		n++
	}
	return b.String()
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

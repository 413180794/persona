package kb

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Searching for the app (docs/knowledge.md「搜索」): results are entries, not lines. Every line containing all the
// words is found (archived messages newest day first), grouped by the entry it belongs to, the entries ranked
// (name matches, then importance, relevance, how much matched, recency), a few lines kept for each. Opening one, Matches
// gives every place in that entry with a few messages around each, and Day the whole day a message is in.

// Found is one line of an entry that matched.
type Found struct {
	Path string `json:"path"` // relative to the data dir
	Line string `json:"line"`
	// N is the line's number in its file (1-based): messages are found again by it on the day page.
	N int `json:"n"`
}

// Result is one entry found by a search: the entry as ranked lists show it and its first lines.
type Result struct {
	Entry Entry   `json:"entry"`
	Count int     `json:"count"` // lines matched in all, at most foundCap
	Hits  []Found `json:"hits"`
	// InName: the name itself matched (those come first).
	InName bool `json:"in_name,omitempty"`
}

const (
	hitsPerResult = 3
	foundCap      = 99
	// resultLines bounds the lines scanned out of the message archive for one search: past it the oldest days
	// are left unread (the entries already found are ranked as they are).
	resultLines = 4000
)

// matcher returns whether a line has every word of the query (case-insensitive); nil for an empty query.
func matcher(query string) func(string) bool {
	words := strings.Fields(strings.ToLower(query))
	if len(words) == 0 {
		return nil
	}
	return func(l string) bool {
		l = strings.ToLower(l)
		for _, w := range words {
			if !strings.Contains(l, w) {
				return false
			}
		}
		return true
	}
}

// entryOf maps a path relative to the data dir to the entry it belongs to: chats/<id>/…, docs/<id>.json.
func entryOf(rel string) (kind, id string, ok bool) {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) < 2 {
		return "", "", false
	}
	switch parts[0] {
	case KindChat, KindPerson, KindProject:
		if len(parts) < 3 {
			return "", "", false
		}
		return parts[0], parts[1], true
	case KindDoc, KindMeeting, KindEvent:
		if id, ok := strings.CutSuffix(parts[1], ".json"); ok && len(parts) == 2 {
			return parts[0], id, true
		}
	}
	return "", "", false
}

// Find searches every entry and returns at most limit, ranked.
func (e *Engine) Find(query string, limit int) []Result {
	match := matcher(query)
	if match == nil {
		return nil
	}
	known := map[string]Entry{}
	for _, k := range []string{KindProject, KindChat, KindPerson, KindDoc, KindMeeting, KindEvent} {
		for _, en := range e.List(k, "") {
			known[k+"/"+en.ID] = en
		}
	}
	by := map[string]*Result{}
	scanned := 0
	add := func(key string, f Found) {
		r := by[key]
		if r == nil {
			r = &Result{Entry: known[key]}
			by[key] = r
		}
		if r.Count < foundCap {
			r.Count++
		}
		if len(r.Hits) < hitsPerResult && !hasLine(r.Hits, f.Line) {
			r.Hits = append(r.Hits, f)
		}
	}
	scan := func(p string) {
		rel, _ := filepath.Rel(e.Store.Dir, p)
		kind, id, ok := entryOf(rel)
		if !ok {
			return
		}
		key := kind + "/" + id
		if _, ok := known[key]; !ok {
			return // an entry the lists do not show (a doc without a title yet): nothing to open
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return
		}
		for i, l := range strings.Split(string(raw), "\n") {
			scanned++
			if t := strings.TrimSpace(l); t != "" && !strings.HasPrefix(t, "# ") && match(t) {
				add(key, Found{Path: filepath.ToSlash(rel), Line: clip(t, 300), N: i + 1})
			}
		}
	}
	files, days := e.files()
	for _, p := range files {
		scan(p)
	}
	for _, p := range days {
		if scanned > resultLines*25 { // ~100k lines: a few hundred ms at most
			break
		}
		scan(p)
	}
	// Names: an entry whose name matches is found even when no file line does.
	for key, en := range known {
		if match(en.Name) {
			if by[key] == nil {
				by[key] = &Result{Entry: en}
			}
			by[key].InName = true
		}
	}
	out := make([]Result, 0, len(by))
	for _, r := range by {
		r.Hits = sortHits(r.Hits)
		out = append(out, *r)
	}
	imp := func(s string) int {
		if s == "" {
			return 4
		}
		return int(s[1] - '0')
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.InName != b.InName {
			return a.InName
		}
		if imp(a.Entry.Importance) != imp(b.Entry.Importance) {
			return imp(a.Entry.Importance) < imp(b.Entry.Importance)
		}
		if a.Entry.Relevance != b.Entry.Relevance {
			return a.Entry.Relevance > b.Entry.Relevance
		}
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		if !a.Entry.Updated.Equal(b.Entry.Updated) {
			return a.Entry.Updated.After(b.Entry.Updated)
		}
		return a.Entry.Kind+a.Entry.ID < b.Entry.Kind+b.Entry.ID
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// files lists the profile files (summaries, metadata, timelines) and the message days, newest day first.
func (e *Engine) files() (files, days []string) {
	sep := string(filepath.Separator)
	_ = filepath.WalkDir(e.Store.Dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == "attachments" {
				return filepath.SkipDir
			}
			return nil
		}
		switch {
		case strings.Contains(p, sep+"messages"+sep):
			days = append(days, p)
		case strings.HasSuffix(p, ".md") || strings.HasSuffix(p, ".json"):
			files = append(files, p)
		}
		return nil
	})
	sort.SliceStable(days, func(i, j int) bool { return filepath.Base(days[i]) > filepath.Base(days[j]) })
	return files, days
}

// sortHits puts what says most first: the profile, the timeline, messages, then metadata (a tag, a member's name).
func sortHits(h []Found) []Found {
	rank := func(f Found) int {
		switch {
		case strings.HasSuffix(f.Path, "/summary.md"):
			return 0
		case strings.HasSuffix(f.Path, "/log.md"):
			return 1
		case strings.Contains(f.Path, "/messages/"):
			return 2
		}
		return 3
	}
	sort.SliceStable(h, func(i, j int) bool { return rank(h[i]) < rank(h[j]) })
	return h
}

func hasLine(h []Found, line string) bool {
	for _, f := range h {
		if f.Line == line {
			return true
		}
	}
	return false
}

// Match is one place in an entry that matched: the line, and for a message the ones around it.
type Match struct {
	Path string `json:"path"`
	N    int    `json:"n"`
	Line string `json:"line"`
	// Day is a message's day ("2006-01-02"); Before / After the messages next to it that day (continuation lines
	// folded into their message).
	Day    string   `json:"day,omitempty"`
	Before []string `json:"before,omitempty"`
	After  []string `json:"after,omitempty"`
}

// Matches returns the places in one entry matching query: the profile, timeline and metadata, then messages, newest
// day first; at most limit.
func (e *Engine) Matches(kind, id, query string, limit int) []Match {
	match := matcher(query)
	if match == nil {
		return nil
	}
	var out []Match
	look := func(p string, msgs bool) {
		raw, err := os.ReadFile(p)
		if err != nil {
			return
		}
		rel, _ := filepath.Rel(e.Store.Dir, p)
		rel = filepath.ToSlash(rel)
		if !msgs {
			for i, l := range strings.Split(string(raw), "\n") {
				if len(out) >= limit {
					return
				}
				if t := strings.TrimSpace(l); t != "" && !strings.HasPrefix(t, "# ") && match(t) {
					out = append(out, Match{Path: rel, N: i + 1, Line: clip(t, 600)})
				}
			}
			return
		}
		day := strings.TrimSuffix(filepath.Base(p), ".md")
		ms := messages(string(raw))
		// Newest first within the day too, as the days are.
		for i := len(ms) - 1; i >= 0; i-- {
			if len(out) >= limit {
				return
			}
			m := ms[i]
			if !match(m.text) {
				continue
			}
			mt := Match{Path: rel, N: m.n, Line: clip(m.text, 1200), Day: day}
			for j := max(0, i-2); j < i; j++ {
				mt.Before = append(mt.Before, clip(ms[j].text, 300))
			}
			for j := i + 1; j < min(len(ms), i+3); j++ {
				mt.After = append(mt.After, clip(ms[j].text, 300))
			}
			out = append(out, mt)
		}
	}
	switch kind {
	case KindChat, KindPerson, KindProject:
		for _, f := range []string{"summary.md", "log.md", "meta.json"} {
			look(e.Store.path(kind, id, f), false)
		}
		if kind == KindChat {
			days := e.Store.Days(id)
			for i := len(days) - 1; i >= 0 && len(out) < limit; i-- {
				look(e.Store.path(KindChat, id, "messages", days[i]+".md"), true)
			}
		}
	case KindDoc, KindMeeting, KindEvent:
		look(filepath.Join(e.Store.Dir, kind, SafeID(id)+".json"), false)
	}
	return out
}

type message struct {
	n    int // line number of its first line (1-based)
	text string
}

// messages splits a day file into messages: "- HH:MM name: text" and the indented lines that continue it.
func messages(raw string) []message {
	var out []message
	for i, l := range strings.Split(raw, "\n") {
		switch {
		case strings.HasPrefix(l, "- "):
			out = append(out, message{n: i + 1, text: strings.TrimPrefix(l, "- ")})
		case strings.HasPrefix(l, "  ") && len(out) > 0:
			out[len(out)-1].text += "\n" + strings.TrimSpace(l)
		}
	}
	return out
}

// DayMessage is one message of a day page.
type DayMessage struct {
	N    int    `json:"n"`
	Text string `json:"text"`
}

// Day returns a chat's messages of one day ("2006-01-02") and the days before and after it that have any.
func (e *Engine) Day(chatID, day string) (msgs []DayMessage, prev, next string, err error) {
	if _, perr := time.Parse("2006-01-02", day); perr != nil {
		return nil, "", "", ErrNotFound
	}
	raw, rerr := os.ReadFile(e.Store.path(KindChat, chatID, "messages", day+".md"))
	if rerr != nil {
		return nil, "", "", ErrNotFound
	}
	for _, m := range messages(string(raw)) {
		msgs = append(msgs, DayMessage{N: m.n, Text: m.text})
	}
	for _, d := range e.Store.Days(chatID) {
		if d < day {
			prev = d
		} else if d > day && next == "" {
			next = d
		}
	}
	return msgs, prev, next, nil
}

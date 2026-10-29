package kb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Ingest appends messages of one chat that a source pushes (mail), creating the chat. Same lines, people and
// links as Archive.
func (e *Engine) Ingest(ctx context.Context, src Source, sc SourceChat, msgs []SourceMessage) (int, error) {
	id := ChatID(src.Name(), sc.ID)
	c, ok := e.Store.Chat(id)
	if !ok {
		c = &Chat{ID: id, Source: src.Name(), SourceID: sc.ID, Kind: sc.Kind, Name: sc.Name, Created: time.Now().UTC()}
	}
	var lines []Line
	for _, m := range msgs {
		l := e.line(ctx, src, c, m)
		if l.Text == "" {
			continue
		}
		lines = append(lines, l)
		e.notePerson(c, src.Name(), m)
		c.Mine = c.Mine || m.Mine
		if m.At.After(c.LastAt) {
			c.LastAt = m.At
		}
	}
	if len(lines) == 0 {
		return 0, nil
	}
	if err := e.Store.AppendMessages(c, lines); err != nil {
		return 0, err
	}
	c.Messages += len(lines)
	c.Pending += len(lines)
	if err := e.Store.PutChat(c); err != nil {
		return 0, err
	}
	e.index(KindChat, c.ID, c.Name, c.Tagging, c.OneLine, c.LastAt)
	return len(lines), nil
}

// Detail is everything about one entity: its metadata, compressed context, a project's timeline, a chat's latest text.
type Detail struct {
	Kind    string          `json:"kind"`
	ID      string          `json:"id"`
	Path    string          `json:"path"` // absolute, for reading more with file tools
	Meta    json.RawMessage `json:"meta"`
	Summary string          `json:"summary,omitempty"`
	Log     []string        `json:"log,omitempty"`
	Recent  string          `json:"recent,omitempty"`
	// Links are the linked projects, chats and people, named and ranked.
	Links []Entry `json:"links,omitempty"`
}

var ErrNotFound = errors.New("not found")

// Get reads one entity. recentChars bounds a chat's latest messages (0: none).
func (e *Engine) Get(kind, id string, recentChars int) (*Detail, error) {
	d := &Detail{Kind: kind, ID: SafeID(id)}
	var meta string
	switch kind {
	case KindChat, KindPerson, KindProject:
		meta = e.Store.path(kind, id, "meta.json")
		d.Path = e.Store.path(kind, id)
		d.Summary = e.Store.Summary(kind, id)
	case KindDoc, KindMeeting, KindEvent:
		meta = filepath.Join(e.Store.Dir, kind, SafeID(id)+".json")
		d.Path = meta
	default:
		return nil, fmt.Errorf("unknown kind %q", kind)
	}
	raw, err := os.ReadFile(meta)
	if err != nil {
		return nil, ErrNotFound
	}
	d.Meta = raw
	if kind == KindProject {
		d.Log = e.Store.Log(id, 30)
	}
	if kind == KindChat && recentChars > 0 {
		d.Recent = e.Store.Since(id, time.Now().AddDate(0, 0, -7), recentChars)
	}
	var refs struct {
		Projects []string `json:"projects"`
		Chats    []string `json:"chats"`
		People   []string `json:"people"`
	}
	_ = json.Unmarshal(raw, &refs)
	d.Links = e.entries(refs.Projects, refs.Chats, refs.People)
	return d, nil
}

// Update is a change by hand (the app) or by the assistant. Set fields only; tagging set here is locked.
type Update struct {
	Name       string   `json:"name,omitempty"`
	Kind       string   `json:"project_kind,omitempty"` // projects: short | long
	Status     string   `json:"status,omitempty"`
	Goal       string   `json:"goal,omitempty"`
	MyRole     string   `json:"my_role,omitempty"`
	Note       string   `json:"note,omitempty"` // people / items
	Importance string   `json:"importance,omitempty"`
	Relevance  *int     `json:"relevance,omitempty"`
	Tags       []string `json:"tags,omitempty"`
	Summary    string   `json:"summary,omitempty"` // replaces summary.md
	Log        string   `json:"log,omitempty"`     // projects: appended to the timeline
	Source     string   `json:"source,omitempty"`  // who writes the log line (default Jarvis)
	Aliases    []string `json:"aliases,omitempty"`
	Chats      []string `json:"chats,omitempty"`    // projects: chat ids to link
	People     []string `json:"people,omitempty"`   // projects: person ids to link
	Dirs       []string `json:"dirs,omitempty"`     // projects: "<machine>:<path>" code directories
	Projects   []string `json:"projects,omitempty"` // chats / people / items: project slugs to link
}

func (u Update) tag(t *Tagging) {
	if u.Importance != "" || u.Relevance != nil {
		t.Locked = true
	}
	if importanceRe.MatchString(u.Importance) {
		t.Importance = u.Importance
	}
	if u.Relevance != nil && *u.Relevance >= 0 && *u.Relevance <= 100 {
		t.Relevance = *u.Relevance
	}
	if len(u.Tags) > 0 {
		t.Tags = u.Tags
	}
}

// Apply changes one entity; for projects it creates the project when it does not exist (id = name or slug).
func (e *Engine) Apply(kind, id string, u Update) (string, error) {
	now := time.Now().UTC()
	switch kind {
	case KindProject:
		p, ok := e.Store.Project(id)
		if !ok {
			p = e.FindProject(id)
		}
		if p == nil {
			name := u.Name
			if name == "" {
				name = id
			}
			if Slug(name) == "" {
				return "", errors.New("a project needs a name")
			}
			p = &Project{Slug: Slug(name), Name: name, Kind: "short", Status: "active", Created: now}
		}
		if u.Name != "" {
			p.Name = u.Name
		}
		if u.Kind == "short" || u.Kind == "long" {
			p.Kind = u.Kind
		}
		if u.Status == "active" || u.Status == "paused" || u.Status == "done" || u.Status == "ignored" {
			p.Status = u.Status
		}
		if u.Goal != "" {
			p.Goal = oneLine(u.Goal)
		}
		if u.MyRole != "" {
			p.MyRole = oneLine(u.MyRole)
		}
		u.tag(&p.Tagging)
		p.Aliases = addUnique(p.Aliases, u.Aliases...)
		p.Dirs = addUnique(p.Dirs, u.Dirs...)
		p.People = addUnique(p.People, u.People...)
		for _, cid := range u.Chats {
			if c, ok := e.Store.Chat(cid); ok {
				p.Chats = addUnique(p.Chats, c.ID)
				c.Projects = addUnique(c.Projects, p.Slug)
				p.People = addUnique(p.People, c.People...)
				_ = e.Store.PutChat(c)
			}
		}
		if u.Summary != "" {
			_ = e.Store.PutSummary(KindProject, p.Slug, u.Summary)
		}
		if u.Log != "" {
			src := u.Source
			if src == "" {
				src = "Jarvis"
			}
			_ = e.Store.AppendLog(p.Slug, now, src, u.Log)
			if u.Summary == "" {
				p.Dirty = true
			}
		}
		if err := e.Store.PutProject(p); err != nil {
			return "", err
		}
		e.index(KindProject, p.Slug, p.Name, p.Tagging, p.OneLine, p.Updated)
		return p.Slug, nil
	case KindChat:
		c, ok := e.Store.Chat(id)
		if !ok {
			return "", ErrNotFound
		}
		u.tag(&c.Tagging)
		if u.Name != "" {
			c.Name = u.Name
		}
		for _, slug := range u.Projects {
			if pr, ok := e.Store.Project(slug); ok {
				c.Projects = addUnique(c.Projects, pr.Slug)
				pr.Chats = addUnique(pr.Chats, c.ID)
				_ = e.Store.PutProject(pr)
			}
		}
		if u.Summary != "" {
			_ = e.Store.PutSummary(KindChat, c.ID, u.Summary)
		}
		if err := e.Store.PutChat(c); err != nil {
			return "", err
		}
		e.index(KindChat, c.ID, c.Name, c.Tagging, c.OneLine, c.LastAt)
		return c.ID, nil
	case KindPerson:
		p, ok := e.Store.Person(id)
		if !ok {
			return "", ErrNotFound
		}
		u.tag(&p.Tagging)
		if u.Note != "" {
			p.Note = oneLine(u.Note)
		}
		for _, slug := range u.Projects {
			if pr, ok := e.Store.Project(slug); ok {
				p.Projects = addUnique(p.Projects, pr.Slug)
				pr.People = addUnique(pr.People, p.ID)
				_ = e.Store.PutProject(pr)
			}
		}
		if err := e.Store.PutPerson(p); err != nil {
			return "", err
		}
		e.WritePersonSummary(p.ID)
		e.index(KindPerson, p.ID, p.Name, p.Tagging, p.Note, p.LastSeen)
		return p.ID, nil
	case KindDoc, KindMeeting, KindEvent:
		it, ok := e.Store.Item(kind, id)
		if !ok {
			return "", ErrNotFound
		}
		u.tag(&it.Tagging)
		if u.Note != "" {
			it.Note = oneLine(u.Note)
		}
		for _, slug := range u.Projects {
			if pr, ok := e.Store.Project(slug); ok {
				it.Projects = addUnique(it.Projects, pr.Slug)
				pr.Items = addUnique(pr.Items, kind+"/"+SafeID(it.ID))
				_ = e.Store.PutProject(pr)
			}
		}
		if err := e.Store.PutItem(it); err != nil {
			return "", err
		}
		e.index(kind, it.ID, it.Title, it.Tagging, it.Note, it.At)
		return it.ID, nil
	}
	return "", fmt.Errorf("unknown kind %q", kind)
}

// ProjectsForDir returns the projects whose code directories contain dir on machine (Agents working there).
func (e *Engine) ProjectsForDir(machine, dir string) []*Project {
	var out []*Project
	for _, p := range e.Store.Projects() {
		for _, d := range p.Dirs {
			m, path, ok := strings.Cut(d, ":")
			if !ok {
				m, path = "", d
			}
			if (m == "" || m == machine) && (dir == path || strings.HasPrefix(dir, strings.TrimRight(path, "/")+"/")) {
				out = append(out, p)
				break
			}
		}
	}
	return out
}

// entries names and ranks linked entities (at most 30 chats and 30 people).
func (e *Engine) entries(projects, chats, people []string) []Entry {
	var out []Entry
	for _, slug := range projects {
		if p, ok := e.Store.Project(slug); ok {
			out = append(out, Entry{Kind: KindProject, ID: p.Slug, Name: p.Name, OneLine: p.OneLine, Importance: p.Importance,
				Relevance: p.Relevance, Tags: p.Tags, Updated: p.Updated, Extra: p.Kind + " · " + p.Status})
		}
	}
	var cs, ps []Entry
	for _, id := range chats {
		if c, ok := e.Store.Chat(id); ok {
			cs = append(cs, Entry{Kind: KindChat, ID: c.ID, Name: c.Name, OneLine: c.OneLine, Importance: c.Importance,
				Relevance: c.Relevance, Tags: c.Tags, Updated: c.LastAt, Extra: c.Kind})
		}
	}
	for _, id := range people {
		if p, ok := e.Store.Person(id); ok {
			ps = append(ps, Entry{Kind: KindPerson, ID: p.ID, Name: p.Name, OneLine: p.Note, Importance: p.Importance,
				Relevance: p.Relevance, Tags: p.Tags, Updated: p.LastSeen})
		}
	}
	Rank(cs)
	Rank(ps)
	return append(append(out, cs[:min(len(cs), 30)]...), ps[:min(len(ps), 30)]...)
}

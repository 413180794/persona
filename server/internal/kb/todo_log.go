package kb

import (
	"path"
	"path/filepath"
	"strings"
)

// TodoLogSource starts the source of a project's timeline line written when a todo's task was finished
// ("to-do · <title>" in Chinese, #2820); a new agent for the project is handed the latest of them (TodoOutcomes).
const TodoLogSource = "待办 · "

// TodoOutcomePrefix starts the text of a todo's timeline line; a line of another writer whose source happens to start
// with TodoLogSource (a chat named so) is not taken for one.
const TodoOutcomePrefix = "结论："

// logSep separates a timeline line's time, source and text (AppendLog).
const logSep = " · "

// TodoLogTitle is a todo's title as it goes in a timeline line's source: one line, without the separator, so the
// line's source and text can be told apart again.
func TodoLogTitle(title string) string {
	t := strings.Join(strings.Fields(title), " ")
	for strings.Contains(t, logSep) {
		t = strings.ReplaceAll(t, logSep, " ")
	}
	return clip(t, 60)
}

// ProjectsForWork are the projects an agent working in dir on machine works for: those whose code directories contain
// it (ProjectsForDir); when none do, the projects without any code directory whose name, slug or alias is the
// directory's name (a project the user never told where its code is). A project with directories of its own is never
// matched by name: its code is elsewhere.
func (e *Engine) ProjectsForWork(machine, dir string) []*Project {
	if out := e.ProjectsForDir(machine, dir); len(out) > 0 {
		return out
	}
	base := dirBase(dir)
	n := normName(base)
	if n == "" {
		return nil
	}
	var out []*Project
	for _, p := range e.Store.Projects() {
		if !p.Live() || p.Status == "ignored" || len(p.Dirs) > 0 {
			continue
		}
		if normName(p.Name) == n || p.Slug == Slug(base) || hasAlias(p, n) {
			out = append(out, p)
		}
	}
	return out
}

// dirBase is the last element of a directory as an agent reports it (a Unix or a Windows path).
func dirBase(dir string) string {
	d := strings.TrimRight(strings.TrimSpace(dir), `/\`)
	if d == "" {
		return ""
	}
	if strings.Contains(d, `\`) {
		return filepath.Base(strings.ReplaceAll(d, `\`, "/"))
	}
	return path.Base(d)
}

// TodoOutcome is one finished todo's line in a project's timeline: when (as written, the archive's zone), the todo's
// title and what it came to.
type TodoOutcome struct {
	When, Title, Text string
}

// TodoOutcomes are the latest n todo lines of a project's timeline (TodoLogSource), newest first.
func (s *Store) TodoOutcomes(slug string, n int) []TodoOutcome {
	if n <= 0 {
		return nil
	}
	var out []TodoOutcome
	lines := s.Log(slug, 1<<30)
	for i := len(lines) - 1; i >= 0 && len(out) < n; i-- {
		if o, ok := parseTodoLine(lines[i]); ok {
			out = append(out, o)
		}
	}
	return out
}

// parseTodoLine reads "- 2006-01-02 15:04 · <to-do> · <title> · <text>" (the to-do word in Chinese); the title has no separator (TodoLogTitle).
func parseTodoLine(l string) (TodoOutcome, bool) {
	rest, ok := strings.CutPrefix(l, "- ")
	if !ok || len(rest) < 16 {
		return TodoOutcome{}, false
	}
	when := rest[:16]
	rest, ok = strings.CutPrefix(rest[16:], logSep+TodoLogSource)
	if !ok {
		return TodoOutcome{}, false
	}
	title, text, ok := strings.Cut(rest, logSep)
	if !ok || !strings.HasPrefix(text, TodoOutcomePrefix) || strings.TrimSpace(strings.TrimPrefix(text, TodoOutcomePrefix)) == "" {
		return TodoOutcome{}, false
	}
	return TodoOutcome{When: when, Title: strings.TrimSpace(title), Text: strings.TrimSpace(text)}, true
}

package kb

import (
	"testing"
	"time"
)

// A project is found for an agent's directory by its code directories; by the directory's name only when no project
// has that directory and the named project has no directories of its own (#2820).
func TestProjectsForWork(t *testing.T) {
	st, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{Store: st}
	for name, u := range map[string]Update{
		"天气 App":  {Dirs: []string{"box:/code/weather"}},
		"persona": {},
		"notes":   {Dirs: []string{"other:/srv/notes"}},
	} {
		if _, err := e.Apply(KindProject, name, u); err != nil {
			t.Fatal(err)
		}
	}
	names := func(ps []*Project) []string {
		var out []string
		for _, p := range ps {
			out = append(out, p.Slug)
		}
		return out
	}
	for _, c := range []struct {
		machine, dir string
		want         []string
	}{
		{"box", "/code/weather/server", []string{"天气-app"}},
		{"box", "/home/me/persona", []string{"persona"}},  // no directories registered: by name
		{"box", "/home/me/persona/", []string{"persona"}}, // a trailing slash
		{"box", `C:\src\persona`, []string{"persona"}},
		{"box", "/home/me/notes", nil}, // notes keeps its code elsewhere: not by name
		{"box", "/tmp/x", nil},
		{"box", "", nil},
	} {
		if got := names(e.ProjectsForWork(c.machine, c.dir)); len(got) != len(c.want) || len(got) > 0 && got[0] != c.want[0] {
			t.Errorf("ProjectsForWork(%s, %q) = %v, want %v", c.machine, c.dir, got, c.want)
		}
	}
}

// The todo lines of a timeline come back newest first; other lines and a title with the separator do not confuse it.
func TestTodoOutcomes(t *testing.T) {
	st, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 11, 5, 10, 0, 0, 0, time.Local)
	add := func(min int, source, line string) {
		if err := st.AppendLog("p", at.Add(time.Duration(min)*time.Minute), source, line); err != nil {
			t.Fatal(err)
		}
	}
	add(0, TodoLogSource+TodoLogTitle("修 · 登录"), "结论：修好了 https://github.com/o/r/pull/1")
	add(1, "Agent · 修排序 · box", "完成：排好了")
	add(2, TodoLogSource+TodoLogTitle("发 1.3\n版"), "结论：发了 TestFlight")
	add(3, "待办 · 本周", "群里在说下周的排期") // a chat named so: not a todo's line
	got := st.TodoOutcomes("p", 5)
	if len(got) != 2 || got[0].Title != "发 1.3 版" || got[0].Text != "结论：发了 TestFlight" || got[0].When != "2026-11-05 10:02" ||
		got[1].Title != "修 登录" || got[1].Text != "结论：修好了 https://github.com/o/r/pull/1" {
		t.Fatalf("outcomes = %+v", got)
	}
	if one := st.TodoOutcomes("p", 1); len(one) != 1 || one[0].Title != "发 1.3 版" {
		t.Fatalf("latest one = %+v", one)
	}
	if none := st.TodoOutcomes("nope", 3); len(none) != 0 {
		t.Fatalf("no project: %+v", none)
	}
}

package extension

import (
	"strings"
	"testing"
)

// An app of UI version 5 gets the code review fields as they are.
func TestFitKeepsV5ForV5(t *testing.T) {
	v := codeExample(t, "code-review-v5.json")
	page, _ := Slot("nav_page")
	if _, err := Fit(v, page, 5); err != nil {
		t.Fatal(err)
	}
	d := v.Sections[2].Items[0]
	if v.Review == nil || d.Context == nil || d.MarkViewed == nil || !d.Viewed || d.Draft == nil || !d.Threads[1].Pending {
		t.Fatalf("v5 fields lost: review=%v diff=%+v", v.Review, d)
	}
}

// An app of UI version 4 gets none of version 5: the review bar becomes a section on top with a composer, a draft
// thread says so in a badge, the new actions are gone.
func TestFitDrawsV5ForV4(t *testing.T) {
	v := codeExample(t, "code-review-v5.json")
	page, _ := Slot("nav_page")
	if _, err := Fit(v, page, 4); err != nil {
		t.Fatal(err)
	}
	if v.Review != nil {
		t.Fatal("review left")
	}
	top := v.Sections[0].Items
	if len(top) != 3 || top[0].Type != "text" || !strings.Contains(top[0].Text, "2 条") || top[1].Type != "composer" || top[2].Type != "button" {
		t.Fatalf("review bar drawn as %+v", top)
	}
	for _, a := range Actions(v) {
		switch a.ID {
		case "context", "mark_viewed", "draft_comment", "resolve":
			t.Errorf("action %s left", a.ID)
		}
	}
	for _, s := range v.Sections {
		for _, e := range s.Items {
			if e.Viewed || e.Generated || e.Revision != "" || e.Similarity != 0 || e.Suggestions || e.LineURL != "" {
				t.Errorf("%s keeps a v5 field: %+v", e.Type, e)
			}
			for _, en := range e.Entries {
				if en.Viewed || en.Generated {
					t.Errorf("tree entry %s keeps a v5 field", en.Path)
				}
			}
			for _, th := range e.Threads {
				if th.Pending {
					t.Error("a pending thread left")
				}
				if th.ID == "t2" && (th.Comments[0].Badge == nil || th.Comments[0].Badge.Text != "草稿") {
					t.Errorf("draft thread without its badge: %+v", th.Comments[0])
				}
			}
		}
	}
	// and on down to version 1 it still holds
	for _, version := range []int{1, 2, 3} {
		v := codeExample(t, "code-review-v5.json")
		if _, err := Fit(v, page, version); err != nil {
			t.Fatalf("v%d: %v", version, err)
		}
	}
}

// What an action answers past the view: lines and components in place for version 5, held to their limits; an older
// app gets neither (it draws the page again).
func TestFitResult(t *testing.T) {
	page, _ := Slot("nav_page")
	diff := UIElem{Type: "diff", Key: "diff:a.go", Path: "a.go", Hunks: []Hunk{{OldStart: 1, NewStart: 1, Lines: []string{"+x"}}}}
	res := ActionResult{Lines: &FileLines{Path: "a.go", Side: "new", Start: 1, Lines: []string{"a", "b"}}, Items: []UIElem{diff}}
	if err := FitResult(&res, page, 5); err != nil || res.Lines == nil || len(res.Items) != 1 {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	old := res
	if err := FitResult(&old, page, 4); err != nil || old.Lines != nil || old.Items != nil {
		t.Fatalf("v4 keeps lines or items: %+v", old)
	}
	for name, bad := range map[string]ActionResult{
		"no key":     {Items: []UIElem{{Type: "diff", Path: "a.go"}}},
		"bad line":   {Items: []UIElem{{Type: "diff", Key: "k", Path: "a.go", Hunks: []Hunk{{Lines: []string{"x"}}}}}},
		"no path":    {Lines: &FileLines{Start: 1}},
		"start zero": {Lines: &FileLines{Path: "a", Start: 0}},
		"side":       {Lines: &FileLines{Path: "a", Start: 1, Side: "both"}},
		"too many":   {Lines: &FileLines{Path: "a", Start: 1, Lines: make([]string, 20001)}},
	} {
		if err := FitResult(&bad, page, 5); err == nil {
			t.Errorf("%s: taken", name)
		}
	}
}

// A code's line link is https only.
func TestCheckLineURL(t *testing.T) {
	ok := View{Sections: []Section{{Items: []UIElem{{Type: "code", Text: "x", LineURL: "https://example.com/a.go#L{start}-L{end}"}}}}}
	if err := CheckView(&ok); err != nil {
		t.Fatal(err)
	}
	bad := View{Sections: []Section{{Items: []UIElem{{Type: "code", Text: "x", LineURL: "javascript:alert(1)//{start}"}}}}}
	if err := CheckView(&bad); err == nil {
		t.Fatal("javascript: link taken")
	}
}

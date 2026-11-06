package kb

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

func extApply(t *testing.T, e *Engine, ext, kind, id string, u Update, evidence ...Evidence) ExtensionResult {
	t.Helper()
	res, err := e.ApplyExtension(kind, id, u, ExtensionWrite{Extension: ext, Evidence: evidence})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func conflictOwners(t *testing.T, err error) map[string]string {
	t.Helper()
	var c *ConflictError
	if !errors.As(err, &c) {
		t.Fatalf("want a conflict, got %v", err)
	}
	return c.Owners
}

func str(s string) *string { return &s }

// An extension's summary is held against the pipeline, says where it came from, and gives back what it replaced.
func TestExtensionWriteHoldsAgainstThePipeline(t *testing.T) {
	e := editEngine(t)
	_ = e.Store.PutChat(&Chat{ID: "chat", Name: "Chat"})
	_ = e.Store.PutSummary(KindChat, "chat", "built-in summary")
	ev := Evidence{Chat: "chat", Day: "2026-01-02", N: 3}
	res := extApply(t, e, "curator", KindChat, "chat", Update{Summary: "reviewed summary"}, ev)
	if string(res.Before["summary"]) != `"built-in summary"` {
		t.Fatalf("before: %s", res.Before)
	}
	_ = e.Store.PutSummary(KindChat, "chat", "pipeline again")
	if got := strings.TrimSpace(e.Store.Summary(KindChat, "chat")); got != "reviewed summary" {
		t.Fatalf("pipeline overwrote the extension's summary: %q", got)
	}
	c, _ := e.Store.Chat("chat")
	p := c.Provenance["summary"]
	if p.By != "ext:curator" || len(p.Evidence) != 1 || p.Evidence[0] != ev || p.At.IsZero() {
		t.Fatalf("provenance: %+v", c.Provenance)
	}
	c.Name = "renamed by the pipeline"
	_ = e.Store.PutChat(c) // a background write from a stale object keeps the provenance
	if c, _ := e.Store.Chat("chat"); c.Provenance["summary"].By != "ext:curator" {
		t.Fatalf("pipeline write dropped provenance: %+v", c.Provenance)
	}
}

// What the user locked or pinned is refused, whole: nothing of the update is written.
func TestExtensionCannotOverrideTheUser(t *testing.T) {
	e := editEngine(t)
	_ = e.Store.PutPerson(&Person{ID: "person", Name: "Person"})
	editApply(t, e, KindPerson, "person", Update{Note: "the user's note", Pinned: str("the user's fact")})
	_, err := e.ApplyExtension(KindPerson, "person", Update{Note: "extension note", Importance: "P1"}, ExtensionWrite{Extension: "curator"})
	if owners := conflictOwners(t, err); owners["note"] != UserWriter || len(owners) != 1 {
		t.Fatalf("owners: %v", owners)
	}
	_, err = e.ApplyExtension(KindPerson, "person", Update{Pinned: str("extension fact")}, ExtensionWrite{Extension: "curator"})
	if owners := conflictOwners(t, err); owners["pinned"] != UserWriter {
		t.Fatalf("owners: %v", owners)
	}
	p, _ := e.Store.Person("person")
	if p.Note != "the user's note" || p.Pinned != "the user's fact" || p.Importance != "" {
		t.Fatalf("a refused write changed something: %+v", p)
	}
	// Unlocking is only for what the extension holds.
	_, err = e.ApplyExtension(KindPerson, "person", Update{}, ExtensionWrite{Extension: "curator", Unlock: []string{"note"}})
	if owners := conflictOwners(t, err); owners["note"] != UserWriter {
		t.Fatalf("owners: %v", owners)
	}
}

// Two extensions don't take each other's fields; the user takes any back by editing it.
func TestUserTakesOverAndExtensionsDoNotShare(t *testing.T) {
	e := editEngine(t)
	_ = e.Store.PutProject(&Project{Slug: "project", Name: "Project", Status: "active"})
	extApply(t, e, "a", KindProject, "project", Update{Goal: "goal from a", Pinned: str("fact from a")})
	extApply(t, e, "a", KindProject, "project", Update{Goal: "goal from a, revised"}) // its own: fine
	_, err := e.ApplyExtension(KindProject, "project", Update{Goal: "goal from b"}, ExtensionWrite{Extension: "b"})
	if owners := conflictOwners(t, err); owners["goal"] != "ext:a" {
		t.Fatalf("owners: %v", owners)
	}
	editApply(t, e, KindProject, "project", Update{Goal: "the user's goal"})
	p, _ := e.Store.Project("project")
	if _, ok := p.Provenance["goal"]; ok || p.Provenance["pinned"].By != "ext:a" {
		t.Fatalf("provenance after the user's edit: %+v", p.Provenance)
	}
	if _, err := e.ApplyExtension(KindProject, "project", Update{Goal: "a again"}, ExtensionWrite{Extension: "a"}); err == nil {
		t.Fatal("the extension rewrote a field the user took over")
	}
}

// Handing a field back lets the pipeline write it again; lock: false never holds anything.
func TestUnlockAndLockFalseLeaveFieldsToThePipeline(t *testing.T) {
	e := editEngine(t)
	_ = e.Store.PutChat(&Chat{ID: "chat", Name: "Chat"})
	extApply(t, e, "curator", KindChat, "chat", Update{Summary: "reviewed", Pinned: str("fact")})
	if _, err := e.ApplyExtension(KindChat, "chat", Update{}, ExtensionWrite{Extension: "curator", Unlock: []string{"summary", "pinned"}}); err != nil {
		t.Fatal(err)
	}
	_ = e.Store.PutSummary(KindChat, "chat", "pipeline")
	c, _ := e.Store.Chat("chat")
	if got := strings.TrimSpace(e.Store.Summary(KindChat, "chat")); got != "pipeline" || c.Pinned != "" || len(c.Provenance) != 0 || len(c.LockedFields) != 0 {
		t.Fatalf("after unlock: %q %+v", got, c.Tagging)
	}
	off := false
	extApply(t, e, "curator", KindChat, "chat", Update{Summary: "a suggestion", Lock: &off})
	_ = e.Store.PutSummary(KindChat, "chat", "pipeline wins")
	c, _ = e.Store.Chat("chat")
	if got := strings.TrimSpace(e.Store.Summary(KindChat, "chat")); got != "pipeline wins" || len(c.Provenance) != 0 {
		t.Fatalf("lock: false held the field: %q %+v", got, c.Provenance)
	}
}

// A link's other side counts too: the user's locked links on a chat stop a project-side write.
func TestExtensionConflictsOnTheReverseSide(t *testing.T) {
	e := editEngine(t)
	_ = e.Store.PutChat(&Chat{ID: "chat", Name: "Chat"})
	_ = e.Store.PutProject(&Project{Slug: "other", Name: "Other"})
	_ = e.Store.PutProject(&Project{Slug: "project", Name: "Project"})
	editApply(t, e, KindChat, "chat", Update{Projects: []string{"other"}})
	_, err := e.ApplyExtension(KindProject, "project", Update{Chats: []string{"chat"}}, ExtensionWrite{Extension: "curator"})
	if owners := conflictOwners(t, err); owners["chats/chat.projects"] != UserWriter {
		t.Fatalf("owners: %v", owners)
	}
	// A free reverse side is held by the extension along with the target.
	_ = e.Store.PutChat(&Chat{ID: "chat2", Name: "Chat 2"})
	extApply(t, e, "curator", KindProject, "project", Update{Chats: []string{"chat2"}})
	c, _ := e.Store.Chat("chat2")
	if c.Provenance["projects"].By != "ext:curator" {
		t.Fatalf("reverse provenance: %+v", c.Provenance)
	}
}

// An extension's importance is held through locked_fields only: the user's legacy "locked" (which also covers tags)
// stays as it was.
func TestExtensionImportanceDoesNotSetLegacyLock(t *testing.T) {
	e := editEngine(t)
	_ = e.Store.PutPerson(&Person{ID: "person", Name: "Person"})
	res := extApply(t, e, "curator", KindPerson, "person", Update{Importance: "P1"})
	p, _ := e.Store.Person("person")
	if p.Locked || p.Importance != "P1" || p.Provenance["importance"].By != "ext:curator" || string(res.Before["importance"]) != "null" {
		t.Fatalf("%+v before=%s", p.Tagging, res.Before)
	}
	extApply(t, e, "curator", KindPerson, "person", Update{Tags: []string{"work"}}) // tags were never the user's
}

// An extension may not fold away a project the user holds anything on.
func TestExtensionMergeRespectsTheUser(t *testing.T) {
	e := editEngine(t)
	_ = e.Store.PutProject(&Project{Slug: "a", Name: "A", Status: "active"})
	_ = e.Store.PutProject(&Project{Slug: "b", Name: "B", Status: "active"})
	editApply(t, e, KindProject, "a", Update{Goal: "the user's"})
	_, err := e.ApplyExtension(KindProject, "a", Update{MergeInto: "b"}, ExtensionWrite{Extension: "curator"})
	if owners := conflictOwners(t, err); owners["goal"] != UserWriter {
		t.Fatalf("owners: %v", owners)
	}
}

// Without extensions the user's writes leave no provenance in metadata.
func TestUserWritesAddNoProvenance(t *testing.T) {
	e := editEngine(t)
	_ = e.Store.PutChat(&Chat{ID: "chat", Name: "Chat"})
	editApply(t, e, KindProject, "project", Update{Name: "Project", Summary: "s", Chats: []string{"chat"}, Pinned: str("fact")})
	for _, path := range []string{e.Store.path(KindProject, "project", "meta.json"), e.Store.path(KindChat, "chat", "meta.json")} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "provenance") {
			t.Fatalf("%s: %s", path, raw)
		}
	}
	var meta map[string]json.RawMessage
	raw, _ := os.ReadFile(e.Store.path(KindProject, "project", "meta.json"))
	_ = json.Unmarshal(raw, &meta)
	if _, ok := meta["locked_fields"]; !ok {
		t.Fatalf("user write should still lock: %s", raw)
	}
}

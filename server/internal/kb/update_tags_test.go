package kb

import (
	"encoding/json"
	"os"
	"slices"
	"testing"
)

func TestApplyTags(t *testing.T) {
	for _, kind := range []string{KindChat, KindPerson, KindProject, KindDoc, KindMeeting, KindEvent} {
		for _, tc := range []struct {
			name    string
			payload string
			want    []string
		}{
			{"omitted_preserves", `{}`, []string{"old-tag"}},
			{"null_preserves", `{"tags":null}`, []string{"old-tag"}},
			{"empty_clears", `{"tags":[]}`, nil},
			{"nonempty_replaces", `{"tags":["new-tag"]}`, []string{"new-tag"}},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				e := editEngine(t)
				const id = "synthetic"
				tagging := Tagging{Tags: []string{"old-tag"}}
				var err error
				switch kind {
				case KindChat:
					err = e.Store.PutChat(&Chat{ID: id, Tagging: tagging})
				case KindPerson:
					err = e.Store.PutPerson(&Person{ID: id, Tagging: tagging})
				case KindProject:
					err = e.Store.PutProject(&Project{Slug: id, Name: "Synthetic", Tagging: tagging})
				default:
					err = e.Store.PutItem(&Item{Kind: kind, ID: id, Tagging: tagging})
				}
				if err != nil {
					t.Fatal(err)
				}
				var update Update
				if err := json.Unmarshal([]byte(tc.payload), &update); err != nil {
					t.Fatal(err)
				}
				editApply(t, e, kind, id, update)
				assertStoredTags(t, e, kind, id, tc.want)
			})
		}
	}
}

func TestApplyTagsClearSurvivesAutomaticWrite(t *testing.T) {
	e := editEngine(t)
	const id = "synthetic"
	if err := e.Store.PutChat(&Chat{ID: id, Tagging: Tagging{Tags: []string{"old-tag"}}}); err != nil {
		t.Fatal(err)
	}
	stale, ok := e.Store.Chat(id)
	if !ok {
		t.Fatal("seeded chat not found")
	}
	editApply(t, e, KindChat, id, Update{Tags: []string{}})
	assertStoredTags(t, e, KindChat, id, nil)
	if err := e.Store.PutChat(stale); err != nil {
		t.Fatal(err)
	}
	assertStoredTags(t, e, KindChat, id, nil)
}

func assertStoredTags(t *testing.T, e *Engine, kind, id string, want []string) {
	t.Helper()
	raw, err := os.ReadFile(e.Store.entityPath(kind, id))
	if err != nil {
		t.Fatal(err)
	}
	var disk Tagging
	if err := json.Unmarshal(raw, &disk); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(disk.Tags, want) {
		t.Errorf("persisted tags = %v, want %v", disk.Tags, want)
	}
	detail, err := e.Get(kind, id, 0)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(detail.Meta, &fields); err != nil {
		t.Fatal(err)
	}
	rawTags, present := fields["tags"]
	if !present {
		t.Error("Get metadata is missing tags; want a JSON array")
	} else {
		var tags []string
		if err := json.Unmarshal(rawTags, &tags); err != nil || tags == nil {
			t.Errorf("Get tags must be a JSON array, got %s (decode error: %v)", rawTags, err)
		}
	}
	var got Tagging
	if err := json.Unmarshal(detail.Meta, &got); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.Tags, want) {
		t.Errorf("Get tags = %v, want %v", got.Tags, want)
	}
}

package proto

import "testing"

func TestHasDuplicateQuestionText(t *testing.T) {
	cases := []struct {
		qs   []Question
		want bool
	}{
		{[]Question{{Question: "a?"}}, false},
		{[]Question{{Question: "a?"}, {Question: "b?"}}, false},
		{[]Question{{Question: "a?"}, {Question: "a?"}}, true},
		{[]Question{{Question: "a?"}, {Question: " a? "}}, true},
		{[]Question{{Question: "a?"}, {Question: " 　a？ "}}, false}, // a full-width question mark differs from ?: no punctuation folding
		{[]Question{{Question: "a?", MultiSelect: true}, {Question: "a?", MultiSelect: true}}, true},
		{[]Question{{Question: ""}, {Question: " "}}, true},
	}
	for i, c := range cases {
		if got := HasDuplicateQuestionText(c.qs); got != c.want {
			t.Errorf("#%d: got %v, want %v", i, got, c.want)
		}
	}
}

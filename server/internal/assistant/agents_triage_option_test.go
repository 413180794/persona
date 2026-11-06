package assistant

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

func TestTriageQuestionOptionAction(t *testing.T) {
	var out triageOut
	if err := json.Unmarshal([]byte(`{"phase":"blocked","on":"user","questions":[{"question":"Ready to verify?","options":[{"label":"Continue"},{"label":"Later","action":"defer"},{"label":"Other","action":"cancel"}]}]}`), &out); err != nil {
		t.Fatal(err)
	}
	w := triageWork(out, nil, time.Now(), "en")
	if len(w.Questions) != 1 || len(w.Questions[0].Options) != 3 {
		t.Fatalf("unexpected questions: %+v", w.Questions)
	}
	opts := w.Questions[0].Options
	if opts[0].Action != "" || opts[1].Action != proto.QuestionOptionDefer || opts[2].Action != "" {
		t.Fatalf("unexpected sanitized actions: %+v", opts)
	}
	for _, action := range []string{"DEFER", " defer ", "cancel", "terminate"} {
		out.Questions[0].Options[1].Action = action
		if got := triageWork(out, nil, time.Now(), "en").Questions[0].Options[1].Action; got != "" {
			t.Errorf("invalid action %q survived as %q", action, got)
		}
	}
	out.Questions[0].Options[1].Action = proto.QuestionOptionDefer
	out.Questions[0].MultiSelect = true
	if got := triageWork(out, nil, time.Now(), "en").Questions[0].Options[1].Action; got != "" {
		t.Fatalf("multi-select retained defer: %q", got)
	}
}

func TestTriageDoesNotInferActionFromAnswerText(t *testing.T) {
	qs := []proto.Question{{Question: "Ready?", Options: []proto.QuestionOption{{Label: "先不管，稍后再验收"}, {Label: "Later"}}}}
	for _, opt := range cardQuestions(qs, "en")[0].Options {
		if opt.Action != "" {
			t.Fatalf("text invented action: %+v", opt)
		}
	}
}

package proto

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestQuestionOptionActionRoundTrip(t *testing.T) {
	for _, option := range []QuestionOption{{Label: "Continue"}, {Label: "Later", Action: QuestionOptionDefer}} {
		encoded, err := json.Marshal(option)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), `"action"`) != (option.Action != "") {
			t.Fatalf("optional action field: %s", encoded)
		}
		var decoded QuestionOption
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(option, decoded) {
			t.Fatalf("round trip: %+v != %+v", option, decoded)
		}
	}
}

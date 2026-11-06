package uistream

import (
	"bytes"
	"testing"
)

func TestPageResultRawBoundary(t *testing.T) {
	maximum := append([]byte(`{"lines":{"path":"fixture.go","start":1,"lines":[`), bytes.Repeat([]byte(`"",`), 19999)...)
	maximum = append(maximum, []byte(`""]}}`)...)
	if err := ValidateActionResult(maximum); err != nil {
		t.Fatalf("native maximum line count: %v", err)
	}
	for _, raw := range [][]byte{
		[]byte(`{"toast":"","pop":false}`),
		[]byte(`{"lines":{"path":"fixture.go","start":1,"lines":[],"total":0}}`),
		[]byte(`{"push":{"ui":"detail","state":"fixture"}}`),
	} {
		if err := ValidateActionResult(raw); err != nil {
			t.Fatalf("valid result %s: %v", raw, err)
		}
	}
	for _, raw := range [][]byte{
		[]byte(`null`), []byte(`[]`),
		[]byte(`{"toast":"first","toast":"second"}`),
		[]byte(`{"Toast":"alias"}`),
		[]byte(`{"toast":"\ud800"}`),
		[]byte(`{"pop":"true"}`),
		[]byte(`{"lines":{"path":"fixture.go","start":0,"lines":[]}}`),
		[]byte(`{"view":{"sections":[]}}`),
		[]byte(`{"push":{"page":"other"}}`),
		append([]byte(`{"toast":"`), append(bytes.Repeat([]byte("x"), RequestMost), []byte(`"}`)...)...),
	} {
		if err := ValidateActionResult(raw); err == nil {
			t.Fatalf("invalid result accepted: %.100s", raw)
		}
	}
}

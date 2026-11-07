package herdr

import (
	"encoding/json"
	"testing"
)

// mouseWire is what Mouse writes to herdr's control session, one JSON line per event (the shapes were measured against
// herdr 0.9.5: docs/integrations/terminal.md "Mouse").
func mouseWire(t *testing.T, e MouseEvent) (string, error) {
	t.Helper()
	s := &Control{writes: make(chan []byte, 4), done: make(chan struct{})}
	if err := s.Mouse(e); err != nil {
		return "", err
	}
	line := <-s.writes
	if line[len(line)-1] != '\n' {
		t.Fatalf("not a line: %q", line)
	}
	var m map[string]any
	if err := json.Unmarshal(line, &m); err != nil {
		t.Fatal(err)
	}
	return string(line[:len(line)-1]), nil
}

func TestMouseEncoding(t *testing.T) {
	cases := []struct {
		e    MouseEvent
		want string
	}{
		{MouseEvent{"down", "left", 10, 5, 0}, `{"action":"down","button":"left","column":10,"row":5,"type":"terminal.mouse"}`},
		{MouseEvent{"up", "left", 10, 5, 0}, `{"action":"up","button":"left","column":10,"row":5,"type":"terminal.mouse"}`},
		{MouseEvent{"drag", "middle", 0, 0, ModShift}, `{"action":"drag","button":"middle","column":0,"modifiers":1,"row":0,"type":"terminal.mouse"}`},
		{MouseEvent{"move", "right", 79, 23, ModCtrl | ModAlt}, `{"action":"move","button":"right","column":79,"modifiers":6,"row":23,"type":"terminal.mouse"}`},
		{MouseEvent{"down", "left", 1, 1, 0xff}, `{"action":"down","button":"left","column":1,"modifiers":7,"row":1,"type":"terminal.mouse"}`}, // higher bits are herdr's to ignore: not sent
	}
	for _, c := range cases {
		got, err := mouseWire(t, c.e)
		if err != nil || got != c.want {
			t.Errorf("%+v: got %q (%v), want %q", c.e, got, err, c.want)
		}
	}
}

func TestMouseRefusesWhatHerdrWouldNotParse(t *testing.T) {
	for _, e := range []MouseEvent{
		{"press", "left", 1, 1, 0},
		{"down", "wheel", 1, 1, 0}, // the wheel is terminal.scroll
		{"down", "", 1, 1, 0},
		{"down", "left", -1, 1, 0},
		{"down", "left", 1, maxMouseCell, 0},
	} {
		if _, err := mouseWire(t, e); err == nil {
			t.Errorf("%+v was sent", e)
		}
	}
}

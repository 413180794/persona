package transcript

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

func pngOf(t *testing.T, w, h int) *proto.Image {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewGray(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return &proto.Image{Mime: "image/png", Data: base64.StdEncoding.EncodeToString(b.Bytes())}
}

// The images a turn's calls looked at, as a fold carries them: what the results carried (with their item, size and
// shape), a picture Read opened whose result carried none (only its path), each once and in order; not a failed
// Read, not a file that is no picture, not one a call is still reading.
func TestViewedImages(t *testing.T) {
	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	seq := 0
	var page []proto.Item
	call := func(id, tool string, input map[string]any) {
		seq++
		in, _ := json.Marshal(input)
		page = append(page, proto.Item{ID: fmt.Sprintf("l%d#0", seq), Seq: seq, TS: ts, Role: "assistant", Kind: proto.KindToolUse,
			ToolUseID: id, ToolName: tool, ToolInput: in})
	}
	result := func(id string, failed bool, imgs ...*proto.Image) {
		seq++
		page = append(page, proto.Item{ID: fmt.Sprintf("l%d#0", seq), Seq: seq, TS: ts, Role: "user", Kind: proto.KindToolResult,
			ToolUseID: id, IsError: failed, Text: "x"})
		for j, img := range imgs {
			page = append(page, proto.Item{ID: fmt.Sprintf("l%d#%d", seq, j+1), Seq: seq, TS: ts, Role: "user", Kind: proto.KindImage,
				ToolUseID: id, Image: img})
		}
	}
	seq++
	page = append(page, proto.Item{ID: "l1#0", Seq: seq, TS: ts, Role: "user", Kind: proto.KindText, Text: "look"})
	call("a", "Read", map[string]any{"file_path": "/work/shot.png"})
	result("a", false, pngOf(t, 4, 3))
	call("b", "mcp__persona__computer_state", map[string]any{"machine": "m1", "app": "Notes"})
	result("b", false, pngOf(t, 16, 10))
	call("c", "Read", map[string]any{"file_path": "/work/shot.png"}) // the same file again
	result("c", false, pngOf(t, 4, 3))
	call("d", "Read", map[string]any{"file_path": "/work/big.JPG"}) // its picture was too big to keep, or Codex's view_image
	result("d", false)
	call("e", "Read", map[string]any{"file_path": "/work/main.go"})
	result("e", false)
	call("f", "Read", map[string]any{"file_path": "/work/gone.png"})
	result("f", true)
	call("g", "mcp__persona__browser_look", map[string]any{"machine": "m2"})
	result("g", false, pngOf(t, 8, 5))
	call("h", "mcp__persona__computer_state", map[string]any{"machine": "m1"}) // the screen had not changed
	result("h", false, pngOf(t, 16, 10))
	call("i", "mcp__other__snap", map[string]any{})
	result("i", false, pngOf(t, 2, 2), pngOf(t, 3, 2))
	call("j", "Read", map[string]any{"file_path": "/work/later.png"}) // still reading

	got := FoldWork(page)
	if len(got) != 3 || got[1].Kind != proto.KindFold {
		t.Fatalf("page: %+v", got)
	}
	var lines []string
	for _, im := range got[1].Fold.Images {
		lines = append(lines, fmt.Sprintf("%s %s %s %s %s %s %dx%d %v", im.ToolUseID, im.ItemID, im.Source, im.Tool, im.Path, im.Machine,
			im.Width, im.Height, im.Size > 0 && im.Mime == "image/png"))
	}
	want := []string{
		"a l3#1 read Read /work/shot.png  4x3 true",
		"b l5#1 screenshot mcp__persona__computer_state  m1 16x10 true",
		"d  read Read /work/big.JPG  0x0 false",
		"g l15#1 browser mcp__persona__browser_look  m2 8x5 true",
		"i l19#1 mcp mcp__other__snap   2x2 true",
		"i l19#2 mcp mcp__other__snap   3x2 true",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
	if b, _ := json.Marshal(got[1].Fold.Images[2]); strings.Contains(string(b), "item_id") || strings.Contains(string(b), "width") {
		t.Fatalf("a path-only image says more than it knows: %s", b)
	}
}

// A run without images says nothing about them.
func TestFoldWithoutImages(t *testing.T) {
	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	page := []proto.Item{
		{ID: "l1#0", Seq: 1, TS: ts, Role: "assistant", Kind: proto.KindThinking, Text: "x"},
		{ID: "l2#0", Seq: 2, TS: ts, Role: "assistant", Kind: proto.KindToolUse, ToolUseID: "a", ToolName: "Bash", ToolInput: json.RawMessage(`{"command":"ls"}`)},
		{ID: "l3#0", Seq: 3, TS: ts, Role: "user", Kind: proto.KindToolResult, ToolUseID: "a", Text: "x"},
	}
	got := FoldWork(page)
	if len(got) != 1 || got[0].Fold == nil || got[0].Fold.Images != nil {
		t.Fatalf("%+v", got)
	}
	if b, _ := json.Marshal(got[0]); strings.Contains(string(b), "images") {
		t.Fatalf("%s", b)
	}
}

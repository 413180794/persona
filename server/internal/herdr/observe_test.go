package herdr

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestObserveStreamsFramesAtTheGivenSize(t *testing.T) {
	bin, args := cliScript(t, `{"type":"something.else"}
{"bytes":"G1sySg==","encoding":"ansi","full":true,"height":24,"seq":1,"type":"terminal.frame","width":80}
not json
{"bytes":"eA==","encoding":"ansi","full":false,"height":24,"seq":2,"type":"terminal.frame","width":80}`)
	var got []Frame
	err := (&Client{Bin: bin}).Observe(context.Background(), "w1:p2", 80, 24, func(f Frame) { got = append(got, f) })
	if err == nil || !strings.Contains(err.Error(), "ended") {
		t.Fatalf("err = %v, want the stream ended", err)
	}
	if len(got) != 2 || !got[0].Full || got[0].Data != "G1sySg==" || got[1].Full || got[1].Width != 80 || got[1].Height != 24 {
		t.Fatalf("frames = %+v", got)
	}
	b, _ := os.ReadFile(args)
	if strings.TrimSpace(string(b)) != "terminal session observe w1:p2 --cols 80 --rows 24" {
		t.Fatalf("args = %q", b)
	}
}

func TestObserveSaysWhyHerdrRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in herdr is a shell script")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "herdr")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho 'unknown terminal session observe option' >&2\nexit 2\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := (&Client{Bin: bin}).Observe(context.Background(), "w1:p2", 80, 24, func(Frame) { t.Fatal("no frames") })
	if err == nil || !strings.Contains(err.Error(), "unknown terminal session observe option") {
		t.Fatalf("err = %v", err)
	}
}

func TestPaneSizeIsItsRect(t *testing.T) {
	sock := fakeServer(t, func(method string, params map[string]any) string {
		if method != "pane.layout" || params["pane_id"] != "w1:p3" {
			return `"error":{"code":"invalid_request","message":"unexpected"}`
		}
		return `"result":{"type":"pane_layout","layout":{"area":{"width":172,"height":45},"panes":[` +
			`{"pane_id":"w1:p2","rect":{"x":0,"y":0,"width":86,"height":45}},{"pane_id":"w1:p3","rect":{"x":86,"y":0,"width":86,"height":44}}]}}`
	})
	cols, rows, err := (&Client{Bin: "/nonexistent/herdr", Socket: sock}).PaneSize(context.Background(), "w1:p3")
	if err != nil || cols != 86 || rows != 44 {
		t.Fatalf("size = %d×%d, %v", cols, rows, err)
	}
}

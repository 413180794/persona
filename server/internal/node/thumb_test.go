package node

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

func writeImage(t *testing.T, path string, w, h int, opaque bool) {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			a := uint8(255)
			if !opaque && x < w/2 {
				a = 0
			}
			img.Set(x, y, color.NRGBA{uint8(x), uint8(y), 90, a})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestThumbnailOfAnImageFitsTheSize(t *testing.T) {
	dir := t.TempDir()
	n := &Node{home: dir}
	writeImage(t, filepath.Join(dir, "wide.png"), 800, 200, true)
	writeImage(t, filepath.Join(dir, "clear.png"), 100, 300, false)
	th, err := n.fileThumb(context.Background(), proto.FileThumbRequest{Path: "~/wide.png", Size: 128})
	if err != nil || th.Mime != "image/jpeg" || th.ETag == "" {
		t.Fatalf("%+v, %v", th, err)
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(th.Data))
	if err != nil || cfg.Width != 128 || cfg.Height != 32 {
		t.Fatalf("%dx%d, %v", cfg.Width, cfg.Height, err)
	}
	again, err := n.fileThumb(context.Background(), proto.FileThumbRequest{Path: "wide.png", Cwd: dir, Size: 128, ETag: th.ETag})
	if err != nil || !again.NotModified || len(again.Data) != 0 {
		t.Fatalf("revalidate = %+v, %v", again, err)
	}
	// transparency is kept (PNG); a small image is never enlarged; sizes are clamped
	th, err = n.fileThumb(context.Background(), proto.FileThumbRequest{Path: "~/clear.png", Size: 5000})
	if err != nil || th.Mime != "image/png" {
		t.Fatalf("%+v, %v", th, err)
	}
	if cfg, _ := png.DecodeConfig(bytes.NewReader(th.Data)); cfg.Width != 100 || cfg.Height != 300 {
		t.Fatalf("%dx%d", cfg.Width, cfg.Height)
	}
	// the file changed: a new ETag, drawn again
	time.Sleep(10 * time.Millisecond)
	writeImage(t, filepath.Join(dir, "wide.png"), 400, 400, true)
	th2, err := n.fileThumb(context.Background(), proto.FileThumbRequest{Path: "~/wide.png", Size: 128})
	if err != nil || th2.ETag == again.ETag {
		t.Fatalf("changed file kept its thumbnail: %+v, %v", th2, err)
	}
}

func TestThumbnailOfAnSVG(t *testing.T) {
	dir := t.TempDir()
	n := &Node{home: dir}
	svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 100"><rect width="200" height="100" fill="#3a6"/></svg>`
	os.WriteFile(filepath.Join(dir, "logo.svg"), []byte(svg), 0600)
	th, err := n.fileThumb(context.Background(), proto.FileThumbRequest{Path: "~/logo.svg", Size: 64})
	if err != nil {
		t.Fatal(err)
	}
	img, _, err := image.Decode(bytes.NewReader(th.Data))
	if err != nil || img.Bounds().Dx() != 64 || img.Bounds().Dy() != 32 {
		t.Fatalf("%v, %v", img.Bounds(), err)
	}
	if _, _, _, a := img.At(32, 16).RGBA(); a == 0 {
		t.Fatal("svg drew nothing")
	}
}

func TestThumbnailRefusals(t *testing.T) {
	dir := t.TempDir()
	n := &Node{home: dir}
	os.WriteFile(filepath.Join(dir, "x.bin"), []byte{0, 1, 2, 3}, 0600)
	os.WriteFile(filepath.Join(dir, "broken.png"), []byte("not a png"), 0600)
	for _, p := range []string{"~/x.bin", "~/broken.png"} {
		if runtime.GOOS == "darwin" {
			break // Quick Look may draw a generic icon for these
		}
		_, err := n.fileThumb(context.Background(), proto.FileThumbRequest{Path: p})
		var re *rpc.Error
		if !errors.As(err, &re) || re.Code != proto.CodeNoThumbnail {
			t.Fatalf("%s: %v", p, err)
		}
	}
	if _, err := n.fileThumb(context.Background(), proto.FileThumbRequest{Path: dir}); err == nil {
		t.Fatal("a directory got a thumbnail")
	}
	if _, err := n.fileThumb(context.Background(), proto.FileThumbRequest{Path: "/Users/x/Library/Mobile Documents/a.png"}); err == nil {
		t.Fatal("iCloud Drive was read")
	}
}

func TestAHungThumbnailToolIsSkippedForAWhile(t *testing.T) {
	dir := t.TempDir()
	n := &Node{home: dir}
	tool := "pdftoppm"
	if runtime.GOOS == "darwin" {
		tool = "qlmanage"
	}
	bin := filepath.Join(dir, "bin")
	os.Mkdir(bin, 0700)
	os.WriteFile(filepath.Join(bin, tool), []byte("#!/bin/sh\nexec /bin/sleep 30\n"), 0700)
	t.Setenv("PATH", bin)
	// Counted here, not by the tool writing a file: on a busy machine the shell may not get that far before
	// toolTimeout kills it.
	var calls atomic.Int32
	defer func(f func(*exec.Cmd) error) { runCmd = f }(runCmd)
	run := runCmd
	runCmd = func(cmd *exec.Cmd) error { calls.Add(1); return run(cmd) }
	defer func(d time.Duration) { toolTimeout = d }(toolTimeout)
	toolTimeout = 800 * time.Millisecond
	t.Cleanup(func() { tools.mu.Lock(); tools.blockedUntil = time.Time{}; tools.mu.Unlock() })
	os.WriteFile(filepath.Join(dir, "a.pdf"), []byte("%PDF-1.4\n"), 0600)
	os.WriteFile(filepath.Join(dir, "b.pdf"), []byte("%PDF-1.4\nb"), 0600)
	start := time.Now()
	for _, p := range []string{"~/a.pdf", "~/b.pdf"} {
		var re *rpc.Error
		if _, err := n.fileThumb(context.Background(), proto.FileThumbRequest{Path: p}); !errors.As(err, &re) || re.Code != proto.CodeNoThumbnail {
			t.Fatalf("%s: %v", p, err)
		}
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("took %v", d)
	}
	if c := calls.Load(); c != 1 {
		t.Fatalf("tool ran %d times, want once and then skipped", c)
	}
}

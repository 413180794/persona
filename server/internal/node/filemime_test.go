package node

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

func TestFileMimeNamesWhatTheViewersOpen(t *testing.T) {
	heic := append([]byte{0, 0, 0, 24}, []byte("ftypheic\x00\x00\x00\x00mif1heic")...)
	for _, test := range []struct {
		name string
		head []byte
		want string
	}{
		{"a.DOCX", nil, "application/vnd.openxmlformats-officedocument.wordprocessingml.document"},
		{"b.xlsx", nil, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"},
		{"c.ipynb", nil, "application/x-ipynb+json"},
		{"d.heic", nil, "image/heic"},
		{"e.m4a", nil, "audio/mp4"},
		{"slides.key", []byte("PK\x03\x04rest"), "application/vnd.apple.keynote"},
		{"id.key", []byte("-----BEGIN PRIVATE KEY-----"), "text/plain; charset=utf-8"},
		{"photo", heic, "image/heic"}, // no extension: sniffed
		{"clip", []byte("\x00\x00\x00\x18ftypqt  "), "video/quicktime"},
		{"doc", []byte("%PDF-1.7\n"), "application/pdf"},
		{"notes", []byte("plain words\n"), "text/plain; charset=utf-8"},
		{"empty", nil, ""},
	} {
		if got := fileMime(test.name, test.head); got != test.want {
			t.Errorf("%s: %q, want %q", test.name, got, test.want)
		}
	}
}

func TestReadFileStreamDescribesBinaryFiles(t *testing.T) {
	dir := t.TempDir()
	n := &Node{home: dir}
	video := bytes.Repeat([]byte{0, 1, 2, 3}, maxFileBytes) // bigger than a page
	os.WriteFile(filepath.Join(dir, "clip.mp4"), video, 0600)
	os.WriteFile(filepath.Join(dir, "blob"), []byte{0, 1, 2, 0xff}, 0600)
	os.WriteFile(filepath.Join(dir, "a.csv"), []byte("a,b\n1,2\n"), 0600)
	fc, err := n.readFile("~/clip.mp4", "", 0, true)
	if err != nil || !fc.Stream || fc.Data != "" || fc.Mime != "video/mp4" || fc.Size != int64(len(video)) || fc.Revision == "" || fc.Truncated {
		t.Fatalf("video = %+v, %v", fc, err)
	}
	chunk, err := n.readFileChunk(proto.FileChunkRequest{Path: "~/clip.mp4"})
	if err != nil || chunk.Revision != fc.Revision {
		t.Fatalf("download revision %+v differs from %q", chunk, fc.Revision)
	}
	fc, err = n.readFile("~/blob", "", 0, true) // not known by name: read, found binary, still described only
	if err != nil || !fc.Stream || fc.Data != "" || fc.Mime != "application/octet-stream" {
		t.Fatalf("blob = %+v, %v", fc, err)
	}
	fc, err = n.readFile("~/a.csv", "", 0, true) // text stays text
	if err != nil || fc.Stream || fc.Text != "a,b\n1,2\n" || fc.Mime != "text/csv" {
		t.Fatalf("csv = %+v, %v", fc, err)
	}
	fc, err = n.readFile("~/clip.mp4", "", 0, false) // older apps: base64 as before
	if err != nil || fc.Stream || fc.Data == "" || !fc.Truncated {
		t.Fatalf("legacy = stream %v data %d truncated %v, %v", fc.Stream, len(fc.Data), fc.Truncated, err)
	}
}

func TestDownloadSniffsWhatTheNameDoesNotSay(t *testing.T) {
	dir := t.TempDir()
	n := &Node{home: dir}
	os.WriteFile(filepath.Join(dir, "scan"), []byte("%PDF-1.4\n%rest"), 0600)
	info, err := n.readFileChunk(proto.FileChunkRequest{Path: "~/scan"})
	if err != nil || info.Mime != "application/pdf" {
		t.Fatalf("%+v, %v", info, err)
	}
}

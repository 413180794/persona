package node

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chyroc/jarvis/server/internal/proto"
	"github.com/chyroc/jarvis/server/internal/rpc"
)

var uploadNow = time.Date(2026, 10, 29, 15, 30, 12, 0, time.Local)

func sendFile(t *testing.T, u *uploads, name string, data []byte, chunk int) (string, error) {
	t.Helper()
	id, err := u.begin(name, int64(len(data)))
	if err != nil {
		return "", err
	}
	for off := 0; off < len(data); off += chunk {
		end := min(off+chunk, len(data))
		if _, err := u.chunk(id, int64(off), data[off:end]); err != nil {
			return "", err
		}
	}
	path, _, err := u.end(id, uploadNow)
	return path, err
}

func TestUploadInChunks(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	var u uploads
	data := bytes.Repeat([]byte("0123456789"), 1000)
	path, err := sendFile(t, &u, "report.pdf", data, 777)
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(dir, "report-20261029-153012.pdf") || !filepath.IsAbs(path) {
		t.Fatalf("path %s", path)
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, data) {
		t.Fatal("content differs")
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode())
	}
	// No part files left behind.
	ents, _ := os.ReadDir(dir)
	if len(ents) != 1 {
		t.Fatalf("left %v", ents)
	}

	// The same name in the same second: a short random suffix.
	again, err := sendFile(t, &u, "report.pdf", data[:10], 4)
	if err != nil {
		t.Fatal(err)
	}
	b := filepath.Base(again)
	if again == path || !strings.HasPrefix(b, "report-20261029-153012-") || !strings.HasSuffix(b, ".pdf") || len(b) != len("report-20261029-153012-abcd.pdf") {
		t.Fatalf("second: %s", again)
	}
	// An empty file is a file.
	if p, err := sendFile(t, &u, "empty.txt", nil, 1); err != nil || filepath.Base(p) != "empty-20261029-153012.txt" {
		t.Fatalf("empty: %s %v", p, err)
	}
}

func TestUploadRefusals(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	var u uploads
	_, err := u.begin("big.bin", proto.MaxUploadBytes+1)
	var re *rpc.Error
	if !errors.As(err, &re) || re.Code != proto.CodeTooLarge {
		t.Fatalf("too large: %v", err)
	}
	if _, err := u.begin("x", -1); err == nil {
		t.Fatal("no size")
	}

	// A chunk out of order, more bytes than announced, too few at the end: refused, part file removed.
	id, _ := u.begin("a.txt", 4)
	if _, err := u.chunk(id, 2, []byte("ab")); err == nil {
		t.Fatal("offset")
	}
	if _, err := u.chunk(id, 0, []byte("ab")); err == nil {
		t.Fatal("the upload is gone after a bad chunk")
	}
	id, _ = u.begin("a.txt", 4)
	if _, err := u.chunk(id, 0, []byte("abcde")); err == nil {
		t.Fatal("overflow")
	}
	id, _ = u.begin("a.txt", 4)
	u.chunk(id, 0, []byte("ab"))
	if _, _, err := u.end(id, uploadNow); err == nil {
		t.Fatal("short")
	}
	id, _ = u.begin("a.txt", 4)
	u.chunk(id, 0, []byte("ab"))
	u.abort(id)
	if _, err := u.chunk(id, 2, []byte("cd")); err == nil {
		t.Fatal("aborted")
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 0 {
		t.Fatalf("left %v", ents)
	}
}

func TestUploadNames(t *testing.T) {
	cases := map[string]string{
		"report.pdf":         "report-20261029-153012.pdf",
		"backup.tar.gz":      "backup-20261029-153012.tar.gz",
		"Makefile":           "Makefile-20261029-153012",
		".env":               ".env-20261029-153012",
		"会议纪要 v2.docx":       "会议纪要 v2-20261029-153012.docx",
		"../../etc/passwd":   ".._.._etc_passwd-20261029-153012",
		"a\\b\nc.txt":        "a_b_c-20261029-153012.txt",
		"..":                 "file-20261029-153012",
		"  ":                 "file-20261029-153012",
		"notes. final draft": "notes. final draft-20261029-153012",
	}
	for in, want := range cases {
		dir := t.TempDir()
		p, err := reserveName(dir, in, uploadNow)
		if err != nil || filepath.Dir(p) != dir || filepath.Base(p) != want {
			t.Errorf("%q -> %q %v, want %q", in, filepath.Base(p), err, want)
		}
	}
	long := strings.Repeat("长", 200) + ".txt"
	p, err := reserveName(t.TempDir(), long, uploadNow)
	if err != nil || len(filepath.Base(p)) > 255 || !strings.HasSuffix(p, ".txt") {
		t.Fatalf("long: %d %v", len(filepath.Base(p)), err)
	}
}

func TestUploadIdleRemovesPart(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	var u uploads
	id, _ := u.begin("a.txt", 4)
	u.mu.Lock()
	u.open[id].idle.Reset(time.Millisecond)
	u.mu.Unlock()
	time.Sleep(50 * time.Millisecond)
	if ents, _ := os.ReadDir(dir); len(ents) != 0 {
		t.Fatalf("left %v", ents)
	}
}

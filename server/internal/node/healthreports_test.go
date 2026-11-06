package node

import (
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

func writeReports(t *testing.T, dir string, names ...string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for i, name := range names {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
		at := time.Unix(1_800_000_000+int64(i)*60, 0)
		if err := os.Chtimes(p, at, at); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHealthReportsPairRecordsNewestFirst(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "reports")
	list, err := listHealthReports(dir)
	if err != nil || list == nil || len(list) != 0 {
		t.Fatalf("no directory yet: %v, %v", list, err)
	}
	writeReports(t, dir, "体检-20261106-093000.pdf", "体检-20261106-093000.md", "血常规.png", "2025-年度体检.md", ".jarvis-upload-1")
	if err := os.Mkdir(filepath.Join(dir, "folder"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/hosts", filepath.Join(dir, "link.pdf")); err != nil {
		t.Fatal(err)
	}
	list, err = listHealthReports(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("got %+v", list)
	}
	record, photo, paired := list[0], list[1], list[2]
	if record.Name != "2025-年度体检.md" || record.Path != "" || record.RecordPath != filepath.Join(dir, record.Name) {
		t.Fatalf("a record alone: %+v", record)
	}
	if photo.Name != "血常规.png" || photo.RecordPath != "" || photo.Title != "血常规" {
		t.Fatalf("an original alone: %+v", photo)
	}
	if paired.Title != "体检" || paired.Path != filepath.Join(dir, "体检-20261106-093000.pdf") || paired.RecordPath != filepath.Join(dir, "体检-20261106-093000.md") {
		t.Fatalf("an original with its record: %+v", paired)
	}
}

func TestHealthReportTitle(t *testing.T) {
	for name, want := range map[string]string{
		"2026 体检报告-20261106-093000.pdf":   "2026 体检报告",
		"2026 体检报告-20261106-093000-2.pdf": "2026 体检报告",
		"-20261106-093000.pdf":            "-20261106-093000",
		"血常规.png":                         "血常规",
	} {
		if got := healthReportTitle(name); got != want {
			t.Errorf("%s: %q, want %q", name, got, want)
		}
	}
}

func TestDeleteHealthReportTakesItsRecord(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "reports")
	writeReports(t, dir, "a.pdf", "a.md", "b.pdf", "c.pdf", "c.png", "c.md")
	if err := deleteHealthReport(dir, "a.pdf"); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]bool{"a.pdf": false, "a.md": false, "b.pdf": true} {
		if _, err := os.Stat(filepath.Join(dir, name)); (err == nil) != want {
			t.Errorf("%s there: %v", name, err == nil)
		}
	}
	// Two originals share the record: it stays until the last of them goes.
	if err := deleteHealthReport(dir, "c.pdf"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "c.md")); err != nil {
		t.Fatal("the record went with one of two originals")
	}
	if err := deleteHealthReport(dir, "c.png"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "c.md")); err == nil {
		t.Fatal("the record outlived its last original")
	}
	// Already gone (another device): fine.
	if err := deleteHealthReport(dir, "a.pdf"); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteHealthReportStaysInItsDirectory(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "reports")
	writeReports(t, dir, "a.pdf")
	writeReports(t, root, "outside.pdf")
	if err := os.Symlink(filepath.Join(root, "outside.pdf"), filepath.Join(dir, "link.pdf")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"", ".", "..", "../outside.pdf", "sub/../../outside.pdf", filepath.Join(root, "outside.pdf"), `..\outside.pdf`, ".hidden", "link.pdf"} {
		if err := deleteHealthReport(dir, name); err == nil {
			t.Errorf("%q was accepted", name)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "outside.pdf")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "link.pdf")); err != nil {
		t.Fatal(err)
	}
}

func TestHealthReportsCall(t *testing.T) {
	n := &Node{dir: t.TempDir(), home: t.TempDir(), logger: log.New(io.Discard, "", 0)}
	out, err := callUpload(t, n, proto.UploadDestHealth, "体检.pdf", []byte("%PDF"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := n.healthReportsCall("health.reports.list", nil)
	if err != nil {
		t.Fatal(err)
	}
	list := got.(proto.HealthReports).Reports
	if len(list) != 1 || list[0].Path != out.Path || list[0].Title != "体检" || list[0].Size != 4 {
		t.Fatalf("got %+v", list)
	}
	params, _ := json.Marshal(map[string]string{"name": list[0].Name})
	if _, err := n.healthReportsCall("health.reports.delete", params); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(out.Path); err == nil {
		t.Fatal("still there")
	}
}

package node

import (
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/identity"
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
	writeReports(t, dir, "体检-20261106-093000.pdf", "体检-20261106-093000.md", "血常规.png", "2025-年度体检.md", ".persona-upload-1")
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

// Another manager's health space (health-<id>, beside the built-in's health) takes its reports when the hub names it
// (health_dir, echoed), lists and deletes only its own; the built-in's stays where it was; any other name is refused.
func TestHealthReportsOfAManagersSpace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PERSONA_HOME", filepath.Join(home, ".persona"))
	n := &Node{dir: t.TempDir(), home: home, logger: log.New(io.Discard, "", 0)}
	call := func(method string, p map[string]any) (any, error) {
		raw, _ := json.Marshal(p)
		if strings.HasPrefix(method, "health.reports") {
			return n.healthReportsCall(method, raw)
		}
		return n.uploadCall(method, raw)
	}
	upload := func(space, name string) (proto.UploadedFile, error) {
		p := map[string]any{"name": name, "size": 4, "dest": proto.UploadDestHealth}
		if space != "" {
			p["health_dir"] = space
		}
		res, err := call("file.upload.begin", p)
		if err != nil {
			return proto.UploadedFile{}, err
		}
		if got, _ := res.(map[string]any)["health_dir"].(string); got != space {
			t.Fatalf("health_dir echoed %q, want %q", got, space)
		}
		id := res.(map[string]any)["upload_id"]
		if _, err := call("file.upload.chunk", map[string]any{"upload_id": id, "offset": 0, "data": []byte("%PDF")}); err != nil {
			return proto.UploadedFile{}, err
		}
		out, err := call("file.upload.end", map[string]any{"upload_id": id})
		if err != nil {
			return proto.UploadedFile{}, err
		}
		return out.(proto.UploadedFile), nil
	}
	theirs, err := upload("health-m-0b0b0b", "b.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(filepath.Dir(identity.HealthDir(home)), "health-m-0b0b0b", "reports"); filepath.Dir(theirs.Path) != want {
		t.Fatalf("another manager's report in %s, want %s", filepath.Dir(theirs.Path), want)
	}
	builtin, err := upload("", "a.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(identity.HealthDir(home), "reports"); filepath.Dir(builtin.Path) != want {
		t.Fatalf("the built-in's report in %s, want %s", filepath.Dir(builtin.Path), want)
	}
	for _, bad := range []string{"health", "../x", "health-bin", "assistant-m-0b0b0b", "health-m-0b0b0b/../health"} {
		if _, err := upload(bad, "c.pdf"); err == nil {
			t.Errorf("health_dir %q was taken", bad)
		}
		if _, err := call("health.reports.list", map[string]any{"health_dir": bad}); err == nil {
			t.Errorf("health.reports.list of %q was answered", bad)
		}
	}
	list := func(space string) (proto.HealthReports, error) {
		p := map[string]any{}
		if space != "" {
			p["health_dir"] = space
		}
		got, err := call("health.reports.list", p)
		if err != nil {
			return proto.HealthReports{}, err
		}
		return got.(proto.HealthReports), nil
	}
	b, err := list("health-m-0b0b0b")
	if err != nil || len(b.Reports) != 1 || b.Reports[0].Path != theirs.Path || b.HealthDir != "health-m-0b0b0b" {
		t.Fatalf("another manager's list: %+v %v", b, err)
	}
	if a, err := list(""); err != nil || len(a.Reports) != 1 || a.Reports[0].Path != builtin.Path {
		t.Fatalf("the built-in's list: %+v %v", a, err)
	}
	if _, err := call("health.reports.delete", map[string]any{"name": filepath.Base(builtin.Path), "health_dir": "health-m-0b0b0b"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(builtin.Path); err != nil {
		t.Fatal("deleting in another manager's space took the built-in's report")
	}
	if _, err := call("health.reports.delete", map[string]any{"name": filepath.Base(theirs.Path), "health_dir": "health-m-0b0b0b"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(theirs.Path); err == nil {
		t.Fatal("another manager's report is still there")
	}
}

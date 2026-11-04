package hub

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// new → test → pack, on each template: what `personad extension …` does for someone writing an extension.
func TestNewTestPackTemplates(t *testing.T) {
	if testing.Short() {
		t.Skip("builds programs")
	}
	for _, template := range []string{"go", "ts"} {
		t.Run(template, func(t *testing.T) {
			tool := map[string]string{"go": "go", "ts": "bun"}[template]
			if _, err := exec.LookPath(tool); err != nil {
				t.Skipf("no %s here", tool)
			}
			dir := filepath.Join(t.TempDir(), "demo")
			var out bytes.Buffer
			if err := newExtension("demo-"+template, template, "演示", dir, &out); err != nil {
				t.Fatal(err)
			}
			if err := newExtension("demo-"+template, template, "演示", dir, &out); err == nil {
				t.Fatal("new into a directory that has files")
			}
			for _, f := range []string{"persona-extension.json", "README.md", ".gitignore"} {
				if b, err := os.ReadFile(filepath.Join(dir, f)); err != nil || strings.Contains(string(b), "{{") {
					t.Fatalf("%s: %v %q", f, err, b)
				}
			}
			sdkFile := map[string]string{"go": "sdk/go/ext/ext.go", "ts": "sdk/ts/src/index.ts"}[template]
			if _, err := os.Stat(filepath.Join(dir, sdkFile)); err != nil {
				t.Fatalf("the SDK is not copied in: %v", err)
			}
			if template == "go" {
				cmd := exec.Command("go", "test", "./...")
				cmd.Dir = dir
				if b, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("go test in the new extension: %v\n%s", err, b)
				}
			}
			ctx := context.Background()
			out.Reset()
			var errs bytes.Buffer
			if err := testExtension(ctx, dir, &out, &errs); err != nil {
				t.Fatalf("test: %v\n%s\n%s", err, out.String(), errs.String())
			}
			if !strings.Contains(out.String(), "✓ 协议") {
				t.Fatalf("test said: %s", out.String())
			}
			here := runtime.GOOS + "-" + runtime.GOARCH
			out.Reset()
			if err := packExtension(ctx, dir, map[string]string{"platforms": here}, &out, &errs); err != nil {
				t.Fatalf("pack: %v\n%s", err, errs.String())
			}
			names := tarNames(t, filepath.Join(dir, "dist", "demo-"+template+"-0.1.0.tar.gz"))
			want := []string{"demo-" + template + "-0.1.0/README.md", "demo-" + template + "-0.1.0/bin/demo-" + template + "-" + here,
				"demo-" + template + "-0.1.0/persona-extension.json"}
			if !slices.Equal(names, want) {
				t.Fatalf("the package holds %v, want %v (no source, no SDK)", names, want)
			}
			if !strings.Contains(out.String(), `"sha256"`) {
				t.Fatalf("pack printed no index entry: %s", out.String())
			}
		})
	}
}

func tarNames(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	tr := tar.NewReader(zr)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, h.Name)
	}
	slices.Sort(names)
	return names
}

func TestNewRefusesBadInput(t *testing.T) {
	var out bytes.Buffer
	for _, c := range [][3]string{{"Bad ID", "go", "x"}, {"ok", "rust", "x"}, {"ok", "go", `a"b`}} {
		if err := newExtension(c[0], c[1], c[2], filepath.Join(t.TempDir(), "x"), &out); err == nil {
			t.Errorf("new %v: no error", c)
		}
	}
}

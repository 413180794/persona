package extmgr

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/extension/denohost"
	"github.com/nwverse/persona/server/internal/extension/host"
	"github.com/nwverse/persona/server/internal/extension/sourcevalidate"
)

func requireSourceBuildBun(t *testing.T) string {
	t.Helper()
	bun, err := exec.LookPath("bun")
	if err != nil {
		t.Skip("source bundling integration requires Bun")
	}
	return bun
}

func TestSourceNewBuildAndPackage(t *testing.T) {
	requireSourceBuildBun(t)
	dir := t.TempDir()
	if err := newExtension("example", "deno", "Example", dir, io.Discard); err != nil {
		t.Fatal(err)
	}
	p, err := openProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	if p.kind != "deno" {
		t.Fatal(p.kind)
	}
	if err := p.buildSource(context.Background(), io.Discard); err != nil {
		if errors.Is(err, sourcevalidate.ErrUnavailable) {
			t.Skip("UNAVAILABLE: independent source-validator resource gate; build acceptance not claimed")
		}
		t.Fatal(err)
	}
	if _, err := extension.CheckSourceBundle(dir, p.manifest.Runtime.Source); err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.check(); err != nil {
		t.Fatal(err)
	}
	archive, err := p.tarball([]string{"linux-amd64"})
	if err != nil {
		t.Fatal(err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	tr := tar.NewReader(zr)
	bundled := false
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if filepath.Base(h.Name) == "main.mjs" {
			bundled = true
		}
		if filepath.Ext(h.Name) == ".ts" || filepath.Ext(h.Name) == ".tsx" {
			t.Fatal("source entered package", h.Name)
		}
	}
	if !bundled {
		t.Fatal("package omitted bundle")
	}
	if err := testExtension(context.Background(), dir, io.Discard, io.Discard); err == nil {
		t.Fatal("CLI counted unsandboxed engine as acceptance")
	}
}
func TestSourceBuildRejectsOutputLinksBeforeWriting(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dir, "bundle")); err != nil {
		t.Fatal(err)
	}
	if err := makeSourceOutputDir(dir, "bundle/subdir"); err == nil {
		t.Fatal("accepted output symlink")
	}
	if _, err := os.Stat(filepath.Join(outside, "subdir")); !os.IsNotExist(err) {
		t.Fatal("created outside output before rejecting symlink")
	}
}
func TestSourceHasNoOptOutOrUnconfiguredFallback(t *testing.T) {
	ps := &extensions{h: &Manager{cfg: &Config{}}}
	ps.sandboxes.noDefault = true
	p := &extension.Extension{Manifest: &extension.Manifest{Runtime: &extension.Runtime{Mode: extension.RuntimeDeno}}, Record: extension.Record{ID: "example", Source: extension.SourceInstalled}}
	if err := ps.sourceSpec(context.Background(), p, &host.Spec{}); err == nil {
		t.Fatal("missing pin accepted")
	}
	if err := ps.confineSource(context.Background(), p, &host.Spec{}); err == nil {
		t.Fatal("test noDefault caused source fallback")
	}
	off := false
	if err := ps.setSandbox(context.Background(), p, &off, "test"); err == nil {
		t.Fatal("owner opt-out accepted")
	}
	ps.h.cfg.ExtensionRuntime = &denohost.Config{}
	for _, granted := range []extension.Permissions{{HubAPI: true}, {AgentRuntimes: true}} {
		p.Record.Granted = granted
		if err := ps.sourceSpec(context.Background(), p, &host.Spec{}); err == nil || !strings.Contains(err.Error(), "scoped hub grants") {
			t.Fatal("inherited unrestricted/native grant reached the runtime", err)
		}
	}
}

func TestSourceBuildAllowsSymlinkedProjectRoot(t *testing.T) {
	requireSourceBuildBun(t)
	dir := t.TempDir()
	if err := newExtension("example", "deno", "Example", dir, io.Discard); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "project")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	p, err := openProject(link)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.buildSource(context.Background(), io.Discard); err != nil {
		if errors.Is(err, sourcevalidate.ErrUnavailable) {
			t.Skip("UNAVAILABLE: independent source-validator resource gate; build acceptance not claimed")
		}
		t.Fatal(err)
	}
	if _, err := extension.CheckSourceBundle(link, p.manifest.Runtime.Source); err != nil {
		t.Fatal(err)
	}
}

func TestSourcePackBuildsOneArtifactForAllPlatforms(t *testing.T) {
	bun := requireSourceBuildBun(t)
	dir := t.TempDir()
	if err := newExtension("example", "deno", "Example", dir, io.Discard); err != nil {
		t.Fatal(err)
	}
	shim := t.TempDir()
	calls := filepath.Join(shim, "calls")
	// Record invocations while preserving the real bundler and all its arguments.
	script := "#!/bin/sh\nprintf 'build\\n' >> \"$SOURCE_BUILD_CALLS\"\nexec \"$SOURCE_BUILD_BUN\" \"$@\"\n"
	if err := os.WriteFile(filepath.Join(shim, "bun"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SOURCE_BUILD_CALLS", calls)
	t.Setenv("SOURCE_BUILD_BUN", bun)
	t.Setenv("PATH", shim+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := packExtension(context.Background(), dir, map[string]string{}, io.Discard, io.Discard); err != nil {
		if errors.Is(err, sourcevalidate.ErrUnavailable) {
			t.Skip("UNAVAILABLE: independent source-validator resource gate; multi-platform build acceptance not claimed")
		}
		t.Fatal(err)
	}
	data, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "build\n" {
		t.Fatalf("platform-independent artifact was built repeatedly: %q", data)
	}
}

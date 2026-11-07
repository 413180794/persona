package extension

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func examplePackage() string {
	return filepath.Join("..", "..", "..", "schema", "extension", "examples", "example-looks")
}

// The example looks extension parses, its files hold to the bounds, and the schema's example is the same manifest.
func TestExampleLooks(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(examplePackage(), ManifestFile))
	if err != nil {
		t.Fatal(err)
	}
	copyRaw, err := os.ReadFile(filepath.Join(examplePackage(), "..", "Manifest.example-looks.json"))
	if err != nil || !bytes.Equal(raw, copyRaw) {
		t.Fatal("schema/extension/examples/Manifest.example-looks.json is the package's manifest, byte for byte:", err)
	}
	m, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	looks := m.Looks()
	if len(looks) != 2 || looks[0].ID != "aurora" || looks[0].Names["en"] != "Aurora" || looks[1].AppIconFallback != "persona" {
		t.Fatalf("%+v", looks)
	}
	for _, l := range looks {
		if err := CheckLookFiles(l, examplePackage()); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(looks[0].Files()); n != 11 {
		t.Fatalf("aurora names %d files, want 11", n)
	}
}

func TestReservedLookIDsAndRetiredIconFallbacks(t *testing.T) {
	for _, id := range append([]string{DefaultLook}, RetiredLookIDs...) {
		t.Run(id, func(t *testing.T) {
			l := LookItem{ID: id, Name: "Custom look", AppIconFallback: DefaultLook, Avatar: LookAvatar{Sizes: map[string]string{"128": "a.png"}}}
			if err := l.check(); err == nil {
				t.Fatal("a package can reclaim a reserved drawing ID")
			}
			if id != DefaultLook {
				l.ID, l.AppIconFallback = "custom-look", id
				if err := l.check(); err == nil {
					t.Fatal("a new manifest can request a removed app icon")
				}
			}
		})
	}
}

func TestLookItemRules(t *testing.T) {
	ok := `"avatar": {"sizes": {"128": "a.png"}}, "app_icon_fallback": "persona"`
	bad := map[string]string{
		"a built-in look's id":            `{"id": "persona", "name": "x", ` + ok + `}`,
		"an id with an underscore":        `{"id": "my_look", "name": "x", ` + ok + `}`,
		"a fallback that is not built in": `{"id": "x", "name": "x", "avatar": {"sizes": {"128": "a.png"}}, "app_icon_fallback": "nwverse"}`,
		"no fallback":                     `{"id": "x", "name": "x", "avatar": {"sizes": {"128": "a.png"}}}`,
		"no PNG":                          `{"id": "x", "name": "x", "avatar": {"sizes": {}, "svg": "a.svg"}, "app_icon_fallback": "persona"}`,
		"a size that is not a side":       `{"id": "x", "name": "x", "avatar": {"sizes": {"big": "a.png"}}, "app_icon_fallback": "persona"}`,
		"a side too large":                `{"id": "x", "name": "x", "avatar": {"sizes": {"4096": "a.png"}}, "app_icon_fallback": "persona"}`,
		"a language not supported":        `{"id": "x", "name": "x", "names": {"fr": "x"}, ` + ok + `}`,
		"a color role not known":          `{"id": "x", "name": "x", "colors": {"light": {"background": "#FFFFFF"}}, ` + ok + `}`,
		"a color not #RRGGBB":             `{"id": "x", "name": "x", "colors": {"light": {"accent": "blue"}}, ` + ok + `}`,
		"a dot without idle":              `{"id": "x", "name": "x", "dot": {"states": {"working": {"file": "w.png"}}}, ` + ok + `}`,
		"an unknown state":                `{"id": "x", "name": "x", "dot": {"states": {"idle": {"file": "i.png"}, "sleeping": {"file": "s.png"}}}, ` + ok + `}`,
		"a file out of the package":       `{"id": "x", "name": "x", "avatar": {"sizes": {"128": "../a.png"}}, "app_icon_fallback": "persona"}`,
		"a hidden file":                   `{"id": "x", "name": "x", "avatar": {"sizes": {"128": "looks/.a.png"}}, "app_icon_fallback": "persona"}`,
		"an avatar that is not a PNG":     `{"id": "x", "name": "x", "avatar": {"sizes": {"128": "a.jpg"}}, "app_icon_fallback": "persona"}`,
		"an svg that is not .svg":         `{"id": "x", "name": "x", "avatar": {"sizes": {"128": "a.png"}, "svg": "a.xml"}, "app_icon_fallback": "persona"}`,
		"an unknown field":                `{"id": "x", "name": "x", "avatar": {"sizes": {"128": "a.png"}, "gif": "a.gif"}, "app_icon_fallback": "persona"}`,
	}
	for name, item := range bad {
		doc := `{"manifest_version": 1, "id": "kit", "name": "Kit", "version": "1.0.0", "contributes": {"looks": [` + item + `]}}`
		if _, err := Parse([]byte(doc)); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
	doc := `{"manifest_version": 1, "id": "kit", "name": "Kit", "version": "1.0.0", "contributes": {"looks": [{"id": "x", "name": "x", ` + ok + `}]}}`
	if _, err := Parse([]byte(doc)); err != nil {
		t.Fatal(err)
	}
}

func writePNG(t *testing.T, path string, w, h int) {
	t.Helper()
	var b bytes.Buffer
	_ = png.Encode(&b, image.NewRGBA(image.Rect(0, 0, w, h)))
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCheckLookFiles(t *testing.T) {
	l := LookItem{ID: "x", Name: "x", AppIconFallback: "persona", Avatar: LookAvatar{Sizes: map[string]string{"128": "a.png"}}}
	cases := map[string]func(dir string){
		"missing":      func(dir string) {},
		"not a PNG":    func(dir string) { _ = os.WriteFile(filepath.Join(dir, "a.png"), []byte("png?"), 0o644) },
		"not square":   func(dir string) { writePNG(t, filepath.Join(dir, "a.png"), 128, 64) },
		"not its side": func(dir string) { writePNG(t, filepath.Join(dir, "a.png"), 64, 64) },
		"a link out of it": func(dir string) {
			out := filepath.Join(t.TempDir(), "a.png")
			writePNG(t, out, 128, 128)
			_ = os.Symlink(out, filepath.Join(dir, "a.png"))
		},
	}
	for name, lay := range cases {
		dir := t.TempDir()
		lay(dir)
		if err := CheckLookFiles(l, dir); err == nil {
			t.Errorf("%s: passed", name)
		}
	}
	dir := t.TempDir()
	writePNG(t, filepath.Join(dir, "a.png"), 128, 128)
	if err := CheckLookFiles(l, dir); err != nil {
		t.Fatal(err)
	}
	l.Avatar.SVG = "a.svg"
	_ = os.WriteFile(filepath.Join(dir, "a.svg"), []byte(`<html><script>alert(1)</script></html>`), 0o644)
	if err := CheckLookFiles(l, dir); err == nil {
		t.Fatal("an SVG whose root is not <svg> passed")
	}
	_ = os.WriteFile(filepath.Join(dir, "a.svg"), []byte(strings.Repeat(" ", LookMaxSVG+1)), 0o644)
	if err := CheckLookFiles(l, dir); err == nil {
		t.Fatal("an SVG over the bound passed")
	}
	l.Avatar.SVG = ""
	l.Dot = &LookDot{Size: 144, States: map[string]LookDotState{"idle": {File: "i.png"}}}
	writePNG(t, filepath.Join(dir, "i.png"), 600, 600)
	if err := CheckLookFiles(l, dir); err == nil {
		t.Fatal("a dot frame over 512 px passed")
	}
}

func TestCleanSVG(t *testing.T) {
	dirty := `<?xml version="1.0"?>
<!DOCTYPE svg>
<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" viewBox="0 0 10 10" onload="alert(1)">
  <script>alert(2)</script>
  <style>circle { fill: url(https://example.com/x) }</style>
  <foreignObject><div xmlns="http://www.w3.org/1999/xhtml">hi</div></foreignObject>
  <defs><linearGradient id="g"><stop offset="0" stop-color="#fff"/></linearGradient></defs>
  <a href="https://example.com"><circle r="9"/></a>
  <image href="https://example.com/track.png"/>
  <use xlink:href="https://example.com/x.svg#a"/>
  <use href="#g"/>
  <circle cx="5" cy="5" r="4" fill="url(#g)" onclick="alert(3)"/>
  <rect width="2" height="2" style="fill: url(https://example.com/y)"/>
  <path d="M0 0" fill="javascript:alert(4)"/>
  <animate attributeName="href" to="javascript:alert(5)"/>
</svg>`
	clean, err := CleanSVG([]byte(dirty))
	if err != nil {
		t.Fatal(err)
	}
	s := string(clean)
	for _, gone := range []string{"script", "alert", "style", "foreignObject", "example.com", "<a", "<image", "animate", "onload", "onclick", "DOCTYPE", "javascript"} {
		if strings.Contains(s, gone) {
			t.Errorf("%q is still in %s", gone, s)
		}
	}
	for _, kept := range []string{`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10">`, `<use href="#g">`, `fill="url(#g)"`, `<linearGradient id="g">`, `<circle cx="5"`} {
		if !strings.Contains(s, kept) {
			t.Errorf("%q is not in %s", kept, s)
		}
	}
	// Cleaning what was cleaned changes nothing.
	again, err := CleanSVG(clean)
	if err != nil || !bytes.Equal(again, clean) {
		t.Fatalf("%v\n%s\n%s", err, clean, again)
	}
	for name, svg := range map[string]string{
		"entities":   `<!DOCTYPE svg [<!ENTITY x "y">]><svg viewBox="0 0 1 1">&x;</svg>`,
		"not an SVG": `<html/>`,
		"no size":    `<svg/>`,
		"broken":     `<svg viewBox="0 0 1 1"><g></svg>`,
		"two roots":  `<svg viewBox="0 0 1 1"/><svg viewBox="0 0 1 1"/>`,
	} {
		if _, err := CleanSVG([]byte(svg)); err == nil {
			t.Errorf("%s: cleaned", name)
		}
	}
}

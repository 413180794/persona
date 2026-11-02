// Package prompts holds every prompt jarvisd writes for a model: one file per prompt under a directory per scene
// (assistant/, lark/, inbox/, kb/, tools/, shared/), Go text/template syntax, embedded in the binary. Callers pass
// data (machines, the owner's name, collected context…) to a typed render function here; no prompt text is put
// together with fmt or + anywhere else. See docs/prompts.md.
//
// File conventions:
//   - A template's name is its path without ".md" ("inbox/triage"); one prompt includes another with
//     {{template "shared/user_rules" .}}.
//   - The file's last newline is not part of the prompt (editors add one). A prompt that ends with a newline
//     has an empty last line.
//   - mcp/<scene>/<tool>.md are the MCP tools' descriptions and input schemas, read as they are (mcp.go).
package prompts

import (
	"embed"
	"fmt"
	"io/fs"
	"reflect"
	"strings"
	"text/template"
)

//go:embed assistant lark inbox kb tools shared mcp voice
var files embed.FS

var tmpl = parse()

func parse() *template.Template {
	root := template.New("").Option("missingkey=error").Funcs(funcs)
	err := fs.WalkDir(files, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".md") || strings.HasPrefix(path, "mcp/") {
			return err // mcp/ holds tool definitions, not templates (mcp.go)
		}
		raw, err := files.ReadFile(path)
		if err != nil {
			return err
		}
		_, err = root.New(strings.TrimSuffix(path, ".md")).Parse(strings.TrimSuffix(string(raw), "\n"))
		return err
	})
	return template.Must(root, err)
}

// render executes one template. The templates are fixed and every one has a golden test, so a failure is a bug in
// this package: it panics rather than hand a model half a prompt.
func render(name string, data any) string {
	var b strings.Builder
	if err := tmpl.ExecuteTemplate(&b, name, data); err != nil {
		panic(fmt.Sprintf("prompts: %s: %v", name, err))
	}
	return b.String()
}

// Names lists every template (for tests).
func Names() []string {
	var out []string
	for _, t := range tmpl.Templates() {
		if t.Name() != "" {
			out = append(out, t.Name())
		}
	}
	return out
}

var funcs = template.FuncMap{
	// attr escapes text for an XML attribute.
	"attr": func(s string) string {
		return strings.NewReplacer("&", "&amp;", `"`, "&quot;", "<", "&lt;", ">", "&gt;").Replace(s)
	},
	"join": strings.Join,
	// indent puts by before every line and ends with one newline.
	"indent": func(by, s string) string {
		lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
		for i, l := range lines {
			lines[i] = by + l
		}
		return strings.Join(lines, "\n") + "\n"
	},
	// blank: only whitespace.
	"blank": func(s string) bool { return strings.TrimSpace(s) == "" },
	// first is at most n elements of a slice.
	"first": func(n int, list any) any {
		v := reflect.ValueOf(list)
		if v.Len() > n {
			return v.Slice(0, n).Interface()
		}
		return list
	},
}

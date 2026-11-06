package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestCommandName(t *testing.T) {
	none := func(string) string { return "" }
	newEnv := func(k string) string {
		if k == "PERSONA_LINK_NAME" {
			return "persona-link"
		}
		return ""
	}
	for _, c := range []struct {
		arg0 string
		env  func(string) string
		want string
	}{
		{"/home/me/.jarvis/bin/jarvis-link", none, "jarvis-link"},
		{"/Applications/Persona Link.app/Contents/Helpers/jarvis-link", none, "jarvis-link"},
		{"/home/me/.persona/bin/persona-link", none, "persona-link"},
		{"persona-link", none, "persona-link"},
		{"/home/me/.jarvis/bin/jarvis-link", newEnv, "persona-link"},
		{"/tmp/jarvis-link-linux-amd64", none, "jarvis-link"},
	} {
		if got := commandName(c.arg0, c.env); got != c.want {
			t.Errorf("commandName(%q) = %q, want %q", c.arg0, got, c.want)
		}
	}
}

func TestRenameCommandKeepsPathsUnitsAndDownloads(t *testing.T) {
	for in, want := range map[string]string{
		"看状态：jarvis-link status；看日志：jarvis-link logs -f": "看状态：persona-link status；看日志：persona-link logs -f",
		"还没配对：jarvis-link pair '<链接>'。":                  "还没配对：persona-link pair '<链接>'。",
		"（jarvis-link logs -f）":                          "（persona-link logs -f）",
		"jarvis-link":                                    "persona-link",
		"~/.jarvis/bin/jarvis-link 一起删":                  "~/.jarvis/bin/jarvis-link 一起删",
		"Contents/Helpers/jarvis-link":                   "Contents/Helpers/jarvis-link",
		"systemctl --user restart jarvis-link.service":   "systemctl --user restart jarvis-link.service",
		"jarvis-link-linux-amd64.gz":                     "jarvis-link-linux-amd64.gz",
		"jarvis://connector?x=1 jarvisd hub pair":        "jarvis://connector?x=1 jarvisd hub pair",
		"no name here":                                   "no name here",
	} {
		if got := string(renameCommand([]byte(in))); got != want {
			t.Errorf("renameCommand(%q) = %q, want %q", in, got, want)
		}
	}
	var b bytes.Buffer
	n, err := renamer{&b}.Write([]byte("jarvis-link status\n"))
	if err != nil || n != len("jarvis-link status\n") || b.String() != "persona-link status\n" {
		t.Fatalf("renamer wrote %q (n=%d, err=%v)", b.String(), n, err)
	}
}

func TestIsLinkAlias(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	sym := filepath.Join(dir, "persona-link")
	if err := os.Symlink("jarvis-link", sym); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(dir, "other")
	if err := os.Symlink("somewhere-else", other); err != nil {
		t.Fatal(err)
	}
	script := write("script", "#!/bin/sh\nexec \"$(dirname \"$0\")/jarvis-link\" \"$@\"\n")
	mine := write("mine", "#!/bin/sh\necho my own persona-link\n")
	for p, want := range map[string]bool{sym: true, script: true, other: false, mine: false, filepath.Join(dir, "missing"): false} {
		if got := isLinkAlias(p); got != want {
			t.Errorf("isLinkAlias(%s) = %v, want %v", filepath.Base(p), got, want)
		}
	}
}

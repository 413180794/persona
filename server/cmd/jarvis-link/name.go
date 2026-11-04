package main

import (
	"io"
	"path/filepath"
)

// The connector's command name in what it prints (#1159, docs/rename.md). It is persona-link when run under that name
// (the installer's alias next to jarvis-link) or with PERSONA_LINK_NAME=persona-link (the installer runs the real file
// and says the new name); otherwise jarvis-link, and nothing changes. Only the name as a command is replaced: paths
// (…/bin/jarvis-link), the unit (jarvis-link.service), download names (jarvis-link-linux-amd64) stay as they are,
// because those are what is installed. `run`'s JSON status lines for the menu bar app are never rewritten.
const (
	legacyName = "jarvis-link"
	newName    = "persona-link"
)

func commandName(arg0 string, getenv func(string) string) string {
	if filepath.Base(arg0) == newName || getenv("PERSONA_LINK_NAME") == newName {
		return newName
	}
	return legacyName
}

// renamer writes through with every jarvis-link that stands as a command name turned into persona-link.
type renamer struct{ w io.Writer }

func (r renamer) Write(p []byte) (int, error) {
	if _, err := r.w.Write(renameCommand(p)); err != nil {
		return 0, err
	}
	return len(p), nil
}

// renameCommand replaces jarvis-link where it is not part of a path, a file or unit name or a longer word: the
// byte before it is not one of / . _ - or an ASCII letter / digit, and the byte after it not one of . _ - or an
// ASCII letter / digit.
func renameCommand(p []byte) []byte {
	old := []byte(legacyName)
	var out []byte
	last := 0
	for i := 0; i+len(old) <= len(p); i++ {
		if string(p[i:i+len(old)]) != legacyName {
			continue
		}
		if i > 0 && (nameByte(p[i-1]) || p[i-1] == '/') {
			continue
		}
		if j := i + len(old); j < len(p) && nameByte(p[j]) {
			continue
		}
		out = append(append(out, p[last:i]...), newName...)
		last = i + len(old)
		i = last - 1
	}
	if out == nil {
		return p
	}
	return append(out, p[last:]...)
}

func nameByte(b byte) bool {
	return b == '.' || b == '_' || b == '-' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

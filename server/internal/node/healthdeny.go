package node

import (
	"os"
	"path/filepath"
	"slices"

	"github.com/nwverse/persona/server/internal/identity"
)

// healthDenyRules are the permission rules that keep an assistant conversation outside the health space off the
// health space's files (docs/health.md「目录和记忆怎么隔离」, #3282): its directory (the assistant directory's sibling,
// and the data directory's in case they differ) and its Claude project directories (auto memory, transcripts). Deny
// rules hold over any allow rule: 本会话总是允许 and bypassPermissions included. A health conversation itself gets
// none. Bash is not covered: a command can name a path any way it likes (the issue says what would).
func (n *Node) healthDenyRules(cwd string) []string {
	if cwd == "" || identity.SpaceOf(cwd) == identity.SpaceHealth {
		return nil
	}
	var dirs []string
	add := func(d string) {
		if d != "" && !slices.Contains(dirs, d) {
			dirs = append(dirs, d)
		}
	}
	candidates := []string{filepath.Join(filepath.Dir(cwd), identity.SpaceHealth)}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, identity.HealthDir(home))
	}
	for _, d := range candidates {
		add(filepath.Clean(d))
		if real, err := filepath.EvalSymlinks(d); err == nil {
			add(real) // Claude names a project after the directory as it resolved it
		}
	}
	paths := slices.Clone(dirs)
	for _, root := range n.projectDirs() {
		for _, d := range dirs {
			paths = append(paths, filepath.Join(root, nonAlnum.ReplaceAllString(d, "-")))
		}
	}
	var rules []string
	for _, p := range paths {
		p = filepath.ToSlash(p)
		if p[0] != '/' {
			p = "/" + p // a Windows drive path
		}
		for _, tool := range []string{"Read", "Edit"} { // Read covers the reading tools, Edit the writing ones
			rules = append(rules, tool+"(/"+p+"/**)")
		}
	}
	return rules
}

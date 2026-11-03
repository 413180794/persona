// Package assistantskills puts the assistant's skills in its directory (<assistant dir>/.claude/skills,
// docs/hub-update.md「skill 从二进制里拿出来」): the user's own, kept in ~/.jarvis/skills/<name> on the machine that runs
// the assistant and linked in, and the built-in ones this binary carries, copied in. Built-in skills are only those
// Persona may redistribute (telegram, the tg CLI's): a copied one, like lark-connector, lives in the user's space. jarvisd
// never writes that space, so an update cannot touch the user's skills.
package assistantskills

import (
	"bufio"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"github.com/nwverse/persona/server/internal/identity"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

//go:embed all:builtin
var embedded embed.FS

// builtin is the skills this binary carries: one directory each under builtin/ (its README is not one).
var builtin, _ = fs.Sub(embedded, "builtin")

// Redistributable is every built-in skill and why Persona may ship it (TestBuiltinsAreRedistributable).
var Redistributable = map[string]string{
	"telegram": "Persona's own (the tg CLI's skill, docs/plan-telegram.md「tg 命令行」)",
}

// Requires are the built-in skills that need a connector of the hub signed in (its id): put in while it is, taken
// out once it is not (the hub says which are with assistant.prepare).
var Requires = map[string]string{"telegram": "telegram"}

// wanted says whether the built-in skill name goes in with the connectors signed in.
func wanted(name string, connectors []string) bool {
	need, ok := Requires[name]
	return !ok || slices.Contains(connectors, need)
}

// legacyBundled are the skills older jarvisd binaries copied into the assistant's directory: moved to the user's
// space once (migrate).
var legacyBundled = []string{"lark-connector"}

const (
	builtinStamp = ".jarvis-builtin" // the built-in skills last copied in
	legacyStamp  = ".jarvis-skills"  // what older jarvisd binaries left next to their copies
)

// UserDir is the user's skill space: ~/.jarvis/skills (JARVIS_SKILLS_DIR elsewhere, for tests).
func UserDir() string {
	if d := identity.Getenv("JARVIS_SKILLS_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(identity.DataDir(home), "skills")
}

// Skill is one skill the assistant gets.
type Skill struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Source      string `json:"source"` // user | builtin
}

// builtinNames lists the built-in skills.
func builtinNames() []string {
	var out []string
	entries, _ := fs.ReadDir(builtin, ".")
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out
}

// userNames lists the skills in the user's space: directories with a SKILL.md, not hidden.
func userNames(user string) []string {
	var out []string
	entries, _ := os.ReadDir(user)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if st, err := os.Stat(filepath.Join(user, e.Name(), "SKILL.md")); err == nil && !st.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out
}

// Sync makes dir (<assistant dir>/.claude/skills) hold the user's skills, as links into user, and the built-in ones
// the user has none of, as copies: those that need a connector (Requires) only while it is among connectors, the hub's
// signed in. A link into user whose skill is gone is removed; a directory the user put in dir themselves is left
// alone. logf reports what changed.
func Sync(dir, user string, connectors []string, logf func(string, ...any)) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	migrate(dir, user, logf)
	mine := userNames(user)
	if err := installBuiltins(dir, mine, connectors); err != nil {
		return err
	}
	for _, name := range mine {
		target := filepath.Join(user, name)
		at := filepath.Join(dir, name)
		if cur, err := os.Readlink(at); err == nil {
			if cur == target {
				continue
			}
			_ = os.Remove(at)
		} else if st, err := os.Lstat(at); err == nil && st.IsDir() {
			if !slices.Contains(builtinNames(), name) {
				logf("assistant skills: %s is a directory of its own in %s; %s is not linked over it", name, dir, target)
				continue
			}
			_ = os.RemoveAll(at) // a built-in copy: the user's skill of that name wins
		}
		if err := os.Symlink(target, at); err != nil {
			return err
		}
	}
	// Links into the user's space whose skill is gone.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		at := filepath.Join(dir, e.Name())
		target, err := os.Readlink(at)
		if err != nil || filepath.Dir(target) != filepath.Clean(user) || slices.Contains(mine, e.Name()) {
			continue
		}
		_ = os.Remove(at)
	}
	return nil
}

// migrate moves the skills an older jarvisd copied into dir (the stamp says it did) into the user's space, once: the
// user keeps them (lark-connector on the hub that had it), and Sync links them back.
func migrate(dir, user string, logf func(string, ...any)) {
	if _, err := os.Stat(filepath.Join(dir, legacyStamp)); err != nil {
		return
	}
	for _, name := range legacyBundled {
		from, to := filepath.Join(dir, name), filepath.Join(user, name)
		st, err := os.Lstat(from)
		if err != nil || !st.IsDir() {
			continue
		}
		if _, err := os.Stat(to); err == nil {
			_ = os.RemoveAll(from) // the user's space has its own: the old copy goes, Sync links theirs
			continue
		}
		if err := os.MkdirAll(user, 0o755); err != nil {
			logf("assistant skills: move %s to %s: %v", name, user, err)
			return
		}
		if err := os.Rename(from, to); err != nil {
			logf("assistant skills: move %s to %s: %v", name, user, err)
			return
		}
		logf("assistant skills: moved %s to %s (skills are no longer in jarvisd)", name, to)
	}
	_ = os.Remove(filepath.Join(dir, legacyStamp))
}

// builtinVersion identifies the built-in skills (a hash of every file).
func builtinVersion() string {
	h := sha256.New()
	_ = fs.WalkDir(builtin, ".", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			b, _ := fs.ReadFile(builtin, p)
			h.Write([]byte(p))
			h.Write(b)
		}
		return err
	})
	return hex.EncodeToString(h.Sum(nil))[:12]
}

// installBuiltins copies the built-in skills into dir, except those the user has their own of and those whose
// connector is not signed in; the stamp (version and what was copied) skips an identical install and lets a copy no
// longer carried, or no longer wanted, be removed.
func installBuiltins(dir string, mine, connectors []string) error {
	var names []string
	for _, n := range builtinNames() {
		if !slices.Contains(mine, n) && wanted(n, connectors) {
			names = append(names, n)
		}
	}
	want := builtinVersion() + " " + strings.Join(names, ",")
	stamp := filepath.Join(dir, builtinStamp)
	old, _ := os.ReadFile(stamp)
	if strings.TrimSpace(string(old)) == want {
		return nil
	}
	if f := strings.Fields(string(old)); len(f) == 2 {
		for _, n := range strings.Split(f[1], ",") {
			if at := filepath.Join(dir, n); n != "" && !slices.Contains(names, n) {
				if st, err := os.Lstat(at); err == nil && st.IsDir() {
					_ = os.RemoveAll(at)
				}
			}
		}
	}
	for _, name := range names {
		tmp := filepath.Join(dir, "."+name+".new")
		_ = os.RemoveAll(tmp)
		err := fs.WalkDir(builtin, name, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			out := filepath.Join(tmp, strings.TrimPrefix(p, name))
			if d.IsDir() {
				return os.MkdirAll(out, 0o755)
			}
			b, err := fs.ReadFile(builtin, p)
			if err != nil {
				return err
			}
			mode := os.FileMode(0o644)
			if strings.HasSuffix(p, ".sh") {
				mode = 0o755
			}
			return os.WriteFile(out, b, mode)
		})
		if err != nil {
			return err
		}
		final := filepath.Join(dir, name)
		_ = os.RemoveAll(final)
		if err := os.Rename(tmp, final); err != nil {
			return err
		}
	}
	return os.WriteFile(stamp, []byte(want+"\n"), 0o644)
}

// List is the skills the assistant gets on this machine: the user's, then the built-in ones they have none of whose
// connector, if they need one, is signed in.
func List(user string, connectors []string) []Skill {
	out := []Skill{}
	mine := userNames(user)
	for _, n := range mine {
		b, _ := os.ReadFile(filepath.Join(user, n, "SKILL.md"))
		out = append(out, Skill{Name: n, Description: description(string(b)), Source: "user"})
	}
	for _, n := range builtinNames() {
		if !slices.Contains(mine, n) && wanted(n, connectors) {
			b, _ := fs.ReadFile(builtin, n+"/SKILL.md")
			out = append(out, Skill{Name: n, Description: description(string(b)), Source: "builtin"})
		}
	}
	return out
}

// Has says whether the assistant directory's skills (dir) include name.
func Has(dir, name string) bool {
	_, err := os.Stat(filepath.Join(dir, name, "SKILL.md"))
	return err == nil
}

// description is the `description:` of a SKILL.md's front matter (a folded `>` or `|` one joined on one line).
func description(md string) string {
	sc := bufio.NewScanner(strings.NewReader(md))
	if !sc.Scan() || strings.TrimSpace(sc.Text()) != "---" {
		return ""
	}
	var folded []string
	inDesc := false
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == "---" {
			break
		}
		if inDesc {
			if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
				folded = append(folded, strings.TrimSpace(line))
				continue
			}
			break
		}
		if v, ok := strings.CutPrefix(line, "description:"); ok {
			v = strings.TrimSpace(v)
			if v == ">" || v == "|" || v == ">-" || v == "|-" || v == "" {
				inDesc = true
				continue
			}
			return strings.Trim(v, `"'`)
		}
	}
	return strings.Join(folded, " ")
}

// OlScript is the lark-connector helper's path once the skill is in dir.
func OlScript(dir string) string { return filepath.Join(dir, "lark-connector", "scripts", "ol.sh") }

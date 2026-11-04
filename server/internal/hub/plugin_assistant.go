package hub

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// The assistant extension point (docs/plugins.md「助手」): a plugin's skills go to the machine that runs the assistant
// with assistant.prepare (the files only when that machine does not have them yet), and its instructions are added to
// the assistant's CLAUDE.md.

// pluginSkillMost is the most a plugin's skills may weigh, all files together.
const pluginSkillMost = 8 << 20

// PluginSkill is one plugin skill as assistant.prepare carries it: Files only when the machine needs them.
type PluginSkill struct {
	Name   string            `json:"name"`
	Plugin string            `json:"plugin"`
	Digest string            `json:"digest"`
	Files  map[string][]byte `json:"files,omitempty"` // relative path → content
}

// pluginSkills are the skills of every plugin switched on, read from their packages.
func (ps *plugins) pluginSkills() []PluginSkill {
	var out []PluginSkill
	if ps == nil {
		return out
	}
	for _, p := range ps.reg.List() {
		if !p.Record.Enabled || p.Problem != "" || p.Dir == "" {
			continue
		}
		total := 0
		for _, a := range p.Manifest.Assistants() {
			for _, dir := range a.Skills {
				root := filepath.Join(p.Dir, filepath.FromSlash(dir))
				files := map[string][]byte{}
				h := sha256.New()
				err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
					if err != nil || d.IsDir() || !d.Type().IsRegular() {
						return err
					}
					b, err := os.ReadFile(path)
					if err != nil {
						return err
					}
					total += len(b)
					rel, _ := filepath.Rel(root, path)
					files[filepath.ToSlash(rel)] = b
					return nil
				})
				if err != nil || total > pluginSkillMost {
					ps.logf("plugin %s: skill %s: not sent (%v, %d bytes)", p.Record.ID, dir, err, total)
					continue
				}
				names := make([]string, 0, len(files))
				for n := range files {
					names = append(names, n)
				}
				sort.Strings(names)
				for _, n := range names {
					h.Write([]byte(n))
					h.Write([]byte{0})
					h.Write(files[n])
				}
				out = append(out, PluginSkill{Name: filepath.Base(root), Plugin: p.Record.ID, Digest: hex.EncodeToString(h.Sum(nil))[:16], Files: files})
			}
		}
	}
	return out
}

// pluginSkillsFor is pluginSkills for machine id, the files left out of those it was sent already.
func (ps *plugins) pluginSkillsFor(machine string) []PluginSkill {
	all := ps.pluginSkills()
	if ps == nil {
		return all
	}
	v, _ := ps.skillsSent.LoadOrStore(machine, &sync.Map{})
	sent := v.(*sync.Map)
	out := make([]PluginSkill, 0, len(all))
	for _, s := range all {
		if d, ok := sent.Load(s.Name); ok && d == s.Digest {
			s.Files = nil
		} else {
			sent.Store(s.Name, s.Digest)
		}
		out = append(out, s)
	}
	return out
}

// pluginInstructions are the plugins' parts of the assistant's CLAUDE.md, each under its plugin's name.
func (ps *plugins) pluginInstructions() string {
	if ps == nil {
		return ""
	}
	var b strings.Builder
	for _, p := range ps.reg.List() {
		if !p.Record.Enabled || p.Problem != "" || p.Dir == "" {
			continue
		}
		for _, a := range p.Manifest.Assistants() {
			if a.Instructions == "" {
				continue
			}
			text, err := os.ReadFile(filepath.Join(p.Dir, filepath.FromSlash(a.Instructions)))
			if err != nil || len(strings.TrimSpace(string(text))) == 0 {
				continue
			}
			b.WriteString("\n\n<!-- plugin " + p.Record.ID + " -->\n")
			b.WriteString(strings.TrimSpace(string(text)))
			b.WriteString("\n")
		}
	}
	return b.String()
}

// prepareAssistant tells m's node what it needs before a message to the assistant (assistantPrepare) and sets cwd to
// the assistant's directory there. A plugin skill the node says it does not have goes with its files next time.
func (h *Hub) prepareAssistant(ctx context.Context, m *Machine, cwd *string) error {
	var prep struct {
		Cwd  string   `json:"cwd"`
		Need []string `json:"plugin_skills_need"`
	}
	if err := m.call(ctx, "assistant.prepare", h.assistantPrepare(ctx, m), &prep); err != nil {
		return err
	}
	*cwd = prep.Cwd
	if h.plugins == nil {
		return nil
	}
	if v, ok := h.plugins.skillsSent.Load(m.cfg.ID); ok {
		for _, n := range prep.Need {
			v.(*sync.Map).Delete(n)
		}
	}
	return nil
}

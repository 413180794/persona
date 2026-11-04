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
	"time"

	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/plugin"
	"github.com/nwverse/persona/server/internal/proto"
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

// assistantGates are the command gates of the plugins' assistant contributions (their gate files), each with the
// credentials its plugin gives now (assistant.credentials, only the names the file declares) and the user's rules for
// its connector first. For the assistant's machine only: the credentials are never kept or logged.
func (ps *plugins) assistantGates(ctx context.Context, userRules func(connectorID string) []proto.LarkRule) []proto.CommandGate {
	var out []proto.CommandGate
	if ps == nil {
		return out
	}
	for _, p := range ps.reg.List() {
		if !p.Record.Enabled || p.Problem != "" || p.Dir == "" {
			continue
		}
		for _, a := range p.Manifest.Assistants() {
			if a.Gate == "" {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(p.Dir, filepath.FromSlash(a.Gate)))
			if err != nil {
				continue
			}
			gf, err := plugin.ParseGate(raw)
			if err != nil {
				ps.logf("plugin %s: gate %s: %v", p.Record.ID, a.Gate, err)
				continue
			}
			g := proto.CommandGate{Plugin: p.Record.ID, Command: gf.Command, Script: gf.Script, Title: gf.Title, BaseEnv: gf.BaseEnv,
				Secret: gf.Secret, AllowedEnv: gf.AllowedEnv, Headers: gf.Headers, PathPrefix: gf.PathPrefix, Actions: gf.Actions, Tool: gf.Tool}
			if gf.Connector != "" && userRules != nil {
				g.Rules = append(g.Rules, userRules(gf.Connector)...)
			}
			g.Rules = append(g.Rules, gf.Rules...)
			if len(gf.Env) > 0 {
				g.Env = ps.assistantCredentials(ctx, p.Record.ID, a.ID, gf.Env)
			}
			out = append(out, g)
		}
	}
	return out
}

// assistantCredentials asks a plugin for its gate's credentials (assistant.credentials → {env}), keeping only the
// names its gate file declares; nothing when it cannot answer (the command then says its API is not reachable).
func (ps *plugins) assistantCredentials(ctx context.Context, id, contribution string, names []string) map[string]string {
	hp := ps.get(id)
	if hp == nil || hp.proc == nil {
		return nil
	}
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	params, _ := plugin.Targeted(struct{}{}, contribution)
	var res struct {
		Env map[string]string `json:"env"`
	}
	if err := hp.proc.Call(cctx, "assistant.credentials", params, &res); err != nil {
		ps.logf("plugin %s: assistant.credentials: %v", id, err) // never the values
		return nil
	}
	out := map[string]string{}
	for _, n := range names {
		if v, ok := res.Env[n]; ok {
			out[n] = v
		}
	}
	return out
}

// assistantSummary lets the plugin whose gate is titled title rewrite a phone card's text (ids into names:
// assistant.summary → {summary}); the text as it was when none does or it cannot.
func (ps *plugins) assistantSummary(ctx context.Context, title, summary string) (string, bool) {
	if ps == nil {
		return summary, false
	}
	for _, p := range ps.reg.List() {
		if !p.Record.Enabled || p.Problem != "" || p.Dir == "" {
			continue
		}
		for _, a := range p.Manifest.Assistants() {
			if a.Gate == "" {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(p.Dir, filepath.FromSlash(a.Gate)))
			if err != nil {
				continue
			}
			gf, err := plugin.ParseGate(raw)
			if err != nil || gf.Title != title {
				continue
			}
			hp := ps.get(p.Record.ID)
			if hp == nil || hp.proc == nil {
				return summary, false
			}
			cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			params, _ := plugin.Targeted(map[string]any{"summary": summary}, a.ID)
			var res struct {
				Summary string `json:"summary"`
			}
			err = hp.proc.Call(cctx, "assistant.summary", params, &res)
			cancel()
			if err != nil || strings.TrimSpace(res.Summary) == "" {
				return summary, true
			}
			return res.Summary, true
		}
	}
	return summary, false
}

// assistantLark is what the assistant's machine gets for its skills' commands (proto.AssistantLark): lark-cli's
// settings and rules, and the plugins' command gates.
func (h *Hub) assistantLark(ctx context.Context) (proto.AssistantLark, error) {
	lc := h.cfg.Lark
	larkRules := connector.LarkRules(h.cfg.ConnectorGate, h.cfg.LarkGate)
	out := proto.AssistantLark{Rules: larkRules, Prefer: lc.Prefer}
	if lc.CLI != nil {
		out.CLI = &proto.LarkCLI{Bin: lc.CLI.Bin, Profile: lc.CLI.Profile}
	}
	out.Gates = h.plugins.assistantGates(ctx, func(id string) []proto.LarkRule {
		if id == connector.Lark {
			return larkRules
		}
		return connector.GateRules(h.cfg.ConnectorGate, id)
	})
	return out, nil
}

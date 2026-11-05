package proto

import (
	"path/filepath"
	"slices"
	"strings"
)

// AgentFilter is an extension's rule of agent sessions kept out of Agents (extension point agent_filters, #2428), as
// the hub hands it to a node (node.configure agent_filters): a session of one of Agents (none: both) goes when any
// matcher fits it. Only the filters that hold are sent.
type AgentFilter struct {
	Extension            string   `json:"extension"`
	ID                   string   `json:"id"`
	Agents               []string `json:"agents,omitempty"`
	Originators          []string `json:"originators,omitempty"`
	AddedDirs            []string `json:"added_dirs,omitempty"`
	FirstMessagePrefixes []string `json:"first_message_prefixes,omitempty"`
}

// SessionMarks are what a session file says of where the session came from, read from its head.
type SessionMarks struct {
	Originator string   // Codex: session_meta.originator
	AddedDirs  []string // Claude Code: the environment's additionalWorkingDirectories
	First      string   // the first user message
}

// Matches says whether the filter takes a session of agent ("claude", "codex") with these marks; home expands "~/".
func (f AgentFilter) Matches(agent string, m SessionMarks, home string) bool {
	if len(f.Agents) > 0 && !slices.Contains(f.Agents, agent) {
		return false
	}
	if m.Originator != "" && slices.Contains(f.Originators, m.Originator) {
		return true
	}
	for _, want := range f.AddedDirs {
		want = filepath.Clean(ExpandHome(want, home))
		for _, d := range m.AddedDirs {
			d = filepath.Clean(d)
			if d == want || strings.HasPrefix(d, want+string(filepath.Separator)) {
				return true
			}
		}
	}
	first := strings.TrimSpace(m.First)
	for _, p := range f.FirstMessagePrefixes {
		if p = strings.TrimSpace(p); p != "" && strings.HasPrefix(first, p) {
			return true
		}
	}
	return false
}

// AgentFiltered says whether any of filters takes the session.
func AgentFiltered(filters []AgentFilter, agent string, m SessionMarks, home string) bool {
	return slices.ContainsFunc(filters, func(f AgentFilter) bool { return f.Matches(agent, m, home) })
}

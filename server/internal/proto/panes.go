package proto

// PanesResponse is a machine's terminal host layout for the terminal browser (docs/terminal.md「独立终端」): every
// workspace, tab and pane of each host, agent or plain shell. It is read from the snapshot the node already takes;
// ids are host-local.
type PanesResponse struct {
	Hosts []PaneHost `json:"hosts"`
}

// PaneHost is one terminal host (herdr, herdr-core, Orca…) and what it can do with a pane.
type PaneHost struct {
	Kind string `json:"kind"`
	// Stream: the host streams a pane's terminal (observe); without it a pane can only be read once a second.
	Stream bool `json:"stream"`
	// Control: a phone can drive a pane at its own size (接管).
	Control bool `json:"control"`
	// NewTab: the host can open a new shell tab in a workspace (panes.new).
	NewTab     bool        `json:"new_tab"`
	Workspaces []PaneSpace `json:"workspaces"`
	Tabs       []PaneTab   `json:"tabs"`
	Panes      []PaneItem  `json:"panes"`
}

type PaneSpace struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Number int    `json:"number,omitempty"`
	Cwd    string `json:"cwd,omitempty"`
	Branch string `json:"branch,omitempty"`
}

type PaneTab struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	Label       string `json:"label"`
	Number      int    `json:"number,omitempty"`
}

// PaneItem is one pane. Terminal is what the terminal stream calls it ("pane:<pane>", docs/protocol.md「终端流」);
// SessionID is set when an agent session of this machine runs in it (open that one instead: it shares the stream the
// agent's own terminal panel uses).
type PaneItem struct {
	Pane        string `json:"pane"`
	Terminal    string `json:"terminal"`
	TabID       string `json:"tab_id"`
	WorkspaceID string `json:"workspace_id"`
	// Agent is the kind of agent running there ("claude", "codex"…), "" for a plain shell.
	Agent     string `json:"agent,omitempty"`
	Status    string `json:"status,omitempty"` // the host's status for the agent (Status*)
	Title     string `json:"title,omitempty"`
	Cwd       string `json:"cwd,omitempty"`
	SessionID string `json:"session_id,omitempty"`
}

// NewShellResult is what panes.new opened.
type NewShellResult struct {
	Pane     string `json:"pane"`
	Terminal string `json:"terminal"`
	TabID    string `json:"tab_id,omitempty"`
}

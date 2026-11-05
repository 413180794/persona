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
	NewTab bool `json:"new_tab"`
	// Worktree: the host can make a Git worktree of a workspace's repository and open it as a workspace (worktree.create).
	Worktree   bool        `json:"worktree"`
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

// PaneSplitResult is what panes.split opened: the new pane next to the one split.
type PaneSplitResult struct {
	Pane     string `json:"pane"`
	Terminal string `json:"terminal"`
}

// PaneGitResult is what panes.git did in a pane's or workspace's directory. Message is a one-line summary for a toast,
// Output what git printed (cut short).
type PaneGitResult struct {
	Op      string `json:"op"`
	Message string `json:"message"`
	Output  string `json:"output,omitempty"`
}

// WorktreePrepare is what worktree.prepare knows of a workspace's repository, for 新建 worktree…'s dialog. Path is the
// directory the host puts a repository's checkouts under (`<dir>/<repo>`): a checkout of branch b goes in
// Path + "/" + Slug(b), the host's own naming, which is what the dialog fills in from the branch.
type WorktreePrepare struct {
	Root     string `json:"root"`
	RepoName string `json:"repoName"`
	// Branch is the branch checked out in the workspace ("" on a detached HEAD).
	Branch string `json:"branch"`
	Path   string `json:"path"`
}

// WorktreeCreateResult is what worktree.create opened: the new workspace, and the checkout it is in.
type WorktreeCreateResult struct {
	WorkspaceID string `json:"workspace_id"`
	TabID       string `json:"tab_id,omitempty"`
	Pane        string `json:"pane,omitempty"`
	Terminal    string `json:"terminal,omitempty"`
	Path        string `json:"path"`
	Branch      string `json:"branch"`
}

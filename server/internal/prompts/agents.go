package prompts

// AgentTriage is the system prompt that reads a stopped agent's last reply and says where its work stands
// (agents/triage, docs/agent-work.md).
type AgentTriage struct {
	Owner string // "" is 用户
}

func RenderAgentTriage(t AgentTriage) string { return render("agents/triage", t) }

// AgentTriageInput is the turn to triage (agents/triage_input).
type AgentTriageInput struct {
	Now     string // the user's local time, "2006-01-02 15:04 Mon"
	Agent   string // its tab / title
	Machine string
	Cwd     string
	Project string // the knowledge base's project for its directory, "" when none
	Ask     string // what the user asked in this turn, "" when it is not known
	Reply   string // its last reply, clipped
	Others  []AgentTriageOther
}

// AgentTriageOther is another agent working on the same machine: what this one may be waiting for.
type AgentTriageOther struct {
	ID, Label, Line string
}

func RenderAgentTriageInput(in AgentTriageInput) string { return render("agents/triage_input", in) }

// AgentWake is what the hub sends an agent whose wait should be over (agents/wake).
type AgentWake struct {
	Attempt   int
	Wait      string
	Then      string
	Agent     string // the agent it waited for, when it is that one stopping that wakes it
	AgentLine string // what that agent said last
}

func RenderAgentWake(w AgentWake) string { return render("agents/wake", w) }

// AgentAnswer is the user's answers to the hub's decision card, sent on to the agent (agents/answer).
type AgentAnswer struct {
	Items []AgentAnswerItem
}

type AgentAnswerItem struct {
	Question, Answer string
}

func RenderAgentAnswer(a AgentAnswer) string { return render("agents/answer", a) }

// LauncherRepair is the first message of the agent that repairs a machine's launchers (agents/launcher_repair,
// docs/launcher-health.md).
type LauncherRepair struct {
	Skill     string // the skill that sets up and repairs the agent CLIs
	Machine   string // the broken machine's id
	Name      string // its name, "" when the same as the id
	SSHHost   string // the hub machine's ssh alias for it, "" when none
	Launchers []LauncherRepairItem
}

// LauncherRepairItem is one launcher that failed its check there.
type LauncherRepairItem struct {
	Name  string
	Agent string // claude, codex…
	Kind  string // the hub's class of the error (model_route, auth, cli_outdated, timeout, other)
	Error string
}

func RenderLauncherRepair(r LauncherRepair) string { return render("agents/launcher_repair", r) }

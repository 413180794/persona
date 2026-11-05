package prompts

// TodoDecisions is the context a handoff to an agent carries for a todo's decisions in effect (todo/decisions,
// #1618, docs/todos.md「决策记录」). The caller leaves out the closing tags
// ("</persona-context>", "</jarvis-context>") from the texts.
type TodoDecisions struct {
	Title     string
	Decisions []TodoDecision
}

// TodoDecision is one decision: N counts from 1; When is its time as the user reads it, From where it came from
// ("在 <Agent> 的提问里答的"), empty for one written in the todo.
type TodoDecision struct {
	N                        int
	Question, Choice, Reason string
	When, From               string
}

// RenderTodoDecisions is the context block of a todo's decisions; empty when it has none.
func RenderTodoDecisions(d TodoDecisions) string {
	if len(d.Decisions) == 0 {
		return ""
	}
	return render("todo/decisions", d)
}

// TodoTitle is the system prompt naming a todo a task opened after what the agent is asked to do (todo/title, #2775,
// docs/todos.md「待办标题」).
type TodoTitle struct {
	// Language is the title's language by its own name ("English", "日本語"); "" is Chinese.
	Language string
	Max      int // characters
}

func RenderTodoTitle(t TodoTitle) string { return render("todo/title", t) }

// RenderTodoTitleInput is what the title model reads: the task's prompt (the todo's notes).
func RenderTodoTitleInput(prompt string) string {
	return render("todo/title_input", struct{ Prompt string }{prompt})
}

// TodoOutcome is the system prompt pulling what a finished task came to out of its agents' last replies: a one-line
// outcome, links, decisions, pitfalls (todo/outcome, docs/todos.md「做完记结论」, #2820).
type TodoOutcome struct {
	Owner string
}

func RenderTodoOutcome(d TodoOutcome) string { return render("todo/outcome", d) }

// TodoOutcomeInput is the task as it was handed out and what its agents said when they stopped (todo/outcome_input).
type TodoOutcomeInput struct {
	Title, Prompt string
	Replies       []TodoOutcomeReply
}

// TodoOutcomeReply is one agent's last reply: the machine's name and the directory it worked in.
type TodoOutcomeReply struct {
	Machine, Cwd, Text string
}

func RenderTodoOutcomeInput(in TodoOutcomeInput) string { return render("todo/outcome_input", in) }

// TodoProject is the <jarvis-context> a new agent gets before its first prompt when its directory belongs to a
// project (todo/project, #2820): the project's one line and the outcomes of its latest finished todos, newest first.
// The caller leaves out "</jarvis-context>" from the texts.
type TodoProject struct {
	Name, OneLine string
	Outcomes      []TodoProjectOutcome
}

// TodoProjectOutcome is one finished todo's outcome as the project's timeline keeps it.
type TodoProjectOutcome struct {
	When, Title, Text string
}

// RenderTodoProject is the block; empty when there is nothing to say (no one line and no outcomes).
func RenderTodoProject(p TodoProject) string {
	if p.OneLine == "" && len(p.Outcomes) == 0 {
		return ""
	}
	return render("todo/project", p)
}

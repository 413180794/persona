package prompts

// TodoDecisions is the context a handoff to an agent carries for a todo's decisions in effect (todo/decisions,
// #1618, docs/todos.md "Decision log"). The caller leaves out the closing tags
// ("</persona-context>", "</jarvis-context>") from the texts.
type TodoDecisions struct {
	Title     string
	Decisions []TodoDecision
}

// TodoDecision is one decision: N counts from 1; When is its time as the user reads it, From where it came from
// ("answered in <Agent>'s question", in Chinese), empty for one written in the todo.
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

// TodoThreadReply is what the user said in the chat thread a todo is bound to, as it goes to the todo's agent (todo/thread_reply,
// #5279, docs/todos.md "Chat thread"): one line saying whose decision it is, then the words as they were said. Title
// is the todo's, on one line, with nothing in it that closes a context block; Recorded: the words were written into
// the todo's decisions (it is not said when they were not: a message of only a picture, no database, a failed write).
type TodoThreadReply struct {
	Title, Text string
	Recorded    bool
}

func RenderTodoThreadReply(r TodoThreadReply) string { return render("todo/thread_reply", r) }

// TodoTitle is the system prompt naming a todo a task opened after what the agent is asked to do (todo/title, #2775,
// docs/todos.md "To-do titles").
type TodoTitle struct {
	// Language is the title's language by its own name ("English", "Japanese" written in Japanese); "" is Chinese.
	Language string
	Max      int // characters
}

func RenderTodoTitle(t TodoTitle) string { return render("todo/title", t) }

// RenderTodoTitleInput is what the title model reads: the task's prompt (the todo's notes).
func RenderTodoTitleInput(prompt string) string {
	return render("todo/title_input", struct{ Prompt string }{prompt})
}

// TodoOutcome is the system prompt pulling what a finished task came to out of its agents' last replies: a one-line
// outcome, links, decisions, pitfalls (todo/outcome, docs/todos.md "Recording the outcome", #2820).
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

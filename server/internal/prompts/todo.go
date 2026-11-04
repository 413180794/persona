package prompts

// TodoDecisions is the context a handoff to an agent carries for a todo's decisions in effect (todo/decisions,
// #1618, docs/todos.md「决策记录」). The caller leaves out "</jarvis-context>" from the texts.
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

// RenderTodoDecisions is the <jarvis-context> block of a todo's decisions; empty when it has none.
func RenderTodoDecisions(d TodoDecisions) string {
	if len(d.Decisions) == 0 {
		return ""
	}
	return render("todo/decisions", d)
}

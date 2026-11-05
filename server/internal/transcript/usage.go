package transcript

// A model gateway between Claude Code and the API can report usage that no single request can have: one reported
// zero on every reply of a turn and, on its last reply, the input tokens of all the turn's requests added up (1.7M
// for a conversation of 85k, #3300). Claude Code sizes the context by the last reply's usage, so on the next message
// it took the conversation as over the window: it compacted (losing the context for nothing) or, when the summary
// failed too, answered "Prompt is too long" to every message from then on. A number like that says nothing about the
// context, and Persona does not use it (the context meter, the idle compaction); node/assistant_usage.go repairs it.

// MaxContextTokens is the largest context of any model Claude Code runs (the 1M variants): a single request's input
// beyond it was not one request.
const MaxContextTokens = 1_000_000

// usageSlack is what a request carries besides the transcript's lines (the system prompt, tools, CLAUDE.md, memory):
// a request's input beyond the transcript's size plus this cannot be the conversation's.
const usageSlack = 200_000

// PlausibleContext says whether tokens can be the input of one request on a transcript of size bytes (0: size not
// known). A token is at least about a byte of the text it stands for, so a conversation cannot hold more tokens than
// its transcript has bytes, plus what the request brings along.
func PlausibleContext(tokens int, size int64) bool {
	if tokens > MaxContextTokens {
		return false
	}
	return size <= 0 || int64(tokens) <= size+usageSlack
}

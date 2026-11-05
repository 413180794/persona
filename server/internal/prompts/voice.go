package prompts

import "regexp"

// The voice extension's prompts (voice/, docs/voice.md「提示词」). The ones a voice turn is told and sends (report,
// refused, delegated, spoken) come in the user's language: their Lang is an i18n code, en or ja, anything else is
// Chinese.

// VoiceLine is one final line of a voice conversation.
type VoiceLine struct {
	Role string // user | assistant
	Text string
}

// VoiceInstructions is what the built-in voice plugin's realtime model is told about itself (voice/instructions).
type VoiceInstructions struct {
	Owner string // "" is 用户
	Notes string
	// Tools is which of the plugin's function tools (mcp/voice) the session declares: "all"; "ask_jarvis" (only that
	// one: another extension calling, docs/voice.md「权限」); "" none, the model delegates.
	Tools string
	// History: what was said before the connection dropped (a reconnected session), oldest first.
	History []VoiceLine
}

func RenderVoiceInstructions(d VoiceInstructions) string { return render("voice/instructions", d) }

// VoiceReport is what the voice model is told when a turn handed to the main conversation is over, or stops to wait for
// the user (voice/report). Its first sentence and the <agent_output> block around the answer are the wording measured
// to keep the model from doing what an agent wrote (docs/lessons/voice.md「注入实测用的原文」).
type VoiceReport struct {
	Lang   string
	Status string   // done | failed | needs_you
	Tasks  []string // what the user asked for, oldest first (several: messages answered together)
	// Answer is the turn's answer made speakable (voice/spoken.go), "" when it ended without words; Stopped why it
	// stopped then, when known.
	Answer, Stopped string
	// Background is the background work still running (done): its turn is answered too; Again: this is that answer.
	Background int
	Again      bool
	Reason     string // failed: why there is no answer
}

func RenderVoiceReport(d VoiceReport) string { return render("voice/report", d) }

// VoiceRefused is what the voice model is told when what it handed over was not sent (voice/refused).
type VoiceRefused struct {
	Lang string
	// Code: unprompted (the user had not spoken since the last agent's words, or not lately: docs/voice.md「用户发言
	// 门槛」), empty (no task was heard), unknown (the send broke off and may have been typed), unreachable (the hub
	// did not answer before anything was sent), failed (Reason says why, when the hub said).
	Code   string
	Task   string
	Reason string
	// Agent: it was for an agent (the send_to_agent tool), not the Persona main conversation.
	Agent bool
}

func RenderVoiceRefused(d VoiceRefused) string { return render("voice/refused", d) }

// VoiceDelegated is the message a voice conversation sends the main conversation (voice/delegated): the task on its
// first line (a run finds the message by it when the transcript has no client_msg_id), then a context block.
type VoiceDelegated struct {
	Lang    string
	Task    string
	History []VoiceLine // the last lines of the voice conversation (a delegation's own text can be terse)
}

func RenderVoiceDelegated(d VoiceDelegated) string { return render("voice/delegated", d) }

// VoiceSpoken is a stand-in for what an answer holds that is not to be read out (voice/spoken): code, a table, a
// link, a path, a hash; omitted marks a long answer's cut middle.
type VoiceSpoken struct {
	Lang string
	Kind string // code | table | link | image | path | line | hash | snippet | omitted
	N    int    // code: its lines; table: its rows; line: the line number
	Name string // path, line: the file's name
}

func RenderVoiceSpoken(d VoiceSpoken) string { return render("voice/spoken", d) }

// VoiceToolResult is what one of the voice model's function tools answers it (voice/tool_result): what it found, with
// what other people wrote in it (an agent's last words, a chat line in the knowledge base) inside the <agent_output>
// framing measured to keep the model from doing what it says (docs/lessons/voice.md row 5).
type VoiceToolResult struct {
	Lang string
	Tool string // search_knowledge | list_agents | send_to_agent (another name: unknown_tool)
	// Code: "" it worked; bad_args (the arguments are not what the tool takes); unknown_tool; failed (Reason: the hub's
	// words, when it said).
	Code, Reason string
	Hits         []VoiceHit   // search_knowledge
	Agents       []VoiceAgent // list_agents
}

// VoiceHit is a line the knowledge base search found.
type VoiceHit struct{ Path, Line string }

// VoiceAgent is one agent as list_agents tells it: ID machine/session (what send_to_agent takes), State its
// proto.Session state (needs_you, working, unread, idle), Last its last words.
type VoiceAgent struct{ ID, Title, State, Last string }

func RenderVoiceToolResult(d VoiceToolResult) string { return render("voice/tool_result", d) }

// agentOutputTag is anything a reader could take for the start or end of an <agent_output> block.
var agentOutputTag = regexp.MustCompile(`(?i)<\s*/?\s*agent_output\b[^>]*>`)

// AgentOutput is an agent's words to go inside an <agent_output> block, with nothing left in them that opens or closes
// one, so none of them can stand outside it (docs/voice.md「给快层的上下文」rule 5). Like ContextBody, a tag the removal
// puts together has its `<` written as `&lt;`. Templates call it as `output`.
func AgentOutput(s string) string {
	s = agentOutputTag.ReplaceAllString(s, "")
	return agentOutputTag.ReplaceAllStringFunc(s, func(tag string) string { return "&lt;" + tag[1:] })
}

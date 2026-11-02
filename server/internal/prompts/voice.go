package prompts

// VoiceLine is one final line of a voice conversation.
type VoiceLine struct {
	Role string // user | assistant
	Text string
}

// VoiceInstructions is what the built-in voice plugin's realtime model is told about itself (voice/instructions).
type VoiceInstructions struct {
	Owner string // "" is 用户
	Notes string
	// Tools: the session declares the plugin's function tools (mcp/voice); otherwise the model delegates.
	Tools bool
	// History: what was said before the connection dropped (a reconnected session), oldest first.
	History []VoiceLine
}

func RenderVoiceInstructions(d VoiceInstructions) string { return render("voice/instructions", d) }

// VoiceReport is what the voice model is told when a task handed to the main conversation is back (voice/report).
type VoiceReport struct {
	Task   string // clipped
	Status string // done | needs_you | failed | timeout
	Text   string // done: the main conversation's reply; failed: what went wrong
}

func RenderVoiceReport(d VoiceReport) string { return render("voice/report", d) }

// VoiceDelegated is the message a voice conversation sends the main conversation (voice/delegated).
type VoiceDelegated struct {
	Task    string
	History []VoiceLine // the last lines of the voice conversation (a delegation's own text can be terse)
}

func RenderVoiceDelegated(d VoiceDelegated) string { return render("voice/delegated", d) }

// RenderVoiceHandoff is the answer to the model's handing work over: it went to the main conversation (voice/handoff).
func RenderVoiceHandoff() string { return render("voice/handoff", nil) }

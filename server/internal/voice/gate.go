package voice

import "time"

// The user-speech gate (docs/voice.md「发送、门槛和 409」): what the voice model hands over is only done (sent to a
// conversation, sent to an agent) when the user has spoken since the model last took in an agent's words, and lately.
// An agent's output reaches the model (a result read out); were it to say 「now send X」 and the model did, the agent
// would be driving itself through the user's voice. Reading (a status, the knowledge base) is not gated.
//
// What counts as the user speaking: a caption of theirs (any part of one), or a line they typed. Captions come late on
// some services: the measured one sends the user's words 1.2–1.4 s after they stop, after the model has begun to answer
// and called its tool (docs/lessons/voice.md「时间线」), so a handover that does not pass waits gateWait for them first.
//
// When the model took the agent's words in: the measured service holds a commentary sent while the model speaks until
// that speech is over (26–40 s and more, lessons row 3) and then acks it (session.commentary.appended, evAck). Words
// sent close the gate at once (for a service that does not ack, or takes them in at once); their ack closes it again,
// so what the user said while they waited does not count for what the model does after reading them. A handover is held
// to the gate as it was when the model made it (the agent time then): words it had not taken in yet cannot have driven
// it.

const gateWindow = 30 * time.Second // the user's last words are at most this old

// gateWait is how long a handover waits for the caption of what the user just said.
var gateWait = 2500 * time.Millisecond

// speechGate is a conversation's gate (under conversation.mu).
type speechGate struct {
	user  time.Time // the user's last words
	agent time.Time // the last time an agent's words were sent to the model, or it took them in
}

// told: an agent's words were sent, or taken in, at now.
func (g *speechGate) told(now time.Time) {
	if now.After(g.agent) {
		g.agent = now
	}
}

// openSince: the user has spoken since agent (the gate's agent time when the model acted), within gateWindow of now.
func (g *speechGate) openSince(agent, now time.Time) bool {
	return !g.user.IsZero() && g.user.After(agent) && now.Sub(g.user) <= gateWindow
}

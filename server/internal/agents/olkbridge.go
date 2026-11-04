package agents

// OLKBridge is the personal agents source lark-connector's agent-bridge had in the hub; a plugin serves it now, under the
// same name so the sessions keep their ids.
const OLKBridge = "olk-bridge"

// MigratableSources are source names a plugin may use though the hub once had them itself, keeping their session
// ids (<source>:<key>): the hub reserves none of them.
var MigratableSources = []string{OLKBridge}

// Wrap is a session of source as the apps see it: its id <source>:<sessionKey>, and its avatar when there is one.
func Wrap(source string, s AgentSession, avatarURL string) Session {
	return withAvatar(Session{ID: source + ":" + s.SessionKey, Source: source, AgentSession: s}, avatarURL)
}

package connector

import "context"

// Field is one thing a sign-in step asks for.
type Field struct {
	Name  string `json:"name"`
	Label string `json:"label"`
	// Secret: a password; the app hides what is typed.
	Secret      bool   `json:"secret,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
}

// Prompt is where a sign-in stands: the step to fill in next, or Done.
type Prompt struct {
	Step   string  `json:"step,omitempty"`
	Fields []Field `json:"fields,omitempty"`
	// Hint is for people: where the code went (「验证码发到了你别的 Telegram 设备上」), the password's hint.
	Hint string `json:"hint,omitempty"`
	// Error is what was wrong with the values just given (「验证码不对」), for people: the same step is asked again.
	Error string `json:"error,omitempty"`
	Done  bool   `json:"done,omitempty"`
	// Account is who is signed in, once Done.
	Account string `json:"account,omitempty"`
}

// Login is a connector the user signs in to from the app, in steps (Telegram: phone number, code, 2FA password; a
// token is a single step). LoginStep with step "" starts over; with a step and its values it goes on. A mistake the
// user can fix comes back as Prompt.Error with the same step; an error is for what should not happen. What signing
// in yields (a session, a token) goes to the connector's Secrets: never into a Prompt, a Status or a log.
type Login interface {
	LoginStep(ctx context.Context, step string, values map[string]string) (Prompt, error)
	Logout(ctx context.Context) error
}

// Secrets keeps one connector's secret (a session, a token) for it, sealed in the hub's database
// (connector_secrets). Load gives nil when nothing is stored. The hub hands each connector its own: one cannot read
// another's.
type Secrets interface {
	Load(ctx context.Context) ([]byte, error)
	Save(ctx context.Context, data []byte) error
	Delete(ctx context.Context) error
}

// Capabler is a connector that says itself which capabilities it has (Capabilities' names): one that answers for a
// program it runs (connector/proc) implements every method and has only some of them for real.
type Capabler interface {
	Capabilities() []string
}

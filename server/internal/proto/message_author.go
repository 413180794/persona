package proto

// MessageAuthor is the external author's identity, independent of the role a model sees.
type MessageAuthor struct {
	ID       string        `json:"id"`
	Name     string        `json:"name"`
	Avatar   string        `json:"avatar,omitempty"` // opaque connector media reference
	Source   MessageSource `json:"source"`
	Owner    bool          `json:"owner"`
	OnBehalf bool          `json:"on_behalf,omitempty"` // confirmed sent as the owner by Persona
}

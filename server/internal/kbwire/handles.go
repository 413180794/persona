package kbwire

// Handle kinds.
const (
	HandleEmail    = "email"
	HandlePhone    = "phone"
	HandleGitHub   = "github"
	HandleTelegram = "telegram"
	HandleLark     = "lark"
)

// Handle is a raw hint as a connector gives it, in memory only: hashed before it is written.
type Handle struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

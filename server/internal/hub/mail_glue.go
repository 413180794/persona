package hub

import "github.com/chyroc/jarvis/server/internal/mail"

func init() {
	NewMail = func(dir string) (Mail, error) { return mail.New(dir) }
}

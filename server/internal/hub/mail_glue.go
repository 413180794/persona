package hub

import "github.com/chyroc/jarvis/server/internal/mail"

func init() {
	NewMail = func(dir string, store Store) (Mail, error) { return mail.New(dir, store) }
}

package hub

import "github.com/chyroc/jarvis/server/internal/mail"

func init() {
	NewMail = func(dir string, store Store, googleClientID string) (Mail, error) {
		s, err := mail.New(dir, store)
		if err != nil {
			return nil, err
		}
		s.SetGoogleClientID(googleClientID)
		return s, nil
	}
}

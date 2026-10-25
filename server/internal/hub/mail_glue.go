package hub

import "github.com/chyroc/jarvis/server/internal/mail"

func init() {
	NewMail = func(dir string, store Store, google GoogleConfig) (Mail, error) {
		s, err := mail.New(dir, store)
		if err != nil {
			return nil, err
		}
		s.SetGoogleClientID(google.ClientID)
		if err := s.SetGoogleProxy(google.Proxy); err != nil {
			s.Close()
			return nil, err
		}
		return s, nil
	}
}

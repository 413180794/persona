package hub

import "github.com/nwverse/persona/server/internal/mail"

func init() {
	NewMail = func(dir string, store Store, google GoogleConfig, microsoft MicrosoftConfig) (Mail, error) {
		s, err := mail.New(dir, store)
		if err != nil {
			return nil, err
		}
		s.SetGoogleClientID(google.ClientID)
		s.SetMicrosoft(microsoft.ClientID, microsoft.RedirectURI)
		if err := s.SetGoogleProxy(google.Proxy); err != nil {
			s.Close()
			return nil, err
		}
		return s, nil
	}
}

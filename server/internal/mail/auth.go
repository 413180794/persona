package mail

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-message/charset"
	"github.com/emersion/go-sasl"
)

const googleTokenURL = "https://oauth2.googleapis.com/token"

// mailWordDecoder decodes RFC 2047 encoded words in IMAP envelope fields with
// the full charset set registered by go-message/charset.
func mailWordDecoder() *mime.WordDecoder {
	return &mime.WordDecoder{CharsetReader: charset.Reader}
}

// oauthCache holds a Google access token and its expiry.
type oauthCache struct {
	mu      sync.Mutex
	token   string
	expires time.Time
}

// oauthToken is an error-aware cache for one account's access token.
type oauthToken struct {
	cache *oauthCache
}

// accessTokenFn returns the function used to fetch Google access tokens.
func (s *Service) accessTokenFn() func(context.Context, *OAuthCreds) (string, time.Time, error) {
	if s.accessToken != nil {
		return s.accessToken
	}
	return s.cachedAccessToken
}

// cachedAccessToken returns the cached token for creds, refreshing it when it
// is missing or expires within a minute.
func (s *Service) cachedAccessToken(ctx context.Context, creds *OAuthCreds) (string, time.Time, error) {
	key := creds.ClientID + "\x00" + creds.RefreshToken
	s.mu.Lock()
	cache := s.oauths[key]
	if cache == nil {
		cache = &oauthCache{}
		s.oauths[key] = cache
	}
	s.mu.Unlock()

	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.token != "" && s.now().Add(time.Minute).Before(cache.expires) {
		return cache.token, cache.expires, nil
	}
	token, expires, err := fetchGoogleToken(ctx, creds)
	if err != nil {
		return "", time.Time{}, err
	}
	cache.token, cache.expires = token, expires
	return token, expires, nil
}

// invalidateAccessToken forgets the cached token for creds.
func (s *Service) invalidateAccessToken(creds *OAuthCreds) {
	key := creds.ClientID + "\x00" + creds.RefreshToken
	s.mu.Lock()
	cache := s.oauths[key]
	s.mu.Unlock()
	if cache == nil {
		return
	}
	cache.mu.Lock()
	cache.token, cache.expires = "", time.Time{}
	cache.mu.Unlock()
}

// tokenResponse is the subset of Google's token endpoint reply we need.
type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
	Scope       string `json:"scope"`
	TokenType   string `json:"token_type"`
	Error       string `json:"error"`
	ErrorDesc   string `json:"error_description"`
}

var httpClient = &http.Client{Timeout: networkTimeout}

// fetchGoogleToken exchanges a refresh token for a short-lived access token.
func fetchGoogleToken(ctx context.Context, creds *OAuthCreds) (string, time.Time, error) {
	form := url.Values{
		"client_id":     {creds.ClientID},
		"client_secret": {creds.ClientSecret},
		"refresh_token": {creds.RefreshToken},
		"grant_type":    {"refresh_token"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, googleTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", time.Time{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(creds.ClientID, creds.ClientSecret)

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("oauth: token request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("oauth: read token response: %w", err)
	}
	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return "", time.Time{}, fmt.Errorf("oauth: parse token response (HTTP %d): %w", resp.StatusCode, err)
	}
	if resp.StatusCode != http.StatusOK || tr.AccessToken == "" {
		msg := tr.Error
		if tr.ErrorDesc != "" {
			msg += ": " + tr.ErrorDesc
		}
		if msg == "" {
			msg = strings.TrimSpace(string(body))
		}
		return "", time.Time{}, fmt.Errorf("oauth: token endpoint returned HTTP %d: %s", resp.StatusCode, msg)
	}
	lifetime := time.Duration(tr.ExpiresIn) * time.Second
	if lifetime <= 0 {
		lifetime = time.Hour
	}
	return tr.AccessToken, time.Now().Add(lifetime), nil
}

// xoauth2Client implements the SASL XOAUTH2 mechanism used by Gmail.
//
// The wire format is "user=<email>\x01auth=Bearer <token>\x01\x01".
type xoauth2Client struct {
	email string
	token string
	done  bool
}

func newXOAUTH2Client(email, token string) sasl.Client {
	return &xoauth2Client{email: email, token: token}
}

func (c *xoauth2Client) Start() (string, []byte, error) {
	return "XOAUTH2", []byte("user=" + c.email + "\x01auth=Bearer " + c.token + "\x01\x01"), nil
}

func (c *xoauth2Client) Next(challenge []byte) ([]byte, error) {
	if c.done {
		return nil, errors.New("mail: XOAUTH2 server sent too many challenges")
	}
	c.done = true
	// The server rejected the token; answer with an empty response to make it
	// send the final failure status, which surfaces as a normal IMAP error.
	return []byte{}, nil
}

// loginIMAP authenticates an IMAP client with the account's credentials.
func loginIMAP(c *imapclient.Client, a *MailAccount, accessToken func(context.Context, *OAuthCreds) (string, time.Time, error)) error {
	switch a.Auth {
	case AuthOAuth2:
		if a.OAuth == nil {
			return errors.New("mail: oauth2 auth without credentials")
		}
		token, _, err := accessToken(context.Background(), a.OAuth)
		if err != nil {
			return err
		}
		if err := c.Authenticate(newXOAUTH2Client(a.Username, token)); err != nil {
			return fmt.Errorf("imap: XOAUTH2: %w", err)
		}
		return nil
	case "", AuthPassword:
		if err := c.Login(a.Username, a.Password).Wait(); err != nil {
			return fmt.Errorf("imap: login: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("mail: unsupported auth %q", a.Auth)
	}
}

// smtpAuth returns the SASL client used to authenticate to SMTP.
func (s *Service) smtpAuth(ctx context.Context, a *MailAccount) (sasl.Client, error) {
	switch a.Auth {
	case AuthOAuth2:
		if a.OAuth == nil {
			return nil, errors.New("mail: oauth2 auth without credentials")
		}
		token, _, err := s.accessTokenFn()(ctx, a.OAuth)
		if err != nil {
			return nil, err
		}
		// SMTP XOAUTH2 uses the same SASL mechanism as IMAP.
		return newXOAUTH2Client(a.Username, token), nil
	case "", AuthPassword:
		return sasl.NewPlainClient("", a.Username, a.Password), nil
	default:
		return nil, fmt.Errorf("mail: unsupported auth %q", a.Auth)
	}
}

// encodeSASLResponse is only used in tests to inspect the XOAUTH2 payload.
func encodeSASLResponse(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}

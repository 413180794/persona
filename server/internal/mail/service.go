package mail

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	_ "github.com/emersion/go-message/charset" // register GBK etc. charset decoders
)

const (
	// networkTimeout bounds every single network operation (dial, command).
	networkTimeout = 20 * time.Second
	// idleTimeout closes a cached IMAP connection that has been unused this long.
	idleTimeout = 2 * time.Minute
	// sweepInterval is how often idle connections are checked.
	sweepInterval = 30 * time.Second
)

// Service is the mail module. One Service per hub process.
type Service struct {
	dir   string
	store *store

	mu    sync.Mutex
	accts map[string]*MailAccount // by ID, as loaded from disk
	conns map[string]*conn        // by account ID
	opMu  map[string]*sync.Mutex  // per-account IMAP serialization

	stop     chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup

	// now is swappable in tests.
	now func() time.Time
	// imapDial and smtpDial are swappable in tests; nil means the real network.
	imapDial func(a *MailAccount) (*imapclient.Client, error)
	// googleClientID is the iOS OAuth client used for "Sign in with Google" (hub.json google.client_id).
	googleClientID string
	usedStates     map[string]time.Time    // OAuth states already finished, against replays
	googleProxy    *url.URL                // hub.json google.proxy for Gmail traffic (proxy.go), nil = direct
	googleHTTP     *http.Client            // talks to Google's OAuth and Gmail API endpoints
	access         map[string]*accessCache // access tokens, by refresh token
	// verify replaces the IMAP + SMTP login check of new accounts in tests.
	verify func(ctx context.Context, a *MailAccount) error
	// refreshAccess replaces the Google token endpoint in tests.
	refreshAccess func(*OAuthCreds) (string, error)
	// sendMail delivers a rendered message over SMTP; nil uses the real network.
	sendMail func(ctx context.Context, a *MailAccount, from string, to []string, raw []byte) error
}

// conn is a cached, logged-in IMAP client.
type conn struct {
	c        *imapclient.Client
	lastUsed time.Time
}

// New creates a Service. Accounts live in recs; dataDir holds the encryption key (secret.key).
func New(dataDir string, recs Records) (*Service, error) {
	st, err := openStore(dataDir, recs)
	if err != nil {
		return nil, err
	}
	s := &Service{
		dir:        dataDir,
		store:      st,
		accts:      make(map[string]*MailAccount),
		conns:      make(map[string]*conn),
		usedStates: make(map[string]time.Time),
		googleHTTP: httpClient,
		access:     make(map[string]*accessCache),
		stop:       make(chan struct{}),
		now:        time.Now,
	}
	accounts, err := st.load()
	if err != nil {
		return nil, err
	}
	for _, a := range accounts {
		s.accts[a.ID] = a
	}
	s.wg.Add(1)
	go s.sweep()
	return s, nil
}

// SetGoogleClientID enables "Sign in with Google" for Gmail with an iOS OAuth client ID.
func (s *Service) SetGoogleClientID(id string) { s.googleClientID = strings.TrimSpace(id) }

// Close closes every cached connection and stops the idle sweeper.
func (s *Service) Close() error {
	s.stopOnce.Do(func() { close(s.stop) })
	s.wg.Wait()
	s.mu.Lock()
	conns := s.conns
	s.conns = make(map[string]*conn)
	s.mu.Unlock()
	for _, c := range conns {
		c.c.Close()
	}
	return nil
}

// sweep closes IMAP connections that have been idle for too long.
func (s *Service) sweep() {
	defer s.wg.Done()
	t := time.NewTicker(sweepInterval)
	defer t.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-t.C:
		}
		s.sweepOnce()
	}
}

// sweepOnce closes every idle connection.
func (s *Service) sweepOnce() {
	now := s.now()
	s.mu.Lock()
	var stale []*conn
	for id, c := range s.conns {
		if now.Sub(c.lastUsed) > idleTimeout {
			stale = append(stale, c)
			delete(s.conns, id)
		}
	}
	s.mu.Unlock()
	for _, c := range stale {
		c.c.Close()
	}
}

// ListAccounts returns every configured account without secrets.
func (s *Service) ListAccounts() ([]MailAccount, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]MailAccount, 0, len(s.accts))
	for _, a := range s.accts {
		out = append(out, *a.public())
	}
	sortAccounts(out)
	return out, nil
}

func sortAccounts(a []MailAccount) {
	// Stable by email so the app shows a predictable order.
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && strings.ToLower(a[j].Email) < strings.ToLower(a[j-1].Email); j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
}

// account returns a copy of the account with the given ID.
func (s *Service) account(id string) (*MailAccount, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.accts[id]
	if !ok {
		return nil, fmt.Errorf("unknown account %q", id)
	}
	cp := *a
	return &cp, nil
}

// public returns a copy without secrets.
func (a *MailAccount) public() *MailAccount {
	cp := *a
	cp.Password, cp.OAuth = "", nil
	return &cp
}

// AddAccount validates the account (by testing the connections) and saves it.
func (s *Service) AddAccount(req AddAccountRequest) (*MailAccount, error) {
	if req.Auth == AuthOAuth2 {
		return nil, errors.New("invalid auth \"oauth2\": use Sign in with Google")
	}
	a, err := buildAccount(req)
	if err != nil {
		return nil, err
	}
	if err := s.testAccount(context.Background(), a); err != nil {
		return nil, err
	}

	s.mu.Lock()
	for _, existing := range s.accts {
		if strings.EqualFold(existing.Email, a.Email) {
			s.mu.Unlock()
			return nil, fmt.Errorf("account %s already exists", a.Email)
		}
	}
	s.accts[a.ID] = a
	accounts := s.snapshotLocked()
	s.mu.Unlock()

	if err := s.store.save(accounts); err != nil {
		s.mu.Lock()
		delete(s.accts, a.ID)
		s.mu.Unlock()
		return nil, err
	}
	return a.public(), nil
}

// upsertAccount saves a verified account; an existing account with the same address keeps its ID
// and name but takes the new credentials (signing in with Google again repairs a revoked token).
func (s *Service) upsertAccount(a *MailAccount) (*MailAccount, error) {
	s.mu.Lock()
	var prev *MailAccount
	for _, existing := range s.accts {
		if strings.EqualFold(existing.Email, a.Email) {
			prev = existing
			break
		}
	}
	if prev != nil {
		a.ID, a.Name = prev.ID, prev.Name
	}
	s.accts[a.ID] = a
	c := s.conns[a.ID]
	delete(s.conns, a.ID)
	accounts := s.snapshotLocked()
	s.mu.Unlock()
	if c != nil {
		c.c.Close()
	}

	if err := s.store.save(accounts); err != nil {
		s.mu.Lock()
		if prev != nil {
			s.accts[a.ID] = prev
		} else {
			delete(s.accts, a.ID)
		}
		s.mu.Unlock()
		return nil, err
	}
	return a.public(), nil
}

// DeleteAccount removes an account and closes its connection.
func (s *Service) DeleteAccount(id string) error {
	s.mu.Lock()
	if _, ok := s.accts[id]; !ok {
		s.mu.Unlock()
		return fmt.Errorf("unknown account %q", id)
	}
	delete(s.accts, id)
	c := s.conns[id]
	delete(s.conns, id)
	accounts := s.snapshotLocked()
	s.mu.Unlock()

	if c != nil {
		c.c.Close()
	}
	return s.store.save(accounts)
}

// snapshotLocked copies the account map. The caller holds s.mu.
func (s *Service) snapshotLocked() []*MailAccount {
	out := make([]*MailAccount, 0, len(s.accts))
	for _, a := range s.accts {
		out = append(out, a)
	}
	return out
}

// TestAccount connects to IMAP and authenticates, and checks SMTP is reachable.
func (s *Service) TestAccount(id string) error {
	a, err := s.account(id)
	if err != nil {
		return err
	}
	return s.testAccount(context.Background(), a)
}

func (s *Service) testAccount(ctx context.Context, a *MailAccount) error {
	if s.verify != nil {
		return s.verify(ctx, a)
	}
	ctx, cancel := context.WithTimeout(ctx, networkTimeout*2)
	defer cancel()

	c, err := s.dialIMAP(ctx, a)
	if err != nil {
		return fmt.Errorf("imap: %w", err)
	}
	c.Close()

	if a.Auth == AuthOAuth2 {
		// Sent through the Gmail API, not SMTP (gmailAPIBase).
		return s.gmailAPICheck(ctx, a)
	}
	if err := s.checkSMTP(ctx, a); err != nil {
		return fmt.Errorf("smtp: %w", err)
	}
	return nil
}

// buildAccount normalizes a request, applying the domain preset for any field
// the request leaves empty.
func buildAccount(req AddAccountRequest) (*MailAccount, error) {
	email := normalizeEmail(req.Email)
	if email == "" || !strings.Contains(email, "@") {
		return nil, errors.New("email is required")
	}
	a := &MailAccount{
		ID:           newAccountID(),
		Name:         strings.TrimSpace(req.Name),
		Email:        email,
		Username:     strings.TrimSpace(req.Username),
		IMAPHost:     strings.TrimSpace(req.IMAPHost),
		IMAPPort:     req.IMAPPort,
		IMAPSecurity: strings.ToLower(strings.TrimSpace(req.IMAPSecurity)),
		SMTPHost:     strings.TrimSpace(req.SMTPHost),
		SMTPPort:     req.SMTPPort,
		SMTPSecurity: strings.ToLower(strings.TrimSpace(req.SMTPSecurity)),
		Password:     req.Password,
		Status:       "ok",
	}
	if a.Name == "" {
		a.Name = email
	}
	if a.Username == "" {
		a.Username = email
	}

	preset := presetFor(email)
	// Providers with fixed servers (Feishu) win over the email domain, which is the company's own.
	if p := providerPresets[req.Provider]; p != nil {
		preset = p
		a.IMAPHost, a.IMAPPort, a.SMTPHost, a.SMTPPort, a.SMTPSecurity = "", 0, "", 0, ""
	}
	if preset != nil {
		if a.Provider == "" {
			a.Provider = preset.Provider
		}
		if a.IMAPHost == "" {
			a.IMAPHost = preset.IMAPHost
		}
		if a.IMAPPort == 0 {
			a.IMAPPort = preset.IMAPPort
		}
		if a.SMTPHost == "" {
			a.SMTPHost = preset.SMTPHost
		}
		if a.SMTPPort == 0 {
			a.SMTPPort = preset.SMTPPort
		}
		if a.SMTPSecurity == "" {
			a.SMTPSecurity = preset.SMTPSecurity
		}
	}
	if a.Provider == "" {
		a.Provider = req.Provider
	}
	if a.Provider == "" {
		a.Provider = ProviderIMAP
	}
	if a.Provider != ProviderGmail && a.Provider != ProviderIMAP && a.Provider != ProviderFeishu {
		return nil, fmt.Errorf("invalid provider %q", a.Provider)
	}

	switch a.Auth = req.Auth; a.Auth {
	case "", AuthPassword:
		a.Auth = AuthPassword
		if a.Password == "" {
			return nil, errors.New("password is required")
		}
	case AuthOAuth2:
		if a.Provider != ProviderGmail {
			return nil, errors.New("invalid auth \"oauth2\": only Gmail signs in with Google")
		}
	default:
		return nil, fmt.Errorf("invalid auth %q", a.Auth)
	}

	if a.IMAPHost == "" || a.IMAPPort == 0 {
		return nil, fmt.Errorf("unknown mail provider for %s: provide imap_host and imap_port", email)
	}
	if a.SMTPHost == "" || a.SMTPPort == 0 {
		return nil, fmt.Errorf("unknown mail provider for %s: provide smtp_host and smtp_port", email)
	}
	if a.SMTPSecurity == "" {
		switch a.SMTPPort {
		case 465:
			a.SMTPSecurity = "tls"
		case 587, 25:
			a.SMTPSecurity = "starttls"
		default:
			a.SMTPSecurity = "tls"
		}
	}
	return a, nil
}

// dialIMAP opens and authenticates a fresh IMAP connection.
func (s *Service) dialIMAP(ctx context.Context, a *MailAccount) (*imapclient.Client, error) {
	if s.imapDial != nil {
		return s.imapDial(a)
	}
	token, err := s.oauthToken(ctx, a)
	if err != nil {
		return nil, err
	}
	type result struct {
		c   *imapclient.Client
		err error
	}
	ch := make(chan result, 1)
	go func() {
		c, err := dialAndLogin(a, token, s.proxyFor(a))
		ch <- result{c, err}
	}()
	select {
	case <-ctx.Done():
		go func() {
			if r := <-ch; r.c != nil {
				r.c.Close()
			}
		}()
		return nil, ctx.Err()
	case r := <-ch:
		return r.c, r.err
	}
}

func dialAndLogin(a *MailAccount, token string, proxy *url.URL) (*imapclient.Client, error) {
	addr := fmt.Sprintf("%s:%d", a.IMAPHost, a.IMAPPort)
	opts := &imapclient.Options{
		TLSConfig:   &tls.Config{ServerName: a.IMAPHost},
		WordDecoder: mailWordDecoder(),
	}
	ctx, cancel := context.WithTimeout(context.Background(), networkTimeout)
	defer cancel()
	var (
		c    *imapclient.Client
		conn net.Conn
		err  error
	)
	switch imapSecurity(a) {
	case "tls":
		if conn, err = dialTLS(ctx, proxy, addr, a.IMAPHost); err == nil {
			c = imapclient.New(conn, opts)
		}
	case "starttls":
		if conn, err = dialTCP(ctx, proxy, addr); err == nil {
			c, err = imapclient.NewStartTLS(conn, opts)
		}
	default:
		if conn, err = dialTCP(ctx, proxy, addr); err == nil {
			c = imapclient.New(conn, opts)
		}
	}
	if err != nil {
		return nil, err
	}
	// NetEase (163/126) rejects logins from clients that do not identify
	// themselves, so send ID before authenticating. Servers without the ID
	// extension answer BAD; that is harmless here.
	if c.Caps().Has(imap.CapID) {
		sendClientID(c)
	}
	if err := loginIMAP(c, a, token); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

// sendClientID announces the client to the server (RFC 2971), best effort.
func sendClientID(c *imapclient.Client) {
	data := &imap.IDData{Name: "Jarvis", Version: "1.0", Vendor: "chyroc"}
	cmd := c.ID(data)
	_, _ = cmd.Wait()
}

func imapSecurity(a *MailAccount) string {
	if a.IMAPSecurity != "" {
		return a.IMAPSecurity
	}
	switch a.IMAPPort {
	case 993:
		return "tls"
	case 143:
		return "starttls"
	default:
		return "none"
	}
}

// getConn returns a logged-in IMAP client for the account, dialing if needed.
func (s *Service) getConn(ctx context.Context, a *MailAccount) (*imapclient.Client, error) {
	s.mu.Lock()
	if c, ok := s.conns[a.ID]; ok && s.now().Sub(c.lastUsed) <= idleTimeout {
		c.lastUsed = s.now()
		s.mu.Unlock()
		return c.c, nil
	}
	stale := s.conns[a.ID]
	delete(s.conns, a.ID)
	s.mu.Unlock()
	if stale != nil {
		stale.c.Close()
	}

	type result struct {
		c   *imapclient.Client
		err error
	}
	ch := make(chan result, 1)
	go func() {
		c, err := s.dialIMAP(context.Background(), a)
		ch <- result{c, err}
	}()
	var c *imapclient.Client
	select {
	case <-ctx.Done():
		go func() {
			if r := <-ch; r.c != nil {
				r.c.Close()
			}
		}()
		return nil, ctx.Err()
	case r := <-ch:
		if r.err != nil {
			return nil, r.err
		}
		c = r.c
	}

	s.mu.Lock()
	// A concurrent caller may have installed a connection meanwhile; prefer it
	// and drop ours so we never leak.
	if existing, ok := s.conns[a.ID]; ok {
		s.mu.Unlock()
		c.Close()
		return existing.c, nil
	}
	s.conns[a.ID] = &conn{c: c, lastUsed: s.now()}
	s.mu.Unlock()
	return c, nil
}

// dropConn removes the cached connection for an account, closing it.
func (s *Service) dropConn(id string) {
	s.mu.Lock()
	c := s.conns[id]
	delete(s.conns, id)
	s.mu.Unlock()
	if c != nil {
		c.c.Close()
	}
}

// withConn runs fn on the cached connection; a transport-level error closes the
// connection and fn is retried once on a fresh one.
func (s *Service) withConn(ctx context.Context, a *MailAccount, fn func(*imapclient.Client) error) error {
	// IMAP is stateful (SELECT), so a cached connection can only serve one
	// command sequence at a time.
	mu := s.accountMutex(a.ID)
	mu.Lock()
	defer mu.Unlock()

	deadline := time.Now().Add(networkTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	run := func() error {
		c, err := s.getConn(ctx, a)
		if err != nil {
			return err
		}
		done := make(chan error, 1)
		go func() { done <- fn(c) }()
		select {
		case err := <-done:
			return err
		case <-time.After(time.Until(deadline)):
			s.dropConn(a.ID)
			return errors.New("imap: operation timed out")
		case <-ctx.Done():
			s.dropConn(a.ID)
			return ctx.Err()
		}
	}

	err := run()
	if err == nil || !isConnError(err) {
		return err
	}
	s.dropConn(a.ID)
	return run()
}

// isConnError reports whether err means the connection is unusable, so a
// reconnect is worth trying.
func isConnError(err error) bool {
	if err == nil {
		return false
	}
	var ie *imap.Error
	if errors.As(err, &ie) {
		// A tagged NO/BAD is a protocol answer, not a broken connection.
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	return true
}

var netDialer = net.Dialer{Timeout: networkTimeout}

// logf is a thin indirection so tests can silence output.
var logf = func(format string, args ...any) { log.Printf(format, args...) }

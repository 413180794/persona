package mail

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/emersion/go-smtp"
)

// Gmail traffic can go through an HTTP proxy (hub.json google.proxy): some networks reach only some of Google's
// IPv4 addresses, so imap.gmail.com and smtp.gmail.com time out on most resolutions. The proxy carries IMAP
// and SMTP with CONNECT tunnels, and the OAuth token endpoint and the Gmail API as ordinary HTTPS.

// SetGoogleProxy routes Gmail accounts and Google's OAuth / Gmail API through an http:// proxy ("" = direct).
func (s *Service) SetGoogleProxy(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		s.googleProxy, s.googleHTTP = nil, httpClient
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.Host == "" {
		return fmt.Errorf("mail: google.proxy must be http://[user:pass@]host:port, got %q", raw)
	}
	s.googleProxy = u
	s.googleHTTP = &http.Client{Timeout: networkTimeout, Transport: &http.Transport{Proxy: http.ProxyURL(u)}}
	return nil
}

// proxyFor returns the proxy for an account's servers, nil to dial directly.
func (s *Service) proxyFor(a *MailAccount) *url.URL {
	if a.Provider == ProviderGmail {
		return s.googleProxy
	}
	return nil
}

// dialTCP connects to addr directly or through an HTTP CONNECT tunnel.
func dialTCP(ctx context.Context, proxy *url.URL, addr string) (net.Conn, error) {
	if proxy == nil {
		return netDialer.DialContext(ctx, "tcp", addr)
	}
	c, err := netDialer.DialContext(ctx, "tcp", proxy.Host)
	if err != nil {
		return nil, fmt.Errorf("proxy %s: %w", proxy.Host, err)
	}
	if d, ok := ctx.Deadline(); ok {
		c.SetDeadline(d)
	}
	req := "CONNECT " + addr + " HTTP/1.1\r\nHost: " + addr + "\r\n"
	if u := proxy.User; u != nil {
		pw, _ := u.Password()
		req += "Proxy-Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(u.Username()+":"+pw)) + "\r\n"
	}
	if _, err := c.Write([]byte(req + "\r\n")); err != nil {
		c.Close()
		return nil, fmt.Errorf("proxy %s: %w", proxy.Host, err)
	}
	// Read the reply byte by byte up to the blank line, so nothing of the tunnelled stream is buffered away.
	br := bufio.NewReaderSize(c, 1)
	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodConnect})
	if err != nil {
		c.Close()
		return nil, fmt.Errorf("proxy %s: %w", proxy.Host, err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		c.Close()
		return nil, fmt.Errorf("proxy %s: CONNECT %s: %s", proxy.Host, addr, resp.Status)
	}
	c.SetDeadline(time.Time{})
	return c, nil
}

// dialTLS connects and completes a TLS handshake for serverName.
func dialTLS(ctx context.Context, proxy *url.URL, addr, serverName string) (net.Conn, error) {
	c, err := dialTCP(ctx, proxy, addr)
	if err != nil {
		return nil, err
	}
	tc := tls.Client(c, &tls.Config{ServerName: serverName})
	if err := tc.HandshakeContext(ctx); err != nil {
		c.Close()
		return nil, err
	}
	return tc, nil
}

// dialSMTP opens an SMTP client with the given security, optionally through the proxy.
func dialSMTP(ctx context.Context, proxy *url.URL, addr, security string, tlsConfig *tls.Config) (*smtp.Client, error) {
	switch security {
	case "tls":
		c, err := dialTLS(ctx, proxy, addr, tlsConfig.ServerName)
		if err != nil {
			return nil, err
		}
		return smtp.NewClient(c), nil
	case "starttls":
		c, err := dialTCP(ctx, proxy, addr)
		if err != nil {
			return nil, err
		}
		return smtp.NewClientStartTLS(c, tlsConfig)
	case "none":
		c, err := dialTCP(ctx, proxy, addr)
		if err != nil {
			return nil, err
		}
		return smtp.NewClient(c), nil
	default:
		return nil, fmt.Errorf("mail: unknown smtp security %q", security)
	}
}

// Package netproxy parses the http:// proxies hub.json gives for outbound traffic (google.proxy, a connector's
// proxy) and builds HTTP clients that use them. Proxy addresses are deployment-specific: they live only in a hub's
// own hub.json, never in code, docs or tests.
package netproxy

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Parse reads an http://[user:pass@]host:port proxy; "" (or blanks) is no proxy (nil). Errors never show a
// password.
func Parse(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("proxy must be http://[user:pass@]host:port: %v", redactErr(raw, err))
	}
	if u.Scheme != "http" || u.Host == "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" {
		return nil, fmt.Errorf("proxy must be http://[user:pass@]host:port, got %q", u.Redacted())
	}
	return u, nil
}

// HTTPClient is a client with timeout that sends everything through proxy, or directly when proxy is nil. Direct
// means direct: the process's HTTP(S)_PROXY environment is not consulted, so only hub.json decides.
func HTTPClient(proxy *url.URL, timeout time.Duration) *http.Client {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.Proxy = nil
	if proxy != nil {
		t.Proxy = http.ProxyURL(proxy)
	}
	return &http.Client{Timeout: timeout, Transport: t}
}

// redactErr keeps a password in raw out of a parse error's text.
func redactErr(raw string, err error) string {
	s := err.Error()
	if i := strings.Index(raw, "://"); i >= 0 {
		if j := strings.LastIndex(raw, "@"); j > i {
			s = strings.ReplaceAll(s, raw[i+3:j], "xxxxx")
		}
	}
	return s
}

package mail

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

// fakeConnectProxy is a minimal HTTP CONNECT proxy that counts tunnels and checks basic auth.
func fakeConnectProxy(t *testing.T, wantAuth string) (addr string, tunnels *int32) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	tunnels = new(int32)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				req, err := http.ReadRequest(bufio.NewReader(c))
				if err != nil || req.Method != http.MethodConnect {
					return
				}
				if wantAuth != "" && req.Header.Get("Proxy-Authorization") != wantAuth {
					io.WriteString(c, "HTTP/1.1 407 Proxy Authentication Required\r\n\r\n")
					return
				}
				up, err := net.Dial("tcp", req.Host)
				if err != nil {
					io.WriteString(c, "HTTP/1.1 502 Bad Gateway\r\n\r\n")
					return
				}
				defer up.Close()
				atomic.AddInt32(tunnels, 1)
				io.WriteString(c, "HTTP/1.1 200 Connection established\r\n\r\n")
				go io.Copy(up, c)
				io.Copy(c, up)
			}(c)
		}
	}()
	return ln.Addr().String(), tunnels
}

func TestGmailThroughProxy(t *testing.T) {
	srv := newTestIMAP(t)
	srv.appendMessage(t, "INBOX", rawMessage("via proxy", "a@example.com", "hello"))
	s := newServiceWithAccount(t, srv)
	// "user:pass" in base64.
	addr, tunnels := fakeConnectProxy(t, "Basic dXNlcjpwYXNz")
	if err := s.SetGoogleProxy("http://user:pass@" + addr); err != nil {
		t.Fatal(err)
	}

	accts, _ := s.ListAccounts()
	id := accts[0].ID
	// Only Gmail accounts use the proxy.
	if _, err := s.ListMessages(context.Background(), ListOptions{AccountID: id}); err != nil {
		t.Fatal(err)
	}
	if n := atomic.LoadInt32(tunnels); n != 0 {
		t.Fatalf("non-Gmail account used the proxy %d times", n)
	}

	s.mu.Lock()
	s.accts[id].Provider = ProviderGmail
	s.mu.Unlock()
	s.dropConn(id)
	res, err := s.ListMessages(context.Background(), ListOptions{AccountID: id})
	if err != nil {
		t.Fatalf("list through proxy: %v", err)
	}
	if len(res.Messages) != 1 || res.Messages[0].Subject != "via proxy" {
		t.Fatalf("messages = %+v", res.Messages)
	}
	if n := atomic.LoadInt32(tunnels); n == 0 {
		t.Fatal("Gmail account did not use the proxy")
	}

	// Wrong credentials surface as a proxy error.
	_ = s.SetGoogleProxy("http://user:nope@" + addr)
	s.dropConn(id)
	if _, err := s.ListMessages(context.Background(), ListOptions{AccountID: id}); err == nil || !strings.Contains(err.Error(), "407") {
		t.Fatalf("bad proxy auth: %v", err)
	}
}

func TestSetGoogleProxyValidates(t *testing.T) {
	s, _ := newOAuthService(t)
	for _, bad := range []string{"socks5://127.0.0.1:1080", "127.0.0.1:8080", "http://"} {
		if err := s.SetGoogleProxy(bad); err == nil {
			t.Fatalf("%q accepted", bad)
		}
	}
	if err := s.SetGoogleProxy(""); err != nil || s.googleProxy != nil {
		t.Fatalf("empty proxy: %v", err)
	}
}

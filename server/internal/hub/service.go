package hub

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"golang.org/x/net/proxy"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/link"
)

// Computers of kind service (docs/plan-hub-center.md 4, docs/link.md「被动模式」): the computer's connector
// (jarvis-link in passive mode) listens on a TLS port of its own and the hub dials it — no ssh. The hub pins the
// connector's key (the SPKI SHA-256 pairing gave it), shows the credential it made up at pairing, and opens
// GET /api/v1/connector/stream, which the connector joins to the resident node's attach socket: from there it is
// the node protocol, as over ssh. GET /api/v1/connector/info, every half minute, says whether the connector is there
// and how far (the choice between a computer's paths uses it). Pairing goes the other way round from link: the
// computer makes a one-time code and a link (jarvis://connector?addr=…&fp=…&code=…), pasted into the admin page; the
// hub trades the code for a credential it generates (POST /api/v1/connector/pair) and keeps it sealed in Postgres.

const (
	kindService          = "service"
	connectorPairPath    = link.ConnectorPairPath
	connectorInfoPath    = link.ConnectorInfoPath
	connectorStreamPath  = link.ConnectorStreamPath
	connectorDialTimeout = 10 * time.Second
	// connectorRevoked: the connector was paired with another hub; the old credential is gone for good. (Switched
	// back to active mode or stopping is 1001: tried again as usual.)
	connectorRevoked = link.CloseRevoked
)

// serviceProbeEvery: how often each connector's info is read; connectorRefusedWait: how long a connector that refused
// the credential is left alone (it blocks an address after 10 failures a minute). Vars: tests shorten them.
var (
	serviceProbeEvery    = 30 * time.Second
	connectorRefusedWait = 5 * time.Minute
)

// serviceState is what the hub knows of a computer's connector (under Machine.mu).
type serviceState struct {
	probed  time.Time // last info read
	ok      bool      // it answered, for this machine
	rtt     int       // ms, its round trip
	err     string    // why not
	invalid bool      // the credential is gone (4002): pair again
	wait    time.Time // rate limited (429): not before this
	version string    // the connector's build
	pairGen int       // bumped when it is paired again
}

// connectorCred is a service machine's pin and credential.
type connectorCred struct{ fp, token, version string }

func (m *Machine) connectorCred(ctx context.Context) (*connectorCred, error) {
	cs := m.hub.centerStore()
	if cs == nil {
		return nil, errors.New("no database")
	}
	row, err := cs.ConnectorCredential(ctx, m.cfg.ID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, errors.New("这台电脑的连接器还没配对：在管理界面这台电脑上点「重新配对」，粘贴它的 jarvis://connector 链接")
	}
	tok, err := m.hub.openKey(row.Token)
	if err != nil {
		return nil, fmt.Errorf("读不出连接器的凭据：%w", err)
	}
	return &connectorCred{fp: row.FP, token: tok, version: row.Version}, nil
}

// connectorTransport is an HTTP/1.1 transport to a connector pinned to fp, dialing through proxyURL when set.
func connectorTransport(fp, proxyURL string) (*http.Transport, error) {
	var dial func(ctx context.Context, network, addr string) (net.Conn, error)
	base := &net.Dialer{Timeout: connectorDialTimeout, KeepAlive: 30 * time.Second}
	dial = base.DialContext
	if proxyURL != "" {
		u, err := url.Parse(proxyURL)
		if err != nil || u.Scheme != "socks5" && u.Scheme != "socks5h" || u.Host == "" {
			return nil, fmt.Errorf("proxy 要写成 socks5://主机:端口")
		}
		var auth *proxy.Auth
		if u.User != nil {
			pw, _ := u.User.Password()
			auth = &proxy.Auth{User: u.User.Username(), Password: pw}
		}
		d, err := proxy.SOCKS5("tcp", u.Host, auth, base)
		if err != nil {
			return nil, err
		}
		cd, ok := d.(proxy.ContextDialer)
		if !ok {
			return nil, errors.New("socks5 dialer without context")
		}
		dial = cd.DialContext
	}
	return &http.Transport{
		Proxy: nil,
		DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			raw, err := dial(ctx, network, addr)
			if err != nil {
				return nil, err
			}
			cfg := &tls.Config{MinVersion: tls.VersionTLS12, NextProtos: []string{"http/1.1"},
				InsecureSkipVerify: true, // the pin below is the check: the connector's certificate is its own
				VerifyConnection: func(cs tls.ConnectionState) error {
					if len(cs.PeerCertificates) == 0 {
						return errConnectorPin
					}
					sum := sha256.Sum256(cs.PeerCertificates[0].RawSubjectPublicKeyInfo)
					if base64.RawURLEncoding.EncodeToString(sum[:]) != fp {
						return errConnectorPin
					}
					return nil
				}}
			c := tls.Client(raw, cfg)
			if err := c.HandshakeContext(ctx); err != nil {
				raw.Close()
				return nil, err
			}
			return c, nil
		},
		ResponseHeaderTimeout: connectorDialTimeout,
		MaxIdleConns:          1,
		IdleConnTimeout:       time.Minute,
	}, nil
}

var errConnectorPin = errors.New("TLS 公钥指纹和配对时的不一样：那里不是这台电脑的连接器")

// connectorError reads a connector's error answer: {"error": code, "message": …}.
func connectorError(resp *http.Response) error {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
	var e struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(b, &e)
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return &connectorHTTPError{status: resp.StatusCode, retry: retryAfterOf(resp), msg: "连接器那边试错太多次，暂时拒绝：稍后再试"}
	case resp.StatusCode == http.StatusUnauthorized:
		// Not tried again for a while: the connector counts failures and would block this hub's address.
		return &connectorHTTPError{status: resp.StatusCode, retry: connectorRefusedWait, msg: "连接器不认这台 hub 的凭据：重新配对"}
	case e.Error == "node_unavailable":
		msg := "这台电脑的常驻 node 没在跑"
		if e.Message != "" {
			msg += "：" + clip(oneLine(e.Message), 200)
		}
		return &connectorHTTPError{status: resp.StatusCode, msg: msg}
	case e.Message != "":
		return &connectorHTTPError{status: resp.StatusCode, msg: clip(oneLine(e.Message), 200)}
	}
	return &connectorHTTPError{status: resp.StatusCode, msg: "连接器回了 " + resp.Status}
}

type connectorHTTPError struct {
	status int
	retry  time.Duration
	msg    string
}

func (e *connectorHTTPError) Error() string { return e.msg }

func retryAfterOf(resp *http.Response) time.Duration {
	if n, err := strconv.Atoi(strings.TrimSpace(resp.Header.Get("Retry-After"))); err == nil && n > 0 {
		return time.Duration(n) * time.Second
	}
	return time.Minute
}

// dialService opens a stream to the node through the computer's connector.
func (m *Machine) dialService(ctx context.Context) (*nodePipe, error) {
	m.mu.Lock()
	st := m.svc
	m.mu.Unlock()
	if st.invalid {
		return nil, errors.New(errConnectorInvalid)
	}
	if time.Now().Before(st.wait) {
		if st.err != "" {
			return nil, fmt.Errorf("%s（%s 后再试）", st.err, time.Until(st.wait).Round(time.Second))
		}
		return nil, fmt.Errorf("连接器暂时拒绝，%s 后再试", time.Until(st.wait).Round(time.Second))
	}
	if m.cfg.Address == "" {
		return nil, errors.New("hub.json 里这台没有 address（连接器的 主机:端口）")
	}
	cred, err := m.connectorCred(ctx)
	if err != nil {
		return nil, err
	}
	tr, err := connectorTransport(cred.fp, m.cfg.Proxy)
	if err != nil {
		return nil, err
	}
	dctx, cancel := context.WithTimeout(ctx, 2*connectorDialTimeout)
	conn, resp, err := websocket.Dial(dctx, "wss://"+m.cfg.Address+connectorStreamPath, &websocket.DialOptions{
		HTTPClient: &http.Client{Transport: tr, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		HTTPHeader: http.Header{"Authorization": {"Bearer " + cred.token}}, CompressionMode: websocket.CompressionContextTakeover})
	cancel()
	if err != nil {
		tr.CloseIdleConnections()
		if resp != nil && resp.StatusCode != http.StatusSwitchingProtocols {
			herr := connectorError(resp)
			var ce *connectorHTTPError
			if errors.As(herr, &ce) && ce.retry > 0 {
				m.mu.Lock()
				m.svc.wait, m.svc.err = time.Now().Add(ce.retry), herr.Error()
				m.mu.Unlock()
			}
			return nil, herr
		}
		return nil, err
	}
	conn.SetReadLimit(64 << 20)
	sctx, scancel := context.WithCancelCause(context.Background())
	nc := websocket.NetConn(sctx, conn, websocket.MessageBinary)
	every, timeout := m.hub.linkPingTimes()
	go func() { // a stream whose TCP connection died quietly
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-sctx.Done():
				return
			case <-t.C:
				pctx, pcancel := context.WithTimeout(sctx, timeout)
				err := conn.Ping(pctx)
				pcancel()
				if err != nil && sctx.Err() == nil {
					scancel(fmt.Errorf("connector: no pong within %s", timeout))
					_ = conn.CloseNow()
					return
				}
			}
		}
	}()
	r := &closeWatch{r: nc, onClose: func(err error) {
		if websocket.CloseStatus(err) == connectorRevoked {
			m.serviceInvalidated()
		}
	}}
	var once sync.Once
	return &nodePipe{r: r, w: nc,
		lastErr: func() string {
			if r.err != nil && websocket.CloseStatus(r.err) == connectorRevoked {
				return errConnectorInvalid
			}
			if c := context.Cause(sctx); c != nil && !errors.Is(c, context.Canceled) {
				return c.Error()
			}
			return ""
		},
		close: func() {
			once.Do(func() {
				scancel(nil)
				_ = nc.Close()
				tr.CloseIdleConnections()
			})
		}}, nil
}

const errConnectorInvalid = "连接器已经配对给了别的 hub，原来的凭据失效：在管理界面重新配对这台电脑"

// closeWatch passes reads through and reports the error that ended them (a close frame's code).
type closeWatch struct {
	r       io.Reader
	onClose func(error)
	once    sync.Once
	err     error
}

func (c *closeWatch) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if err != nil {
		c.once.Do(func() {
			c.err = err
			c.onClose(err)
		})
	}
	return n, err
}

// serviceInvalidated: the connector ended the stream with 4002: it was paired with another hub. The path is not tried
// again until it is paired again here.
func (m *Machine) serviceInvalidated() {
	m.mu.Lock()
	m.svc.invalid, m.svc.ok, m.svc.err = true, false, errConnectorInvalid
	m.mu.Unlock()
	m.hub.logf("[%s] connector: credential revoked (paired with another hub)", m.cfg.ID)
	m.hub.meshTouch()
	m.hub.broadcastMachines()
}

// serviceReachable: the connector answered its last info (or was not asked yet), and its round trip.
func (m *Machine) serviceReachable() (bool, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.svc
	if st.invalid || time.Now().Before(st.wait) || !st.probed.IsZero() && !st.ok {
		return false, 0
	}
	return true, st.rtt
}

func (m *Machine) serviceWhy() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.svc.err != "" {
		return m.svc.err
	}
	if time.Now().Before(m.svc.wait) {
		return "连接器暂时拒绝，稍后再试"
	}
	return ""
}

// probeService reads the connector's info: there, the computer pairing named, and how far.
func (m *Machine) probeService(ctx context.Context) {
	m.mu.Lock()
	st := m.svc
	m.mu.Unlock()
	if st.invalid || time.Now().Before(st.wait) || m.cfg.Address == "" {
		return
	}
	set := func(ok bool, rtt int, err string, version string) {
		m.mu.Lock()
		was := m.svc.ok
		m.svc.probed, m.svc.ok, m.svc.rtt, m.svc.err = time.Now(), ok, rtt, err
		if version != "" {
			m.svc.version = version
		}
		m.mu.Unlock()
		if was != ok {
			m.hub.meshTouch()
			m.wake()
			if ok {
				m.hub.logf("[%s] connector answers (%d ms)", m.cfg.ID, rtt)
			} else {
				m.hub.logf("[%s] connector: %s", m.cfg.ID, err)
			}
		}
	}
	cred, err := m.connectorCred(ctx)
	if err != nil {
		set(false, 0, err.Error(), "")
		return
	}
	tr, err := connectorTransport(cred.fp, m.cfg.Proxy)
	if err != nil {
		set(false, 0, err.Error(), "")
		return
	}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: 2 * connectorDialTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+m.cfg.Address+connectorInfoPath, nil)
	req.Header.Set("Authorization", "Bearer "+cred.token)
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		set(false, 0, "连不上连接器："+err.Error(), "")
		return
	}
	defer resp.Body.Close()
	rtt := max(1, int(time.Since(start).Milliseconds()))
	if resp.StatusCode != http.StatusOK {
		herr := connectorError(resp)
		var ce *connectorHTTPError
		if errors.As(herr, &ce) && ce.retry > 0 {
			m.mu.Lock()
			m.svc.wait = time.Now().Add(ce.retry)
			m.mu.Unlock()
		}
		set(false, 0, herr.Error(), "")
		return
	}
	var info struct {
		Machine string `json:"machine"`
		Version string `json:"version"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<10)).Decode(&info); err != nil {
		set(false, 0, "连接器的回答看不懂："+err.Error(), "")
		return
	}
	if info.Machine != "" && info.Machine != m.cfg.ID {
		set(false, 0, fmt.Sprintf("那里的连接器配对的是 %q，不是这台", clip(oneLine(info.Machine), 64)), info.Version)
		return
	}
	set(true, rtt, "", info.Version)
}

// probeServices reads every service machine's connector info now and then, whatever this hub's role (a member says
// in its record whether its connectors answer).
func (h *Hub) probeServices(ctx context.Context) {
	for {
		for _, m := range h.cfgMachines() {
			if m.cfg.Kind == kindService && m.dialNode == nil {
				pctx, cancel := context.WithTimeout(ctx, 2*connectorDialTimeout)
				m.probeService(pctx)
				cancel()
			}
		}
		if !sleepCtx(ctx, serviceProbeEvery) {
			return
		}
	}
}

func validHostPort(a string) bool {
	if strings.Contains(a, "/") {
		return false
	}
	host, port, err := net.SplitHostPort(a)
	if err != nil || host == "" {
		return false
	}
	n, err := strconv.Atoi(port)
	return err == nil && n > 0 && n < 65536
}

// pairConnector trades a connector's one-time code for a credential this hub makes up, pinned to the link's key: the
// address that worked (the one given, else the link's candidates in order), and the connector's build. The credential
// is kept sealed for machine id.
func (h *Hub) pairConnector(ctx context.Context, pairLink, id, address, proxyURL string) (string, string, error) {
	addrs, fp, code, err := link.ParseConnectorLink(pairLink)
	if err != nil {
		return "", "", err
	}
	cs := h.centerStore()
	if cs == nil {
		return "", "", errors.New("no database")
	}
	if address = strings.TrimSpace(address); address != "" {
		if !validHostPort(address) {
			return "", "", errors.New("地址写成 主机:端口（IPv6 带方括号）")
		}
		addrs = []string{address}
	}
	if len(addrs) == 0 {
		return "", "", errors.New("配对链接里没有地址：填上那台电脑的 主机:端口")
	}
	tr, err := connectorTransport(fp, proxyURL)
	if err != nil {
		return "", "", err
	}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: 2 * connectorDialTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	token := "jc_" + randomHex(32)
	body, _ := json.Marshal(map[string]string{"code": code, "token": token, "machine": id, "hub_id": h.hubID(), "hub_name": h.hubName()})
	var errs []string
	for _, a := range addrs {
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://"+a+connectorPairPath, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			if errors.Is(err, errConnectorPin) {
				return "", "", errConnectorPin
			}
			errs = append(errs, a+"："+err.Error())
			continue
		}
		if resp.StatusCode != http.StatusOK {
			herr := connectorError(resp)
			resp.Body.Close()
			if resp.StatusCode == http.StatusUnauthorized {
				return "", "", errors.New("配对码不对、用过了或过期了：在那台电脑上重新生成")
			}
			return "", "", herr
		}
		var out struct {
			Version string `json:"version"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 16<<10)).Decode(&out)
		resp.Body.Close()
		sealed, err := h.sealKey(token)
		if err != nil {
			return "", "", err
		}
		if err := cs.SaveConnectorCredential(ctx, db.ConnectorCredential{MachineID: id, FP: fp, Token: sealed,
			Version: clip(oneLine(out.Version), 80)}); err != nil {
			return "", "", err
		}
		if m := h.cfgMachine(id); m != nil {
			m.mu.Lock()
			m.svc = serviceState{version: out.Version, pairGen: m.svc.pairGen + 1}
			m.mu.Unlock()
			m.wake()
		}
		h.logf("[%s] connector paired at %s", id, a)
		return a, out.Version, nil
	}
	return "", "", errors.New("连不上那台电脑的连接器：" + strings.Join(errs, "；"))
}

// linkPingTimes are the heartbeats of the streams the hub keeps (link.PingEvery and PingTimeout, shorter in tests).
func (h *Hub) linkPingTimes() (time.Duration, time.Duration) {
	if h.linkPing[0] > 0 {
		return h.linkPing[0], h.linkPing[1]
	}
	return link.PingEvery, link.PingTimeout
}

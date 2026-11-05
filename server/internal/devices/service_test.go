package devices

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// slowConnector is a connector whose info answers status, once the test lets it (release); asked is closed when the
// request is in.
type slowConnector struct {
	srv     *httptest.Server
	fp      string
	asked   chan struct{}
	release chan struct{}
}

func newSlowConnector(t *testing.T, status int) *slowConnector {
	t.Helper()
	sc := &slowConnector{asked: make(chan struct{}), release: make(chan struct{})}
	sc.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(sc.asked)
		<-sc.release
		w.WriteHeader(status)
		if status == http.StatusOK {
			_, _ = io.WriteString(w, `{"machine":"example-service","version":"test-connector"}`)
		} else {
			_, _ = io.WriteString(w, `{"error":"unauthorized"}`)
		}
	}))
	t.Cleanup(sc.srv.Close)
	sum := sha256.Sum256(sc.srv.Certificate().RawSubjectPublicKeyInfo)
	sc.fp = base64.RawURLEncoding.EncodeToString(sum[:])
	return sc
}

// serviceMachine is a service computer reached at sc, paired (pairing gen 1) with token.
func serviceMachine(t *testing.T, sc *slowConnector, token string) *Machine {
	t.Helper()
	d := newTestDevices(t)
	m := newMachine(d, MachineConfig{ID: "example-service", Name: "Example service", Kind: kindService,
		Address: strings.TrimPrefix(sc.srv.URL, "https://")})
	d.addEntry(m)
	m.svc = serviceState{pairGen: 1, cred: &connectorCred{fp: sc.fp, token: token}}
	return m
}

// probeWhile reads m's connector info and does meanwhile while the connector holds the answer.
func probeWhile(t *testing.T, m *Machine, sc *slowConnector, meanwhile func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		m.probeService(ctx)
	}()
	<-sc.asked
	meanwhile()
	close(sc.release)
	<-done
}

// A probe that was out when the connector ended the stream with 4002 (paired with another hub) answers too late to
// count: the machine still says to pair it again (TestCenterServiceConnector lost 「重新配对」 that way under load).
func TestServiceProbeAfterRevokeKeepsPairAgain(t *testing.T) {
	sc := newSlowConnector(t, http.StatusOK)
	m := serviceMachine(t, sc, "jc_old")
	probeWhile(t, m, sc, m.serviceInvalidated)
	m.mu.Lock()
	st := m.svc
	m.mu.Unlock()
	if !st.invalid || st.ok || st.err != errConnectorInvalid {
		t.Fatalf("the late answer overwrote the revocation: invalid=%v ok=%v err=%q", st.invalid, st.ok, st.err)
	}
	if ok, _ := m.serviceReachable(); ok {
		t.Fatal("reachable with a revoked credential")
	}
}

// A probe with the old credential that the connector refuses after the computer was paired again here does not drop
// the new credential or hold the connector off for connectorRefusedWait.
func TestServiceProbeRefusedAfterRepairKeepsNewCredential(t *testing.T) {
	sc := newSlowConnector(t, http.StatusUnauthorized)
	m := serviceMachine(t, sc, "jc_old")
	fresh := &connectorCred{fp: sc.fp, token: "jc_new"}
	probeWhile(t, m, sc, func() {
		m.mu.Lock()
		m.svc = serviceState{pairGen: m.svc.pairGen + 1, cred: fresh} // what PairConnector does
		m.mu.Unlock()
	})
	m.mu.Lock()
	st := m.svc
	m.mu.Unlock()
	if st.cred != fresh || !st.wait.IsZero() || st.err != "" || !st.probed.IsZero() {
		t.Fatalf("the old credential's refusal hit the new pairing: cred=%v wait=%v err=%q probed=%v", st.cred, st.wait, st.err, st.probed)
	}
	if ok, _ := m.serviceReachable(); !ok {
		t.Fatal("the new pairing is held off")
	}
}

// The same refusal without a new pairing is the connector not knowing the credential: dropped, held off, said so.
func TestServiceProbeRefusedDropsCredential(t *testing.T) {
	sc := newSlowConnector(t, http.StatusUnauthorized)
	m := serviceMachine(t, sc, "jc_old")
	probeWhile(t, m, sc, func() {})
	m.mu.Lock()
	st := m.svc
	m.mu.Unlock()
	if st.cred != nil || time.Until(st.wait) < connectorRefusedWait/2 || st.ok || !strings.Contains(st.err, "不认") {
		t.Fatalf("refused: cred=%v wait=%v ok=%v err=%q", st.cred, st.wait, st.ok, st.err)
	}
}

package agents

import (
	"context"
	"net/http"
	"testing"
)

// setupBridge answers the bridge status (or err for every call) and nothing else.
type setupBridge struct {
	fakeBridge
	state *AgentBridgeState
	err   error
}

func (f *setupBridge) AgentBridgeStatus(context.Context) (*AgentBridgeState, error) {
	return f.state, f.err
}

func (f *setupBridge) AgentSessions(ctx context.Context) ([]AgentSession, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.fakeBridge.AgentSessions(ctx)
}

func TestBridgeSetup(t *testing.T) {
	ctx := context.Background()
	acct := []AgentAccount{{AccountID: "a1", Provider: "cue"}}
	cases := []struct {
		name  string
		api   *setupBridge
		setup string
	}{
		{"missing", &setupBridge{err: &apiError{Status: http.StatusNotFound}}, SetupMissing},
		{"disabled", &setupBridge{state: &AgentBridgeState{}}, SetupDisabled},
		{"no accounts", &setupBridge{state: &AgentBridgeState{Enabled: true}}, SetupNoAccounts},
		{"ready", &setupBridge{state: &AgentBridgeState{Enabled: true, Accounts: acct}}, ""},
	}
	for _, c := range cases {
		b := testBridge{API: c.api}
		st, err := b.Status(ctx)
		if err != nil || st.Setup != c.setup || st.Error != "" {
			t.Errorf("%s: status %+v, %v; want setup %q", c.name, st, err, c.setup)
		}
		if list, err := b.Sessions(ctx); err != nil || list == nil {
			t.Errorf("%s: sessions %v, %v; want an empty list", c.name, list, err)
		}
	}

	// Other failures stay errors: a down Lark gateway must not read as "nothing set up".
	b := testBridge{API: &setupBridge{err: &apiError{Status: http.StatusBadGateway}}}
	if st, err := b.Status(ctx); err == nil || st.Error == "" || st.Setup != "" {
		t.Errorf("502: status %+v, %v; want an error", st, err)
	}
	if _, err := b.Sessions(ctx); err == nil {
		t.Error("502: sessions want an error")
	}
}

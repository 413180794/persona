package hub

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/usagesource"
)

func gw(source, ref, provider, name string, sampled time.Time, wins ...proto.UsageWindow) proto.UsageAccount {
	a := proto.UsageAccount{Provider: provider, Name: name, SampledAt: sampled, Windows: wins, Sources: []string{source}}
	if ref != "" {
		a.Ref = "s1:" + ref
	}
	return a
}

func local(provider, name string, sampled time.Time, wins ...proto.UsageWindow) proto.UsageAccount {
	return proto.UsageAccount{Provider: provider, Name: name, SampledAt: sampled, Windows: wins, Local: true}
}

func TestMergeLocalAndGatewayAreOneAccountWhenTheResetsAgree(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	h5, d7 := now.Add(2*time.Hour), now.Add(3*24*time.Hour)
	in := []proto.UsageAccount{
		local("codex", "Codex Pro", now.Add(-time.Minute), uw("5h", 300, 31, h5), uw("7d", 10080, 41, d7)),
		gw("relay-history", "c1", "codex", "work seat", now.Add(-10*time.Minute), uw("5h", 300, 30, h5), uw("7d", 10080, 40, d7)),
		// another account of the gateway: other resets
		gw("relay-history", "c2", "codex", "spare seat", now.Add(-time.Hour), uw("5h", 300, 2, now.Add(4*time.Hour)), uw("7d", 10080, 3, now.Add(5*24*time.Hour))),
	}
	got := mergeUsageAccounts(in, now)
	if len(got) != 2 {
		t.Fatalf("want 2 accounts: %+v", got)
	}
	var merged, spare *proto.UsageAccount
	for i := range got {
		switch got[i].Name {
		case "work seat":
			merged = &got[i]
		case "spare seat":
			spare = &got[i]
		}
	}
	if merged == nil || spare == nil {
		t.Fatalf("names: %+v", got)
	}
	// the gateway's name wins, the newest numbers (the machine's) win, both sources are listed
	if !merged.Local || len(merged.Sources) != 1 || merged.Sources[0] != "relay-history" || merged.Windows[0].UsedPercent != 31 {
		t.Errorf("merged: %+v", merged)
	}
	if spare.Local || len(spare.Sources) != 1 {
		t.Errorf("spare: %+v", spare)
	}
}

func TestMergeKeepsTwoAccountsOfOneGatewayApartWhateverTheirResets(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	reset := now.Add(3 * 24 * time.Hour)
	in := []proto.UsageAccount{
		gw("relay-history", "a", "claude", "seat a", now, uw("7d", 10080, 10, reset)),
		gw("relay-history", "b", "claude", "seat b", now, uw("7d", 10080, 20, reset)),
	}
	if got := mergeUsageAccounts(in, now); len(got) != 2 {
		t.Fatalf("a gateway knows its accounts apart: %+v", got)
	}
	// but the same upstream account behind two gateways (refs of different sources) is one
	other := gw("Other", "z", "claude", "seat z", now, uw("7d", 10080, 11, reset))
	other.Ref = "s2:z"
	if got := mergeUsageAccounts([]proto.UsageAccount{in[0], other}, now); len(got) != 1 || len(got[0].Sources) != 2 {
		t.Fatalf("two gateways: %+v", got)
	}
}

func TestMergeScopedWindowsAreKeptApartAndNeverIdentifyAnAccount(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	reset := now.Add(3 * 24 * time.Hour)
	scoped := uw("7d", 10080, 100, reset)
	scoped.Model = "Fable"
	in := []proto.UsageAccount{
		gw("relay-history", "a", "claude", "seat a", now, uw("5h", 300, 5, now.Add(time.Hour)), uw("7d", 10080, 10, reset), scoped),
		// another account whose only window shares the scoped one's reset second: not the same account
		gw("relay-history", "b", "claude", "seat b", now, uw("7d", 10080, 50, reset.Add(time.Hour)), scoped),
	}
	got := mergeUsageAccounts(in, now)
	if len(got) != 2 {
		t.Fatalf("%+v", got)
	}
	for _, a := range got {
		if a.Name == "seat a" && (len(a.Windows) != 3 || a.Windows[2].Model != "Fable" || a.Windows[1].Model != "") {
			t.Errorf("windows: %+v", a.Windows)
		}
	}
	if id := usageAccountID(got[0]); !strings.HasPrefix(id, "claude-") {
		t.Errorf("id %q", id)
	}
}

func TestMergeGatewayStaleness(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	reset := now.Add(24 * time.Hour)
	idle := gw("relay-history", "a", "codex", "idle seat", now.Add(-2*time.Hour), uw("7d", 10080, 10, reset)) // older than a machine's 30 minutes, within a gateway's
	old := gw("relay-history", "b", "codex", "old seat", now.Add(-usageGatewayStaleAfter-time.Minute), uw("7d", 10080, 20, now.Add(48*time.Hour)))
	flagged := gw("relay-history", "c", "codex", "flagged seat", now, uw("7d", 10080, 30, now.Add(72*time.Hour)))
	flagged.Stale, flagged.Problem = true, "error"
	machine := local("codex", "Codex", now.Add(-2*time.Hour), uw("7d", 10080, 40, now.Add(96*time.Hour)))
	got := map[string]proto.UsageAccount{}
	for _, a := range mergeUsageAccounts([]proto.UsageAccount{idle, old, flagged, machine}, now) {
		got[a.Name] = a
	}
	if got["idle seat"].Stale || !got["old seat"].Stale || !got["flagged seat"].Stale || got["flagged seat"].Problem != "error" || !got["Codex"].Stale {
		t.Errorf("%+v", got)
	}
}

func TestGatewaysPollKeepLastAnswerWhenUnreachableAndMarkItStale(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	var fail atomic.Bool
	var calls atomic.Int32
	var g usageGateways
	g.fetch = func(_ context.Context, cfg usagesource.Config, at time.Time) (usagesource.Result, error) {
		calls.Add(1)
		if cfg.Key != "secret-key" || cfg.Type != "relay-history" {
			t.Errorf("config: %+v", cfg)
		}
		if fail.Load() {
			return usagesource.Result{}, &usagesource.Error{Code: usagesource.CodeUnreachable}
		}
		a := proto.UsageAccount{Provider: "codex", Name: "seat", SampledAt: at, Ref: "c1", Windows: []proto.UsageWindow{uw("7d", 10080, 9, at.Add(time.Hour))}}
		return usagesource.Result{Accounts: []proto.UsageAccount{a}, Seen: usagesource.Seen{Accounts: 1, Windows: 1, Providers: map[string]int{"codex": 1}}}, nil
	}
	g.init(UsageConfig{Sources: []UsageSourceConfig{{ID: "s1", Name: "relay-history", Type: "relay-history", URL: "https://gw.example.com", Key: "secret-key"}}}, func() {}, func(string, ...any) {})

	g.pollDue(context.Background(), now)
	got := g.accounts(now)
	if len(got) != 1 || got[0].Sources[0] != "relay-history" || got[0].Ref != "s1:c1" || got[0].Stale || got[0].Problem != "" {
		t.Fatalf("%+v", got)
	}
	// not due again within the interval
	g.pollDue(context.Background(), now.Add(time.Minute))
	if calls.Load() != 1 {
		t.Errorf("asked %d times", calls.Load())
	}
	// due after 5 minutes; the gateway is down: the last answer stays, stale and marked
	fail.Store(true)
	g.pollDue(context.Background(), now.Add(6*time.Minute))
	got = g.accounts(now.Add(6 * time.Minute))
	if calls.Load() != 2 || len(got) != 1 || !got[0].Stale || got[0].Problem != "unreachable" {
		t.Fatalf("%d %+v", calls.Load(), got)
	}
	if st := g.status("s1"); st.Error != usagesource.CodeUnreachable || st.OKAt == nil || st.Seen == nil || st.Seen.Accounts != 1 {
		t.Errorf("status %+v", st)
	}
	// and it comes back
	fail.Store(false)
	g.pollDue(context.Background(), now.Add(12*time.Minute))
	if got = g.accounts(now.Add(12 * time.Minute)); len(got) != 1 || got[0].Stale || got[0].Problem != "" {
		t.Fatalf("%+v", got)
	}
}

func TestGatewaysOffSwitchesAndEdits(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	var calls atomic.Int32
	var g usageGateways
	g.fetch = func(_ context.Context, cfg usagesource.Config, at time.Time) (usagesource.Result, error) {
		calls.Add(1)
		return usagesource.Result{Accounts: []proto.UsageAccount{{Provider: "codex", Name: cfg.URL, SampledAt: at, Windows: []proto.UsageWindow{uw("7d", 10080, 1, at.Add(time.Hour))}}}}, nil
	}
	off := false
	src := UsageSourceConfig{ID: "s1", Name: "G", Type: "generic", URL: "https://one.example.com"}
	g.init(UsageConfig{Enabled: &off, Sources: []UsageSourceConfig{src}}, func() {}, nil)
	g.pollDue(context.Background(), now)
	if calls.Load() != 0 || len(g.accounts(now)) != 0 {
		t.Fatal("the global off switch still asked")
	}
	g.setConfig(UsageConfig{Sources: []UsageSourceConfig{{ID: "s1", Name: "G", Type: "generic", URL: "https://one.example.com", Enabled: &off}}})
	g.pollDue(context.Background(), now)
	if calls.Load() != 0 {
		t.Fatal("a source that is off was asked")
	}
	g.setConfig(UsageConfig{Sources: []UsageSourceConfig{src}})
	g.pollDue(context.Background(), now)
	if calls.Load() != 1 || len(g.accounts(now)) != 1 {
		t.Fatalf("%d", calls.Load())
	}
	// renaming keeps the samples and shows the new name; pointing it elsewhere forgets them until it is read
	src.Name = "Renamed"
	g.setConfig(UsageConfig{Sources: []UsageSourceConfig{src}})
	if a := g.accounts(now); len(a) != 1 || a[0].Sources[0] != "Renamed" {
		t.Fatalf("%+v", a)
	}
	src.URL = "https://two.example.com"
	g.setConfig(UsageConfig{Sources: []UsageSourceConfig{src}})
	if len(g.accounts(now)) != 0 {
		t.Fatal("samples of the old address kept")
	}
	g.pollDue(context.Background(), now.Add(time.Second))
	if a := g.accounts(now); len(a) != 1 || a[0].Name != "https://two.example.com" {
		t.Fatalf("%+v", a)
	}
	g.setConfig(UsageConfig{})
	if len(g.accounts(now)) != 0 {
		t.Fatal("a removed source still shows")
	}
}

func TestGatewayIntervalConfig(t *testing.T) {
	if (UsageConfig{}).interval() != 5*time.Minute || (UsageConfig{PollMinutes: 1}).interval() != time.Minute || (UsageConfig{PollMinutes: 99999}).interval() != 24*time.Hour {
		t.Error("interval")
	}
}

// The whole chain through the route, with a fake gateway behind it: the machines say nothing, the gateway has an account.
func TestUsageWindowsRouteIncludesGatewayAccounts(t *testing.T) {
	now := time.Now()
	reset := now.Add(3 * time.Hour).UTC().Format(time.RFC3339)
	gate := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer k" {
			w.WriteHeader(401)
			return
		}
		switch r.URL.Path {
		case "/api/admin/accounts":
			fmt.Fprint(w, `{"accounts":[{"id":"c1","name":"seat one","provider":"codex","status":"active"}]}`)
		case "/api/admin/accounts/usage":
			fmt.Fprintf(w, `{"accounts":[{"account_id":"c1","plan_display_name":"Pro","checked_at":%q,"windows":[{"kind":"five_hour","used_ratio":0.4,"resets_at":%q,"checked_at":%q}]}]}`, now.UTC().Format(time.RFC3339), reset, now.UTC().Format(time.RFC3339))
		default:
			w.WriteHeader(404)
		}
	}))
	defer gate.Close()
	h := newTestHub(t)
	h.usageGW.init(UsageConfig{Sources: []UsageSourceConfig{{ID: "s1", Name: "relay-history", Type: "relay-history", URL: gate.URL, Key: "k"}}}, h.usageWin.invalidate, h.logf)
	h.usageGW.pollDue(context.Background(), now)
	mux := http.NewServeMux()
	h.usageWindowRoutes(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/usage/windows", nil))
	body := w.Body.String()
	for _, want := range []string{`"name":"seat one"`, `"plan":"Pro"`, `"sources":["relay-history"]`, `"used_percent":40`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %s in %s", want, body)
		}
	}
	if strings.Contains(body, `"local"`) || strings.Contains(body, "\"k\"") || strings.Contains(body, "c1") {
		t.Errorf("a gateway-only account is not local, and its key and ids stay out: %s", body)
	}
}

func TestOfflineNodeCannotClaimASource(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	var u usageWindowsHub
	src := usageSource{id: "m1", online: true, ask: func(context.Context) (proto.UsageSnapshot, error) {
		return proto.UsageSnapshot{Enabled: true, Accounts: []proto.UsageAccount{{Provider: "codex", Name: "Codex", SampledAt: now, Sources: []string{"relay-history"}, Problem: "unreachable", Windows: []proto.UsageWindow{uw("7d", 10080, 1, now.Add(time.Hour))}}}}, nil
	}}
	got := u.poll(context.Background(), []usageSource{src}, now)
	if len(got) != 1 || !got[0].Local || len(got[0].Sources) != 0 || got[0].Problem != "" {
		t.Fatalf("a node's words about where it came from are not taken: %+v", got)
	}
}

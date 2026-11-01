package quota

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// The shapes relay-history's /me/usage and FAKEMODEL's /api/quota answered with (2026-10-31), values made up.
const asterSample = `{"service":"relay-history","protocol":"relay-history-self-usage","api_key":"cc_demo",
 "today":{"requests":287,"billed_tokens":210552323,"cost_usd":123.12},"week":{"requests":10432,"billed_tokens":3359748900,"cost_usd":1109.02},
 "month":{"requests":20000,"billed_tokens":6000000000,"cost_usd":2100.5},
 "budget":{"limit_usd":3000,"spent_usd":2100.5,"exhausted":false,"weekly":{"limit_usd":800,"spent_usd":800,"exhausted":true,"resets_at":"2026-11-02T00:00:00+08:00"}},
 "account_quota_shared":true,
 "accounts":[{"account_id":"aaaa-111111","provider":"kiro","plan":"unknown","state":"available","available_at":null,
   "windows":[{"kind":"monthly","scope":"account","used_ratio":0.64,"remaining":3600,"limit":10000,"resets_at":"2026-11-01T00:00:00Z"},
              {"kind":"five_hour","scope":"model","scoped_model":"x","used_ratio":0.1}]},
  {"account_id":"bbbb-222222","provider":"claude","plan":"max","state":"cooldown","available_at":"2026-10-31T15:00:00Z",
   "windows":[{"kind":"weekly","scope":"account","used_ratio":0.3},{"kind":"five_hour","scope":"account","used_ratio":1}]}]}`

const fmSample = `{"data":[
 {"provider_name":"火山 · Agent Plan","provider_type":"volcano-ark","status":"ok","limits":[
   {"key":"week","label":"每周额度","unit":"percent","limit":100,"used":100,"resets_at":"2026-11-02T00:00:00+08:00","source":"upstream"},
   {"key":"5h","label":"5 小时额度","unit":"percent","limit":100,"used":42,"resets_at":"2026-10-31T10:00:00Z","source":"upstream"},
   {"key":"rpm","label":"RPM","unit":"requests","limit":null,"used":5,"source":"gateway"}],
  "models":[{"model_id":"deepseek","limits":[{"key":"tpm","label":"TPM","unit":"tokens","limit":500000,"used":8400,"source":"mixed"}]},
            {"model_id":"other","limits":[{"key":"rpm","label":"RPM","unit":"requests","limit":null,"used":1,"source":"gateway"}]}]},
 {"provider_name":"modelhub","provider_type":"provider-ark","status":"unsupported","limits":[{"key":"5h","label":"近 5 小时","unit":"tokens","limit":null,"used":320000,"source":"gateway"}],"models":[]}]}`

// userZone: the report gives times in the user's zone, whatever the machine running the test is in.
var userZone = func() *time.Location {
	l, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		panic(err)
	}
	return l
}()

func Testrelay-historyReport(t *testing.T) {
	var a asterReport
	if err := json.Unmarshal([]byte(asterSample), &a); err != nil {
		t.Fatal(err)
	}
	r := relay-historyReport(a, time.Now(), userZone)
	if r.Gateway != "relay-history" || r.KeyName != "cc_demo" || len(r.Usage) != 3 || r.Usage[0].Label != "今日" || r.Usage[0].Tokens != 210552323 {
		t.Fatalf("usage: %+v", r)
	}
	if len(r.Budget) != 2 || !r.Budget[1].Exhausted || *r.Budget[0].Limit != 3000 {
		t.Fatalf("budget: %+v", r.Budget)
	}
	if len(r.Pools) != 2 {
		t.Fatalf("pools: %+v", r.Pools)
	}
	kiro := r.Pools[0]
	// The plan "unknown" is not shown; the per-model window is left out; the account's credits are used / limit.
	if kiro.Name != "kiro" || len(kiro.Windows) != 1 || kiro.Windows[0].Label != "每月" || *kiro.Windows[0].Used != 6400 || kiro.Windows[0].Unit != "credits" {
		t.Fatalf("kiro: %+v %+v", kiro, kiro.Windows)
	}
	claude := r.Pools[1]
	if claude.Name != "claude · max" || claude.State != "cooldown" || !strings.Contains(claude.Detail, "冷却中") || !strings.Contains(claude.Detail, "23:00") {
		t.Fatalf("claude: %+v", claude)
	}
	// 5 小时 before 每周, and a full window is exhausted.
	if claude.Windows[0].Label != "5 小时" || !claude.Windows[0].Exhausted || claude.Windows[1].Label != "每周" {
		t.Fatalf("order: %+v", claude.Windows)
	}

	hidden := asterReport{Service: "relay-history", AccountQuotaShared: new(bool)}
	if r := relay-historyReport(hidden, time.Now(), userZone); r.Note == "" || len(r.Pools) != 0 {
		t.Fatalf("not shared: %+v", r)
	}
}

func TestFakemodelReport(t *testing.T) {
	var q fmQuota
	if err := json.Unmarshal([]byte(fmSample), &q); err != nil {
		t.Fatal(err)
	}
	r := FakemodelReport(q, time.Now())
	if len(r.Pools) != 2 {
		t.Fatalf("%+v", r.Pools)
	}
	ark := r.Pools[0]
	// Only windows with a limit; 5 小时 first; the full week is exhausted.
	if len(ark.Windows) != 2 || ark.Windows[0].Label != "5 小时额度" || *ark.Windows[0].UsedRatio != 0.42 || !ark.Windows[1].Exhausted {
		t.Fatalf("ark windows: %+v", ark.Windows)
	}
	if len(ark.Models) != 1 || ark.Models[0].Model != "deepseek" {
		t.Fatalf("ark models: %+v", ark.Models)
	}
	if mh := r.Pools[1]; len(mh.Windows) != 0 || mh.Detail == "" {
		t.Fatalf("unsupported provider: %+v", mh)
	}
}

func TestFetchTalksToBoth(t *testing.T) {
	var loggedIn bool
	var asked string // the timezone the usage request carried
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/me/usage":
			if r.Header.Get("Authorization") != "Bearer sk-aster" {
				w.WriteHeader(401)
				return
			}
			asked = r.URL.Query().Get("timezone")
			_, _ = w.Write([]byte(asterSample))
		case "/auth/key-login":
			var in struct{ Key string }
			_ = json.NewDecoder(r.Body).Decode(&in)
			if in.Key != "fm_key" {
				w.WriteHeader(401)
				_, _ = w.Write([]byte(`{"error":{"message":"invalid_api_key"}}`))
				return
			}
			loggedIn = true
			http.SetCookie(w, &http.Cookie{Name: "fm", Value: "s", Path: "/"})
			_, _ = w.Write([]byte(`{"workspace_id":"ws"}`))
		case "/api/quota":
			if c, err := r.Cookie("fm"); err != nil || c.Value != "s" {
				w.WriteHeader(401)
				return
			}
			_, _ = w.Write([]byte(fmSample))
		case "/auth/logout":
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	ctx := context.Background()
	// The gateway counts its daily windows in the user's zone, and is not told one it does not know.
	if r, err := Fetch(ctx, nil, srv.URL+"/v1", "sk-aster", "America/Los_Angeles"); err != nil || r.Gateway != "relay-history" || asked != "America/Los_Angeles" {
		t.Fatalf("relay-history: %+v %v, timezone %q", r, err, asked)
	}
	if _, err := Fetch(ctx, nil, srv.URL+"/v1", "sk-aster", ""); err != nil || asked != "" {
		t.Fatalf("relay-history without a zone: %v, timezone %q", err, asked)
	}
	if r, err := Fetch(ctx, nil, srv.URL, "fm_key", ""); err != nil || r.Gateway != "fakemodel" || !loggedIn {
		t.Fatalf("fakemodel: %+v %v", r, err)
	}
	if _, err := Fetch(ctx, nil, srv.URL, "fm_wrong", ""); err == nil || !strings.Contains(err.Error(), "invalid_api_key") {
		t.Fatalf("bad fakemodel key: %v", err)
	}
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404) }))
	defer other.Close()
	if _, err := Fetch(ctx, nil, other.URL, "sk-x", ""); err != ErrUnsupported {
		t.Fatalf("other gateway: %v", err)
	}
}

// JARVIS_LIVE_QUOTA=<env file with ANTHROPIC_BASE_URL and the key>[,<another>] go test -run QuotaLive -v ./internal/quota
// (the env files a launcher probe writes; keys are read from them, never printed).
func TestQuotaLive(t *testing.T) {
	files := os.Getenv("JARVIS_LIVE_QUOTA")
	if files == "" {
		t.Skip("JARVIS_LIVE_QUOTA not set")
	}
	for _, f := range strings.Split(files, ",") {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		env := map[string]string{}
		for _, l := range strings.Split(string(raw), "\n") {
			if k, v, ok := strings.Cut(l, "="); ok {
				env[k] = v
			}
		}
		key := env["ANTHROPIC_AUTH_TOKEN"]
		if key == "" {
			key = env["ANTHROPIC_API_KEY"]
		}
		r, err := Fetch(context.Background(), nil, env["ANTHROPIC_BASE_URL"], key, "")
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		b, _ := json.Marshal(r)
		if strings.Contains(string(b), key) {
			t.Fatalf("%s: the key is in the report", f)
		}
		t.Logf("%s: %s pools=%d usage=%d %s", f, r.Gateway, len(r.Pools), len(r.Usage), string(b)[:min(len(b), 600)])
	}
}

func TestSameNamedAccountsGetTheirIDs(t *testing.T) {
	var a asterReport
	_ = json.Unmarshal([]byte(`{"service":"relay-history","accounts":[{"account_id":"x-2328da","provider":"kiro"},{"account_id":"y-d975aa","provider":"kiro"},{"account_id":"z-1","provider":"claude"}]}`), &a)
	r := relay-historyReport(a, time.Now(), userZone)
	if r.Pools[0].Name != "kiro #2328da" || r.Pools[1].Name != "kiro #d975aa" || r.Pools[2].Name != "claude" {
		t.Fatalf("%+v", r.Pools)
	}
}

func TestWindowsDedupedAndNamed(t *testing.T) {
	var a asterReport
	_ = json.Unmarshal([]byte(`{"service":"relay-history","accounts":[{"account_id":"x","provider":"claude","plan":"max","windows":[
	  {"kind":"five_hour","scope":"account"},{"kind":"five_hour","scope":"account","used_ratio":0.2},
	  {"kind":"weekly","scope":"account","used_ratio":0.77},{"kind":"weekly","scope":"account","used_ratio":0.77},
	  {"kind":"traex_weekly","scope":"account","used_ratio":0.01},{"kind":"monthly","scope":"account"}]}]}`), &a)
	ws := relay-historyReport(a, time.Now(), userZone).Pools[0].Windows
	if len(ws) != 2 || ws[0].Label != "5 小时" || *ws[0].UsedRatio != 0.2 || ws[1].Label != "每周" {
		t.Fatalf("%+v", ws)
	}
}

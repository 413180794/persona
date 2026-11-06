package usagesource

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// kiroAccounts is a gateway with Kiro credit accounts, a disabled one with only its compact summary, a quarantined
// one whose last error carries free text, and a relay link with no windows of its own.
const kiroAccounts = `{"accounts":[
 {"id":"k1","name":"kiro one","provider":"kiro","status":"active","schedulable":true,"priority":"10"},
 {"id":"k2","name":"","provider":"Kiro","status":"quarantined","schedulable":true,"priority":"10",
  "last_error":"quarantine: upstream said user someone@example.com at relay-host-1 is banned"},
 {"id":"k3","name":"kiro off","provider":"kiro","status":"disabled","schedulable":false,"priority":"10",
  "quota_summary":[{"kind":"monthly","used_ratio":0.5,"resets_at":"2026-12-01T00:00:00Z"},
                   {"kind":"weekly_gemini","scoped_model":"gemini-pro","used_ratio":1,"resets_at":"2026-11-09T00:00:00Z"}]},
 {"id":"k4","name":"kiro cool","provider":"kiro","status":"cooldown","schedulable":true,"priority":"10",
  "cooldown_until":"2026-11-05T09:12:00Z","last_error":"quota_exhausted:monthly credits gone"},
 {"id":"r1","name":"relay link","provider":"claude","status":"disabled","schedulable":false,"priority":"20","last_error":"Not A Token"}]}`

const kiroUsage = `{"accounts":[
 {"account_id":"k1","plan":"free","plan_raw":"KIRO FREE","plan_display_name":"Kiro Free","checked_at":"2026-11-05T08:40:00Z","cached":true,"windows":[
   {"kind":"monthly","scope":"account","limit":50,"remaining":41,"used_ratio":0.18,"resets_at":"2026-12-01T00:00:00Z","checked_at":"2026-11-05T08:40:00Z"},
   {"kind":"kiro_bonus","scope":"account","limit":500,"remaining":500,"used_ratio":0,"resets_at":"2026-11-20T00:00:00Z","checked_at":"2026-11-05T08:40:00Z"},
   {"kind":"kiro_free_trial","scope":"account","limit":100,"remaining":10,"used_ratio":0.9,"resets_at":"2026-11-10T00:00:00Z","checked_at":"2026-11-05T08:40:00Z"},
   {"kind":"kiro_overage","scope":"account","checked_at":"2026-11-05T08:40:00Z"}]},
 {"account_id":"k2","plan":"free","windows":[]},
 {"account_id":"k4","plan":"free","stale":true,"windows":[
   {"kind":"monthly","scope":"account","limit":50,"remaining":0,"used_ratio":1,"resets_at":"2026-12-01T00:00:00Z","checked_at":"2026-11-05T08:00:00Z"}]}]}`

func TestRelayInventoryKeepsEveryAccountAndKind(t *testing.T) {
	var asked []string
	g := newGateway(t, kiroAccounts, func(ids []string) (int, string) { asked = append(asked, ids...); return 200, kiroUsage })
	res, err := Fetch(context.Background(), g.cfg(), now)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(asked) != "[k1 k2 k4]" { // disabled accounts are still not in the usage call
		t.Errorf("asked %v", asked)
	}
	if !res.Complete || len(res.Inventory) != 5 {
		t.Fatalf("complete %v, inventory %+v", res.Complete, res.Inventory)
	}
	byID := map[string]int{}
	for i, a := range res.Inventory {
		byID[a.AccountID] = i
	}
	k1 := res.Inventory[byID["k1"]]
	if k1.Provider != "kiro" || k1.Plan != "free" || k1.PlanRaw != "KIRO FREE" || k1.PlanDisplayName != "Kiro Free" || k1.Status != "active" ||
		!k1.Schedulable || !k1.Cached || k1.Stale || k1.CheckedAt == nil {
		t.Errorf("k1: %+v", k1)
	}
	var kinds []string
	for _, w := range k1.Windows {
		kinds = append(kinds, w.Kind)
	}
	if fmt.Sprint(kinds) != "[monthly kiro_bonus kiro_free_trial kiro_overage]" { // every kind passes through
		t.Errorf("k1 kinds %v", kinds)
	}
	if w := k1.Windows[0]; w.Limit == nil || *w.Limit != 50 || w.Remaining == nil || *w.Remaining != 41 || w.Scope != "account" || w.ResetsAt == nil {
		t.Errorf("monthly %+v", w)
	}
	if w := k1.Windows[3]; w.Limit != nil || w.Remaining != nil || w.UsedRatio != nil { // uncapped overage has no numbers
		t.Errorf("overage %+v", w)
	}
	k2 := res.Inventory[byID["k2"]]
	if k2.Status != "quarantined" || k2.Reason != "quarantine" || k2.Provider != "kiro" || len(k2.Windows) != 0 {
		t.Errorf("k2: %+v", k2)
	}
	k3 := res.Inventory[byID["k3"]]
	if k3.Status != "disabled" || k3.Schedulable || !k3.Stale || len(k3.Windows) != 2 || k3.Plan != "" {
		t.Fatalf("k3 (disabled, from its summary): %+v", k3)
	}
	if w := k3.Windows[0]; w.Kind != "monthly" || w.Scope != "account" || w.UsedRatio == nil || *w.UsedRatio != 0.5 {
		t.Errorf("k3 monthly %+v", w)
	}
	if w := k3.Windows[1]; w.Scope != "model" || w.ScopedModel != "gemini-pro" {
		t.Errorf("k3 model window %+v", w)
	}
	k4 := res.Inventory[byID["k4"]]
	if k4.Reason != "quota_exhausted" || k4.CooldownUntil == nil || !k4.Stale || len(k4.Windows) != 1 {
		t.Errorf("k4: %+v", k4)
	}
	r1 := res.Inventory[byID["r1"]]
	if r1.Reason != "" || r1.Status != "disabled" || r1.Windows == nil { // not a token: nothing of it is passed on
		t.Errorf("r1: %+v", r1)
	}
	b, _ := json.Marshal(res.Inventory)
	for _, leak := range []string{"example.com", "relay-host-1", "banned", "credits gone", "Not A Token"} {
		if strings.Contains(string(b), leak) {
			t.Errorf("free text %q of a last error is in the inventory: %s", leak, b)
		}
	}
	// the samples are what they were: Kiro's credit windows other than monthly are not windows the strip draws
	if len(res.Accounts) != 2 || res.Accounts[0].Ref != "k1" || len(res.Accounts[0].Windows) != 1 || res.Accounts[0].Windows[0].Label != "30d" {
		t.Errorf("samples %+v", res.Accounts)
	}
	if res.Seen.Listed != 5 || res.Seen.Accounts != 2 {
		t.Errorf("seen %+v", res.Seen)
	}
}

func TestReasonOf(t *testing.T) {
	for in, want := range map[string]string{
		"quarantine:upstream 403": "quarantine", "sign_in_required": "sign_in_required", " rate_limited : x": "rate_limited",
		"": "", "Quarantine:x": "", "http 500: oops": "", "a-b:c": "", strings.Repeat("a", 33): "", "x@example.com": "",
	} {
		if got := reasonOf(in); got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
}

// pagedGateway answers the compact list in pages of size, with string cursors (as a relay does), and records the
// cursors asked for.
type pagedGateway struct {
	mu      sync.Mutex
	total   int
	size    int
	failAt  int // the page (0-based) that answers 500; -1 none
	asked   []string
	srv     *httptest.Server
	limited []string
}

func newPaged(t *testing.T, total, size, failAt int) *pagedGateway {
	p := &pagedGateway{total: total, size: size, failAt: failAt}
	p.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+testKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Method == "POST" {
			_, _ = io.WriteString(w, `{"accounts":[]}`)
			return
		}
		q := r.URL.Query()
		p.mu.Lock()
		p.asked = append(p.asked, q.Get("after_priority")+"/"+q.Get("after_id"))
		p.limited = append(p.limited, q.Get("limit"))
		page := len(p.asked) - 1
		p.mu.Unlock()
		if page == p.failAt {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		start := 0
		if id := q.Get("after_id"); id != "" {
			start, _ = strconv.Atoi(strings.TrimPrefix(id, "id"))
			start++
		}
		var list []string
		for i := start; i < min(start+p.size, p.total); i++ {
			list = append(list, fmt.Sprintf(`{"id":"id%d","name":"n","provider":"kiro","status":"disabled","priority":"5"}`, i))
		}
		body := `{"accounts":[` + strings.Join(list, ",") + `]`
		if end := start + p.size; end < p.total {
			body += fmt.Sprintf(`,"next_after_id":"id%d","next_after_priority":"5"`, end-1)
		}
		_, _ = io.WriteString(w, body+"}")
	}))
	t.Cleanup(p.srv.Close)
	return p
}

func (p *pagedGateway) cfg() Config { return Config{Type: TypeRelay, URL: p.srv.URL, Key: testKey} }

func TestRelayPagesTheList(t *testing.T) {
	p := newPaged(t, 1100, 500, -1)
	res, err := Fetch(context.Background(), p.cfg(), now)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Complete || len(res.Inventory) != 1100 || res.Seen.Listed != 1100 {
		t.Fatalf("complete %v, %d accounts, seen %+v", res.Complete, len(res.Inventory), res.Seen)
	}
	if fmt.Sprint(p.asked) != "[/ 5/id499 5/id999]" || fmt.Sprint(p.limited) != "[500 500 500]" {
		t.Errorf("cursors %v limits %v", p.asked, p.limited)
	}
	if res.Inventory[0].AccountID != "id0" || res.Inventory[1099].AccountID != "id1099" {
		t.Errorf("order: %s … %s", res.Inventory[0].AccountID, res.Inventory[1099].AccountID)
	}
}

func TestRelayInventoryCap(t *testing.T) {
	p := newPaged(t, 2600, 500, -1)
	res, err := Fetch(context.Background(), p.cfg(), now)
	if err != nil {
		t.Fatal(err)
	}
	if res.Complete || len(res.Inventory) != InventoryMost || len(p.asked) != 4 {
		t.Errorf("complete %v, %d accounts, %d pages", res.Complete, len(res.Inventory), len(p.asked))
	}
}

func TestRelayFailedPage(t *testing.T) {
	// a page after the first failing: what was read is kept, marked incomplete
	p := newPaged(t, 1100, 500, 1)
	res, err := Fetch(context.Background(), p.cfg(), now)
	if err != nil || res.Complete || len(res.Inventory) != 500 {
		t.Fatalf("%v complete %v, %d accounts", err, res.Complete, len(res.Inventory))
	}
	// the first one failing is the error, as before
	p = newPaged(t, 1100, 500, 0)
	if _, err := Fetch(context.Background(), p.cfg(), now); CodeOf(err) != CodeServer {
		t.Fatalf("first page: %v", err)
	}
}

func TestRelayCursorLoopStops(t *testing.T) {
	// a gateway answering the same cursor again (a bug there) does not keep the hub reading
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			_, _ = io.WriteString(w, `{"accounts":[]}`)
			return
		}
		n++
		_, _ = io.WriteString(w, `{"accounts":[{"id":"a","provider":"kiro","status":"active"}],"next_after_id":"a","next_after_priority":7}`)
	}))
	defer srv.Close()
	res, err := Fetch(context.Background(), Config{Type: TypeRelay, URL: srv.URL, Key: testKey}, now)
	if err != nil || n != 2 || len(res.Inventory) != 1 || res.Complete || res.Seen.Listed != 1 {
		t.Fatalf("%v: %d pages, %d accounts, complete %v, listed %d", err, n, len(res.Inventory), res.Complete, res.Seen.Listed)
	}
}

func TestGenericSourceHasNoInventory(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"accounts":[{"id":"s1","provider":"codex","windows":[{"label":"5h","used_percent":1}]}]}`)
	}))
	defer srv.Close()
	res, err := Fetch(context.Background(), Config{Type: TypeGeneric, URL: srv.URL}, now)
	if err != nil || len(res.Accounts) != 1 || res.Inventory != nil {
		t.Fatalf("%v %+v", err, res)
	}
}

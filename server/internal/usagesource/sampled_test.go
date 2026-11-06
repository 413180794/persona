package usagesource

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// A gateway whose clock runs ahead (or that sends a bogus date) must not make its sample look fresh forever: a
// sampled time later than now is taken as now.
func TestGenericFutureSampledAtIsNow(t *testing.T) {
	future := now.Add(48 * time.Hour).Format(time.RFC3339)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `{"accounts":[{"provider":"codex","sampled_at":%q,"windows":[
		  {"label":"5h","used_percent":10,"resets_at":"2026-11-05T21:00:00+08:00"}]}]}`, future)
	}))
	defer srv.Close()
	res, err := Fetch(context.Background(), Config{Type: TypeGeneric, URL: srv.URL, Key: testKey}, now)
	if err != nil || len(res.Accounts) != 1 {
		t.Fatalf("%v %+v", err, res.Accounts)
	}
	a := res.Accounts[0]
	if !a.SampledAt.Equal(now) {
		t.Errorf("sampled at %v, want %v", a.SampledAt, now)
	}
	r := a.Windows[0].ResetsAt
	if r == nil || r.Location() != time.UTC || !r.Equal(time.Date(2026, 11, 5, 13, 0, 0, 0, time.UTC)) {
		t.Errorf("resets at %v", r)
	}
}

func TestGenericPastSampledAtIsKept(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"accounts":[{"provider":"codex","sampled_at":"2026-11-05T16:30:00+08:00","windows":[
		  {"label":"5h","used_percent":10}]}]}`)
	}))
	defer srv.Close()
	res, err := Fetch(context.Background(), Config{Type: TypeGeneric, URL: srv.URL, Key: testKey}, now)
	if err != nil || len(res.Accounts) != 1 {
		t.Fatalf("%v %+v", err, res.Accounts)
	}
	if want := time.Date(2026, 11, 5, 8, 30, 0, 0, time.UTC); !res.Accounts[0].SampledAt.Equal(want) || res.Accounts[0].SampledAt.Location() != time.UTC {
		t.Errorf("sampled at %v, want %v", res.Accounts[0].SampledAt, want)
	}
}

func TestRelayFutureCheckedAtIsNow(t *testing.T) {
	future := now.Add(48 * time.Hour).Format(time.RFC3339)
	for name, body := range map[string]string{
		"window": fmt.Sprintf(`{"accounts":[{"account_id":"a1","windows":[
		  {"kind":"rolling_5h","used_ratio":0.2,"resets_at":"2026-11-05T21:00:00+08:00","checked_at":%q}]}]}`, future),
		"entry": fmt.Sprintf(`{"accounts":[{"account_id":"a1","checked_at":%q,"windows":[
		  {"kind":"rolling_5h","used_ratio":0.2,"resets_at":"2026-11-05T21:00:00+08:00","checked_at":"2026-11-05T08:00:00Z"}]}]}`, future),
	} {
		t.Run(name, func(t *testing.T) {
			g := newGateway(t, `{"accounts":[{"id":"a1","name":"x","provider":"codex","status":"active"}]}`, echoUsage(body))
			res, err := Fetch(context.Background(), g.cfg(), now)
			if err != nil || len(res.Accounts) != 1 {
				t.Fatalf("%v %+v", err, res.Accounts)
			}
			a := res.Accounts[0]
			if !a.SampledAt.Equal(now) {
				t.Errorf("sampled at %v, want %v", a.SampledAt, now)
			}
			r := a.Windows[0].ResetsAt
			if r == nil || r.Location() != time.UTC || !r.Equal(time.Date(2026, 11, 5, 13, 0, 0, 0, time.UTC)) {
				t.Errorf("resets at %v", r)
			}
		})
	}
}

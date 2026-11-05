package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNotifyRequestValidate(t *testing.T) {
	long := strings.Repeat("长", notifyTitleMost+1)
	for _, tc := range []struct {
		name, body, err, level string
	}{
		{"default level", `{"title":"t"}`, "", levelActive},
		{"time-sensitive", `{"body":"b","level":"time-sensitive"}`, "", levelTimeSensitive},
		{"empty", `{"title":"  ","body":""}`, "title or body is required", ""},
		{"critical refused", `{"title":"t","level":"critical"}`, "level must be", ""},
		{"passive refused", `{"title":"t","level":"passive"}`, "level must be", ""},
		{"session without machine", `{"title":"t","session_id":"s"}`, "go together", ""},
		{"title too long", `{"title":"` + long + `"}`, "too long", ""},
		{"id too long", `{"title":"t","id":"` + strings.Repeat("x", notifyIDMost+1) + `"}`, "too long", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var r notifyRequest
			if err := json.Unmarshal([]byte(tc.body), &r); err != nil {
				t.Fatal(err)
			}
			err := r.validate()
			if tc.err == "" {
				if err != nil || r.Level != tc.level {
					t.Fatalf("err %v level %q", err, r.Level)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.err) {
				t.Fatalf("err %v, want %q", err, tc.err)
			}
		})
	}
}

func TestNotifyPayload(t *testing.T) {
	p := notifyPayload(notifyRequest{Title: "卡住了", Body: "API 错误 30 分钟", MachineID: "m1", SessionID: "s1", Level: levelTimeSensitive}, "笔记本")
	aps := p["aps"].(map[string]any)
	if aps["interruption-level"] != levelTimeSensitive || aps["thread-id"] != "s1" || aps["sound"] != "default" {
		t.Fatalf("aps %v", aps)
	}
	alert := aps["alert"].(map[string]any)
	if alert["title"] != "卡住了" || alert["subtitle"] != "笔记本" || alert["body"] != "API 错误 30 分钟" {
		t.Fatalf("alert %v", alert)
	}
	if p["machine_id"] != "m1" || p["session_id"] != "s1" {
		t.Fatalf("ids %v", p)
	}
	// Without a session: no thread, no subtitle, nothing to open.
	p = notifyPayload(notifyRequest{Title: "t", Level: levelActive}, "")
	aps = p["aps"].(map[string]any)
	if _, ok := aps["thread-id"]; ok {
		t.Fatalf("thread-id without a session: %v", aps)
	}
	if _, ok := aps["alert"].(map[string]any)["subtitle"]; ok {
		t.Fatalf("subtitle without a machine: %v", aps)
	}
	// The pair is still there, empty: Android decodes the WebSocket event's ids as non-null strings.
	if p["machine_id"] != "" || p["session_id"] != "" {
		t.Fatalf("ids without a session: %v", p)
	}
}

func TestNotifyAPI(t *testing.T) {
	h := newTestHub(t)
	c := &client{device: "phone-1", out: make(chan []byte, 8)}
	h.clients[c] = struct{}{}
	h.onClientMessage(context.Background(), c, []byte(`{"type":"client_hello","features":["ws_notify"]}`))
	<-c.out // client_hello_ack
	h.mutes = &Pins{Spaces: []string{}, Sessions: []string{"m1/muted"}}
	srv := httptest.NewServer(h.routes())
	defer srv.Close()
	post := func(token, body string) (int, map[string]any) {
		t.Helper()
		req, _ := http.NewRequest("POST", srv.URL+"/api/v1/notify", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}
	next := func() map[string]any {
		t.Helper()
		select {
		case raw := <-c.out:
			var m map[string]any
			_ = json.Unmarshal(raw, &m)
			return m
		default:
			return nil
		}
	}

	if code, _ := post("wrong", `{"title":"t"}`); code != http.StatusUnauthorized {
		t.Fatalf("bad token: %d", code)
	}
	if code, _ := post(h.cfg.Token, `{"title":"t","level":"critical"}`); code != http.StatusBadRequest {
		t.Fatalf("bad level: %d", code)
	}

	code, out := post(h.cfg.Token, `{"title":"会话卡住了","body":"API 错误已 30 分钟","machine_id":"m1","session_id":"s1","level":"time-sensitive"}`)
	if code != 200 || out["sent"] != true {
		t.Fatalf("send: %d %v", code, out)
	}
	m := next()
	if m == nil || m["type"] != "notify" || m["kind"] != kindAlert || m["session_id"] != "s1" || m["title"] != "会话卡住了" {
		t.Fatalf("ws notify: %v", m)
	}
	// Without a session the ids are empty strings, never null.
	if code, _ := post(h.cfg.Token, `{"title":"只是告诉你"}`); code != 200 {
		t.Fatalf("send: %d", code)
	}
	if m := next(); m == nil || m["machine_id"] != "" || m["session_id"] != "" {
		t.Fatalf("ws notify without a session: %v", m)
	}
	// Too big for APNs once encoded (escapes count): refused instead of reported as sent.
	if code, _ := post(h.cfg.Token, `{"title":"t","body":"`+strings.Repeat(`\u0001`, notifyBodyMost)+`"}`); code != http.StatusBadRequest {
		t.Fatalf("oversized: %d", code)
	}
	if code, _ := post(h.cfg.Token, `{"title":"t","machine_id":"m1","session_id":"`+strings.Repeat("s", notifyRefMost+1)+`"}`); code != http.StatusBadRequest {
		t.Fatalf("long session id: %d", code)
	}

	// A muted session: nothing goes out, and the caller is told why.
	code, out = post(h.cfg.Token, `{"title":"t","machine_id":"m1","session_id":"muted"}`)
	if code != 200 || out["sent"] != false || out["reason"] != "muted" {
		t.Fatalf("muted: %d %v", code, out)
	}
	if m := next(); m != nil {
		t.Fatalf("muted session notified: %v", m)
	}

	// A device that turned 需要你 off gets none.
	if err := h.push.setPrefs("phone-1", NotifyPrefs{Input: false, Done: DoneLong}); err != nil {
		t.Fatal(err)
	}
	if code, _ := post(h.cfg.Token, `{"title":"t"}`); code != 200 {
		t.Fatalf("send: %d", code)
	}
	if m := next(); m != nil {
		t.Fatalf("input off but notified: %v", m)
	}
}

// An answer-only credential (the watch) cannot send notifications.
func TestNotifyNotForAnswerOnly(t *testing.T) {
	if allowed(principal{Role: roleAnswer}, http.MethodPost, "/api/v1/notify") {
		t.Fatal("answer-only credential may notify")
	}
}

// Through APNs: the level and the prefixed collapse id reach the push; 手机静音 holds it while a desktop is online.
func TestNotifyAPNs(t *testing.T) {
	h, f := macHub(t)
	srv := httptest.NewServer(h.routes())
	defer srv.Close()
	post := func(body string) {
		t.Helper()
		req, _ := http.NewRequest("POST", srv.URL+"/api/v1/notify", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil || resp.StatusCode != 200 {
			t.Fatalf("notify: %v %v", err, resp)
		}
		resp.Body.Close()
	}
	post(`{"title":"卡住了","body":"b","machine_id":"m1","session_id":"s1","level":"time-sensitive","id":"alert-1"}`)
	calls := f.calls(t, 2) // the phone and the Mac
	for _, c := range calls {
		var body map[string]any
		_ = json.Unmarshal(c.Body, &body)
		aps := body["aps"].(map[string]any)
		if c.Type != "alert" || aps["interruption-level"] != levelTimeSensitive || aps["thread-id"] != "s1" {
			t.Fatalf("push %s: %s", c.Token, c.Body)
		}
	}
	for _, c := range calls {
		if c.Collapse != notifyCollapse+"alert-1" {
			t.Fatalf("collapse id %q", c.Collapse)
		}
	}

	// 手机静音 with the Mac app connected: the phone gets nothing.
	if err := h.push.setPrefs("phone", NotifyPrefs{Input: true, Done: DoneLong, MuteWhileDesktop: true}); err != nil {
		t.Fatal(err)
	}
	h.push.desktopOnline = func() bool { return true }
	before := len(f.calls(t, 2))
	post(`{"title":"t"}`)
	time.Sleep(200 * time.Millisecond)
	for _, c := range f.calls(t, before)[before:] {
		if c.Token == "devtok" {
			t.Fatalf("muted phone got a push: %s", c.Body)
		}
	}
}

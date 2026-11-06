package hub

import (
	"context"
	"encoding/json"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestClientHelloMetadata(t *testing.T) {
	c := &client{out: make(chan []byte, 16), device: "credential", role: roleAnswer}
	h := &Hub{}
	if c.has("app_ping") || c.meta.Platform != "" {
		t.Fatal("legacy clients must not declare new capabilities")
	}
	h.onClientMessage(context.Background(), c, []byte(`{"type":"client_hello","device_id":"example-device","platform":"android","version":"1.2","build":42,"os":{"name":"Android","version":"16","sdk":36},"features":["app_ping","example_feature"],"heartbeat":{"interval":240,"pong_timeout":45}}`))
	if c.meta.DeviceID != "example-device" || c.meta.Platform != "android" || c.meta.Version != "1.2" ||
		string(c.meta.Build) != "42" || c.meta.OS.Name != "Android" || c.meta.OS.Version != "16" || c.meta.OS.SDK != 36 {
		t.Fatalf("metadata: %+v", c.meta)
	}
	if !c.has("app_ping") || !c.has("example_feature") || c.has("missing") || c.device != "credential" || c.role != roleAnswer {
		t.Fatal("capabilities or credential changed unexpectedly")
	}
	checkHelloAck(t, <-c.out, c.meta.heartbeat)
	h.onClientHello(c, []byte(`{"platform":"ios","build":"43","features":[]}`))
	if c.has("app_ping") || string(c.meta.Build) != `"43"` || c.meta.heartbeat.Interval < 216 {
		t.Fatal("replacement hello must replace capabilities and preserve omitted heartbeat")
	}
	<-c.out
	before := c.meta.heartbeat
	for _, bad := range []string{`{`, `{"features":7}`, `{"heartbeat":{"interval":"20"}}`, `{"heartbeat":{"interval":1e30}}`} {
		h.onClientHello(c, []byte(bad))
		if len(c.out) != 0 || c.meta.heartbeat != before || c.meta.Platform != "ios" {
			t.Fatalf("invalid hello changed metadata: %s", bad)
		}
	}
}

func checkHelloAck(t *testing.T, data []byte, want heartbeatConfig) {
	t.Helper()
	var ack struct {
		Type      string          `json:"type"`
		Heartbeat heartbeatConfig `json:"heartbeat"`
	}
	if err := json.Unmarshal(data, &ack); err != nil || ack.Type != "client_hello_ack" ||
		ack.Heartbeat.Interval != want.Interval || ack.Heartbeat.PongTimeout != want.PongTimeout {
		t.Fatalf("ack %s, want %+v: %v", data, want, err)
	}
}

func TestClientHelloClamps(t *testing.T) {
	for _, tt := range []struct {
		body              string
		min, max, timeout int
	}{
		{`{}`, 20, 20, 15},
		{`{"heartbeat":{}}`, 20, 20, 15},
		{`{"heartbeat":{"interval":-1,"pong_timeout":0}}`, 10, 10, 5},
		{`{"heartbeat":{"interval":100000,"pong_timeout":100000}}`, 810, 900, 60},
		{`{"heartbeat":{"interval":20,"pong_timeout":15}}`, 18, 20, 15},
		{`{"heartbeat":{"interval":240,"pong_timeout":45}}`, 216, 240, 45},
		{`{"heartbeat":{"pong_timeout":60}}`, 20, 20, 60},
	} {
		t.Run(tt.body, func(t *testing.T) {
			for range 32 {
				c := &client{out: make(chan []byte, 1)}
				(&Hub{}).onClientHello(c, []byte(tt.body))
				hb := c.meta.heartbeat
				if hb.Interval < tt.min || hb.Interval > tt.max || hb.PongTimeout != tt.timeout {
					t.Fatalf("heartbeat: %+v", hb)
				}
				checkHelloAck(t, <-c.out, hb)
			}
		})
	}
}

func TestAppPingStringID(t *testing.T) {
	c := &client{out: make(chan []byte, 1)}
	h := &Hub{}
	for _, id := range []string{"", "00042", "引号\"\n/😀"} {
		b, _ := json.Marshal(map[string]string{"type": "ping", "id": id})
		h.onClientMessage(context.Background(), c, b)
		var pong map[string]string
		if err := json.Unmarshal(<-c.out, &pong); err != nil || pong["type"] != "pong" || pong["id"] != id {
			t.Fatalf("pong: %v (%v)", pong, err)
		}
	}
	for _, bad := range []string{`{}`, `{"id":null}`, `{"id":42}`, `{"id":{}}`} {
		h.onAppPing(c, []byte(bad))
		if len(c.out) != 0 {
			t.Fatalf("answered invalid ping: %s", bad)
		}
	}
}

func TestWSLegacyAndNegotiation(t *testing.T) {
	h := newTestHub(t)
	s := httptest.NewServer(h.routes())
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, s.URL+"/api/v1/ws?token="+h.cfg.Token, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	read := func() map[string]any {
		t.Helper()
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var out map[string]any
		if err := json.Unmarshal(data, &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	if hello := read(); hello["type"] != "hello" || hello["client_id"] == "" || hello["heartbeat"] != nil {
		t.Fatalf("legacy hello changed: %v", hello)
	}
	if machines := read(); machines["type"] != "machines" {
		t.Fatalf("machines: %v", machines)
	}
	if desktops := read(); desktops["type"] != "desktops" {
		t.Fatalf("desktops: %v", desktops)
	}
	h.mu.Lock()
	var c *client
	for candidate := range h.clients {
		c = candidate
	}
	h.mu.Unlock()
	if c == nil || c.has("app_ping") {
		t.Fatal("legacy client gained capabilities")
	}
	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":"client_hello","platform":"android","heartbeat":{"interval":1,"pong_timeout":100}}`)); err != nil {
		t.Fatal(err)
	}
	ack := read()
	if ack["type"] != "client_hello_ack" || ack["heartbeat"].(map[string]any)["interval"] != float64(10) || ack["heartbeat"].(map[string]any)["pong_timeout"] != float64(60) {
		t.Fatalf("ack: %v", ack)
	}
	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":"ping","id":"00042"}`)); err != nil {
		t.Fatal(err)
	}
	if pong := read(); pong["type"] != "pong" || pong["id"] != "00042" {
		t.Fatalf("pong: %v", pong)
	}
	// Reading still services the legacy client's protocol ping/pong after negotiation.
	readCtx := conn.CloseRead(ctx)
	if err := conn.Ping(readCtx); err != nil {
		t.Fatal(err)
	}
}

type keepAliveConn struct {
	net.Conn
	configs []net.KeepAliveConfig
}

func (c *keepAliveConn) SetKeepAliveConfig(config net.KeepAliveConfig) error {
	c.configs = append(c.configs, config)
	return nil
}

type wrappedKeepAliveConn struct{ net.Conn }

func (c wrappedKeepAliveConn) NetConn() net.Conn { return c.Conn }

func TestHeartbeatKeepAlive(t *testing.T) {
	c := &keepAliveConn{}
	if err := setHeartbeatKeepAlive(c, defaultHeartbeat()); err != nil || len(c.configs) != 0 {
		t.Fatal("legacy keepalive changed")
	}
	hb := heartbeatConfig{Interval: 240, PongTimeout: 45, negotiated: true}
	if err := setHeartbeatKeepAlive(wrappedKeepAliveConn{c}, hb); err != nil {
		t.Fatal(err)
	}
	if len(c.configs) != 1 || !c.configs[0].Enable || c.configs[0].Idle != 240*time.Second || c.configs[0].Interval != 45*time.Second {
		t.Fatalf("keepalive: %+v", c.configs)
	}
	if err := setHeartbeatKeepAlive(nil, hb); err != nil {
		t.Fatal(err)
	}
}

func TestWSFeatures(t *testing.T) {
	b, _ := json.Marshal(newTestHub(t).hello())
	for _, feature := range []string{"client_hello", "heartbeat", "app_ping"} {
		if !strings.Contains(string(b), `"`+feature+`"`) {
			t.Fatalf("missing feature %s", feature)
		}
	}
}

func TestWSLegacyProtocolPing(t *testing.T) {
	h := newTestHub(t)
	s := httptest.NewServer(h.routes())
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, s.URL+"/api/v1/ws?token="+h.cfg.Token, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	for _, kind := range []string{"hello", "machines"} {
		_, b, err := conn.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var msg map[string]any
		if err := json.Unmarshal(b, &msg); err != nil || msg["type"] != kind {
			t.Fatalf("legacy message: %s (%v)", b, err)
		}
	}
	// No client_hello is sent on this connection.
	// Ping needs a concurrent reader to receive the pong. Do not use
	// conn.CloseRead here: the hub keeps sending data messages, CloseRead's
	// goroutine then calls Close, and Close plus the deferred CloseNow wait
	// on each other until the library's 15 s timeout. Drain frames instead.
	go func() {
		for {
			if _, _, err := conn.Read(ctx); err != nil {
				return
			}
		}
	}()
	if err := conn.Ping(ctx); err != nil {
		t.Fatal(err)
	}
}

package hub

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/chyroc/jarvis/server/internal/db"
	"github.com/chyroc/jarvis/server/internal/proto"
)

// Device is an iPhone or Watch that wants push notifications.
type Device = db.Device

// apnsConfig lives in ~/.jarvis/apns.json; without it push is disabled.
type apnsConfig struct {
	KeyID   string `json:"key_id"`
	TeamID  string `json:"team_id"`
	KeyPath string `json:"key_path"`
	Topic   string `json:"topic"`
}

type Pusher struct {
	dir   string
	store Store
	logf  func(string, ...any)
	cfg   *apnsConfig
	key   *ecdsa.PrivateKey
	http  *http.Client

	mu      sync.Mutex
	devices map[string]Device
	jwt     string
	jwtAt   time.Time
}

func newPusher(dir string, store Store, logf func(string, ...any)) *Pusher {
	p := &Pusher{dir: dir, store: store, logf: logf, devices: map[string]Device{}, http: &http.Client{Timeout: 15 * time.Second}}
	if store != nil {
		if list, err := store.Devices(context.Background()); err == nil {
			for _, d := range list {
				p.devices[d.DeviceID] = d
			}
		} else {
			logf("load devices: %v", err)
		}
	}
	b, err := os.ReadFile(filepath.Join(dir, "apns.json"))
	if err != nil {
		logf("push disabled: no %s", filepath.Join(dir, "apns.json"))
		return p
	}
	var cfg apnsConfig
	if err := json.Unmarshal(b, &cfg); err != nil {
		logf("push disabled: %v", err)
		return p
	}
	keyPEM, err := os.ReadFile(cfg.KeyPath)
	if err != nil {
		logf("push disabled: %v", err)
		return p
	}
	block, _ := pem.Decode(keyPEM)
	if block == nil {
		logf("push disabled: bad key file")
		return p
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	ek, ok := k.(*ecdsa.PrivateKey)
	if err != nil || !ok {
		logf("push disabled: key is not an EC private key")
		return p
	}
	p.cfg, p.key = &cfg, ek
	logf("push enabled (key %s, topic %s)", cfg.KeyID, cfg.Topic)
	return p
}

func (p *Pusher) register(d Device) {
	d.UpdatedAt = time.Now().UTC()
	p.mu.Lock()
	p.devices[d.DeviceID] = d
	p.mu.Unlock()
	if p.store != nil {
		if err := p.store.UpsertDevice(context.Background(), d); err != nil {
			p.logf("save device: %v", err)
		}
	}
}

func (p *Pusher) token() (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.jwt != "" && time.Since(p.jwtAt) < 40*time.Minute {
		return p.jwt, nil
	}
	enc := base64.RawURLEncoding
	header := enc.EncodeToString([]byte(fmt.Sprintf(`{"alg":"ES256","kid":%q}`, p.cfg.KeyID)))
	claims := enc.EncodeToString([]byte(fmt.Sprintf(`{"iss":%q,"iat":%d}`, p.cfg.TeamID, time.Now().Unix())))
	sum := sha256.Sum256([]byte(header + "." + claims))
	r, s, err := ecdsa.Sign(rand.Reader, p.key, sum[:])
	if err != nil {
		return "", err
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	p.jwt, p.jwtAt = header+"."+claims+"."+enc.EncodeToString(sig), time.Now()
	return p.jwt, nil
}

func (p *Pusher) send(payload map[string]any, collapseID string) {
	p.sendAs(payload, collapseID, "alert", "10")
}

// ready reports whether pushes can go anywhere (APNs key loaded and a device registered).
func (p *Pusher) ready() bool {
	if p == nil || p.key == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.devices) > 0
}

// wake sends a silent background push: iOS gives the app ~30 s to connect and run a pending phone_* call.
// Best effort: the system throttles these and never delivers them to a force-quit app.
func (p *Pusher) wake(reason string) {
	p.sendAs(map[string]any{"aps": map[string]any{"content-available": 1}, "jarvis_wake": reason}, "", "background", "5")
}

func (p *Pusher) sendAs(payload map[string]any, collapseID, pushType, priority string) {
	if p == nil || p.key == nil {
		return
	}
	p.mu.Lock()
	devices := make([]Device, 0, len(p.devices))
	for _, d := range p.devices {
		if d.APNSToken != "" {
			devices = append(devices, d)
		}
	}
	p.mu.Unlock()
	if len(devices) == 0 {
		return
	}
	jwt, err := p.token()
	if err != nil {
		p.logf("apns jwt: %v", err)
		return
	}
	body, _ := json.Marshal(payload)
	for _, d := range devices {
		host := "https://api.push.apple.com"
		if d.APNSEnv == "sandbox" {
			host = "https://api.sandbox.push.apple.com"
		}
		topic := d.Topic
		if topic == "" {
			topic = p.cfg.Topic
		}
		req, _ := http.NewRequest("POST", host+"/3/device/"+d.APNSToken, bytes.NewReader(body))
		req.Header.Set("authorization", "bearer "+jwt)
		req.Header.Set("apns-topic", topic)
		req.Header.Set("apns-push-type", pushType)
		req.Header.Set("apns-priority", priority)
		if collapseID != "" {
			req.Header.Set("apns-collapse-id", collapseID)
		}
		go func(d Device) {
			resp, err := p.http.Do(req)
			if err != nil {
				p.logf("apns %s: %v", d.Name, err)
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode != 200 {
				var e struct {
					Reason string `json:"reason"`
				}
				_ = json.NewDecoder(resp.Body).Decode(&e)
				p.logf("apns %s: %d %s", d.Name, resp.StatusCode, e.Reason)
				if resp.StatusCode == 410 || e.Reason == "BadDeviceToken" || e.Reason == "Unregistered" {
					p.mu.Lock()
					delete(p.devices, d.DeviceID)
					p.mu.Unlock()
					if p.store != nil {
						_ = p.store.DeleteDevice(context.Background(), d.DeviceID)
					}
				}
			}
		}(d)
	}
}

func (p *Pusher) notifyInteraction(ix proto.Interaction, machine string) {
	p.send(interactionPayload(ix, machine), ix.ID)
}

// interactionPayload is the alert for a new interaction. `category` must match a UNNotificationCategory the app
// registers (ios/Jarvis/App/NotificationActions.swift): that is what puts 允许 / 拒绝 / 批准 on the notification, and the
// buttons answer `interaction_id` through the resolve API.
func interactionPayload(ix proto.Interaction, machine string) map[string]any {
	title := map[string]string{
		proto.IxQuestion: "Claude 在问你", proto.IxPlan: "计划等你审批", proto.IxPermission: "需要你授权", proto.IxScreen: "终端等你处理",
	}[ix.Kind]
	if ix.ToolName == "ask_user" {
		title = "Jarvis 在问你"
	}
	body := ix.Summary
	switch ix.Kind {
	case proto.IxQuestion:
		if len(ix.Questions) > 0 {
			body = ix.Questions[0].Question
		}
	case proto.IxPlan:
		body = firstLine(ix.Plan)
	case proto.IxScreen:
		body = "打开 Jarvis 查看屏幕并操作"
	}
	return map[string]any{
		"aps": map[string]any{
			"alert":              map[string]any{"title": title, "subtitle": ix.SessionTitle + " · " + machine, "body": body},
			"sound":              "default",
			"thread-id":          ix.SessionID,
			"category":           "INTERACTION_" + strings.ToUpper(ix.Kind),
			"interruption-level": "time-sensitive",
		},
		"interaction_id": ix.ID, "machine_id": ix.MachineID, "session_id": ix.SessionID,
	}
}

func (p *Pusher) notifyDone(machineID, machine, sessionID, title, text string) {
	if text == "" {
		text = "已完成"
	}
	p.send(map[string]any{
		"aps": map[string]any{
			"alert":     map[string]any{"title": title, "subtitle": machine, "body": text},
			"sound":     "default",
			"thread-id": sessionID,
		},
		"machine_id": machineID, "session_id": sessionID,
	}, "")
}

func (p *Pusher) clear(string) {}

func firstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		l = strings.TrimSpace(strings.TrimLeft(l, "# "))
		if l != "" {
			return l
		}
	}
	return ""
}

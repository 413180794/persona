// Package apns sends to Apple Push Notification service with a token-based (.p8) key. The hub uses it when it has
// its own key (~/.jarvis/apns.json); the push relay (jarvisd relay, docs/push-relay.md) uses it for everyone else.
package apns

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
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// Config is the key: ~/.jarvis/apns.json on a hub, the "apns" object in relay.json.
type Config struct {
	KeyID   string `json:"key_id"`
	TeamID  string `json:"team_id"`
	KeyPath string `json:"key_path"`
	Topic   string `json:"topic,omitempty"` // the hub's default topic when a device registered none
}

// Client signs provider tokens and posts notifications. Safe for concurrent use.
type Client struct {
	Cfg  Config
	key  *ecdsa.PrivateKey
	http *http.Client
	// Base replaces Apple's host (tests).
	Base string

	mu    sync.Mutex
	jwt   string
	jwtAt time.Time
}

// New loads the .p8 key the config names ("~/" is the home directory).
func New(cfg Config) (*Client, error) {
	path := cfg.KeyPath
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		path = home + path[1:]
	}
	keyPEM, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(keyPEM)
	if block == nil {
		return nil, errors.New("bad key file")
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	ek, ok := k.(*ecdsa.PrivateKey)
	if err != nil || !ok {
		return nil, errors.New("key is not an EC private key")
	}
	return NewWithKey(cfg, ek), nil
}

// NewWithKey uses a key already in memory (tests).
func NewWithKey(cfg Config, key *ecdsa.PrivateKey) *Client {
	return &Client{Cfg: cfg, key: key, http: &http.Client{Timeout: 15 * time.Second}}
}

func (c *Client) token() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.jwt != "" && time.Since(c.jwtAt) < 40*time.Minute {
		return c.jwt, nil
	}
	enc := base64.RawURLEncoding
	header := enc.EncodeToString([]byte(fmt.Sprintf(`{"alg":"ES256","kid":%q}`, c.Cfg.KeyID)))
	claims := enc.EncodeToString([]byte(fmt.Sprintf(`{"iss":%q,"iat":%d}`, c.Cfg.TeamID, time.Now().Unix())))
	sum := sha256.Sum256([]byte(header + "." + claims))
	r, s, err := ecdsa.Sign(rand.Reader, c.key, sum[:])
	if err != nil {
		return "", err
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	c.jwt, c.jwtAt = header+"."+claims+"."+enc.EncodeToString(sig), time.Now()
	return c.jwt, nil
}

// Request is one notification.
type Request struct {
	Token      string // device token (hex), or a Live Activity's
	Env        string // "sandbox" or production
	Topic      string // bundle id; for liveactivity the ".push-type.liveactivity" suffix is added here
	PushType   string // alert | background | liveactivity
	Priority   string // "10" | "5"
	CollapseID string
	Body       []byte
}

// Result is APNs' answer when it answered at all.
type Result struct {
	Status int
	Reason string
}

// Gone: the token is dead and should be forgotten.
func (r Result) Gone() bool {
	return r.Status == 410 || r.Reason == "BadDeviceToken" || r.Reason == "Unregistered"
}

// Host is APNs' host for an environment.
func Host(env string) string {
	if env == "sandbox" {
		return "https://api.sandbox.push.apple.com"
	}
	return "https://api.push.apple.com"
}

// Send posts one notification and waits for APNs' answer.
func (c *Client) Send(ctx context.Context, r Request) (Result, error) {
	jwt, err := c.token()
	if err != nil {
		return Result{}, fmt.Errorf("apns jwt: %w", err)
	}
	host := Host(r.Env)
	if c.Base != "" {
		host = c.Base
	}
	topic := r.Topic
	if topic == "" {
		topic = c.Cfg.Topic
	}
	if r.PushType == "liveactivity" {
		topic += ".push-type.liveactivity"
	}
	req, err := http.NewRequestWithContext(ctx, "POST", host+"/3/device/"+r.Token, bytes.NewReader(r.Body))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("authorization", "bearer "+jwt)
	req.Header.Set("apns-topic", topic)
	req.Header.Set("apns-push-type", r.PushType)
	req.Header.Set("apns-priority", r.Priority)
	if r.CollapseID != "" {
		req.Header.Set("apns-collapse-id", r.CollapseID)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		// The APNs connection is one long-lived HTTP/2 connection, and Go does not notice when it dies
		// silently (no health pings before Go 1.24): after the hub's proxy routing broke and came back,
		// every push still timed out on the dead connection until the hub restarted. Drop it so the next
		// push dials a new one.
		c.http.CloseIdleConnections()
		return Result{}, err
	}
	defer resp.Body.Close()
	res := Result{Status: resp.StatusCode}
	if resp.StatusCode != 200 {
		var e struct {
			Reason string `json:"reason"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&e)
		res.Reason = e.Reason
	}
	return res, nil
}

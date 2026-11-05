package hub

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/nwverse/persona/server/internal/apns"
	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/pushcrypto"
	"github.com/nwverse/persona/server/internal/relayproto"
	"github.com/nwverse/persona/server/internal/textfmt"
	"github.com/nwverse/persona/server/internal/transcript"
)

// Device is an iPhone, a Mac or a Watch that wants push notifications.
type Device = db.Device

// kindMac is a Mac app's device (PushRegistrar on macOS): it gets alerts, not the phone's wake pushes.
const kindMac = "mac"

// apnsConfig lives in ~/.jarvis/apns.json; without it the hub pushes through the relay each phone registered with
// (docs/push-relay.md), and with neither push is off.
type apnsConfig = apns.Config

type Pusher struct {
	dir   string
	store Store
	logf  func(string, ...any)
	// direct: the hub's own APNs key. Set, every push goes straight to APNs as before; nil, through the relay.
	direct *apns.Client
	relay  *http.Client
	// retryWaits: how long a push the relay did not take for a reason that may pass waits before each further try
	// (postRelayRetrying); tests shorten them.
	retryWaits []time.Duration
	now        func() time.Time
	// desktopOnline: a desktop app is connected (Hub.desktopOnline), for 手机静音.
	desktopOnline func() bool
	// hubID is this hub's hub_id: every push says which hub sent it, so the app knows the group (plan 2.6「推送」).
	hubID func() string
	// userLang is the user's language (Hub.userLang): pushes to a device that registered without one read in it.
	userLang func() i18n.Lang
	// desktopOpen: the desktop app with this push device id is connected (Hub.desktopOpen); it posts its own
	// notifications for agents then (the Mac's MacAttentionWatcher).
	desktopOpen func(device string) bool

	mu      sync.Mutex
	devices map[string]Device
	// replaced: per device, the grant the hub renewed away from (renewGrant). The app keeps reporting the one it
	// cached; that one must not undo the renewal.
	replaced map[string]string
	saveMu   sync.Mutex // one device row write at a time, always the latest state (save, setPrefs)
	// storeTimeout bounds each device row write, which never runs under mu; tests shorten it.
	storeTimeout time.Duration
}

// deviceStoreTimeout: how long a device row write may take before it gives up (the pool has no statement_timeout).
const deviceStoreTimeout = 5 * time.Second

func newPusher(dir string, store Store, logf func(string, ...any)) *Pusher {
	p := &Pusher{dir: dir, store: store, logf: logf, devices: map[string]Device{}, replaced: map[string]string{}, relay: &http.Client{Timeout: 20 * time.Second},
		retryWaits: []time.Duration{time.Second, 4 * time.Second}, now: time.Now, storeTimeout: deviceStoreTimeout}
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
		logf("push: no %s, pushing through each phone's relay (docs/push-relay.md)", filepath.Join(dir, "apns.json"))
		return p
	}
	var cfg apnsConfig
	if err := json.Unmarshal(b, &cfg); err != nil {
		logf("push: apns.json: %v; pushing through the relay", err)
		return p
	}
	c, err := apns.New(cfg)
	if err != nil {
		logf("push: apns key: %v; pushing through the relay", err)
		return p
	}
	p.direct = c
	logf("push enabled (key %s, topic %s)", cfg.KeyID, cfg.Topic)
	return p
}

// validRelayURL: an https URL (the phone's build config; the hub sends the grant and Live Activity contents there),
// or http to this machine (the end-to-end script's local relay).
func validRelayURL(s string) bool {
	u, err := url.Parse(s)
	if err != nil || u.Host == "" || u.RawQuery != "" {
		return false
	}
	switch u.Scheme {
	case "https":
		return true
	case "http":
		ip := net.ParseIP(u.Hostname())
		return u.Hostname() == "localhost" || (ip != nil && ip.IsLoopback())
	}
	return false
}

func (p *Pusher) register(d Device) {
	d.UpdatedAt = time.Now().UTC()
	d.RelayURL = strings.TrimRight(d.RelayURL, "/")
	if d.RelayURL != "" && !validRelayURL(d.RelayURL) {
		p.logf("device %s: ignoring relay url %q", d.Name, d.RelayURL)
		d.RelayURL, d.RelayGrant = "", ""
	}
	p.mu.Lock()
	cur := p.devices[d.DeviceID]
	d.Prefs = cur.Prefs // registration carries the token, not the settings
	if d.RelayGrant != "" && d.RelayGrant == p.replaced[d.DeviceID] && d.RelayURL == cur.RelayURL && d.APNSToken == cur.APNSToken {
		d.RelayGrant = cur.RelayGrant // the app's cached grant, which the hub already renewed
	}
	p.devices[d.DeviceID] = d
	p.mu.Unlock()
	p.save(d.DeviceID)
}

// save writes a device's current row (whatever the latest register / renewal left), one write at a time.
func (p *Pusher) save(id string) {
	if p.store == nil {
		return
	}
	p.saveMu.Lock()
	defer p.saveMu.Unlock()
	p.mu.Lock()
	d, ok := p.devices[id]
	p.mu.Unlock()
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), p.storeTimeout)
	defer cancel()
	if err := p.store.UpsertDevice(ctx, d); err != nil {
		p.logf("save device: %v", err)
	}
}

// relayed: this device can be pushed through its relay (the hub has no key of its own).
func (p *Pusher) relayed(d Device) bool {
	return p.direct == nil && d.RelayURL != "" && d.RelayGrant != "" && d.PushKey != ""
}

// reachable: a push to this device can go anywhere.
func (p *Pusher) reachable(d Device) bool {
	if p.direct != nil {
		return d.APNSToken != ""
	}
	return p.relayed(d)
}

// send alerts every device whose settings take it (nil: all of them).
// A device that chose 手机静音 gets none while a desktop app is connected (desktops.go), and a desktop app that is
// connected gets none of these: it shows them itself. What was asked for explicitly (nil) goes to it all the same.
func (p *Pusher) send(payload map[string]any, collapseID string, wants func(NotifyPrefs) bool) {
	p.sendLocalized(func(i18n.Lang) map[string]any { return payload }, collapseID, wants)
}

// sendLocalized is send with the alert written per device, in its language (langOf).
func (p *Pusher) sendLocalized(build localized, collapseID string, wants func(NotifyPrefs) bool) {
	if p == nil {
		return
	}
	online := p.desktopOnline != nil && p.desktopOnline()
	byPrefs := alertWanted(wants, online)
	p.sendBuilt(build, collapseID, "alert", "10", func(d Device) bool {
		if wants != nil && p.desktopOpen != nil && p.desktopOpen(d.DeviceID) {
			return false
		}
		return byPrefs(devicePrefs(d.Prefs))
	})
}

func (p *Pusher) device(id string) (Device, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	d, ok := p.devices[id]
	return d, ok
}

// signedOut forgets the devices a revoked credential registered (退出那台 Mac, 设置 › 设备): an app signed out gets
// no more pushes, its Live Activity included. Its notification settings go with it; pairing again starts from the
// defaults.
func (p *Pusher) signedOut(credential string) {
	if p == nil || credential == "" {
		return
	}
	p.mu.Lock()
	var gone []Device
	for _, d := range p.devices {
		if d.Credential == credential {
			gone = append(gone, d)
		}
	}
	p.mu.Unlock()
	for _, d := range gone {
		p.forget(d)
		p.logf("device %s signed out: no more pushes", d.Name)
	}
}

// forget drops a device, whatever token it has now (signedOut).
func (p *Pusher) forget(d Device) {
	p.mu.Lock()
	delete(p.devices, d.DeviceID)
	p.mu.Unlock()
	if p.store != nil {
		ctx, cancel := context.WithTimeout(context.Background(), p.storeTimeout)
		defer cancel()
		_ = p.store.DeleteDevice(ctx, d.DeviceID)
	}
}

// forgetIfCurrent drops a device whose token APNs says is dead, only while that token (and relay) is still the one
// it has: a push's answer can come seconds after the app registered a new token (the relay retries, renews its
// grant), and that late answer must not delete the new registration and its settings. Under saveMu, so a save of a
// newer registration is not undone by the delete that follows it, or the other way round.
func (p *Pusher) forgetIfCurrent(d Device) {
	p.saveMu.Lock()
	defer p.saveMu.Unlock()
	p.mu.Lock()
	cur, ok := p.devices[d.DeviceID]
	still := ok && cur.APNSToken == d.APNSToken && cur.RelayURL == d.RelayURL
	if still {
		delete(p.devices, d.DeviceID)
	}
	p.mu.Unlock()
	if !still {
		p.logf("device %s: its old token is gone; kept, it has registered a new one", d.Name)
		return
	}
	if p.store != nil {
		_ = p.store.DeleteDevice(context.Background(), d.DeviceID)
	}
}

// sendLiveActivity pushes to a Live Activity token (an activity's own, or the push-to-start one): its own push type and
// topic, the device's APNs environment. gone runs when APNs says the token is dead. Through the relay the payload is
// not sealed: iOS reads a Live Activity's content-state itself (docs/push-relay.md「Live Activity 是明文」).
func (p *Pusher) sendLiveActivity(d Device, token string, payload map[string]any, priority string, gone func()) {
	if p == nil || token == "" || !p.reachable(d) {
		return
	}
	if p.relayed(d) {
		aps, _ := payload["aps"].(map[string]any)
		go func() {
			res, err := p.postRelay(d, relayproto.PushRequest{Type: "liveactivity", Priority: priority, Token: token, APS: aps})
			if err != nil {
				p.logf("relay live activity %s: %v", d.Name, err)
			} else if res.gone {
				gone()
			}
		}()
		return
	}
	body, _ := json.Marshal(payload)
	go func() {
		res, err := p.direct.Send(context.Background(), apns.Request{Token: token, Env: d.APNSEnv, Topic: d.Topic, PushType: "liveactivity", Priority: priority, Body: body})
		if err != nil {
			p.logf("apns live activity %s: %v", d.Name, err)
			return
		}
		if res.Status != 200 {
			p.logf("apns live activity %s: %d %s", d.Name, res.Status, res.Reason)
			if res.Gone() {
				gone()
			}
		}
	}()
}

type relayResult struct{ gone bool }

// postRelay hands one push to the device's relay (relayproto.PushRequest, docs/push-relay.md「接口」). A grant the relay
// no longer takes (its secret was replaced) is renewed once with the device's token, which the hub already has. A
// failure that may pass is tried again (postRelayRetrying): one dropped connection used to lose the push (#2150).
func (p *Pusher) postRelay(d Device, req relayproto.PushRequest) (relayResult, error) {
	res, status, err := p.postRelayRetrying(d, req)
	if status != http.StatusUnauthorized {
		return res, err
	}
	d, rerr := p.renewGrant(d)
	if rerr != nil {
		return res, fmt.Errorf("%w; renewing the grant: %v", err, rerr)
	}
	res, _, err = p.postRelayRetrying(d, req)
	return res, err
}

// postRelayRetrying is postRelayOnce, tried again after each of p.retryWaits while the failure is one that may pass
// (relayRetryable). Its callers each run in their own goroutine, so the waits hold up nothing else; a Live Activity
// update that lands late is harmless, as its aps.timestamp makes iOS drop it behind a newer one.
func (p *Pusher) postRelayRetrying(d Device, req relayproto.PushRequest) (relayResult, int, error) {
	res, status, err := p.postRelayOnce(d, req)
	tries, first := 1, err
	for _, wait := range p.retryWaits {
		if err == nil || !relayRetryable(status) {
			break
		}
		time.Sleep(wait)
		res, status, err = p.postRelayOnce(d, req)
		tries++
	}
	switch {
	case tries == 1:
	case err == nil:
		p.logf("relay %s: %s push went through on try %d, after %v", d.Name, req.Type, tries, first)
	default:
		err = fmt.Errorf("%w (%d tries)", err, tries)
	}
	return res, status, err
}

// relayRetryable: a failure worth another try. That is the network's (no status: a TLS handshake or connection that
// timed out, a reset) or the relay's 5xx and 429; any other status is the relay's answer and stands.
func relayRetryable(status int) bool {
	return status == 0 || status == http.StatusTooManyRequests || status >= 500
}

func (p *Pusher) postRelayOnce(d Device, req relayproto.PushRequest) (relayResult, int, error) {
	body, _ := json.Marshal(req)
	hr, err := http.NewRequest("POST", d.RelayURL+"/v1/push", bytes.NewReader(body))
	if err != nil {
		return relayResult{}, -1, err // not a status: a request that cannot be made is not tried again
	}
	hr.Header.Set("Authorization", "Bearer "+d.RelayGrant)
	hr.Header.Set("Content-Type", "application/json")
	sent := time.Now()
	resp, err := p.relay.Do(hr)
	if err != nil {
		return relayResult{}, 0, err
	}
	defer resp.Body.Close()
	var out struct {
		Error  string `json:"error"`
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&out)
	switch {
	case resp.StatusCode == 200:
		if req.Type != "liveactivity" { // when the relay (and APNs behind it) took it: wake-up latency (docs/assistant.md)
			p.logf("relay %s: %s push accepted in %d ms", d.Name, req.Type, time.Since(sent).Milliseconds())
		}
		return relayResult{}, 200, nil
	case resp.StatusCode == http.StatusGone:
		p.logf("relay %s: %s token gone (%s)", d.Name, req.Type, out.Reason)
		return relayResult{gone: true}, resp.StatusCode, nil
	default:
		return relayResult{}, resp.StatusCode, fmt.Errorf("%d %s %s", resp.StatusCode, out.Error, out.Reason)
	}
}

// renewGrant registers the device's token with its relay again and keeps the new grant.
func (p *Pusher) renewGrant(d Device) (Device, error) {
	if d.APNSToken == "" || d.Topic == "" {
		return d, fmt.Errorf("device has no token to register")
	}
	env := d.APNSEnv
	if env != "sandbox" {
		env = "production"
	}
	body, _ := json.Marshal(relayproto.RegisterRequest{APNSToken: d.APNSToken, Env: env, Topic: d.Topic})
	resp, err := p.relay.Post(d.RelayURL+"/v1/register", "application/json", bytes.NewReader(body))
	if err != nil {
		return d, err
	}
	defer resp.Body.Close()
	var out struct {
		Grant string `json:"grant"`
		Error string `json:"error"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&out)
	if resp.StatusCode != 200 || out.Grant == "" {
		return d, fmt.Errorf("register: %d %s", resp.StatusCode, out.Error)
	}
	p.mu.Lock()
	cur, ok := p.devices[d.DeviceID]
	kept := ok && cur.RelayURL == d.RelayURL && cur.APNSToken == d.APNSToken
	if kept {
		if cur.RelayGrant != out.Grant {
			p.replaced[d.DeviceID] = cur.RelayGrant
		}
		cur.RelayGrant = out.Grant
		p.devices[d.DeviceID] = cur
	}
	p.mu.Unlock()
	if kept {
		p.save(d.DeviceID)
	}
	p.logf("relay %s: grant renewed", d.Name)
	d.RelayGrant = out.Grant
	return d, nil
}

// sealFor seals a push's payload to the device's key, stamped with when it was sealed (the phone shows only
// 「有新消息」 for one older than a day, so a replayed old one tells nothing new). Texts too long for one push (an
// ask_user question, a long command) are shortened until it fits: the full text is in the app.
func (p *Pusher) sealFor(d Device, payload map[string]any) (string, error) {
	pub, err := pushcrypto.PublicKey(d.PushKey)
	if err != nil {
		return "", err
	}
	maxPlain := relayproto.MaxSealed/4*3 - pushcrypto.MinSealed
	stamped := make(map[string]any, len(payload)+1)
	for k, v := range payload {
		stamped[k] = v
	}
	stamped["iat"] = p.now().Unix()
	plain, _ := json.Marshal(stamped)
	for limit := 1000; len(plain) > maxPlain; limit /= 2 {
		if limit < 20 {
			return "", fmt.Errorf("payload of %d bytes does not fit", len(plain))
		}
		stamped["aps"] = shortenAlert(stamped["aps"], limit)
		plain, _ = json.Marshal(stamped)
	}
	sealed, err := pushcrypto.Seal(pub, plain)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// shortenAlert returns a copy of aps whose alert texts have at most n characters (the original is shared by all devices).
func shortenAlert(v any, n int) any {
	aps, ok := v.(map[string]any)
	if !ok {
		return v
	}
	alert, ok := aps["alert"].(map[string]any)
	if !ok {
		return v
	}
	a2 := make(map[string]any, len(alert))
	for k, t := range alert {
		if s, ok := t.(string); ok {
			t = clip(s, n)
		}
		a2[k] = t
	}
	out := make(map[string]any, len(aps))
	for k, t := range aps {
		out[k] = t
	}
	out["alert"] = a2
	return out
}

// relayCollapseID: the relay sees a hash of the collapse id (an interaction id), not the id.
func relayCollapseID(id string) string {
	if id == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:16])
}

// prefs is a device's notification settings (defaults when it never set any).
func (p *Pusher) prefs(id string) NotifyPrefs {
	p.mu.Lock()
	defer p.mu.Unlock()
	return devicePrefs(p.devices[id].Prefs)
}

// setPrefs stores a device's notification settings (also for a device that has not registered a token yet).
func (p *Pusher) setPrefs(id string, prefs NotifyPrefs) error { return p.setPrefsBy(id, "", prefs) }

// setPrefsBy is setPrefs written with a device token (`credential`): a device that has none recorded takes it, so
// revoking the token forgets a device only its prefs made (a browser's 设置 › 通知, which never registers a push token).
// The row is written under saveMu, not mu: a slow database holds this one save (up to storeTimeout), not every
// other device's settings and every push. The settings take effect once the row is written.
func (p *Pusher) setPrefsBy(id, credential string, prefs NotifyPrefs) error {
	raw, _ := json.Marshal(prefs)
	p.saveMu.Lock()
	defer p.saveMu.Unlock()
	if p.store != nil {
		ctx, cancel := context.WithTimeout(context.Background(), p.storeTimeout)
		err := p.store.SetDevicePrefs(ctx, id, credential, raw)
		cancel()
		if err != nil {
			return err
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	d, ok := p.devices[id]
	if !ok {
		d = Device{DeviceID: id}
	}
	if d.Credential == "" {
		d.Credential = credential
	}
	d.Prefs = raw
	p.devices[id] = d
	return nil
}

// mailTargets are the languages some device wants its mail translated into (设置 › 翻译 on, mail in scope).
func (p *Pusher) mailTargets() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	seen := map[string]bool{}
	var out []string
	for _, d := range p.devices {
		t := devicePrefs(d.Prefs).Translate
		if t.Enabled && t.Mail && assistantwire.TranslateLanguages[t.Target] != "" && !seen[t.Target] {
			seen[t.Target] = true
			out = append(out, t.Target)
		}
	}
	sort.Strings(out)
	return out
}

// ready reports whether pushes can go anywhere (a device that can be reached: the hub's own key, or its relay).
func (p *Pusher) ready() bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, d := range p.devices {
		if p.reachable(d) {
			return true
		}
	}
	return false
}

// wake sends a silent background push: iOS gives the app ~30 s to connect and run a pending phone_* call.
// Best effort: the system throttles these and never delivers them to a force-quit app. Phones only: the phone_*
// tools run on the iPhone (a Mac registers as a desktop, docs/mac.md).
func (p *Pusher) wake(reason string) {
	p.sendAs(map[string]any{"aps": map[string]any{"content-available": 1}, "jarvis_wake": reason}, "", "background", "5",
		func(d Device) bool { return d.Kind != kindMac })
}

// wakeDevices preserves the iOS background push payload while limiting its recipients (the phone_* tools' wake).
func (p *Pusher) wakeDevices(reason string, ids map[string]bool) {
	if p == nil {
		return
	}
	p.mu.Lock()
	bound := false
	for _, d := range p.devices {
		if p.reachable(d) && (ids[d.DeviceID] || (d.CredentialID != "" && ids[d.CredentialID])) {
			bound = true
			break
		}
	}
	p.mu.Unlock()
	p.sendAs(map[string]any{"aps": map[string]any{"content-available": 1}, "jarvis_wake": reason}, "", "background", "5",
		func(d Device) bool {
			return d.Kind != kindMac && (ids[d.DeviceID] || (d.CredentialID != "" && ids[d.CredentialID]) || (!bound && ids["legacy"] && d.CredentialID == "" && d.Kind != "watch"))
		})
}

// sendAs pushes to every reachable device that to picks (nil: all of them). to runs outside p.mu: it may ask the hub.
func (p *Pusher) sendAs(payload map[string]any, collapseID, pushType, priority string, to func(Device) bool) {
	p.sendBuilt(func(i18n.Lang) map[string]any { return payload }, collapseID, pushType, priority, to)
}

// localized builds a push (or WebSocket notification) in a language.
type localized func(i18n.Lang) map[string]any

// langOf is the language of what is pushed to d: the one it registered with, else the user's (locale.go).
func (p *Pusher) langOf(d Device) i18n.Lang {
	if l := i18n.Lang(d.Locale); i18n.Valid(l) {
		return l
	}
	if p.userLang != nil {
		return p.userLang()
	}
	return i18n.Default
}

func (p *Pusher) sendBuilt(build localized, collapseID, pushType, priority string, to func(Device) bool) {
	if p == nil {
		return
	}
	p.mu.Lock()
	reachable := make([]Device, 0, len(p.devices))
	for _, d := range p.devices {
		if p.reachable(d) {
			reachable = append(reachable, d)
		}
	}
	p.mu.Unlock()
	devices := reachable[:0]
	for _, d := range reachable {
		if to == nil || to(d) {
			devices = append(devices, d)
		}
	}
	if len(devices) == 0 {
		return
	}
	type built struct {
		payload map[string]any
		body    []byte
	}
	byLang := map[i18n.Lang]built{}
	for _, d := range devices {
		lang := p.langOf(d)
		b, ok := byLang[lang]
		if !ok {
			b.payload = cleanAlert(build(lang))
			if p.hubID != nil {
				b.payload = maps.Clone(b.payload)
				b.payload["hub_id"] = p.hubID()
			}
			b.body, _ = json.Marshal(b.payload)
			byLang[lang] = b
		}
		payload, body := b.payload, b.body
		if p.relayed(d) {
			sealed, err := p.sealFor(d, payload)
			if err != nil {
				p.logf("relay %s: seal: %v", d.Name, err) // never falls back to sending it in the clear
				continue
			}
			go func(d Device) {
				res, err := p.postRelay(d, relayproto.PushRequest{Type: pushType, Priority: priority, CollapseID: relayCollapseID(collapseID), Sealed: sealed})
				if err != nil {
					p.logf("relay %s: %v", d.Name, err)
				} else if res.gone {
					p.forgetIfCurrent(d)
				}
			}(d)
			continue
		}
		go func(d Device) {
			sent := time.Now()
			res, err := p.direct.Send(context.Background(), apns.Request{Token: d.APNSToken, Env: d.APNSEnv, Topic: d.Topic,
				PushType: pushType, Priority: priority, CollapseID: collapseID, Body: body})
			if err != nil {
				p.logf("apns %s: %v", d.Name, err)
				return
			}
			if res.Status != 200 {
				p.logf("apns %s: %d %s", d.Name, res.Status, res.Reason)
				if res.Gone() {
					p.forgetIfCurrent(d)
				}
			} else { // when APNs took it: for measuring wake-up latency (docs/assistant.md)
				p.logf("apns %s: %s push accepted in %d ms", d.Name, pushType, time.Since(sent).Milliseconds())
			}
		}(d)
	}
}

func (p *Pusher) notifyInteraction(ix proto.Interaction, machine string) {
	p.sendLocalized(func(l i18n.Lang) map[string]any { return interactionPayloadIn(l, ix, machine) }, ix.ID, func(p NotifyPrefs) bool { return p.Input })
}

// interactionPayload is the alert for a new interaction. `category` must match a UNNotificationCategory the app
// registers (apple/Persona/App/NotificationActions.swift): that is what puts 允许 / 拒绝 / 批准 on the notification, and the
// buttons answer `interaction_id` through the resolve API.
func interactionPayload(ix proto.Interaction, machine string) map[string]any {
	return interactionPayloadIn(i18n.Default, ix, machine)
}

// interactionPayloadIn is interactionPayload written in l.
func interactionPayloadIn(l i18n.Lang, ix proto.Interaction, machine string) map[string]any {
	title := ""
	if key, ok := map[string]string{
		proto.IxElicitation: "push.elicitation_title",
		proto.IxQuestion:    "push.question_title", proto.IxPlan: "push.plan_title", proto.IxPermission: "push.permission_title", proto.IxScreen: "push.screen_title",
	}[ix.Kind]; ok {
		title = i18n.T(l, key)
	}
	if ix.ToolName == "ask_user" {
		title = i18n.T(l, "push.assistant_asks")
	}
	if ix.ToolName == workCardTool { // the manager asks on an agent's behalf (agents_link.go)
		title = i18n.T(l, "push.decide_title")
	}
	name := map[string]string{"codex": "Codex", "traex": "TRAEX"}[ix.Agent]
	if name == "" && ix.Agent != "" && ix.Agent != "claude" {
		name = ix.AgentLabel // an agent runtime's card (docs/runtimes.md): "DSH 需要你授权"
	}
	if name != "" {
		switch ix.Kind {
		case proto.IxQuestion:
			title = i18n.T(l, "push.agent_asks", i18n.Args{"agent": name})
		case proto.IxPermission:
			title = i18n.T(l, "push.agent_permission", i18n.Args{"agent": name})
		}
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
		body = screenAlertBody(l, ix)
	}
	if d := ix.Delegate; d != nil && d.Recommend != "" { // the assistant left it to the user (assistant/fleet_delegate.go)
		body += i18n.T(l, "push.delegate_recommend_suffix", i18n.Args{"answer": d.Recommend})
	}
	subtitle := transcript.Title(ix.SessionTitle)
	if inHealth(ix.Cwd) { // a health conversation's card: the lock screen does not say what it is about (assistant.go)
		subtitle, body = i18n.T(l, "push.health_title"), i18n.T(l, "push.health_body")
	}
	category := "INTERACTION_" + strings.ToUpper(ix.Kind)
	if len(ix.Fields) > 0 { // something to fill in first: no buttons on the notification, a tap opens the card
		category = "INTERACTION_INPUT"
		body += i18n.T(l, "push.fill_in_suffix")
	}
	return map[string]any{
		"aps": map[string]any{
			"alert":              map[string]any{"title": title, "subtitle": joinPlace(subtitle, machine), "body": body},
			"sound":              "default",
			"thread-id":          ix.SessionID,
			"category":           category,
			"interruption-level": "time-sensitive",
		},
		"interaction_id": ix.ID, "machine_id": ix.MachineID, "session_id": ix.SessionID,
	}
}

// screenAlertMost is how many characters of a screen card's dialog its push shows.
const screenAlertMost = 300

// screenAlertBody is a screen card's push body: what its dialog asks (the command or text above the question, then
// the question), as the Feishu card and the channels say it; the generic line when no dialog was read off the screen
// or the screen asks for a password.
func screenAlertBody(l i18n.Lang, ix proto.Interaction) string {
	if d := ix.Dialog; d != nil && !ix.Secret {
		// The question is kept whole; a long command above it is what gets cut.
		q := textfmt.Clip(strings.TrimSpace(d.Question), screenAlertMost)
		if detail := strings.TrimSpace(d.Detail); detail != "" {
			if room := screenAlertMost - utf8.RuneCountInString(q); room > 0 {
				q = strings.TrimSpace(textfmt.Clip(detail, room) + "\n" + q)
			}
		}
		if q != "" {
			return q
		}
	}
	return i18n.T(l, "push.screen_body")
}

func (p *Pusher) notifyDone(machineID, machine, sessionID, title, text string, wants func(NotifyPrefs) bool) {
	p.sendLocalized(func(l i18n.Lang) map[string]any { return donePayloadIn(l, machineID, machine, sessionID, title, text) }, "", wants)
}

func donePayloadIn(l i18n.Lang, machineID, machine, sessionID, title, text string) map[string]any {
	text = plainMarkdown(text) // an agent's last words are Markdown; a notification shows them as they are
	if text == "" {
		text = i18n.T(l, "push.done_body")
	}
	return map[string]any{
		"aps": map[string]any{
			"alert":     map[string]any{"title": title, "subtitle": machine, "body": text},
			"sound":     "default",
			"thread-id": sessionID,
		},
		"machine_id": machineID, "session_id": sessionID,
	}
}

func (p *Pusher) clear(string) {}

// joinPlace is "会话 · 机器" without the parts that are empty.
func joinPlace(parts ...string) string {
	return strings.Join(slices.DeleteFunc(parts, func(s string) bool { return strings.TrimSpace(s) == "" }), " · ")
}

// cleanAlert is payload with the context blocks a session title or message can carry taken out of its alert's
// lines (a headless conversation's title is its first message, context blocks and all): a notification shows
// what the user wrote, and a line left empty is not shown. Every alert goes out through here (APNs, notifyMessage).
func cleanAlert(payload map[string]any) map[string]any {
	aps, _ := payload["aps"].(map[string]any)
	alert, _ := aps["alert"].(map[string]any)
	if alert == nil {
		return payload
	}
	clean := maps.Clone(alert)
	for _, k := range []string{"title", "subtitle", "body"} {
		v, ok := clean[k].(string)
		if !ok || !strings.Contains(v, "<") {
			continue
		}
		if v = transcript.StripInjected(v); v == "" {
			delete(clean, k)
		} else {
			clean[k] = v
		}
	}
	aps = maps.Clone(aps)
	aps["alert"] = clean
	payload = maps.Clone(payload)
	payload["aps"] = aps
	return payload
}

package hub

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/apierr"
	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/i18n"
)

// Pairing two hubs (docs/archive/operations/plan-hub-mesh.md 2.2, docs/server/mesh.md "Pairing"): hub B makes a one-time code good for 10 minutes
// (admin page, `personad hub pair --peer`), a link carrying B's addresses, hub_id and key pin. Hub A, given the code,
// connects to B pinned to that key, hands over a credential it issued for B and its own pin and addresses, and gets
// B's credential for A in return. Both sides then pin each other, hold each other's credential and dial each other.
// A credential of kind and role peer opens /api/v1/peer and nothing else; only its hash is kept.

const (
	rolePeer     = "peer"      // another hub's mesh link (peer_link.go)
	rolePeerPair = "peer_pair" // a pairing code: all it can do is pair (POST /api/v1/peer/pair)
	peerCodeLife = 10 * time.Minute
)

// peerCode is a pairing code as the admin page shows it.
type peerCode struct {
	Code      string    `json:"code"` // persona://peer?…
	ExpiresAt time.Time `json:"expires_at"`
}

// peerInfo is what each side tells the other when pairing.
type peerInfo struct {
	HubID     string   `json:"hub_id"`
	Name      string   `json:"name"`
	Version   string   `json:"version,omitempty"`
	FP        string   `json:"fp"`
	TLSPort   int      `json:"tls_port"`
	Endpoints []string `json:"endpoints"`
	// PlainEndpoints: http:// addresses the other hub may use without TLS (mesh.plaintext, peer_plain.go).
	PlainEndpoints []string `json:"plain_endpoints,omitempty"`
	Token          string   `json:"token"` // the credential the sender issued to the other hub
	// Center and Term: the sender's group (mesh_group.go). The hub that used the code joins the code's group.
	Center string `json:"center,omitempty"`
	Term   int64  `json:"term,omitempty"`
}

func (h *Hub) selfPeerInfo() peerInfo {
	port, fp := h.tlsPin()
	g := h.group()
	info := peerInfo{HubID: h.hubID(), Name: h.hubName(), Version: h.version, FP: fp, TLSPort: port, Center: g.Center, Term: g.Term,
		PlainEndpoints: h.plainEndpoints()}
	for _, e := range h.endpointList() {
		if e.Scheme == "https" {
			info.Endpoints = append(info.Endpoints, e.URL)
		}
	}
	return info
}

// canPeer: this hub can be paired and linked with: it has a TLS key, or plaintext links are on (mesh.plaintext).
func (h *Hub) canPeer(info peerInfo) bool {
	return (info.TLSPort != 0 && info.FP != "") || h.plainPeersOn()
}

// newPeerCode makes a pairing code for this hub.
func (h *Hub) newPeerCode(ctx context.Context) (*peerCode, error) {
	ps := h.peerStore()
	if ps == nil {
		return nil, errors.New("no database")
	}
	info := h.selfPeerInfo()
	if !h.canPeer(info) {
		return nil, errNoTLS
	}
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return nil, err
	}
	code := "jx_" + hex.EncodeToString(secret[:])
	expires := time.Now().UTC().Add(peerCodeLife)
	if err := ps.CreatePeerCode(ctx, hashToken(code), expires); err != nil {
		return nil, err
	}
	q := url.Values{"hub": {info.HubID}, "name": {info.Name}, "fp": {info.FP}, "tls": {strconv.Itoa(info.TLSPort)}, "code": {code}}
	for i, e := range info.Endpoints {
		if i == 6 {
			break
		}
		q.Add("url", e)
	}
	for i, e := range info.PlainEndpoints { // hubs before #4514 skip these
		if i == 3 {
			break
		}
		q.Add("url", e)
	}
	return &peerCode{Code: "persona://peer?" + q.Encode(), ExpiresAt: expires}, nil
}

var errNoTLS = i18n.Err("hub-peer_pair.no_tls")

type parsedPeerCode struct {
	hubID, name, fp, secret string
	tlsPort                 int
	endpoints               []string
}

func parsePeerCode(raw string) (*parsedPeerCode, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "persona" || u.Host != "peer" {
		return nil, i18n.Err("hub-peer_pair.not_a_code")
	}
	q := u.Query()
	p := &parsedPeerCode{hubID: q.Get("hub"), name: q.Get("name"), fp: q.Get("fp"), secret: q.Get("code")}
	p.tlsPort, _ = strconv.Atoi(q.Get("tls"))
	if p.hubID == "" || !strings.HasPrefix(p.secret, "jx_") {
		return nil, i18n.Err("hub-peer_pair.incomplete")
	}
	p.endpoints = rankEndpoints(q["url"])
	if len(p.endpoints) == 0 {
		return nil, i18n.Err("hub-peer_pair.no_address")
	}
	// No fp only from a hub without a TLS key, which hands out http:// addresses alone (mesh.plaintext).
	if p.fp == "" && slices.ContainsFunc(p.endpoints, func(e string) bool { return !isPlainEndpoint(e) }) {
		return nil, i18n.Err("hub-peer_pair.incomplete")
	}
	return p, nil
}

// joinPeer pairs this hub with the hub whose code it was given (this hub is A).
func (h *Hub) joinPeer(ctx context.Context, raw string) (*db.Peer, error) {
	code, err := parsePeerCode(raw)
	if err != nil {
		return nil, err
	}
	ps := h.peerStore()
	if ps == nil || h.store == nil {
		return nil, errors.New("no database")
	}
	me := h.selfPeerInfo()
	if !h.canPeer(me) {
		return nil, errNoTLS
	}
	eps := h.peerDialable(code.endpoints)
	if len(eps) == 0 {
		return nil, errPlainNotAllowed
	}
	if code.hubID == me.HubID {
		return nil, i18n.Err("hub-peer_pair.own_code")
	}
	if old, had := h.pairedWith(code.hubID); had && old.FP != code.fp {
		return nil, i18n.Err("hub-peer_pair.fingerprint_changed")
	}
	tok := "jd_" + randomHex(24)
	cred := db.DeviceToken{ID: "dev_" + randomHex(6), Name: peerCredName(code.name, code.hubID), Kind: "peer", Role: rolePeer, MachineID: code.hubID}
	if err := h.store.CreateDeviceToken(ctx, cred, hashToken(tok)); err != nil {
		return nil, err
	}
	me.Token = tok
	body, _ := json.Marshal(me)
	client := h.peerClient(code.fp)
	defer client.CloseIdleConnections()
	got, address, err := raceEndpoints(ctx, eps, func(ctx context.Context, ep string) (peerInfo, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, ep+peerPairPath, bytes.NewReader(body))
		if err != nil {
			return peerInfo{}, err
		}
		req.Header.Set("Authorization", "Bearer "+code.secret)
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return peerInfo{}, err
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		if resp.StatusCode != http.StatusOK {
			why := apierr.Reason(b) // the other hub's cause, not the summary it localized it into (#5728)
			if resp.StatusCode == http.StatusUnauthorized {
				why = i18n.T(h.contextLang(ctx), "hub-peer_pair.code_invalid")
			}
			if why == "" {
				why = resp.Status
			}
			return peerInfo{}, i18n.Err("hub-peer_pair.refused", i18n.Args{"err": why})
		}
		var out peerInfo
		if err := json.Unmarshal(b, &out); err != nil || out.Token == "" {
			return peerInfo{}, i18n.Err("hub-peer_pair.no_credentials")
		}
		return out, nil
	}, func(peerInfo) {})
	if err == nil && got.HubID != code.hubID {
		err = i18n.Err("hub-peer_pair.wrong_hub", i18n.Args{"got": strconv.Quote(got.HubID), "want": strconv.Quote(code.hubID)})
	}
	if err != nil {
		_, _ = h.store.RevokeDeviceToken(context.WithoutCancel(ctx), cred.ID)
		return nil, err
	}
	sealed, err := h.sealKey(got.Token)
	if err != nil {
		return nil, err
	}
	p := db.Peer{HubID: got.HubID, Name: got.Name, FP: code.fp, Endpoints: rankEndpoints(slices.Concat(got.Endpoints, got.PlainEndpoints, code.endpoints)),
		Token: sealed, CredentialID: cred.ID}
	// Using its code joins this hub, and its group with it, to that hub's group (plan-hub-center.md 2): before the
	// link comes up, so the records it brings agree.
	center := got.Center
	if center == "" {
		center = got.HubID
	}
	h.setGroup(center, got.Term, "joined the group of "+clip(oneLine(got.Name), 40))
	if err := h.savePeer(ctx, p); err != nil {
		return nil, err
	}
	h.logf("mesh: paired with %q (%s) via %s", got.Name, got.HubID, address)
	return &p, nil
}

func peerCredName(name, hubID string) string {
	if strings.TrimSpace(name) == "" {
		name = shortID(hubID)
	}
	return "互联：" + clip(oneLine(name), 60)
}

// savePeer keeps a new pairing (replacing an earlier one with the same hub, whose credential is revoked) and starts
// dialing it.
func (h *Hub) savePeer(ctx context.Context, p db.Peer) error {
	ps := h.peerStore()
	ms := &h.mesh
	ms.mu.Lock()
	old, had := ms.peers[p.HubID]
	ms.mu.Unlock()
	if err := ps.SavePeer(ctx, p); err != nil {
		return err
	}
	if had && old.CredentialID != "" && old.CredentialID != p.CredentialID {
		if _, err := h.store.RevokeDeviceToken(ctx, old.CredentialID); err != nil {
			h.logf("mesh: revoke the old credential of %s: %v", p.HubID, err)
		}
		h.forgetToken(old.CredentialID)
		h.closePeerLinksOf(old.CredentialID)
	}
	h.peerSaved(p)
	return nil
}

// peerSaved puts a pairing into the running mesh: the old link (other credentials) goes, the dialer starts.
func (h *Hub) peerSaved(p db.Peer) {
	ms := &h.mesh
	ms.mu.Lock()
	if ms.peers == nil {
		ms.peers = map[string]db.Peer{}
	}
	ms.peers[p.HubID] = p
	delete(ms.dialErr, p.HubID)
	old := ms.links[p.HubID]
	ms.mu.Unlock()
	if old != nil {
		old.close(peerCloseDuplicate, "paired again")
	}
	h.startDialer(p.HubID)
	h.meshTouch()
}

// servePeerPair is POST /api/v1/peer/pair: another hub (A) brings this hub's code (this hub is B).
func (h *Hub) servePeerPair(w http.ResponseWriter, r *http.Request) {
	p := principalOf(r.Context())
	if p.Role != rolePeerPair || p.PairHash == "" {
		writeErr(w, http.StatusForbidden, errors.New("pairing two hubs needs a pairing code"))
		return
	}
	if r.TLS == nil && !h.plainPeerRequest(r) {
		h.securityError(w, r, http.StatusForbidden, "tls_required", 0)
		return
	}
	ps := h.peerStore()
	if ps == nil || h.store == nil {
		writeErr(w, http.StatusServiceUnavailable, errors.New("no database"))
		return
	}
	var req peerInfo
	if err := decodeJSONBody(w, r, &req, 64<<10); err != nil {
		writeBodyErr(w, err, err)
		return
	}
	me := h.selfPeerInfo()
	switch {
	case !h.canPeer(me):
		writeErr(w, http.StatusConflict, errNoTLS)
		return
	case req.HubID == "" || req.HubID == me.HubID || (req.FP == "" && !h.plainPeersOn()) || !strings.HasPrefix(req.Token, "jd_"):
		writeErr(w, http.StatusBadRequest, errors.New("hub_id, fp and token are required, and the hub_id must be another hub's"))
		return
	}
	sealed, err := h.sealKey(req.Token)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	tok := "jd_" + randomHex(24)
	cred := db.DeviceToken{ID: "dev_" + randomHex(6), Name: peerCredName(req.Name, req.HubID), Kind: "peer", Role: rolePeer, MachineID: req.HubID}
	peer := db.Peer{HubID: req.HubID, Name: clip(oneLine(req.Name), 64), FP: req.FP, Endpoints: rankEndpoints(slices.Concat(req.Endpoints, req.PlainEndpoints)),
		Token: sealed, CredentialID: cred.ID}
	old, had := h.pairedWith(req.HubID)
	if had && old.FP != req.FP {
		// Whoever holds a code must not take over a paired hub's place by claiming its hub_id.
		writeErr(w, http.StatusConflict, errors.New("already paired with that hub_id under another key: unpair it first"))
		return
	}
	ok, err := ps.RedeemPeerCode(r.Context(), p.PairHash, peer, cred, hashToken(tok))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if !ok {
		h.securityError(w, r, http.StatusUnauthorized, "", 0)
		return
	}
	if had && old.CredentialID != cred.ID {
		h.forgetToken(old.CredentialID)
		h.closePeerLinksOf(old.CredentialID)
	}
	h.peerSaved(peer)
	h.logf("mesh: paired with %q (%s)", peer.Name, peer.HubID)
	me.Token = tok
	writeJSON(w, me)
}

// unpairPeer ends a pairing from this side: the credential this hub issued is revoked, the other hub told (when it
// can be reached), the link closed and the dialing stopped.
func (h *Hub) unpairPeer(ctx context.Context, hubID string, tell bool) (bool, error) {
	ms := &h.mesh
	ms.mu.Lock()
	row, ok := ms.peers[hubID]
	ms.mu.Unlock()
	if !ok {
		return false, nil
	}
	if tell {
		if l := h.peerLinkTo(hubID); l != nil {
			cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
			_ = l.rpc.Call(cctx, "unpair", map[string]any{}, nil)
			cancel()
		}
	}
	if ps := h.peerStore(); ps != nil {
		if _, err := ps.DeletePeer(ctx, hubID); err != nil {
			return false, err
		}
	}
	ms.mu.Lock()
	delete(ms.peers, hubID)
	delete(ms.dialErr, hubID)
	l := ms.links[hubID]
	ms.mu.Unlock()
	h.stopDialer(hubID)
	if row.CredentialID != "" && h.store != nil {
		if _, err := h.store.RevokeDeviceToken(ctx, row.CredentialID); err != nil {
			h.logf("mesh: revoke the credential of %s: %v", hubID, err)
		}
		h.forgetToken(row.CredentialID)
	}
	if l != nil {
		l.close(peerCloseUnpaired, "unpaired")
		ms.mu.Lock()
		if ms.links[hubID] == l {
			delete(ms.links, hubID)
		}
		ms.mu.Unlock()
	}
	h.meshTouch()
	h.meshRebuild()
	if v := h.meshView(); v != nil && !v.reachable(hubID) {
		ms.mu.Lock()
		delete(ms.records, hubID)
		ms.dirty = true
		ms.mu.Unlock()
		h.meshRebuild()
	}
	h.logf("mesh: unpaired %q (%s)", row.Name, hubID)
	h.leftGroup() // no peer left: this hub is its own center again
	h.fleetRegroup()
	return true, nil
}

// peerUnpaired: the other hub ended the pairing; this side forgets it too.
func (h *Hub) peerUnpaired(hubID string) {
	if _, err := h.unpairPeer(context.Background(), hubID, false); err != nil {
		h.logf("mesh: %s unpaired this hub: %v", hubID, err)
	}
}

// peerForCredential is the paired hub a credential was issued to ("" for none).
func (h *Hub) peerForCredential(id string) string {
	ms := &h.mesh
	ms.mu.Lock()
	defer ms.mu.Unlock()
	for hid, p := range ms.peers {
		if p.CredentialID == id {
			return hid
		}
	}
	return ""
}

// forgetToken drops a credential's cached lookups.
func (h *Hub) forgetToken(id string) {
	t := &h.devtok
	t.mu.Lock()
	for k, c := range t.cache {
		if c.p != nil && c.p.ID == id {
			delete(t.cache, k)
		}
	}
	t.mu.Unlock()
}

// peerPairPrincipal is the principal of a hub pairing code (jx_…), valid until used or expired.
func (h *Hub) peerPairPrincipal(ctx context.Context, tok string) (principal, bool) {
	ps := h.peerStore()
	if ps == nil {
		return principal{}, false
	}
	hash := hashToken(tok)
	ok, err := ps.PeerCodeValid(ctx, hash)
	if err != nil || !ok {
		return principal{}, false
	}
	return principal{ID: "peerpair_" + hash[:12], Role: rolePeerPair, PairHash: hash}, true
}

// pairedWith is the pairing with a hub, if there is one.
func (h *Hub) pairedWith(hubID string) (db.Peer, bool) {
	ms := &h.mesh
	ms.mu.Lock()
	defer ms.mu.Unlock()
	p, ok := ms.peers[hubID]
	return p, ok
}

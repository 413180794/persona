package hub

import (
	"context"
	"time"

	"github.com/nwverse/persona/server/internal/db"
)

// peerStore is where the mesh keeps its peers and pairing codes (internal/db/peers.go on Postgres; tests have their
// own). It is not part of Store so the many test stores need not implement it: a hub whose store lacks it cannot pair.
type peerStore interface {
	Peers(ctx context.Context) ([]db.Peer, error)
	SavePeer(ctx context.Context, p db.Peer) error
	UpdatePeerSeen(ctx context.Context, hubID, name string, endpoints []string) error
	DeletePeer(ctx context.Context, hubID string) (bool, error)
	CreatePeerCode(ctx context.Context, hash string, expiresAt time.Time) error
	PeerCodeValid(ctx context.Context, hash string) (bool, error)
	RedeemPeerCode(ctx context.Context, codeHash string, p db.Peer, cred db.DeviceToken, credHash string) (bool, error)
	PrunePeerCodes(ctx context.Context, before time.Time) error
}

func (h *Hub) peerStore() peerStore {
	ps, _ := h.store.(peerStore)
	return ps
}

// centerStore keeps the group's center (mesh_group.go), the credentials handed to the center and the connectors this
// hub dials (service.go): internal/db/center.go, migration 0191. Optional like peerStore.
type centerStore interface {
	MeshGroup(ctx context.Context, self string) (center string, term int64, ok bool, err error)
	SaveMeshGroup(ctx context.Context, self, center string, term int64) error
	AppTokens(ctx context.Context) ([]db.ExportedToken, error)
	ImportDeviceTokens(ctx context.Context, list []db.ExportedToken) (int, error)
	RevokedAppTokens(ctx context.Context) ([]db.ExportedToken, error)
	ApplyRevokedTokens(ctx context.Context, list []db.ExportedToken) ([]string, error)
	ConnectorCredential(ctx context.Context, machine string) (*db.ConnectorCredential, error)
	SaveConnectorCredential(ctx context.Context, c db.ConnectorCredential) error
	DeleteConnectorCredential(ctx context.Context, machine string) (bool, error)
}

// Asserted here because centerStore() finds out at run time: a method gone astray would turn the whole of it off.
var _ centerStore = (*db.DB)(nil)

func (h *Hub) centerStore() centerStore {
	cs, _ := h.store.(centerStore)
	return cs
}

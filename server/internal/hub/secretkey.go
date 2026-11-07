package hub

import (
	"path/filepath"
	"sync"

	"github.com/nwverse/persona/server/internal/secretkey"
)

// secret.key in the hub's data directory seals the hub's own secrets in Postgres (the peers' tokens, the computers'
// connector credentials): internal/secretkey. The manager seals what is its own (the model providers' keys,
// internal/assistant; the extensions' secrets, the connectors' credentials and the bots' app secrets, internal/extmgr)
// with its own key, a copy of this one at first and rotated to its own once its data is in its own database
// (docs/assistant/managers.md "Its own secret.key"): the hub never opens those, and this key is never rotated with it.

type secretState struct {
	once sync.Once
	box  *secretkey.Box
}

func (h *Hub) secretBox() *secretkey.Box {
	h.secret.once.Do(func() { h.secret.box = secretkey.New(filepath.Join(h.cfg.dir, "secret.key")) })
	return h.secret.box
}

func (h *Hub) sealKey(plain string) (string, error) { return h.secretBox().Seal(plain) }

func (h *Hub) openKey(sealed string) (string, error) { return h.secretBox().Open(sealed) }

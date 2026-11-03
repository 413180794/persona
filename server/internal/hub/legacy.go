package hub

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/proto"
)

// importLegacy moves state from the pre-Postgres JSON files into the database once, then renames the files.
func (h *Hub) importLegacy(ctx context.Context) {
	h.importFile("phone_tools.json", func(b []byte) error {
		var tools []proto.Tool
		if err := json.Unmarshal(b, &tools); err != nil {
			return err
		}
		return h.store.ReplacePhoneTools(ctx, db.PhoneToolRegistration{DeviceID: "legacy", Tools: tools})
	})
	h.importFile("devices.json", func(b []byte) error {
		var devices map[string]Device
		if err := json.Unmarshal(b, &devices); err != nil {
			return err
		}
		for _, d := range devices {
			if err := h.store.UpsertDevice(ctx, d); err != nil {
				return err
			}
		}
		return nil
	})
}

func (h *Hub) importFile(name string, load func([]byte) error) {
	path := filepath.Join(h.cfg.dir, name)
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	if err := load(b); err != nil {
		h.logf("import %s: %v", name, err)
		return
	}
	_ = os.Rename(path, path+".migrated")
	h.logf("imported %s into postgres", name)
}

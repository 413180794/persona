package hub

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/nwverse/persona/server/internal/compat"
	"github.com/nwverse/persona/server/internal/update"
)

func (u *updater) nodesEnabled() bool {
	return u.h.cfg != nil && u.h.cfg.AutoUpdateNodes
}

// stageNodes never fetches or recreates the node package directory unless fleet
// updates are explicitly enabled. Hub-only releases can contain just one platform.
func (u *updater) stageNodes(ctx context.Context, m *update.Manifest, staged, bin string) (string, error) {
	if !u.nodesEnabled() {
		return "", nil
	}
	nodes := filepath.Join(staged, "nodes")
	own := runtime.GOOS + "-" + runtime.GOARCH
	for _, plat := range nodePlatforms(m) {
		u.set("downloading", plat, "")
		if plat == own {
			// This platform's node binary is the hub's own binary: the same file, so copy the one already here
			// rather than fetching it twice (a machine on the hub's platform added later needs it too).
			err := os.MkdirAll(nodes, 0o755)
			if err == nil {
				err = copyInto(bin, filepath.Join(nodes, "personad-"+plat))
				if err == nil {
					err = copyInto(bin, filepath.Join(nodes, compat.DaemonBinary+"-"+plat))
				}
			}
			if err != nil {
				return "", fmt.Errorf("the node binary for the hub's own platform: %w", err)
			}
			continue
		}
		if _, err := update.Download(ctx, u.client, u.h.build.UpdateURL, m.Files[plat], nodes); err != nil {
			return "", err
		}
	}
	return nodes, nil
}

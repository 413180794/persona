package node

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// Where a client may change files (uploads into a folder, and later renames, moves, deletes; docs/protocol.md
// "Changing files"): inside the user's home only, once the read guard (pathguard.go) has followed every link, and never in
// the folders that run Persona and the Mac itself. Outside the home everything stays read-only.

// noWriteDirs are folders under the home that are never written to, whole: the hub's and the node's own state, and
// the Mac's Library (preferences, keychains, app data) that nothing here should edit by hand.
var noWriteDirs = []string{".persona", ".jarvis", "Library"}

func errReadOnly(path string) error {
	return &rpc.Error{Code: proto.CodeReadOnly, Message: fmt.Sprintf("%s is read-only from the phone (only folders in the home are writable)", path)}
}

// writable says whether real (absolute, clean, links already followed) is a place a client may change.
func (n *Node) writable(real string) error {
	if err := n.offLimits(real); err != nil {
		return err
	}
	if n.home == "" {
		return errReadOnly(real)
	}
	p := fold(real)
	rel, ok := homeRel(p, n.home)
	if !ok {
		if isHome(p, n.home) {
			return nil
		}
		return errReadOnly(real)
	}
	for _, d := range noWriteDirs {
		if d = strings.ToLower(d); rel == d || strings.HasPrefix(rel, d+"/") {
			return errReadOnly(real)
		}
	}
	return nil
}

// isHome: the folded path p is the home itself, as written or with its links followed.
func isHome(p, home string) bool {
	home = filepath.Clean(home)
	if p == fold(home) {
		return true
	}
	real, err := filepath.EvalSymlinks(home)
	return err == nil && p == fold(real)
}

// writableDir resolves a folder to write into like a listing does (~/, relative to cwd) and checks it is an
// existing directory a client may change. shown is the path as the app knows it, real the one behind its links.
func (n *Node) writableDir(path, cwd string) (shown, real string, err error) {
	if strings.TrimSpace(path) == "" {
		return "", "", errors.New("empty path")
	}
	shown, real, err = n.resolveDir(path, cwd)
	if err != nil {
		return "", "", err
	}
	if err := n.writable(real); err != nil {
		return "", "", err
	}
	st, err := os.Stat(real)
	if err != nil {
		return "", "", explainDenied(shown, n.home, err)
	}
	if !st.IsDir() {
		return "", "", fmt.Errorf("%s is not a folder", shown)
	}
	return shown, real, nil
}

// errExists: a file of that name is already in the folder; the app asks to replace it or keep both.
func errExists(name string) error {
	data, _ := json.Marshal(map[string]any{"name": name})
	return &rpc.Error{Code: proto.CodeExists, Message: fmt.Sprintf("%s already exists", name), Data: data}
}

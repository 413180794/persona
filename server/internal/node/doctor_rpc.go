package node

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/nwverse/persona/server/internal/nodeclient"
)

// The web machine page's "What this computer can do" card asks the node for the same report `personad node doctor` prints
// (docs/machines.md "Self-check"). The checks run in this process, so the sockets are looked at by the node that owns them.
// node.doctor reads and nothing else; the only thing either method may write is what `--fix` writes
// (writeGeneratedConfig, Persona's own generated files), and only when the caller says fix.

// doctorOptions are the places this node looks at: the directory and program it writes its generated files for, the
// sockets it serves, and the user's Claude Code settings (read, never written).
func (n *Node) doctorOptions() DoctorOptions {
	if n.doctorAt != nil {
		return n.doctorAt()
	}
	opts := DoctorOptions{
		Dir: n.dir, Bin: n.bin,
		NodeSock: nodeclient.SocketPath(), RuntimeSock: RuntimeSocketPath(),
		ClaudeConfigDir: claudeDirOf(n.home), ClaudeVersion: claudeVersion,
	}
	if def, err := DefaultDoctorOptions(); err == nil {
		opts.HerdrSock = def.HerdrSock
	}
	return opts
}

// doctor is node.doctor ({"fix": bool}) and node.doctor.fix (always fix). The report is DoctorReport as it is.
func (n *Node) doctor(ctx context.Context, params json.RawMessage, fix bool) (any, error) {
	var a struct {
		Fix bool `json:"fix"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &a); err != nil {
			return nil, errors.New("bad node.doctor params")
		}
	}
	opts := n.doctorOptions()
	opts.Fix = fix || a.Fix
	return RunDoctor(ctx, opts), nil
}

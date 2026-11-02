package node

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"runtime"
	"sync"

	"github.com/chyroc/jarvis/server/internal/connector/imessage"
	"github.com/chyroc/jarvis/server/internal/proto"
	"github.com/chyroc/jarvis/server/internal/rpc"
)

// Connectors whose data is only on this computer (docs/plan-connectors.md「第三种：node 当本地连接器」): the hub
// asks, this node reads. For now that is Messages on a Mac, read-only (connector/imessage). Only the hub asks — these
// are hub → node RPCs; the node socket of hooks and MCP does not route here — and nothing is logged: no number, no
// message.

var messagesReader = sync.OnceValue(func() *imessage.Reader {
	home, _ := os.UserHomeDir()
	exe, _ := os.Executable()
	// A resident node is started by launchd and has no ssh in its environment: Full Disk Access is then its own.
	via := "launchd"
	if os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_CLIENT") != "" {
		via = "ssh"
	}
	return &imessage.Reader{Home: home, Via: via, Executable: exe}
})

// localConnectorStatus is one entry of connector.list.
type localConnectorStatus struct {
	ID string `json:"id"`
	imessage.Status
}

func localConnector(ctx context.Context, method string, params json.RawMessage) (any, error) {
	return serveLocalConnector(ctx, messagesReader(), runtime.GOOS == "darwin", method, params)
}

// serveLocalConnector answers connector.list, connector.chats and connector.older with r (mac: this is a Mac).
func serveLocalConnector(ctx context.Context, r *imessage.Reader, mac bool, method string, params json.RawMessage) (any, error) {
	var p struct {
		ID     string `json:"id"`
		Chat   string `json:"chat"`
		Before string `json:"before"`
		Limit  int    `json:"limit"`
	}
	_ = json.Unmarshal(params, &p)
	if method == "connector.list" {
		list := []localConnectorStatus{}
		if mac {
			list = append(list, localConnectorStatus{ID: imessage.ID, Status: r.Status(ctx)})
		}
		return map[string]any{"connectors": list}, nil
	}
	if p.ID != imessage.ID || !mac {
		return nil, &rpc.Error{Message: "no connector " + p.ID + " on this machine"}
	}
	var res any
	var err error
	if method == "connector.chats" {
		res, err = r.Chats(ctx, p.Limit, p.Before)
	} else {
		res, err = r.Messages(ctx, p.Chat, p.Limit, p.Before)
	}
	switch {
	case errors.Is(err, imessage.ErrNoPermission):
		return nil, &rpc.Error{Code: proto.CodeNoPermission, Message: r.Status(ctx).Error}
	case errors.Is(err, imessage.ErrNoChat):
		return nil, &rpc.Error{Code: proto.CodeNoChat, Message: "no such conversation"}
	case err != nil:
		return nil, err
	}
	return res, nil
}

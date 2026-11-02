package hub

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/chyroc/jarvis/server/internal/connector/imessage"
	"github.com/chyroc/jarvis/server/internal/proto"
	"github.com/chyroc/jarvis/server/internal/rpc"
)

// 信息 (connector/imessage, docs/plan-connectors.md「五」): the app reads one Mac's Messages, through that Mac's node,
// for the user's own eyes. The hub only passes pages along: it stores nothing, gives nothing to a model, and logs no
// number or message (only the connector, the Mac and the kind of failure).

func (h *Hub) addIMessage(cfg imessage.Config) error {
	name := cfg.Machine
	if m := h.byID[cfg.Machine]; m != nil {
		name = m.cfg.Name
	}
	return h.addConnector(imessage.NewRemote(cfg, name, h.imessageCall(cfg.Machine)))
}

var errNotAMachine = errors.New("imessage.machine is not a machine in hub.json")

// imessageCall asks the Mac's node, turning its failures into the connector's errors.
func (h *Hub) imessageCall(machine string) imessage.Caller {
	return func(ctx context.Context, method string, params, out any) error {
		m := h.byID[machine]
		if m == nil {
			return errNotAMachine
		}
		err := m.call(ctx, method, params, out)
		var re *rpc.Error
		switch {
		case err == nil:
			return nil
		case errors.Is(err, errOffline):
			return imessage.ErrOffline
		case errors.As(err, &re) && strings.HasPrefix(re.Message, "unknown method"):
			return imessage.ErrNodeTooOld
		case errors.As(err, &re) && re.Code == proto.CodeNoPermission:
			return imessage.ErrNoPermission
		case errors.As(err, &re) && re.Code == proto.CodeNoChat:
			return imessage.ErrNoChat
		}
		return fmt.Errorf("%s: %w", method, err)
	}
}

func (h *Hub) imessage() *imessage.Remote {
	c, _ := h.connectors.Get(imessage.ID).(*imessage.Remote)
	return c
}

func (h *Hub) imessageRoutes(api *http.ServeMux) {
	// GET /api/v1/imessage: the status with the steps to fix it (设置 › 连接器 › 信息), asked of the Mac now: the page
	// is opened (or 重新检查 is tapped) right after the permission was given.
	api.HandleFunc("GET /api/v1/imessage", func(w http.ResponseWriter, r *http.Request) {
		c := h.imessage()
		if c == nil {
			writeCode(w, http.StatusNotFound, "not_configured", "没有设置「信息」：在 hub.json 的 imessage.machine 写要读哪台 Mac", nil)
			return
		}
		c.Check(r.Context())
		writeJSON(w, c.Detail(r.Context()))
	})
	api.HandleFunc("GET /api/v1/imessage/chats", func(w http.ResponseWriter, r *http.Request) {
		c := h.imessage()
		if c == nil {
			writeCode(w, http.StatusNotFound, "not_configured", "没有设置「信息」", nil)
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		page, err := c.Chats(r.Context(), limit, r.URL.Query().Get("before"))
		if err != nil {
			h.imessageFailed(w, c, "chats", err)
			return
		}
		writeJSON(w, page)
	})
	api.HandleFunc("GET /api/v1/imessage/chats/{id}/messages", func(w http.ResponseWriter, r *http.Request) {
		c := h.imessage()
		if c == nil {
			writeCode(w, http.StatusNotFound, "not_configured", "没有设置「信息」", nil)
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		page, err := c.Messages(r.Context(), r.PathValue("id"), limit, r.URL.Query().Get("before"))
		if err != nil {
			h.imessageFailed(w, c, "messages", err)
			return
		}
		writeJSON(w, page)
	})
}

// imessageFailed answers a read that failed, with a code the app acts on; only the kind of failure is logged.
func (h *Hub) imessageFailed(w http.ResponseWriter, c *imessage.Remote, what string, err error) {
	msg := c.Explain(err)
	switch {
	case errors.Is(err, imessage.ErrNoChat):
		writeCode(w, http.StatusNotFound, proto.CodeNoChat, msg, nil)
	case errors.Is(err, imessage.ErrNoPermission):
		writeCode(w, http.StatusForbidden, proto.CodeNoPermission, msg, nil)
	case errors.Is(err, imessage.ErrNodeTooOld):
		writeCode(w, http.StatusConflict, proto.CodeNodeOutdated, msg, nil)
	case errors.Is(err, imessage.ErrOffline):
		writeCode(w, http.StatusServiceUnavailable, "offline", msg, nil)
	default:
		h.logf("imessage: %s: %v", what, err)
		writeErr(w, http.StatusBadGateway, errors.New(msg))
	}
}

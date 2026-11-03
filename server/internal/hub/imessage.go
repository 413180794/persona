package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/nwverse/persona/server/internal/connector/imessage"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// 信息 (connector/imessage, docs/plan-connectors.md「五」): the app reads one Mac's Messages through that Mac's node;
// the hub passes the pages along and keeps none of them. With 交给 Jarvis on (the user's switch, on by default,
// imessageAIKey) the real conversations also go to the knowledge base, the inbox and the assistant's tools
// (connector/imessage/jarvis.go). No number or message is logged (only the connector, the Mac and the kind of failure).

// imessageAIKey is the settings key of 交给 Jarvis: absent means on.
const imessageAIKey = "imessage.ai"

func (h *Hub) addIMessage(cfg imessage.Config) error {
	name := cfg.Machine
	if m := h.localMachine(cfg.Machine); m != nil {
		name = m.cfg.Name
	}
	c := imessage.NewRemote(cfg, name, h.imessageCall(cfg.Machine))
	c.Zone = h.userLocation
	c.SetAI(h.imessageAI(context.Background()))
	return h.addConnector(c)
}

// imessageAI is the user's 交给 Jarvis choice: on unless they turned it off.
func (h *Hub) imessageAI(ctx context.Context) bool {
	on := true
	if h.store != nil {
		var v bool
		if ok, err := h.store.Setting(ctx, imessageAIKey, &v); err == nil && ok {
			on = v
		}
	}
	return on
}

var errNotAMachine = errors.New("imessage.machine is not a machine in hub.json")

// imessageCall asks the Mac's node, turning its failures into the connector's errors.
func (h *Hub) imessageCall(machine string) imessage.Caller {
	return func(ctx context.Context, method string, params, out any) error {
		m := h.localMachine(machine)
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
	// PUT /api/v1/imessage/settings {"ai": bool}: 交给 Jarvis on or off. Off stops what goes to Jarvis at once; what
	// the hub already keeps stays until DELETE /api/v1/imessage/data.
	api.HandleFunc("PUT /api/v1/imessage/settings", func(w http.ResponseWriter, r *http.Request) {
		c := h.imessage()
		if c == nil {
			writeCode(w, http.StatusNotFound, "not_configured", "没有设置「信息」", nil)
			return
		}
		var in struct {
			AI *bool `json:"ai"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.AI == nil {
			writeErr(w, http.StatusBadRequest, errors.New("ai: true or false"))
			return
		}
		if h.store == nil {
			writeErr(w, http.StatusServiceUnavailable, errors.New("no database"))
			return
		}
		if err := h.store.PutSetting(r.Context(), imessageAIKey, *in.AI); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		c.SetAI(*in.AI)
		h.logf("imessage: 交给 Jarvis %v", *in.AI)
		if *in.AI {
			h.kbKick()
			h.kickInbox()
		}
		writeJSON(w, c.Detail(r.Context()))
	})
	// DELETE /api/v1/imessage/data: what the hub kept of iMessage (the knowledge base's chats and people, inbox items),
	// once 交给 Jarvis is off — on, the next sync would bring it back.
	api.HandleFunc("DELETE /api/v1/imessage/data", func(w http.ResponseWriter, r *http.Request) {
		c := h.imessage()
		if c == nil {
			writeCode(w, http.StatusNotFound, "not_configured", "没有设置「信息」", nil)
			return
		}
		if c.AI() {
			writeCode(w, http.StatusConflict, "ai_on", "先关掉「交给 Jarvis」再删除", nil)
			return
		}
		res, err := h.forgetIMessage(r.Context())
		if err != nil {
			h.logf("imessage: delete: %v", err)
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		h.logf("imessage: deleted %d chat(s), %d person(s), %d inbox item(s)", res.Chats, res.People, res.Inbox)
		writeJSON(w, res)
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

// forgetIMessage removes what the hub kept of iMessage (forgetSource).
func (h *Hub) forgetIMessage(ctx context.Context) (sourceForgotten, error) {
	return h.forgetSource(ctx, imessage.ID)
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

package imessage

import (
	"context"
	"errors"
	"regexp"
	"sync"
	"time"

	"github.com/chyroc/jarvis/server/internal/connector"
)

// The hub's side: the connector reads through the node of one Mac (Reader runs there), so it is a thin adapter over
// that node's connector.* RPCs. It has no tools: the messages are not given to the assistant or any model. The app
// reads them for the user (hub/imessage.go), and nothing is stored on the hub.

// Config is hub.json's "imessage".
type Config struct {
	// Machine is the Mac whose Messages are read: one only (Macs with the same Apple ID hold the same messages).
	Machine string `json:"machine,omitempty"`
}

// Configured says whether the hub should run the connector at all.
func (c *Config) Configured() bool { return c != nil && c.Machine != "" }

// Why the Mac could not be asked (Caller's errors, errors.Is); ErrNoPermission and ErrNoChat come through too.
var (
	ErrOffline    = errors.New("the Mac is offline")
	ErrNodeTooOld = errors.New("the Mac's jarvisd is too old")
)

// Caller asks the Mac's node: method with params, the result decoded into out.
type Caller func(ctx context.Context, method string, params, out any) error

// Remote is the connector on the hub.
type Remote struct {
	cfg     Config
	machine string // the Mac's name, for messages
	call    Caller

	mu     sync.Mutex
	status connector.Status
	guide  []string
	via    string
	poke   chan struct{}
}

// NewRemote is the connector reading the Messages of cfg.Machine (named machine) through call.
func NewRemote(cfg Config, machine string, call Caller) *Remote {
	return &Remote{cfg: cfg, machine: machine, call: call, poke: make(chan struct{}, 1),
		status: connector.Status{State: connector.StateError, Error: "正在问 " + machine + "…"}}
}

func (c *Remote) ID() string   { return ID }
func (c *Remote) Name() string { return "信息" }

func (c *Remote) Status(context.Context) connector.Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := c.status
	st.Account = c.machine
	st.Via = c.machine + " 上的「信息」"
	return st
}

// Detail is the status with what the app needs to fix it: the steps on the Mac, and how its node runs.
type Detail struct {
	Machine string   `json:"machine"`
	Status  string   `json:"status"`
	Error   string   `json:"error,omitempty"`
	Guide   []string `json:"guide,omitempty"`
	Via     string   `json:"via,omitempty"` // ssh | launchd
}

func (c *Remote) Detail(ctx context.Context) Detail {
	st := c.Status(ctx)
	c.mu.Lock()
	defer c.mu.Unlock()
	return Detail{Machine: c.machine, Status: st.State, Error: st.Error, Guide: c.guide, Via: c.via}
}

// MachineOnline: the Mac just connected, so look again.
func (c *Remote) MachineOnline(id string) {
	if id == c.cfg.Machine {
		c.Refresh()
	}
}

// Refresh checks again soon (a read just failed for want of permission, or it may have been granted).
func (c *Remote) Refresh() {
	select {
	case c.poke <- struct{}{}:
	default:
	}
}

// How often Run checks: when all is well, and when it is not.
var (
	checkOK    = 5 * time.Minute
	checkRetry = time.Minute
)

// Run checks the Mac now and then until ctx ends.
func (c *Remote) Run(ctx context.Context) {
	for {
		c.Check(ctx)
		wait := checkOK
		if c.Status(ctx).State != connector.StateOK {
			wait = checkRetry
		}
		select {
		case <-ctx.Done():
			return
		case <-c.poke:
		case <-time.After(wait):
		}
	}
}

// Check asks the Mac whether its Messages can be read, and keeps the answer for Status and Detail.
func (c *Remote) Check(ctx context.Context) {
	var list struct {
		Connectors []struct {
			ID string `json:"id"`
			Status
		} `json:"connectors"`
	}
	err := c.call(ctx, "connector.list", map[string]any{}, &list)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.guide, c.via = nil, ""
	if err != nil {
		c.status.State, c.status.Error = connector.StateError, c.explain(err)
		return
	}
	for _, l := range list.Connectors {
		if l.ID == ID {
			c.status = connector.Status{State: l.State, Error: l.Error, LastSync: c.status.LastSync}
			if l.State == connector.StateOK {
				c.status.LastSync = time.Now().UTC()
			}
			c.guide, c.via = l.Guide, l.Via
			return
		}
	}
	c.status = connector.Status{State: connector.StateNotConfigured, Error: c.machine + " 不是 Mac，或者上面没有「信息」的数据"}
}

// explain is a failed call in words for the app.
func (c *Remote) explain(err error) string {
	switch {
	case errors.Is(err, ErrOffline):
		return c.machine + " 不在线"
	case errors.Is(err, ErrNodeTooOld):
		return c.machine + " 上的 jarvisd 太旧，更新后才能读「信息」"
	case errors.Is(err, ErrNoPermission):
		return "macOS 不让 Jarvis 读 " + c.machine + " 上的「信息」：要给它「完全磁盘访问权限」"
	case errors.Is(err, ErrNoChat):
		return "没有这个会话"
	}
	return "读不了 " + c.machine + " 上的「信息」：" + err.Error()
}

// Explain is err in words for the app (the same as Status uses).
func (c *Remote) Explain(err error) string { return c.explain(err) }

var chatID = regexp.MustCompile(`^` + ID + `-[0-9a-f]{12}$`)

// ValidChat says whether id looks like a conversation id: nothing else is sent to the Mac.
func ValidChat(id string) bool { return chatID.MatchString(id) }

// Chats is a page of the Mac's conversations (Reader.Chats there).
func (c *Remote) Chats(ctx context.Context, limit int, before string) (ChatsPage, error) {
	var page ChatsPage
	err := c.read(ctx, "connector.chats", map[string]any{"id": ID, "limit": limit, "before": before}, &page)
	return page, err
}

// Messages is a page of one conversation (Reader.Messages there).
func (c *Remote) Messages(ctx context.Context, chat string, limit int, before string) (MessagesPage, error) {
	if !ValidChat(chat) {
		return MessagesPage{}, ErrNoChat
	}
	var page MessagesPage
	err := c.read(ctx, "connector.older", map[string]any{"id": ID, "chat": chat, "limit": limit, "before": before}, &page)
	return page, err
}

func (c *Remote) read(ctx context.Context, method string, params map[string]any, out any) error {
	err := c.call(ctx, method, params, out)
	if errors.Is(err, ErrNoPermission) || errors.Is(err, ErrNodeTooOld) || errors.Is(err, ErrOffline) {
		c.Refresh() // the status says it too, and the settings page shows the steps
	}
	return err
}

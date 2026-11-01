package node

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/chyroc/jarvis/server/internal/proto"
	"github.com/coder/websocket"
)

var errCodexNoOwner = errors.New("没有可共享的 app-server 运行入口")

type remoteThread struct {
	id, publicID, title, cwd, status, turn, parent, partial string
	joined                                                  bool
	buffer                                                  *strings.Builder
	lastDelta                                               time.Time
	revision                                                uint64
	files                                                   map[string][]string
}

type remoteAsk struct {
	cancel              context.CancelFunc
	thread, interaction string
}

type codexRemote struct {
	n          *Node
	spec       proto.CodexServer
	generation string
	wire       *codexConn
	socket     *websocket.Conn
	ctx        context.Context
	cancel     context.CancelFunc
	done       chan struct{}
	once       sync.Once
	delivery   sync.Mutex
	mu         sync.Mutex
	threads    map[string]remoteThread
	asks       map[string]*remoteAsk
	joins      typingLocks
	finished   map[string]bool
	ready      bool
}

func (n *Node) ensureCodexRemote(ctx context.Context, spec proto.CodexServer) {
	r := n.codices
	r.mu.Lock()
	old := r.remotes[spec.ID]
	if old != nil && old.spec == spec {
		r.mu.Unlock()
		return
	}
	cctx, cancel := context.WithCancel(ctx)
	c := &codexRemote{n: n, spec: spec, generation: newUUID(), ctx: cctx, cancel: cancel, done: make(chan struct{}), threads: map[string]remoteThread{}, asks: map[string]*remoteAsk{}, finished: map[string]bool{}}
	r.remotes[spec.ID] = c
	r.mu.Unlock()
	if old != nil {
		old.close()
	}
	go c.run()
}

func rejectCodexRedirect(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

func validateCodexEndpoint(endpoint string) (string, *http.Client, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", nil, errors.New("app-server 入口无效")
	}
	if u.Scheme == "unix" {
		path := strings.TrimPrefix(endpoint, "unix://")
		if !filepath.IsAbs(path) {
			return "", nil, errors.New("Unix 入口必须是绝对路径")
		}
		for _, p := range []string{filepath.Dir(path), path} {
			st, err := os.Lstat(p)
			if err != nil {
				return "", nil, errors.New("Unix 入口不可访问")
			}
			stat, ok := st.Sys().(*syscall.Stat_t)
			if !ok || stat.Uid != uint32(os.Getuid()) || st.Mode()&os.ModeSymlink != 0 || st.Mode().Perm()&0077 != 0 {
				return "", nil, errors.New("Unix 入口或目录不属于当前用户，或权限过宽")
			}
			if p == path && st.Mode()&os.ModeSocket == 0 {
				return "", nil, errors.New("Unix 入口不是 socket")
			}
		}
		tr := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", path)
		}}
		return "ws://localhost/", &http.Client{Transport: tr, CheckRedirect: rejectCodexRedirect}, nil
	}
	if u.Scheme != "ws" || u.Port() == "" || u.Path != "" && u.Path != "/" {
		return "", nil, errors.New("只支持本机 Unix 或回环 WebSocket 入口")
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil || !ip.IsLoopback() {
		return "", nil, errors.New("WebSocket 入口必须是回环 IP，不能是外部地址或主机名")
	}
	// Never use HTTP_PROXY for a private local endpoint.
	return endpoint, &http.Client{Transport: &http.Transport{Proxy: nil}, CheckRedirect: rejectCodexRedirect}, nil
}

func (c *codexRemote) run() {
	defer func() {
		c.close()
		c.n.codices.mu.Lock()
		if c.n.codices.remotes[c.spec.ID] == c {
			delete(c.n.codices.remotes, c.spec.ID)
		}
		c.n.codices.mu.Unlock()
	}()
	target, client, err := validateCodexEndpoint(c.spec.Endpoint)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(c.ctx, 10*time.Second)
	socket, resp, err := websocket.Dial(ctx, target, &websocket.DialOptions{HTTPClient: client})
	cancel()
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		return
	}
	socket.SetReadLimit(64 << 20)
	c.mu.Lock()
	c.socket = socket
	c.mu.Unlock()
	wire := &codexConn{pending: map[int64]chan codexReply{}, ended: c.done}
	wire.writeMessage = func(b []byte) error {
		ctx, cancel := context.WithTimeout(c.ctx, 10*time.Second)
		defer cancel()
		return socket.Write(ctx, websocket.MessageText, b)
	}
	wire.onMessage = c.message
	c.mu.Lock()
	c.wire = wire
	c.mu.Unlock()
	go func() {
		for {
			_, b, err := socket.Read(c.ctx)
			if err != nil {
				c.close()
				return
			}
			var m codexMessage
			if json.Unmarshal(b, &m) == nil {
				wire.receive(m)
			}
		}
	}()
	ctx, cancel = context.WithTimeout(c.ctx, 15*time.Second)
	var init struct {
		CodexHome string `json:"codexHome"`
	}
	err = wire.call(ctx, "initialize", map[string]any{"clientInfo": map[string]any{"name": "jarvis", "title": "Jarvis", "version": c.n.Version}, "capabilities": map[string]any{"experimentalApi": true}}, &init)
	cancel()
	if err != nil {
		return
	}
	if init.CodexHome != "" && canonicalCodexHome(init.CodexHome, c.n.home) != c.spec.Home {
		return
	}
	if err = wire.send(map[string]any{"jsonrpc": "2.0", "method": "initialized", "params": map[string]any{}}); err != nil {
		return
	}
	if err = c.refresh(); err != nil {
		return
	}
	c.mu.Lock()
	c.ready = true
	c.mu.Unlock()
	c.n.notifyCodex()
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		cfg := c.n.codexInventory()
		for id, instance := range cfg.Joined {
			if instance == c.spec.ID {
				if _, _, err := c.n.codexOwner(id); err == nil {
					_ = c.join(c.ctx, id, false)
				}
			}
		}
		select {
		case <-c.ctx.Done():
			return
		case <-tick.C:
			if c.refresh() != nil {
				return
			}
		}
	}
}

func (c *codexRemote) close() {
	c.once.Do(func() {
		c.delivery.Lock()
		defer c.delivery.Unlock()
		c.cancel()
		c.mu.Lock()
		c.ready = false
		socket := c.socket
		wire := c.wire
		for _, a := range c.asks {
			a.cancel()
		}
		ids := make([]string, 0, len(c.threads))
		for _, t := range c.threads {
			ids = append(ids, t.publicID)
		}
		c.mu.Unlock()
		if wire != nil {
			wire.mu.Lock()
			wire.closed = true
			wire.mu.Unlock()
		}
		close(c.done)
		if socket != nil {
			socket.CloseNow()
		}
		for _, id := range ids {
			c.n.emitDelta(id, "")
		}
		c.n.notifyCodex()
	})
}

func (c *codexRemote) loaded(ctx context.Context) ([]string, error) {
	var ids []string
	cursor := ""
	seen := map[string]bool{}
	for {
		var page struct {
			Data       []string `json:"data"`
			NextCursor *string  `json:"nextCursor"`
		}
		p := map[string]any{"limit": 100}
		if cursor != "" {
			p["cursor"] = cursor
		}
		if err := c.wire.call(ctx, "thread/loaded/list", p, &page); err != nil {
			return nil, err
		}
		ids = append(ids, page.Data...)
		if page.NextCursor == nil || *page.NextCursor == "" {
			break
		}
		cursor = *page.NextCursor
		if seen[cursor] {
			return nil, errors.New("app-server 返回重复的分页游标")
		}
		seen[cursor] = true
	}
	return ids, nil
}

type remoteThreadData struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	Preview        string          `json:"preview"`
	Cwd            string          `json:"cwd"`
	Path           string          `json:"path"`
	ParentThreadID string          `json:"parentThreadId"`
	Source         json.RawMessage `json:"source"`
	Status         struct {
		Type        string   `json:"type"`
		ActiveFlags []string `json:"activeFlags"`
	} `json:"status"`
	Turns []struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	} `json:"turns"`
}

func (c *codexRemote) refresh() error {
	ctx, cancel := context.WithTimeout(c.ctx, 20*time.Second)
	defer cancel()
	ids, err := c.loaded(ctx)
	if err != nil {
		return err
	}
	seenRevision := map[string]uint64{}
	next := map[string]remoteThread{}
	for _, raw := range ids {
		if !validID(raw) {
			continue
		}
		c.mu.Lock()
		t := c.threads[raw]
		seenRevision[raw] = t.revision
		c.mu.Unlock()
		var reply struct {
			Thread remoteThreadData `json:"thread"`
		}
		if err := c.wire.call(ctx, "thread/read", map[string]any{"threadId": raw, "includeTurns": false}, &reply); err != nil {
			continue
		}
		d := reply.Thread
		if d.ID != raw || d.Status.Type == "notLoaded" {
			continue
		}
		path := rolloutPath(c.spec.Home, raw)
		public := c.n.registerCodex(c.spec.Home, raw, path)
		t.id, t.publicID, t.title, t.cwd = raw, public, firstNonEmpty(d.Name, d.Preview), d.Cwd
		t.parent = d.ParentThreadID
		if t.parent == "" && strings.Contains(string(d.Source), "subAgent") {
			t.parent = "parent-managed"
		}
		if t.parent != "" && validID(t.parent) {
			t.parent = c.n.registerCodex(c.spec.Home, t.parent, "")
		}
		if t.files == nil {
			t.files = map[string][]string{}
		}
		t.status = proto.StatusDone
		if d.Status.Type == "active" {
			t.status = proto.StatusWorking
		} else {
			t.turn = ""
			t.partial = ""
			t.buffer = nil
		}
		if slices.Contains(d.Status.ActiveFlags, "waitingOnApproval") || slices.Contains(d.Status.ActiveFlags, "waitingOnUserInput") {
			t.status = proto.StatusBlocked
		}
		for _, turn := range d.Turns {
			if turn.Status == "inProgress" {
				t.turn = turn.ID
			}
		}
		next[raw] = t
	}
	c.mu.Lock()
	// Preserve events that arrived during metadata RPCs; metadata never overwrites a joined thread's current turn.
	for raw, t := range next {
		if old, ok := c.threads[raw]; ok && old.revision != seenRevision[raw] {
			t.joined, t.turn, t.partial, t.files, t.status, t.revision, t.buffer, t.lastDelta = old.joined, old.turn, old.partial, old.files, old.status, old.revision, old.buffer, old.lastDelta
			next[raw] = t
		}
	}
	for _, a := range c.asks {
		if _, ok := next[a.thread]; !ok {
			a.cancel()
		}
	}
	c.threads = next
	c.mu.Unlock()
	return nil
}

func (n *Node) codexOwner(id string) (*codexRemote, remoteThread, error) {
	if n.codexBlocked(id) {
		return nil, remoteThread{}, errors.New("会话 ID 与另一来源重复，不能判断操作目标")
	}
	src, known := n.codexSource(id)
	if !known {
		return nil, remoteThread{}, errCodexNoOwner
	}
	n.codices.mu.Lock()
	var remotes []*codexRemote
	for _, c := range n.codices.remotes {
		remotes = append(remotes, c)
	}
	n.codices.mu.Unlock()
	var owner *codexRemote
	var thread remoteThread
	for _, c := range remotes {
		if c.spec.Home != src.home {
			continue
		}
		c.mu.Lock()
		t, ok := c.threads[src.thread]
		ready := c.ready
		c.mu.Unlock()
		if !ready || !ok {
			continue
		}
		if owner != nil {
			return nil, remoteThread{}, errors.New("仅查看：多个 app-server 声称运行这个会话，不能确定发送目标")
		}
		owner, thread = c, t
	}
	if owner == nil {
		return nil, remoteThread{}, errCodexNoOwner
	}
	return owner, thread, nil
}

func (c *codexRemote) join(ctx context.Context, id string, remember bool) error {
	defer c.joins.lock(id)()
	owner, t, err := c.n.codexOwner(id)
	if err != nil || owner != c {
		return refusal("codex_read_only", "无法确定原 app-server，未接入", nil)
	}
	if t.parent != "" {
		return refusal("codex_read_only", "这个子 Agent 由父 Agent 驱动", nil)
	}
	if t.joined {
		return nil
	}
	ids, err := c.loaded(ctx)
	if err != nil || !slices.Contains(ids, t.id) {
		return refusal("codex_read_only", "会话已不在原 app-server 中运行，未恢复或新建会话", nil)
	}
	// This is an explicit user-requested join, not a discovery or global subscription. Do not override runtime config.
	var reply struct {
		Thread remoteThreadData `json:"thread"`
	}
	c.mu.Lock()
	joining := c.threads[t.id]
	joining.joined = true
	joining.revision++
	c.threads[t.id] = joining
	c.mu.Unlock()
	if err = c.wire.call(ctx, "thread/resume", map[string]any{"threadId": t.id, "excludeTurns": true}, &reply); err != nil {
		c.mu.Lock()
		joining = c.threads[t.id]
		joining.joined = false
		joining.revision++
		c.threads[t.id] = joining
		for key, ask := range c.asks {
			if ask.thread == t.id {
				delete(c.asks, key)
				ask.cancel()
			}
		}
		c.mu.Unlock()
		return err
	}
	if reply.Thread.ID != t.id {
		return errors.New("app-server 接入返回了不同的会话")
	}
	c.mu.Lock()
	current, ok := c.threads[t.id]
	if ok {
		current.joined = true
		current.revision++
		for _, turn := range reply.Thread.Turns {
			if turn.Status == "inProgress" {
				current.turn = turn.ID
			}
		}
		c.threads[t.id] = current
	}
	c.mu.Unlock()
	if remember && ok {
		c.n.codices.mu.Lock()
		c.n.codices.config.Joined[id] = c.spec.ID
		c.n.codices.mu.Unlock()
		c.n.rememberCodex()
	}
	c.n.notifyCodex()
	return nil
}

func (n *Node) attachCodex(ctx context.Context, id string) error {
	defer n.typing.lock(id)()
	c, _, err := n.codexOwner(id)
	if err != nil {
		return refusal("codex_read_only", err.Error(), nil)
	}
	return c.join(ctx, id, true)
}

func (n *Node) sendCodex(ctx context.Context, id, text string, images []proto.Image) error {
	c, t, err := n.codexOwner(id)
	if err != nil {
		return refusal("codex_read_only", firstNonEmpty(err.Error(), "仅查看"), nil)
	}
	if err = c.join(ctx, id, true); err != nil {
		return refusal("codex_read_only", err.Error(), nil)
	}
	paths, err := n.saveImages(images)
	if err != nil {
		return refusal("bad_image", err.Error(), nil)
	}
	input := []map[string]any{}
	if text != "" {
		input = append(input, map[string]any{"type": "text", "text": text, "text_elements": []any{}})
	}
	for _, p := range paths {
		input = append(input, map[string]any{"type": "localImage", "path": p})
	}
	c, t, err = n.codexOwner(id)
	if err != nil {
		return refusal("codex_read_only", err.Error(), nil)
	}
	params := map[string]any{"threadId": t.id, "input": input}
	method := "turn/start"
	if t.status == proto.StatusWorking || t.status == proto.StatusBlocked {
		if t.turn == "" {
			return refusal("codex_busy", "这个轮次仍在运行，但尚未收到轮次标识：稍后再发", nil)
		}
		method = "turn/steer"
		params["expectedTurnId"] = t.turn
	}
	var reply struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	err = c.wire.call(ctx, method, params, &reply)
	var re *codexError
	if errors.As(err, &re) {
		if re.Code == -32601 || re.Code == -32602 {
			return refusal("codex_refused", re.Error(), nil)
		}
		return uncertain(re)
	}
	if err != nil {
		return uncertain(err)
	}
	if reply.Turn.ID != "" {
		c.mu.Lock()
		t = c.threads[t.id]
		t.turn = reply.Turn.ID
		t.status = proto.StatusWorking
		t.revision++
		c.threads[t.id] = t
		c.mu.Unlock()
	}
	n.notifyCodex()
	return nil
}

func (n *Node) interruptCodex(ctx context.Context, id string) error {
	defer n.typing.lock(id)()
	c, t, err := n.codexOwner(id)
	if err != nil {
		return refusal("codex_read_only", err.Error(), nil)
	}
	if t.turn == "" || t.parent != "" {
		return refusal("codex_read_only", "没有可停止的已观察轮次", nil)
	}
	return c.wire.call(ctx, "turn/interrupt", map[string]any{"threadId": t.id, "turnId": t.turn}, nil)
}

func (c *codexRemote) message(m codexMessage) {
	if len(m.ID) > 0 {
		c.request(m)
		return
	}
	c.delivery.Lock()
	defer c.delivery.Unlock()
	c.mu.Lock()
	ready := c.ready
	c.mu.Unlock()
	if !ready {
		return
	}
	var ev struct {
		ThreadID  string          `json:"threadId"`
		TurnID    string          `json:"turnId"`
		Delta     string          `json:"delta"`
		RequestID json.RawMessage `json:"requestId"`
		Turn      struct {
			ID string `json:"id"`
		} `json:"turn"`
		Item remoteEventItem `json:"item"`
	}
	if json.Unmarshal(m.Params, &ev) != nil {
		return
	}
	if m.Method == "serverRequest/resolved" {
		c.mu.Lock()
		c.finished[codexRequestKey(ev.RequestID)] = true
		a := c.asks[codexRequestKey(ev.RequestID)]
		if a != nil && a.thread == ev.ThreadID {
			delete(c.asks, codexRequestKey(ev.RequestID))
		}
		c.mu.Unlock()
		if a != nil && a.thread == ev.ThreadID {
			if c.n.peer != nil {
				_ = c.n.peer.Notify("interaction.dismiss", map[string]any{"id": a.interaction})
			}
			a.cancel()
		}
		return
	}
	c.mu.Lock()
	t, ok := c.threads[ev.ThreadID]
	if !ok {
		c.mu.Unlock()
		return
	}
	delta := ""
	emit := false
	refresh := false
	switch m.Method {
	case "turn/started":
		t.turn = ev.Turn.ID
		t.status = proto.StatusWorking
		t.partial = ""
		t.buffer = nil
		emit = true
	case "turn/completed":
		if t.turn == ev.Turn.ID {
			t.turn = ""
			t.status = proto.StatusDone
			t.partial = ""
			t.buffer = nil
			emit = true
			refresh = true
		}
	case "item/started":
		if ev.Item.Type == "fileChange" {
			var paths []string
			for _, ch := range ev.Item.Changes {
				paths = append(paths, ch.Path)
			}
			t.files[ev.Item.ID] = paths
		}
		if ev.Item.Type == "agentMessage" {
			t.partial = ""
			t.buffer = nil
			emit = true
			refresh = true
		}
	case "item/agentMessage/delta":
		if t.buffer == nil {
			t.buffer = &strings.Builder{}
		}
		t.buffer.WriteString(ev.Delta)
		t.partial = t.buffer.String()
		delta = t.partial
		emit = time.Since(t.lastDelta) >= 80*time.Millisecond
		if emit {
			t.lastDelta = time.Now()
		}
	case "item/completed":
		delete(t.files, ev.Item.ID)
		refresh = true
		if ev.Item.Type == "agentMessage" {
			t.partial = ev.Item.Text
			t.buffer = nil
			delta = t.partial
			emit = true
		}
	}
	t.revision++
	c.threads[ev.ThreadID] = t
	c.mu.Unlock()
	if refresh {
		go func() {
			if f, err := c.n.file(t.publicID); err == nil {
				c.n.refresh(t.publicID, f)
			}
		}()
	}
	if emit {
		c.n.emitDelta(t.publicID, delta)
	}
	if m.Method == "turn/started" || m.Method == "turn/completed" {
		c.n.notifyCodex()
	}
}

func remoteInteractionID(machine, instance, generation, thread string, id json.RawMessage) string {
	h := sha256.Sum256([]byte(machine + "\x00" + instance + "\x00" + generation + "\x00" + thread + "\x00" + codexRequestKey(id)))
	return "codex-" + hex.EncodeToString(h[:])
}

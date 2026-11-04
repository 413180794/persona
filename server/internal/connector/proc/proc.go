// Package proc is a connector that runs in a program of its own (connector/serve) and answers for it in the hub. It
// implements every capability and declares what the program said it has (a proxy's manifest): the hub uses only
// those. The program is either a plugin's, run by the plugin host (Config.Link: the hub's way, docs/plugins.md), or
// started here with the connector protocol's own opening (Config.Path). jarvisd-telegram is one: its dependencies
// would double jarvisd's size (docs/plan-telegram.md).
package proc

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/connector/serve"
	"github.com/nwverse/persona/server/internal/kb"
	"github.com/nwverse/persona/server/internal/plugin"
	"github.com/nwverse/persona/server/internal/plugin/host"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// Link is a plugin's process (plugin/host) as one of its connectors reaches it: calls go to the connector namespace
// with the connector's id as the contribution; the process runs while the connector holds it.
type Link struct {
	Proc *host.Proc
	ID   string // the contribution: the connector's id
}

func (l Link) call(ctx context.Context, method string, params, out any) error {
	p, err := plugin.Targeted(params, l.ID)
	if err != nil {
		return err
	}
	err = l.Proc.Call(ctx, serve.InPlugin(method), p, out)
	if errors.Is(err, host.ErrNotRunning) {
		return fmt.Errorf("%w", errNotRunning)
	}
	return err
}

// Config is how to run the program.
type Config struct {
	// ID and Name are the connector's before the program says (while it starts, or cannot).
	ID, Name string
	// Manifest is what the program declares, known before it ever runs: a connector switched off from the start
	// still lists its capabilities and can be signed in to and out of. The program's own (at init) is checked against
	// it, a difference logged; this one is used. Without capabilities here, the program's is used once it runs.
	Manifest connector.Manifest
	Path     string // the program (next to jarvisd)
	Args     []string
	Env      []string // added to the hub's environment
	// Version is what `<Path> version -v` must print (the hub's own): the two come from one build. "" skips it.
	Version string
	// Settings is sent with init: the connector's section of hub.json. SettingsNow, when set, is asked instead at
	// each start (what the hub learns later: the user's time zone).
	Settings    json.RawMessage
	SettingsNow func() json.RawMessage
	// Secrets is the hub's store for this connector: the program's secrets.* calls reach only this one.
	Secrets connector.Secrets
	Logf    func(string, ...any)
	// Link, when set, is the plugin process the connector runs in: Path, Args, Env, Version and Settings are then the
	// plugin host's business, and the hub passes the program's calls and events on (Handle, Event).
	Link *Link
}

// Restarts of the program after it exits: doubling from the first to the last, back to the first once it ran a while.
var (
	restartFirst = 5 * time.Second
	restartLast  = 5 * time.Minute
	ranAWhile    = time.Minute
	statusEvery  = 30 * time.Second // how often a running program's status is asked (Status answers from memory)
	callTimeout  = 2 * time.Minute
)

// Conn is the connector of one program.
type Conn struct {
	cfg    Config
	events chan connector.Scope // scopes the program reported, for Watch

	mu      sync.Mutex
	peer    *rpc.Peer // nil while the program is not running
	man     serve.Manifest
	tools   []proto.Tool
	actions []proto.Tool
	status  connector.Status
}

// New is the connector of the program cfg describes; nothing runs until Run.
func New(cfg Config) *Conn {
	if cfg.Logf == nil {
		cfg.Logf = func(string, ...any) {}
	}
	return &Conn{cfg: cfg, events: make(chan connector.Scope, 16),
		man:    serve.Manifest{ID: cfg.ID, Name: cfg.Name},
		status: connector.Status{State: connector.StateError, Error: cfg.Name + " 进程还没启动"}}
}

func (c *Conn) ID() string { return c.cfg.ID }

func (c *Conn) Name() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.man.Name
}

// Manifest is the one Config declares, as a proxy's; without one, what the program said at init (nothing before).
// Its status is always the hub's answer for it (connector.StatusOf asks a proxy's).
func (c *Conn) Manifest() connector.Manifest {
	if len(c.cfg.Manifest.Capabilities) > 0 {
		m := c.cfg.Manifest
		m.Proxy = true
		return m
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.man.Of()
}

// sameManifest says whether the program declares what Config does (capabilities in any order).
func sameManifest(declared, said connector.Manifest) bool {
	norm := func(m connector.Manifest) connector.Manifest {
		m.Capabilities = slices.Clone(m.Capabilities)
		slices.Sort(m.Capabilities)
		m.Proxy = false
		return m
	}
	a, b := norm(declared), norm(said)
	return slices.Equal(a.Capabilities, b.Capabilities) && slices.Equal(a.Hosts, b.Hosts) && slices.Equal(a.AgentTools, b.AgentTools) &&
		slices.Equal(a.Unlisted, b.Unlisted) && slices.Equal(a.ConfirmAlways, b.ConfirmAlways) && a.Poll == b.Poll && slices.Equal(a.Kinds, b.Kinds)
}

// Status answers from memory: the program's last status, or why it is not running.
func (c *Conn) Status(context.Context) connector.Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status
}

func (c *Conn) setDown(why string) {
	c.mu.Lock()
	c.peer = nil
	c.status = connector.Status{State: connector.StateError, Error: why}
	c.mu.Unlock()
}

// Run keeps the program running until ctx ends: checks it is the hub's build, starts it, restarts it after a
// backoff when it exits. A linked one holds its plugin's process instead, which the plugin host runs.
func (c *Conn) Run(ctx context.Context) {
	if c.cfg.Link != nil {
		c.runLinked(ctx)
		return
	}
	wait := restartFirst
	for ctx.Err() == nil {
		start := time.Now()
		ch, err := c.spawn(ctx)
		if err == nil {
			c.mu.Lock()
			c.peer = ch.peer
			c.mu.Unlock()
			c.refresh(ctx)
			c.keepUp(ctx, ch)
			ch.stop()
		}
		if ctx.Err() != nil {
			c.setDown(c.cfg.Name + " 已停止")
			return
		}
		if time.Since(start) >= ranAWhile {
			wait = restartFirst
		}
		why := c.cfg.Name + " 进程在重启"
		if err != nil {
			why = err.Error()
		}
		c.setDown(why)
		c.cfg.Logf("connector %s: the program stopped (%v), again in %s", c.cfg.ID, ch.err(err), wait)
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = min(wait*2, restartLast)
	}
}

// runLinked holds the plugin's process while ctx lasts, refreshing the status and tools each time it comes up and
// every so often while it is up.
func (c *Conn) runLinked(ctx context.Context) {
	p := c.cfg.Link.Proc
	release := p.Hold()
	defer release()
	t := time.NewTicker(statusEvery)
	defer t.Stop()
	up := false
	for {
		changes := p.Changes()
		switch ready := p.Ready(); {
		case ready && !up:
			up = true
			c.refresh(ctx)
		case !ready:
			up = false
			state, why := p.Status()
			switch state {
			case host.StateError:
			case host.StateStarting:
				why = c.cfg.Name + " 正在启动"
			default:
				why = c.cfg.Name + " 进程还没启动"
			}
			c.setDown(why)
		}
		select {
		case <-ctx.Done():
			c.setDown(c.cfg.Name + " 已停止")
			return
		case <-changes:
		case <-t.C:
			if up {
				c.refresh(ctx)
			}
		}
	}
}

// Handle answers a linked program's calls (connector.secrets.*, without the namespace), as fromProgram.
func (c *Conn) Handle(ctx context.Context, method string, params json.RawMessage) (any, error) {
	return c.fromProgram(ctx, method, params)
}

// Event takes a linked program's event (changed, without the namespace).
func (c *Conn) Event(event string, params json.RawMessage) { c.onEvent(event, params) }

// keepUp refreshes the status every so often until the program exits or ctx ends.
func (c *Conn) keepUp(ctx context.Context, ch *child) {
	t := time.NewTicker(statusEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ch.peer.Done():
			return
		case <-t.C:
			c.refresh(ctx)
		}
	}
}

// refresh asks the running program for its status and tools.
func (c *Conn) refresh(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	if l := c.cfg.Link; l != nil {
		if !l.Proc.Ready() {
			return
		}
		var st connector.Status
		if err := l.call(ctx, serve.MStatus, struct{}{}, &st); err != nil {
			return
		}
		var tools, actions serve.ToolsResult
		_ = l.call(ctx, serve.MTools, struct{}{}, &tools)
		if c.Manifest().Has(connector.CapActions) {
			_ = l.call(ctx, serve.MActions, struct{}{}, &actions)
		}
		c.mu.Lock()
		c.status, c.tools, c.actions = st, tools.Tools, actions.Tools
		c.mu.Unlock()
		return
	}
	c.mu.Lock()
	peer := c.peer
	c.mu.Unlock()
	if peer == nil {
		return
	}
	var st connector.Status
	if err := peer.Call(ctx, serve.MStatus, struct{}{}, &st); err != nil {
		return
	}
	var tools, actions serve.ToolsResult
	_ = peer.Call(ctx, serve.MTools, struct{}{}, &tools)
	if c.Manifest().Has(connector.CapActions) {
		_ = peer.Call(ctx, serve.MActions, struct{}{}, &actions)
	}
	c.mu.Lock()
	if c.peer == peer {
		c.status, c.tools, c.actions = st, tools.Tools, actions.Tools
	}
	c.mu.Unlock()
}

// child is one run of the program.
type child struct {
	cmd    *exec.Cmd
	peer   *rpc.Peer
	stdin  io.Closer
	served chan error
}

func (ch *child) err(start error) error {
	if start != nil || ch == nil {
		return start
	}
	return ch.peer.Err()
}

// stop closes the program's stdin (it exits when the hub goes away) and kills it if it does not.
func (ch *child) stop() {
	_ = ch.stdin.Close()
	ch.peer.Close(rpc.ErrClosed)
	done := make(chan struct{})
	go func() { _ = ch.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		_ = ch.cmd.Process.Kill()
		<-done
	}
}

// spawn checks the program and starts it, sending init.
func (c *Conn) spawn(ctx context.Context) (*child, error) {
	if _, err := os.Stat(c.cfg.Path); err != nil {
		return nil, fmt.Errorf("没有找到 %s 的程序（%s）", c.cfg.Name, c.cfg.Path)
	}
	if c.cfg.Version != "" {
		vctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		vcmd := exec.CommandContext(vctx, c.cfg.Path, "version", "-v")
		vcmd.Env = append(os.Environ(), c.cfg.Env...)
		out, err := vcmd.Output()
		cancel()
		if err != nil {
			return nil, fmt.Errorf("%s 的程序运行不了：%v", c.cfg.Name, err)
		}
		if got := strings.TrimSpace(string(out)); got != strings.TrimSpace(c.cfg.Version) {
			return nil, fmt.Errorf("%s 的程序和 hub 不是同一个版本（%s / %s）", c.cfg.Name, firstLine(got), firstLine(c.cfg.Version))
		}
	}
	cmd := exec.Command(c.cfg.Path, c.cfg.Args...)
	cmd.Env = append(os.Environ(), c.cfg.Env...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("%s 的程序起不来：%v", c.cfg.Name, err)
	}
	go c.logLines(stderr)
	peer := rpc.New("h", stdin)
	peer.OnRequest = c.fromProgram
	peer.OnEvent = c.onEvent
	ch := &child{cmd: cmd, peer: peer, stdin: stdin, served: make(chan error, 1)}
	go func() { ch.served <- peer.Serve(context.Background(), stdout) }()
	ictx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	settings := c.cfg.Settings
	if c.cfg.SettingsNow != nil {
		settings = c.cfg.SettingsNow()
	}
	var man serve.Manifest
	if err := peer.Call(ictx, serve.MInit, serve.InitParams{Protocol: serve.Protocol, Config: settings}, &man); err != nil {
		ch.stop()
		return nil, fmt.Errorf("%s 的程序没准备好：%v", c.cfg.Name, err)
	}
	if man.Protocol != serve.Protocol {
		ch.stop()
		return nil, fmt.Errorf("%s 的程序用的协议版本是 %d，hub 要 %d", c.cfg.Name, man.Protocol, serve.Protocol)
	}
	if man.ID != c.cfg.ID {
		ch.stop()
		return nil, fmt.Errorf("the program says it is %q, not %q", man.ID, c.cfg.ID)
	}
	if d := c.cfg.Manifest; len(d.Capabilities) > 0 && !sameManifest(d, man.Of()) {
		c.cfg.Logf("connector %s: the program declares %v, the hub registered it with %v (the hub's is used)", c.cfg.ID, man.Of(), d)
	}
	c.mu.Lock()
	c.man = man
	c.mu.Unlock()
	return ch, nil
}

// logLines keeps what the program writes to stderr (its own log: it keeps secrets and messages out of it).
func (c *Conn) logLines(r io.Reader) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		c.cfg.Logf("connector %s: %s", c.cfg.ID, sc.Text())
	}
}

// fromProgram answers the program's calls: its secret, and only its own (the hub bound Secrets to this connector;
// nothing the program sends picks another).
func (c *Conn) fromProgram(ctx context.Context, method string, params json.RawMessage) (any, error) {
	if c.cfg.Secrets == nil {
		return nil, errors.New("no secrets store")
	}
	switch method {
	case serve.MSecretsLoad:
		data, err := c.cfg.Secrets.Load(ctx)
		return serve.SecretData{Data: data}, err
	case serve.MSecretsSave:
		var p serve.SecretData
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		return struct{}{}, c.cfg.Secrets.Save(ctx, p.Data)
	case serve.MSecretsDelete:
		return struct{}{}, c.cfg.Secrets.Delete(ctx)
	}
	return nil, fmt.Errorf("unknown method %s", method)
}

func (c *Conn) onEvent(event string, params json.RawMessage) {
	if event != serve.EChanged {
		return
	}
	var e serve.ChangedEvent
	if json.Unmarshal(params, &e) != nil {
		return
	}
	scope := connector.Scope(e.Scope)
	if !scope.Known() {
		c.cfg.Logf("connector %s: unknown scope %q changed", c.cfg.ID, e.Scope)
		return
	}
	select {
	case c.events <- scope:
	default: // the hub has a kick pending already
	}
}

// Watch passes the program's changes on until ctx ends; the hub restarts it like any connector's.
func (c *Conn) Watch(ctx context.Context, changed func(connector.Scope)) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case scope := <-c.events:
			go c.refresh(ctx) // what changed may be its status too (signed out elsewhere)
			changed(scope)
		}
	}
}

var errNotRunning = errors.New("not running")

// call runs one method on the running program.
func (c *Conn) call(ctx context.Context, method string, params, out any) error {
	if l := c.cfg.Link; l != nil {
		ctx, cancel := context.WithTimeout(ctx, callTimeout)
		defer cancel()
		err := l.call(ctx, method, params, out)
		if errors.Is(err, errNotRunning) {
			return fmt.Errorf("%s: %w", c.cfg.Name, errNotRunning)
		}
		return err
	}
	c.mu.Lock()
	peer := c.peer
	c.mu.Unlock()
	if peer == nil {
		return fmt.Errorf("%s: %w", c.cfg.Name, errNotRunning)
	}
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	return peer.Call(ctx, method, params, out)
}

// LoginStep needs the program running: a sign-in keeps its state (Telegram's code request) in it between steps.
func (c *Conn) LoginStep(ctx context.Context, step string, values map[string]string) (connector.Prompt, error) {
	var p connector.Prompt
	if err := c.call(ctx, serve.MLoginStep, serve.LoginParams{Step: step, Values: values}, &p); err != nil {
		if errors.Is(err, errNotRunning) {
			return connector.Prompt{Step: step, Error: "先打开「同步 " + c.cfg.Name + "」再登录"}, nil
		}
		return p, err
	}
	c.refresh(ctx)
	return p, nil
}

// Logout signs out through the program; while it is not running (switched off), a run of it just for this.
func (c *Conn) Logout(ctx context.Context) error {
	err := c.call(ctx, serve.MLogout, struct{}{}, nil)
	if !errors.Is(err, errNotRunning) {
		c.refresh(ctx)
		return err
	}
	if l := c.cfg.Link; l != nil {
		release := l.Proc.Hold()
		defer release()
		wctx, cancel := context.WithTimeout(ctx, callTimeout)
		defer cancel()
		if err := l.Proc.Wait(wctx); err != nil {
			return err
		}
		return c.call(ctx, serve.MLogout, struct{}{}, nil)
	}
	ch, err := c.spawn(ctx)
	if err != nil {
		return err
	}
	defer ch.stop()
	lctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	return ch.peer.Call(lctx, serve.MLogout, struct{}{}, nil)
}

func (c *Conn) Changes(ctx context.Context, cursor string, limit int) ([]connector.ChatBatch, string, bool, error) {
	var out serve.ChangesResult
	err := c.call(ctx, serve.MChanges, serve.ChangesParams{Cursor: cursor, Limit: limit}, &out)
	if err != nil {
		return nil, cursor, false, err
	}
	return out.Batches, out.Next, out.More, nil
}

func (c *Conn) Collect(ctx context.Context) ([]connector.InboxThing, error) {
	var out serve.CollectResult
	err := c.call(ctx, serve.MCollect, struct{}{}, &out)
	return out.Things, err
}

func (c *Conn) Reply(ctx context.Context, r connector.InboxReply) error {
	return c.call(ctx, serve.MReply, r, nil)
}

// Tools answers from memory: what the program listed when it was last asked.
func (c *Conn) Tools() []proto.Tool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.tools)
}

func (c *Conn) Call(ctx context.Context, name string, args json.RawMessage) (string, error) {
	var out serve.CallResult
	err := c.call(ctx, serve.MCall, serve.CallParams{Name: name, Args: args}, &out)
	return out.Text, err
}

// Actions answers from memory, as Tools.
func (c *Conn) Actions() []proto.Tool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.actions)
}

func (c *Conn) Describe(ctx context.Context, name string, args json.RawMessage) (connector.Action, string, error) {
	var out serve.DescribeResult
	err := c.call(ctx, serve.MDescribe, serve.CallParams{Name: name, Args: args}, &out)
	return out.Action, out.Summary, err
}

// Do sends the write once, with an id of its own: the program runs one id at most once, and Do never sends it again
// (a run that timed out may have happened; only the user asks again).
func (c *Conn) Do(ctx context.Context, name string, args json.RawMessage) (string, error) {
	var id [16]byte
	_, _ = rand.Read(id[:])
	var out serve.CallResult
	err := c.call(ctx, serve.MDo, serve.DoParams{Name: name, Args: args, RequestID: hex.EncodeToString(id[:])}, &out)
	return out.Text, err
}

func (c *Conn) Thread(ctx context.Context, thread, before string, limit int) (connector.ThreadPage, error) {
	var out connector.ThreadPage
	err := c.call(ctx, serve.MThread, serve.ThreadParams{Thread: thread, Before: before, Limit: limit}, &out)
	return out, err
}

func (c *Conn) Media(ctx context.Context, ref string) (connector.Media, error) {
	var out connector.Media
	err := c.call(ctx, serve.MMedia, serve.MediaParams{Ref: ref}, &out)
	return out, err
}

// ItemSource is the program's knowledge-base items (items.list, items.doc_title); the hub asks only when it declared
// items.
func (c *Conn) ItemSource() kb.ItemSource { return connItems{c} }

type connItems struct{ c *Conn }

func (s connItems) Items(ctx context.Context) ([]kb.Item, error) {
	var out serve.ItemsResult
	err := s.c.call(ctx, serve.MItems, struct{}{}, &out)
	return out.Items, err
}

// DocTitle is "" when the program cannot tell (or fails): a title is a nicety.
func (s connItems) DocTitle(ctx context.Context, url string) string {
	var out serve.DocTitleResult
	if s.c.call(ctx, serve.MDocTitle, serve.DocTitleParams{URL: url}, &out) != nil {
		return ""
	}
	return out.Title
}

// ArchiveMedia is an archived message's attachment (archive.media); a program without it answers unknown_method and
// the line keeps [附件].
func (c *Conn) ArchiveMedia(ctx context.Context, ref string) (connector.Media, error) {
	var out connector.Media
	err := c.call(ctx, serve.MArchiveMedia, serve.MediaParams{Ref: ref}, &out)
	return out, err
}

func firstLine(s string) string {
	line, _, _ := bytes.Cut([]byte(s), []byte("\n"))
	return string(line)
}

// The capabilities Conn answers for: what the hub's pipes look for.
var (
	_ connector.Accounts     = (*Conn)(nil)
	_ connector.Login        = (*Conn)(nil)
	_ connector.Archive      = (*Conn)(nil)
	_ connector.Inbox        = (*Conn)(nil)
	_ connector.ReadTools    = (*Conn)(nil)
	_ connector.Actions      = (*Conn)(nil)
	_ connector.Threads      = (*Conn)(nil)
	_ connector.Events       = (*Conn)(nil)
	_ connector.Runner       = (*Conn)(nil)
	_ connector.Items        = (*Conn)(nil)
	_ connector.ArchiveMedia = (*Conn)(nil)
)

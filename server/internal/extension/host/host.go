// Package host runs plugins for the hub (docs/plan-extensions.md「四」「六」): it starts a plugin's program when
// something needs it and stops it when nothing does, restarts it with a backoff when it exits, speaks the plugin
// protocol with it (initialize, ping, namespaced calls over internal/rpc), and keeps the HTTP side of a plugin that
// serves one (GET /health). A plugin elsewhere is reached on its sockets instead of started.
package host

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/identity"
	"github.com/nwverse/persona/server/internal/osproc"
	"github.com/nwverse/persona/server/internal/rpc"
)

// States a plugin's process is in.
const (
	StateStopped  = "stopped"  // not running, and nothing needs it (not an error)
	StateStarting = "starting" // started, not ready yet
	StateRunning  = "running"
	StateError    = "error" // exited, failed to start, or not answering; it is tried again after a backoff
)

// Timings (variables: tests shorten them).
var (
	RestartFirst  = 2 * time.Second
	RestartLast   = 5 * time.Minute
	RanAWhile     = time.Minute
	ReadyWithin   = 30 * time.Second // initialize, or a first good /health for a lazily started one
	DefaultIdle   = 10 * time.Minute
	DefaultCall   = 30 * time.Second
	PingEvery     = 30 * time.Second
	PingTimeout   = 10 * time.Second
	HealthFirst   = time.Second      // /health while it is starting
	HealthEvery   = 10 * time.Second // and once it is up
	StopGrace     = 5 * time.Second  // after stdin closes, and again after SIGTERM
	StrikesToKill = 3                // timeouts or missed pings in a row before it is restarted
	LogLineMost   = 4 << 10
)

// ErrNotRunning is a call to a plugin that is not running and is not started by calls.
var ErrNotRunning = errors.New("not running")

// Spec is how to run one plugin.
type Spec struct {
	ID, Name string

	// Command is the program (an absolute path); "" when the hub does not run it (RPCSocket / Socket elsewhere).
	Command string
	Args    []string
	Dir     string
	Env     []string // added to the hub's environment
	// StartEnv is more environment, asked at each start of the program (a credential made new each time).
	StartEnv func() []string
	// CleanEnv: the program gets only the basics of the hub's environment (PassEnv), not all of it: the hub's may
	// hold its own credentials (a database URL). An installed plugin's is clean; the hub's own and hub.json's are not.
	CleanEnv bool
	// Wrap, when set, is the command line that really runs Command and Args (a sandbox around it), and more environment
	// for that one start; asked at each start.
	Wrap func(command string, args []string) (string, []string, []string, error)

	// RPC: it speaks the plugin protocol, on stdin / stdout when the hub runs it, else on RPCSocket.
	RPC       bool
	RPCSocket string
	// HTTP: it serves HTTP on Socket (the hub's path for a program it runs: removed before each start).
	HTTP   bool
	Socket string

	// Startup: it runs from the start. OnCall / OnHTTP: a call / a relayed request starts it, and it stops after
	// Idle without either. Holds (Hold) keep it running too.
	Startup, OnCall, OnHTTP bool
	Idle                    time.Duration
	CallTimeout             time.Duration

	// Init is what initialize sends, asked at each start (the configuration and secrets of the moment).
	Init func(ctx context.Context) (extension.InitializeParams, error)
	// Check runs before each start (a built-in program must be the hub's build); its error is the status's.
	Check func(ctx context.Context) error
	// Accept holds what the program said to what the hub expects; an error stops it.
	Accept func(extension.InitializeResult) error
	// Handle answers the program's calls; OnEvent takes its events.
	Handle  rpc.Handler
	OnEvent rpc.EventHandler
	// OnUp is called each time it becomes ready.
	OnUp func()
	Logf func(string, ...any)
	// Observe, when set, is told what the host sees of the plugin (docs/extensions.md「运行情况」): every call, every
	// run, every line of its log and of the host's own about it.
	Observe Observer
}

// Observer takes what the host sees of one plugin. Its methods are called from the host's goroutines and must not
// block.
type Observer interface {
	// Call is one call: how long it took, and "" when it was answered, else the error code (the plugin's own code,
	// CodeTimeout, CodeNotRunning, CodeCanceled when the caller gave up, CodeError for one without a code).
	Call(method string, took time.Duration, code string)
	// Run is a run starting (RunStart), ending because nothing needed it (RunStop), or ending in error (RunExit,
	// detail is why; it is started again after a backoff).
	Run(event, detail string)
	// Log is a line of the program's own log (LogProgram: its stderr, or its stdout without the protocol) or of the
	// host's about it (LogHub).
	Log(source, line string)
}

// What an Observer is told.
const (
	RunStart = "start"
	RunStop  = "stop"
	RunExit  = "exit"

	LogProgram = "program"
	LogHub     = "hub"

	CodeNotRunning = "not_running"
	CodeCanceled   = "canceled"
	CodeError      = "error"
)

func (s *Spec) idle() time.Duration {
	if s.Idle > 0 {
		return s.Idle
	}
	return DefaultIdle
}

func (s *Spec) callTimeout() time.Duration {
	if s.CallTimeout > 0 {
		return s.CallTimeout
	}
	return DefaultCall
}

// session is one run's protocol connection.
type session struct {
	peer   *rpc.Peer
	result extension.InitializeResult
}

// Proc is one plugin as the hub runs it.
type Proc struct {
	spec   Spec
	cancel context.CancelFunc
	done   chan struct{}
	kick   chan struct{}
	client *http.Client // to its HTTP socket

	mu       sync.Mutex
	state    string
	err      string
	sess     *session
	ready    bool          // initialized (RPC) or answering /health (HTTP only)
	change   chan struct{} // closed and replaced whenever state, sess or ready change
	holds    int
	openHTTP int
	calls    int // in flight
	lastUse  time.Time
	strikes  int
	kill     func() // ends the current run (a restart); nil between runs
}

// Start runs spec until Stop or ctx ends. Nothing starts before something needs it, unless spec.Startup.
func Start(ctx context.Context, spec Spec) *Proc {
	if spec.Logf == nil {
		spec.Logf = func(string, ...any) {}
	}
	if spec.Name == "" {
		spec.Name = spec.ID
	}
	ctx, cancel := context.WithCancel(ctx)
	p := &Proc{spec: spec, cancel: cancel, done: make(chan struct{}), kick: make(chan struct{}, 1), state: StateStopped,
		change: make(chan struct{})}
	if spec.Startup {
		p.state = StateStarting // it starts right away
	}
	if spec.HTTP {
		dial := func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", spec.Socket)
		}
		p.client = &http.Client{Transport: &http.Transport{DialContext: dial}, Timeout: 5 * time.Second}
	}
	go func() {
		defer close(p.done)
		p.loop(ctx)
	}()
	return p
}

// Stop stops the plugin and waits for its program to end.
func (p *Proc) Stop() {
	p.cancel()
	<-p.done
}

// Spec is what the plugin runs as.
func (p *Proc) Spec() Spec { return p.spec }

// Status is its state and, in error, why.
func (p *Proc) Status() (state, err string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state, p.err
}

// Result is what the program said at initialize this run (false while not initialized).
func (p *Proc) Result() (extension.InitializeResult, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.sess == nil {
		return extension.InitializeResult{}, false
	}
	return p.sess.result, true
}

func (p *Proc) setLocked(state, err string) {
	p.state, p.err = state, err
	close(p.change)
	p.change = make(chan struct{})
}

func (p *Proc) set(state, err string) {
	p.mu.Lock()
	p.setLocked(state, err)
	p.mu.Unlock()
}

func (p *Proc) poke() {
	select {
	case p.kick <- struct{}{}:
	default:
	}
}

func (p *Proc) touch() {
	p.mu.Lock()
	p.lastUse = time.Now()
	p.mu.Unlock()
	p.poke()
}

// wantedLocked says whether something needs it running now.
func (p *Proc) wantedLocked() bool {
	lazy := (p.spec.OnCall || p.spec.OnHTTP) && !p.lastUse.IsZero() && time.Since(p.lastUse) < p.spec.idle()
	return p.spec.Startup || p.holds > 0 || p.openHTTP > 0 || lazy
}

// Idle says whether nothing is using it now: no call in flight, no HTTP request or WebSocket open (a connector's hold
// does not count: it holds the plugin for as long as it is on).
func (p *Proc) Idle() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls == 0 && p.openHTTP == 0
}

// Hold keeps it running until release is called (a switched-on connector holds its plugin).
func (p *Proc) Hold() (release func()) {
	p.mu.Lock()
	p.holds++
	p.mu.Unlock()
	p.poke()
	var once sync.Once
	return func() {
		once.Do(func() {
			p.mu.Lock()
			p.holds--
			p.mu.Unlock()
			p.poke()
		})
	}
}

// Wait waits until it is ready or stops trying: nil once ready; ctx's error, or the run's error after it failed and
// nothing will start it again before a backoff.
func (p *Proc) Wait(ctx context.Context) error {
	for {
		p.mu.Lock()
		ready, state, why, ch := p.ready, p.state, p.err, p.change
		p.mu.Unlock()
		switch {
		case ready:
			return nil
		case state == StateError:
			return fmt.Errorf("%s: %s", p.spec.Name, why)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ch:
		}
	}
}

// Changes is closed at the next change of state (a connector refreshes when its plugin comes up).
func (p *Proc) Changes() <-chan struct{} {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.change
}

// Ready says whether it is ready now.
func (p *Proc) Ready() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ready
}

// Call runs a method on the program: when it is not running, a plugin started by calls is started and waited for;
// any other is ErrNotRunning. A call that times out is answered with the timeout code, and the program is
// restarted after StrikesToKill of them in a row.
func (p *Proc) Call(ctx context.Context, method string, params, out any) error {
	if p.spec.Observe == nil {
		return p.call(ctx, method, params, out)
	}
	start := time.Now()
	err := p.call(ctx, method, params, out)
	p.spec.Observe.Call(method, time.Since(start), callCode(ctx, err))
	return err
}

// callCode is how an Observer is told a call ended.
func callCode(ctx context.Context, err error) string {
	var re *rpc.Error
	switch {
	case err == nil:
		return ""
	case errors.As(err, &re) && re.Code != "":
		return re.Code
	case errors.Is(err, ErrNotRunning):
		return CodeNotRunning
	case ctx.Err() != nil:
		return CodeCanceled
	}
	return CodeError
}

func (p *Proc) call(ctx context.Context, method string, params, out any) error {
	if !p.spec.RPC {
		return fmt.Errorf("%s does not speak the plugin protocol", p.spec.Name)
	}
	p.mu.Lock()
	s := p.sess
	lazy := p.spec.OnCall
	starting := p.state == StateStarting
	p.calls++
	p.mu.Unlock()
	defer func() { p.mu.Lock(); p.calls--; p.mu.Unlock() }()
	if lazy {
		// The call keeps it running while it waits and runs (a slow start must not look idle).
		release := p.Hold()
		defer func() { p.touch(); release() }()
		p.touch()
	}
	if s == nil {
		if !lazy && !starting {
			return fmt.Errorf("%s: %w", p.spec.Name, ErrNotRunning)
		}
		wctx, cancel := context.WithTimeout(ctx, ReadyWithin)
		err := p.Wait(wctx)
		cancel()
		if err != nil {
			return err
		}
		p.mu.Lock()
		s = p.sess
		p.mu.Unlock()
		if s == nil {
			return fmt.Errorf("%s: %w", p.spec.Name, ErrNotRunning)
		}
	}
	cctx, cancel := context.WithTimeout(ctx, p.spec.callTimeout())
	defer cancel()
	err := s.peer.Call(cctx, method, params, out)
	if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
		p.strike(method + " timed out")
		return &rpc.Error{Code: extension.CodeTimeout, Message: fmt.Sprintf("%s did not answer %s in %s", p.spec.Name, method, p.spec.callTimeout())}
	}
	if err == nil {
		p.mu.Lock()
		p.strikes = 0
		p.mu.Unlock()
	}
	return err
}

// Notify sends the program an event.
func (p *Proc) Notify(event string, params any) error {
	p.mu.Lock()
	s := p.sess
	p.mu.Unlock()
	if s == nil {
		return ErrNotRunning
	}
	return s.peer.Notify(event, params)
}

// strike counts a timeout or a missed ping; enough in a row restart the program.
func (p *Proc) strike(why string) {
	p.mu.Lock()
	p.strikes++
	kill := p.kill
	n := p.strikes
	if n >= StrikesToKill {
		p.strikes = 0
	}
	p.mu.Unlock()
	if n >= StrikesToKill && kill != nil {
		p.note("%s, %d times in a row: restarting it", why, n)
		kill()
	}
}

// BeginHTTP is a request about to be relayed to the plugin's HTTP side: a plugin started by requests is started and
// waited for. end is called when the request is over (a WebSocket's end included).
func (p *Proc) BeginHTTP(ctx context.Context) (end func(), err error) {
	p.mu.Lock()
	p.openHTTP++
	p.lastUse = time.Now()
	ready := p.ready
	p.mu.Unlock()
	p.poke()
	end = func() {
		p.mu.Lock()
		p.openHTTP--
		p.lastUse = time.Now()
		p.mu.Unlock()
		p.poke()
	}
	if !ready && p.spec.OnHTTP {
		wctx, cancel := context.WithTimeout(ctx, ReadyWithin)
		defer cancel()
		if err := p.Wait(wctx); err != nil {
			end()
			return nil, err
		}
	}
	return end, nil
}

// Healthy says whether its HTTP side answers GET /health with 200.
func (p *Proc) Healthy(ctx context.Context) bool {
	if p.client == nil {
		return false
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://plugin/health", nil)
	res, err := p.client.Do(req)
	if err != nil {
		return false
	}
	_, _ = io.Copy(io.Discard, res.Body)
	res.Body.Close()
	return res.StatusCode == http.StatusOK
}

// Client is an HTTP client to its socket.
func (p *Proc) Client() *http.Client { return p.client }

// loop starts the program whenever something needs it, until ctx ends.
func (p *Proc) loop(ctx context.Context) {
	wait := RestartFirst
	for {
		if !p.waitWanted(ctx) {
			p.set(StateStopped, "")
			return
		}
		start := time.Now()
		err := p.runOnce(ctx)
		p.mu.Lock()
		p.sess, p.ready, p.kill, p.strikes = nil, false, nil, 0
		p.mu.Unlock()
		if ctx.Err() != nil {
			p.set(StateStopped, "")
			return
		}
		if err == nil { // nothing needed it any more
			p.run(RunStop, "")
			p.set(StateStopped, "")
			wait = RestartFirst
			continue
		}
		if time.Since(start) >= RanAWhile {
			wait = RestartFirst
		}
		p.set(StateError, err.Error())
		p.run(RunExit, err.Error())
		p.note("%v (again in %s)", err, wait)
		select {
		case <-ctx.Done():
			p.set(StateStopped, "")
			return
		case <-time.After(wait):
		}
		wait = min(wait*2, RestartLast)
	}
}

// waitWanted waits until something needs it; false when ctx ended first.
func (p *Proc) waitWanted(ctx context.Context) bool {
	for {
		p.mu.Lock()
		wanted := p.wantedLocked()
		p.mu.Unlock()
		if wanted {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-p.kick:
		}
	}
}

// child is a run's program, or a remote plugin's connection.
type child struct {
	cmd   *exec.Cmd
	stdin io.Closer
	conn  net.Conn
	// exited is closed once the program has ended, with why in waitErr (never, for a plugin elsewhere). Closed, not
	// sent on: the run and then end both wait for it, and a value taken by the first left end waiting forever (#2277).
	exited  chan struct{}
	waitErr error
}

// runOnce runs the plugin once: nil when it was stopped because nothing needed it, the reason otherwise.
func (p *Proc) runOnce(ctx context.Context) error {
	p.set(StateStarting, "")
	if p.spec.Check != nil {
		if err := p.spec.Check(ctx); err != nil {
			return err
		}
	}
	ch, out, err := p.spawn(ctx)
	if err != nil {
		return err
	}
	p.run(RunStart, "")
	runCtx, stopRun := context.WithCancel(ctx)
	defer stopRun()
	var s *session
	if p.spec.RPC {
		peer := rpc.New("h", writerOf(ch))
		peer.OnRequest = p.spec.Handle
		peer.OnEvent = p.spec.OnEvent
		go func() { _ = peer.Serve(context.Background(), out) }()
		s = &session{peer: peer}
		if err := p.initialize(runCtx, s); err != nil {
			p.end(ch, peer)
			return err
		}
	}
	killed := make(chan struct{})
	var once sync.Once
	p.mu.Lock()
	p.sess = s
	p.kill = func() { once.Do(func() { close(killed) }) }
	p.mu.Unlock()
	defer func() {
		var peer *rpc.Peer
		if s != nil {
			peer = s.peer
		}
		p.end(ch, peer)
	}()
	if p.spec.RPC {
		p.up()
	}

	health := time.NewTicker(HealthFirst)
	defer health.Stop()
	ping := time.NewTicker(PingEvery)
	defer ping.Stop()
	var peerDone <-chan struct{}
	if s != nil {
		peerDone = s.peer.Done()
	}
	misses := 0
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ch.exited:
			if ch.waitErr == nil {
				return errors.New("exited")
			}
			return ch.waitErr
		case <-peerDone:
			return fmt.Errorf("the connection closed: %v", s.peer.Err())
		case <-killed:
			return errors.New("not answering")
		case <-p.kick:
			p.mu.Lock()
			wanted := p.wantedLocked()
			p.mu.Unlock()
			if !wanted {
				return nil
			}
		case <-health.C:
			p.mu.Lock()
			wanted := p.wantedLocked()
			p.mu.Unlock()
			if !wanted {
				return nil
			}
			if p.spec.HTTP {
				p.checkHealth(runCtx, health)
			}
		case <-ping.C:
			if s == nil {
				continue
			}
			pctx, cancel := context.WithTimeout(runCtx, PingTimeout)
			err := s.peer.Call(pctx, extension.MPing, struct{}{}, nil)
			cancel()
			if err != nil {
				if misses++; misses >= StrikesToKill {
					return fmt.Errorf("missed %d pings", misses)
				}
			} else {
				misses = 0
			}
		}
	}
}

// up marks the plugin ready.
func (p *Proc) up() {
	p.mu.Lock()
	p.ready = true
	p.setLocked(StateRunning, "")
	p.mu.Unlock()
	if p.spec.OnUp != nil {
		go p.spec.OnUp()
	}
}

// checkHealth asks /health: an HTTP-only plugin is ready once it answers. A started one stays starting until then;
// one elsewhere is an error while it does not answer.
func (p *Proc) checkHealth(ctx context.Context, t *time.Ticker) {
	ok := p.Healthy(ctx)
	p.mu.Lock()
	state := p.state
	p.mu.Unlock()
	switch {
	case ok && !p.spec.RPC && state != StateRunning:
		p.up()
		t.Reset(HealthEvery)
	case ok && state == StateError:
		p.set(StateRunning, "")
	case !ok && state == StateRunning:
		p.mu.Lock()
		p.ready = p.spec.RPC
		p.setLocked(StateError, "health check failed")
		p.mu.Unlock()
		t.Reset(HealthFirst)
	case !ok && p.spec.Command == "" && state == StateStarting:
		p.set(StateError, "not answering on "+p.spec.Socket)
	}
}

// initialize sends initialize and holds the answer to the spec.
func (p *Proc) initialize(ctx context.Context, s *session) error {
	params := extension.InitializeParams{Protocol: extension.Protocol}
	if p.spec.Init != nil {
		var err error
		if params, err = p.spec.Init(ctx); err != nil {
			return err
		}
		params.Protocol = extension.Protocol
	}
	ictx, cancel := context.WithTimeout(ctx, ReadyWithin)
	defer cancel()
	var res extension.InitializeResult
	if err := s.peer.Call(ictx, extension.MInitialize, params, &res); err != nil {
		var re *rpc.Error
		if errors.As(err, &re) && re.Code == extension.CodeWrongProtocol {
			return fmt.Errorf("协议版本不对：%s", re.Message)
		}
		return fmt.Errorf("没准备好：%v", err)
	}
	if res.Protocol != extension.Protocol {
		return fmt.Errorf("协议版本是 %d，hub 要 %d", res.Protocol, extension.Protocol)
	}
	if res.ID != params.Extension.ID && params.Extension.ID != "" {
		return fmt.Errorf("the program says it is %q, not %q", res.ID, params.Extension.ID)
	}
	if res.Version != params.Extension.Version && params.Extension.Version != "" {
		return fmt.Errorf("the program says it is version %q, not %q", res.Version, params.Extension.Version)
	}
	if p.spec.Accept != nil {
		if err := p.spec.Accept(res); err != nil {
			return err
		}
	}
	s.result = res
	return nil
}

// spawn starts the program (or connects to the plugin elsewhere): what to write to and read from.
func (p *Proc) spawn(ctx context.Context) (*child, io.Reader, error) {
	if p.spec.Command == "" {
		ch := &child{exited: make(chan struct{})}
		if !p.spec.RPC {
			return ch, nil, nil
		}
		var d net.Dialer
		dctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		conn, err := d.DialContext(dctx, "unix", p.spec.RPCSocket)
		cancel()
		if err != nil {
			return nil, nil, fmt.Errorf("连不上 %s：%v", p.spec.RPCSocket, err)
		}
		ch.conn = conn
		return ch, conn, nil
	}
	if p.spec.HTTP {
		if err := os.MkdirAll(filepath.Dir(p.spec.Socket), 0o700); err != nil {
			return nil, nil, err
		}
		_ = os.Remove(p.spec.Socket) // left by a run that was killed
	}
	if _, err := os.Stat(p.spec.Command); err != nil && filepath.IsAbs(p.spec.Command) {
		return nil, nil, fmt.Errorf("没有找到 %s 的程序（%s）", p.spec.Name, p.spec.Command)
	}
	name, args := p.spec.Command, p.spec.Args
	var wrapEnv []string
	if p.spec.Wrap != nil {
		var err error
		if name, args, wrapEnv, err = p.spec.Wrap(name, args); err != nil {
			return nil, nil, fmt.Errorf("%s 的沙箱起不来：%v", p.spec.Name, err)
		}
	}
	cmd := exec.Command(name, args...)
	cmd.Dir = p.spec.Dir
	base := os.Environ()
	if p.spec.CleanEnv {
		base = basics(base)
	}
	env := p.spec.Env
	if p.spec.StartEnv != nil {
		env = append(slices.Clone(env), p.spec.StartEnv()...)
	}
	env = append(slices.Clone(env), wrapEnv...)
	cmd.Env = identity.Environment(base, env...)
	cmd.SysProcAttr = osproc.GroupAttr() // so stopping it takes its children too
	ch := &child{cmd: cmd, exited: make(chan struct{})}
	var out io.Reader
	if p.spec.RPC {
		stdin, err := cmd.StdinPipe()
		if err != nil {
			return nil, nil, err
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return nil, nil, err
		}
		stderr, err := cmd.StderrPipe()
		if err != nil {
			return nil, nil, err
		}
		ch.stdin, out = stdin, stdout
		go p.logLines(stderr)
	} else {
		logs, err := cmd.StdoutPipe()
		if err != nil {
			return nil, nil, err
		}
		cmd.Stderr = cmd.Stdout
		go p.logLines(logs)
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, fmt.Errorf("%s 的程序起不来：%v", p.spec.Name, err)
	}
	go func() {
		ch.waitErr = cmd.Wait()
		close(ch.exited)
	}()
	return ch, out, nil
}

func writerOf(ch *child) io.Writer {
	if ch.conn != nil {
		return ch.conn
	}
	if w, ok := ch.stdin.(io.Writer); ok {
		return w
	}
	return io.Discard
}

// end stops a run: closes stdin (a program exits when the hub goes away), then SIGTERM, then SIGKILL, to its whole
// process group. A program that does not speak the protocol gets SIGTERM right away.
func (p *Proc) end(ch *child, peer *rpc.Peer) {
	if peer != nil {
		peer.Close(rpc.ErrClosed)
	}
	if ch.conn != nil {
		_ = ch.conn.Close()
	}
	if ch.cmd == nil || ch.cmd.Process == nil {
		return
	}
	pid := ch.cmd.Process.Pid
	if ch.stdin != nil {
		_ = ch.stdin.Close()
		select {
		case <-ch.exited:
			_ = osproc.StopGroup(pid, false) // its children, if it left any
			return
		case <-time.After(StopGrace):
		}
	}
	_ = osproc.StopGroup(pid, false)
	select {
	case <-ch.exited:
	case <-time.After(StopGrace):
		_ = osproc.StopGroup(pid, true)
		<-ch.exited
	}
}

// logLines keeps what the program writes (its own log), a line at a time, each cut to LogLineMost.
func (p *Proc) logLines(r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if len(line) > LogLineMost {
			line = line[:LogLineMost] + "…"
		}
		p.spec.Logf("plugin %s: %s", p.spec.ID, line)
		if p.spec.Observe != nil {
			p.spec.Observe.Log(LogProgram, line)
		}
	}
}

// note is a line of the host's own about the plugin: in the hub's log, and its Observer's.
func (p *Proc) note(format string, args ...any) {
	line := fmt.Sprintf(format, args...)
	p.spec.Logf("plugin %s: %s", p.spec.ID, line)
	if p.spec.Observe != nil {
		p.spec.Observe.Log(LogHub, line)
	}
}

// run tells the Observer about a run.
func (p *Proc) run(event, detail string) {
	if p.spec.Observe != nil {
		p.spec.Observe.Run(event, detail)
	}
}

// PassEnv are the variables of the hub's environment a plugin with a clean environment still gets: where things are,
// the locale and time zone, and how to reach the network (proxies, certificates).
var PassEnv = []string{"PATH", "HOME", "USER", "LOGNAME", "SHELL", "LANG", "LC_ALL", "LC_CTYPE", "TZ", "TMPDIR",
	"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "no_proxy", "all_proxy",
	"SSL_CERT_FILE", "SSL_CERT_DIR"}

func basics(env []string) []string {
	var out []string
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		for _, pass := range PassEnv {
			if k == pass {
				out = append(out, kv)
				break
			}
		}
	}
	return out
}

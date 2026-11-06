package node

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/identity"
	"github.com/nwverse/persona/server/internal/nodeclient"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// While a resident node has no manager connection (the manager restarting, crashed, or not installed yet), it answers
// node.sock itself in standby: what hooks report and what `personad notify` sends goes into a bounded spool persisted
// under the data dir, and the next manager connection's Node replays it, in order, before it takes new hooks. Hooks
// that wait on the phone, and `personad ask`, are told to retry while the manager has been away less than standbyHold,
// so a manager restart does not drop their card to the terminal; past that they go to the terminal at once, as without
// a manager. Everything else on the socket (MCP, launchers, CLI calls) is answered "away", which clients take for "no
// node", exactly as before (docs/protocol.md "When the manager is away").

// spoolFile is the spool's name in the data dir: one JSON entry per line, oldest first.
const spoolFile = "node-spool.jsonl"

// Bounds of the spool: the oldest entries go first when either is reached. An entry older than spoolMaxAge is dropped
// at replay: a day-old "done" is no news.
var (
	spoolMost      = 500
	spoolBytesMost = 1 << 20
	spoolMaxAge    = 24 * time.Hour
)

// standbyAway is what standby says it cannot do.
const standbyAway = "the manager is not connected to this computer"

// spoolBusy is the manager's answer to a notification whose last try it still has (devices/spooldedupe.go).
const spoolBusy = "spool_busy"

// spoolTriesMost: a notification the manager refused this often is dropped (it would hold the spool forever).
const spoolTriesMost = 3

// replayNotifyTimeout bounds one replayed notification, replayStateWait the wait for the session state: new hooks
// wait for the replay.
var (
	replayNotifyTimeout = 5 * time.Second
	replayStateWait     = 2 * time.Second
	// replayGateMost is the longest a new hook / ask / notify waits for the replay.
	replayGateMost = 20 * time.Second
)

// standbyHold is how long after the manager went away hooks that wait on the phone are told to retry.
var standbyHold = 10 * time.Minute

// SpoolPath is where a resident node keeps what it took while its manager was away.
func SpoolPath() string { return filepath.Join(identity.HomeDir(), spoolFile) }

// spoolEntry is one thing taken in standby. ID goes with it to the manager (spool_id), which drops a repeat.
type spoolEntry struct {
	ID      string      `json:"id"`
	At      time.Time   `json:"at"`
	Kind    string      `json:"kind"` // "hook" or "notify"
	Request sockRequest `json:"request"`
	Tries   int         `json:"tries,omitempty"` // replays that failed
}

// spool is the file of entries. Only the resident node's process uses it (Attach): its standby writes it and its
// connections' Nodes replay it, one at a time (replayMu).
type spool struct {
	path     string
	replayMu sync.Mutex
}

var spoolMu sync.Mutex // one spool file per process

func openSpool(path string) *spool { return &spool{path: path} }

// load reads the entries; a line that does not parse is skipped.
func (s *spool) load() []spoolEntry {
	spoolMu.Lock()
	defer spoolMu.Unlock()
	return s.loadLocked()
}

func (s *spool) loadLocked() []spoolEntry {
	b, err := os.ReadFile(s.path)
	if err != nil {
		return nil
	}
	var out []spoolEntry
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 0, 64<<10), spoolBytesMost+1)
	for sc.Scan() {
		var e spoolEntry
		if json.Unmarshal(sc.Bytes(), &e) == nil && e.ID != "" {
			out = append(out, e)
		}
	}
	return out
}

// add appends e, dropping the oldest entries past the bounds. dropped is how many went.
func (s *spool) add(e spoolEntry) (dropped int, err error) {
	spoolMu.Lock()
	defer spoolMu.Unlock()
	line, err := json.Marshal(e)
	if err != nil {
		return 0, err
	}
	if len(line) >= spoolBytesMost {
		return 0, errors.New("too large to keep")
	}
	all := append(s.loadLocked(), e)
	lines := make([][]byte, 0, len(all))
	size := 0
	for _, x := range all {
		b, _ := json.Marshal(x)
		lines = append(lines, b)
		size += len(b) + 1
	}
	for len(lines) > spoolMost || size > spoolBytesMost {
		size -= len(lines[0]) + 1
		lines = lines[1:]
		dropped++
	}
	return dropped, s.writeLocked(lines)
}

// failed counts a failed replay of id and says whether it is to be dropped now.
func (s *spool) failed(id string) (drop bool, err error) {
	spoolMu.Lock()
	defer spoolMu.Unlock()
	var lines [][]byte
	for _, x := range s.loadLocked() {
		if x.ID == id {
			if x.Tries++; x.Tries >= spoolTriesMost {
				drop = true
				continue
			}
		}
		b, _ := json.Marshal(x)
		lines = append(lines, b)
	}
	return drop, s.writeLocked(lines)
}

// remove takes the entry id out (it was delivered).
func (s *spool) remove(id string) error {
	spoolMu.Lock()
	defer spoolMu.Unlock()
	var lines [][]byte
	for _, x := range s.loadLocked() {
		if x.ID != id {
			b, _ := json.Marshal(x)
			lines = append(lines, b)
		}
	}
	return s.writeLocked(lines)
}

// writeLocked replaces the file with lines (atomically: a crash leaves the old file or the new one); none removes it.
func (s *spool) writeLocked(lines [][]byte) error {
	if len(lines) == 0 {
		if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	_ = os.Remove(tmp) // a leftover keeps its mode through O_TRUNC
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	for _, l := range lines {
		_, _ = w.Write(l)
		_ = w.WriteByte('\n')
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// holdsForPhone: the hook would wait on the phone if the manager were there (asksPhone), so in standby it is told to
// retry. A Bash PreToolUse waits only for the assistant's gated commands, which standby cannot tell: it goes on.
func holdsForPhone(in hookInput, req sockRequest) bool {
	if req.ReportOnly || req.Attempt == "" || !asksPhone(in) {
		return false
	}
	return !(in.HookEventName == "PreToolUse" && in.ToolName == "Bash")
}

// spooled says whether standby keeps a hook's event: what the node reports to the manager (notifyHookEvent). A
// resumed hook was reported by the node it first reached; a global report-only one only for the events it reports.
func spooled(in hookInput, req sockRequest) bool {
	if req.Resume || !validID(in.SessionID) {
		return false
	}
	return !req.ReportOnly || reportedEvent(in)
}

// standby answers node.sock while no manager connection is served (Attach). It listens only where nothing else
// answers: a `node --stdio` that serves by itself (no resident node reached in time) keeps the socket, and standby
// takes it once that one is gone.
type standby struct {
	sp   *spool
	logf func(string, ...any)
	now  func() time.Time
	// retry is how often standby looks whether it can (or still does) own node.sock while it is wanted.
	retry time.Duration

	mu      sync.Mutex
	wanted  bool
	ln      net.Listener
	lnFile  os.FileInfo // the socket file ln listens on: another one at the path means someone took it
	looping bool
	conns   sync.WaitGroup
	healthy time.Time // the manager was last there for a while (or the resident node started)
}

func newStandby(sp *spool, logf func(string, ...any)) *standby {
	return &standby{sp: sp, logf: logf, now: time.Now, retry: 5 * time.Second}
}

// want makes standby answer node.sock from now on (as soon as nothing else does).
func (s *standby) want() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wanted = true
	if s.healthy.IsZero() {
		s.healthy = s.now() // the resident node starting: its manager is likely on its way
	}
	s.tryListenLocked()
	if !s.looping {
		s.looping = true
		go s.loop()
	}
}

// managerSeen: a manager connection was served for a while, until at; the hold counts from there.
func (s *standby) managerSeen(at time.Time) {
	s.mu.Lock()
	s.healthy = at
	s.mu.Unlock()
}

// unwant stops standby (a manager connection's Node takes node.sock) and waits for the requests it took.
func (s *standby) unwant() {
	s.mu.Lock()
	s.wanted = false
	s.closeLocked()
	s.mu.Unlock()
	s.conns.Wait()
}

func (s *standby) loop() {
	t := time.NewTicker(s.retry)
	defer t.Stop()
	for range t.C {
		s.mu.Lock()
		if !s.wanted {
			s.looping = false
			s.mu.Unlock()
			return
		}
		if s.ln != nil && !s.ownsPathLocked() {
			s.closeLocked() // a node serving by itself took the path: let it be, and take it back when it is gone
		}
		s.tryListenLocked()
		s.mu.Unlock()
	}
}

func (s *standby) closeLocked() {
	if s.ln != nil {
		s.ln.Close()
		s.ln, s.lnFile = nil, nil
	}
}

func (s *standby) ownsPathLocked() bool {
	fi, err := os.Stat(nodeclient.SocketPath())
	return err == nil && s.lnFile != nil && os.SameFile(fi, s.lnFile)
}

// tryListenLocked listens on node.sock unless standby already does or something else answers there.
func (s *standby) tryListenLocked() {
	if !s.wanted || s.ln != nil {
		return
	}
	path := nodeclient.SocketPath()
	if answers(path) {
		return // a live node serves it
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		s.logf("standby: %v", err)
		return
	}
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		s.logf("standby: %v", err)
		return
	}
	// Never unlink on close: the next Node listens on the same path, maybe before this one is closed.
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	_ = os.Chmod(path, 0o600)
	fi, err := os.Stat(path)
	if err != nil {
		ln.Close()
		s.logf("standby: %v", err)
		return
	}
	s.ln, s.lnFile = ln, fi
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			s.mu.Lock()
			current := s.ln == ln
			if current {
				s.conns.Add(1) // under mu: unwant's Wait sees every request taken before it closed ln
			}
			s.mu.Unlock()
			if !current {
				c.Close()
				return
			}
			go func() {
				defer s.conns.Done()
				s.serve(c)
			}()
		}
	}()
}

// answers says whether a node serves path: it answers a ping (a listener that is closing, or a stopped process whose
// backlog still accepts, does not).
func answers(path string) bool {
	c, err := net.DialTimeout("unix", path, 300*time.Millisecond)
	if err != nil {
		return false
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(time.Second))
	if _, err := c.Write([]byte(`{"kind":"ping"}` + "\n")); err != nil {
		return false
	}
	line, err := bufio.NewReader(c).ReadBytes('\n')
	return err == nil && len(line) > 1
}

func (s *standby) listening() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ln != nil
}

// holding: the manager was there less than standbyHold ago, so a hook that would wait on the phone is told to retry
// (each one for at most nodeclient.ResumeWait). A manager that only flaps (connections shorter than a minute) does not
// renew it.
func (s *standby) holding() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.now().Sub(s.healthy) < standbyHold
}

func (s *standby) serve(c net.Conn) {
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(2 * time.Second)) // a request is one short line; unwant waits for this
	line, err := bufio.NewReader(c).ReadBytes('\n')
	if err != nil {
		return
	}
	var req sockRequest
	if json.Unmarshal(line, &req) != nil {
		return
	}
	b, _ := json.Marshal(s.answer(req))
	_, _ = c.Write(append(b, '\n'))
}

// answer is standby's reply to req (one line on the socket, what a Node would answer when it could).
func (s *standby) answer(req sockRequest) any {
	switch req.Kind {
	case "hook":
		var in hookInput
		if json.Unmarshal(req.Input, &in) != nil {
			return map[string]any{"output": nil}
		}
		if spooled(in, req) {
			s.keep("hook", req)
		}
		if holdsForPhone(in, req) && s.holding() {
			return map[string]any{"retry": true} // the hook asks again; the manager is likely back soon
		}
		return map[string]any{"output": nil} // the terminal decides, as with no manager at all
	case "hook.decided", "ping":
		return map[string]any{"ok": true, "standby": true}
	case "ask":
		if req.Attempt != "" && s.holding() {
			return nodeclient.AskReply{Retry: true}
		}
		return nodeclient.AskReply{Status: nodeclient.AskUnavailable, Error: standbyAway}
	case "notify":
		var nr nodeclient.NotifyRequest
		if json.Unmarshal(req.Input, &nr) != nil {
			return nodeclient.NotifyReply{Error: "bad request"}
		}
		if strings.TrimSpace(nr.Title) == "" && strings.TrimSpace(nr.Body) == "" {
			return nodeclient.NotifyReply{Error: "the notification is empty"}
		}
		if err := s.keep("notify", req); err != nil {
			return nodeclient.NotifyReply{Error: "could not keep it for the manager: " + err.Error()}
		}
		return nodeclient.NotifyReply{Queued: true}
	case "mcp.tools": // an older `personad mcp` (no away_ok): what a node says when it cannot reach its manager
		if !req.AwayOK {
			return map[string]any{"tools": []proto.Tool{}, "error": standbyAway}
		}
	case "mcp.call":
		if !req.AwayOK {
			return proto.TextResult("Persona hub error: "+standbyAway+"; the call was not run", true)
		}
	}
	return map[string]any{"away": true} // the client takes it for no node (nodeclient.dial)
}

func (s *standby) keep(kind string, req sockRequest) error {
	dropped, err := s.sp.add(spoolEntry{ID: newUUID(), At: s.now().UTC(), Kind: kind, Request: req})
	if err != nil {
		s.logf("standby: keep %s: %v", kind, err)
	} else if dropped > 0 {
		s.logf("standby: spool full, dropped the %d oldest", dropped)
	}
	return err
}

// replaySpool hands the manager what standby took, in order, then opens the gate for new hooks (n.replayed). It
// waits for the session state first (takesReport needs it: an empty state took every session for one Persona does not
// report for). An entry goes from the file once it was sent; one that could not be sent stays, and the replay stops
// there, for the next connection. One the manager got twice (the node stopped between sending and removing) it drops
// by spool_id.
func (n *Node) replaySpool(ctx context.Context, sp *spool) {
	defer close(n.replayed)
	if len(sp.load()) == 0 {
		return
	}
	select { // not for long: new hooks wait meanwhile, and a report-only one gives up after 3 s
	case <-n.stateReady:
	case <-time.After(replayStateWait):
	case <-ctx.Done():
		return
	}
	sp.replayMu.Lock() // the last connection's replay may still be on its last entry
	defer sp.replayMu.Unlock()
	sent, stale := 0, 0
	slow := false // a notification timed out: the others wait for the next connection, not the gate
	defer func() {
		if sent+stale > 0 {
			n.logger.Printf("spool: replayed %d taken while the manager was away (%d too old, dropped)", sent, stale)
		}
	}()
	for _, e := range sp.load() {
		if ctx.Err() != nil {
			return
		}
		if time.Since(e.At) > spoolMaxAge {
			stale++
		} else {
			var err error
			switch e.Kind {
			case "hook":
				err = n.replayHook(e)
			case "notify":
				if slow {
					continue // the manager is not answering notifications this time: kept, untried, for the next
				}
				cctx, cancel := context.WithTimeout(ctx, replayNotifyTimeout)
				rep, cerr := n.notifyCall(cctx, e.Request, e.ID)
				timedOut := cctx.Err() != nil && ctx.Err() == nil
				cancel()
				if cerr == nil && rep.Error != "" { // refused before the manager was asked (an empty one): never sendable
					cerr = &rpc.Error{Message: rep.Error}
				}
				var refused *rpc.Error
				switch {
				case cerr == nil:
				case errors.Is(cerr, rpc.ErrClosed):
					return // the connection is gone: it and the rest stay for the next one, no try counted
				case errors.As(cerr, &refused) && refused.Code == spoolBusy:
					continue // the manager still has the last try: kept, not counted
				case errors.As(cerr, &refused) || timedOut:
					err = cerr
					slow = timedOut
				default:
					return // the connection is gone: it and the rest stay for the next one, no try counted
				}
			}
			if err != nil {
				if ctx.Err() != nil || e.Kind == "hook" {
					return // the connection is gone (a hook.event write fails only then): all kept for the next one
				}
				// The manager refused or did not answer a notification: it is kept (dropped after a few tries) and
				// the rest goes on, so the hook events after it keep their order.
				drop, ferr := sp.failed(e.ID)
				n.logger.Printf("spool: notification %s not handed over (%v); dropped: %v %v", e.ID, err, drop, ferr)
				continue
			}
			sent++
		}
		if err := sp.remove(e.ID); err != nil {
			n.logger.Printf("spool: %v", err)
		}
	}
}

// replayHook is what handleHook does with an event before it asks the phone (bookkeeping and the report), for a hook
// that standby answered: no card, it is long over or asks again by itself.
func (n *Node) replayHook(e spoolEntry) error {
	req := e.Request
	var in hookInput
	if json.Unmarshal(req.Input, &in) != nil {
		return nil
	}
	if req.ReportOnly && !n.takesReport(in) {
		return nil
	}
	stopSig := n.recordHookTurn(req, in, e.At)
	if !req.ReportOnly {
		if paneKindOK(n, req.Pane) {
			n.bindPane(req.Pane, in.SessionID)
		}
	}
	n.rememberTranscript(in)
	p := hookEventParams(in, stopSig, req.ReportOnly)
	p["spool_id"], p["spooled_at"] = e.ID, e.At
	return n.peer.Notify("hook.event", p)
}

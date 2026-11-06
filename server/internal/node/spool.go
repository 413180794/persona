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
}

// spool is the file of entries. One resident process writes it (standby) and replays it (the next Node), never both
// at once; spoolMu is for safety.
type spool struct {
	path string
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

// standby answers node.sock while no manager connection is served (Attach).
type standby struct {
	sp   *spool
	logf func(string, ...any)
	now  func() time.Time

	mu    sync.Mutex
	ln    net.Listener
	conns sync.WaitGroup
	since time.Time // when it last started: the manager went away (or was never there)
}

func newStandby(path string, logf func(string, ...any)) *standby {
	return &standby{sp: openSpool(path), logf: logf, now: time.Now}
}

// start listens on node.sock unless it already does.
func (s *standby) start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ln != nil {
		return
	}
	path := nodeclient.SocketPath()
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
	s.ln, s.since = ln, s.now()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			s.conns.Add(1)
			go func() {
				defer s.conns.Done()
				s.serve(c)
			}()
		}
	}()
}

// stop closes the listener and waits for the requests it took (none waits: standby answers at once).
func (s *standby) stop() {
	s.mu.Lock()
	ln := s.ln
	s.ln = nil
	s.mu.Unlock()
	if ln != nil {
		ln.Close()
	}
	s.conns.Wait()
}

func (s *standby) holding() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.now().Sub(s.since) < standbyHold
}

func (s *standby) serve(c net.Conn) {
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(10 * time.Second))
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
		return nodeclient.AskReply{Status: nodeclient.AskUnavailable, Error: "the manager is not connected to this computer"}
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

// replaySpool hands the manager what standby took, in order, then opens the gate for new hooks (n.replayed). An entry
// goes from the file once it was sent; one the manager got twice (the node stopped between sending and removing) it
// drops by spool_id.
func (n *Node) replaySpool(ctx context.Context, sp *spool) {
	defer close(n.replayed)
	entries := sp.load()
	if len(entries) == 0 {
		return
	}
	sent, stale := 0, 0
	for _, e := range entries {
		if ctx.Err() != nil {
			return // kept for the next connection
		}
		if time.Since(e.At) > spoolMaxAge {
			stale++
		} else {
			switch e.Kind {
			case "hook":
				n.replayHook(e)
			case "notify":
				if rep := n.sendNotify(ctx, e.Request, e.ID); rep.Error != "" && ctx.Err() != nil {
					return // the connection ended under it: try again on the next one
				}
			}
			sent++
		}
		if err := sp.remove(e.ID); err != nil {
			n.logger.Printf("spool: %v", err)
		}
	}
	n.logger.Printf("spool: replayed %d taken while the manager was away (%d too old, dropped)", sent, stale)
}

// replayHook is what handleHook does with an event before it asks the phone (bookkeeping and the report), for a hook
// that standby answered: no card, it is long over or asks again by itself.
func (n *Node) replayHook(e spoolEntry) {
	req := e.Request
	var in hookInput
	if json.Unmarshal(req.Input, &in) != nil {
		return
	}
	if req.ReportOnly && !n.takesReport(in) {
		return
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
	_ = n.peer.Notify("hook.event", p)
}

package node

import (
	"context"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/chyroc/jarvis/server/internal/proto"
)

// Starting a session in the background (docs/protocol.md「新建会话」). Opening a herdr tab and waiting for claude's
// input box takes seconds (up to a minute behind a slow ~/.zshrc or a trust dialog), so `session.start` with async
// answers right away with the session as it will be (id, directory, launcher) and starts it after. Until then the
// session is listed as idle with `starting` (not a turn: nothing is timed or pushed), its chat is empty instead of an error, and messages sent to it wait
// for claude to be ready and then go in, in order. A start that fails stays listed for a while with its error, and
// the messages waiting on it get that error.

const startFailedFor = 10 * time.Minute

type startRec struct {
	done    chan struct{}
	session proto.Session // what is listed while it starts
	err     error
	ended   time.Time
	tabID   string // the herdr tab, once made
	tabName string // a name that came before the tab
}

type startLog struct {
	mu sync.Mutex
	m  map[string]*startRec
}

func (l *startLog) get(id string) *startRec {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.m[id]
}

func (l *startLog) add(id string, r *startRec) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.m == nil {
		l.m = map[string]*startRec{}
	}
	for k, old := range l.m { // failed ones are shown for a while
		if !old.ended.IsZero() && time.Since(old.ended) > startFailedFor {
			delete(l.m, k)
		}
	}
	l.m[id] = r
}

// finish records how the start went: a started session is listed by herdr from now on, a failed one keeps its entry.
func (l *startLog) finish(id string, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	r := l.m[id]
	if r == nil {
		return
	}
	r.err, r.ended = err, time.Now()
	if err == nil {
		delete(l.m, id)
	} else {
		r.session.Starting = false
		r.session.Status = proto.StatusOffline
		r.session.StartError = err.Error()
	}
	close(r.done)
}

// wait blocks while the session is starting and returns the start's error, if it failed.
func (l *startLog) wait(ctx context.Context, id string) error {
	r := l.get(id)
	if r == nil {
		return nil
	}
	select {
	case <-r.done:
	case <-ctx.Done():
		return fmt.Errorf("这个 Agent 还在启动，没等到它准备好: %w", ctx.Err())
	}
	if r.err != nil {
		return fmt.Errorf("这个 Agent 没有启动起来：%v", r.err)
	}
	return nil
}

// tabCreated notes the new tab and returns a name given for it while it was being made.
func (l *startLog) tabCreated(id, tabID string) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	r := l.m[id]
	if r == nil {
		return ""
	}
	r.tabID = tabID
	return r.tabName
}

// nameTab takes a tab name for a session that is starting: kept for the tab (true) while there is none yet, else
// the tab to rename. Neither: the session is not starting.
func (l *startLog) nameTab(id, name string) (kept bool, tabID string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	r := l.m[id]
	if r == nil || !r.ended.IsZero() {
		return false, ""
	}
	if r.tabID == "" {
		r.tabName = name
		return true, ""
	}
	return false, r.tabID
}

// listed are the sessions starting (or failed to) that herdr does not show yet.
func (l *startLog) listed(seen map[string]bool) []proto.Session {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []proto.Session
	for id, r := range l.m {
		if seen[id] || (!r.ended.IsZero() && time.Since(r.ended) > startFailedFor) {
			continue
		}
		out = append(out, r.session)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SessionID < out[j].SessionID })
	return out
}

// startAsync checks the arguments, answers with the session it is going to be and starts it in the background.
// id is the session id to use (the hub may pick it); empty: a new one.
func (n *Node) startAsync(id, cwd, launcher, prompt, workspaceID, mode, effort, tabName string) (*proto.Session, error) {
	if id == "" {
		id = newUUID()
	} else if !validID(id) {
		return nil, fmt.Errorf("invalid session id %q", id)
	}
	if n.starts.get(id) != nil {
		return nil, fmt.Errorf("session %s is already starting", id)
	}
	cwd, launcher, err := n.checkStart(cwd, launcher, "", mode, effort)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	r := &startRec{done: make(chan struct{}), session: proto.Session{
		SessionID: id, Cwd: cwd, Launcher: launcher, Agent: "claude", Status: proto.StatusIdle, Starting: true,
		StartedByJarvis: true, PermissionMode: mode, Effort: effort, UpdatedAt: now, WorkspaceID: workspaceID,
	}}
	n.starts.add(id, r)
	// Known from now on: its chat loads empty (startingSession) and its watch waits for the transcript.
	n.state.put(id, sessionState{Launcher: launcher, Cwd: cwd, StartedAt: now})
	n.noteModes(id, mode, effort)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		_, err := n.start(ctx, cwd, launcher, prompt, "", workspaceID, mode, effort, tabName, id)
		if err != nil {
			log.Printf("start %s: %v", id, err)
		}
		n.starts.finish(id, err)
		n.notifySessions()
	}()
	s := r.session
	n.decorate(&s)
	go n.notifySessions()
	return &s, nil
}

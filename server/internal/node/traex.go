package node

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/chyroc/jarvis/server/internal/herdr"
	"github.com/chyroc/jarvis/server/internal/proto"
)

// TRAEX support. TRAEX (`traex`, an in-house fork of Codex) runs in herdr panes that herdr does not recognize as
// agents, so the node finds them itself:
//   - ~/.trae/cli/thread-writer-locks/<thread id>.owner.json names the pid writing each thread (session);
//   - that pid's environment holds herdr's HERDR_PANE_ID, so pane → session is exact, no guessing by directory;
//   - its conversation is a rollout in ~/.trae/cli/sessions/YYYY/MM/DD, the newer Codex format (transcript/codex.go);
//   - herdr has no state for the pane: a spinner in the terminal title or "esc to interrupt" on screen is working,
//     a numbered choice on screen is blocked, anything else is a finished turn (the hub turns that into 完成未看 / 已看).

const traexAgent = "traex"

func (n *Node) traexHome() string { return filepath.Join(n.home, ".trae", "cli") }

func (n *Node) traexPath(id string) string  { return rolloutPath(n.traexHome(), id) }
func (n *Node) traexTitle(id string) string { return threadTitle(n.traexHome(), id) }

var traexCache = struct {
	sync.Mutex
	panes  map[int]string // pid → herdr pane (from its environment; a pid never moves)
	status map[string]traexState
}{panes: map[int]string{}, status: map[string]traexState{}}

type traexState struct {
	status string
	at     time.Time
}

// traexOwners maps the live TRAEX processes to the thread each writes (the newest owner file per pid).
func (n *Node) traexOwners() map[int]string {
	dir := filepath.Join(n.traexHome(), "thread-writer-locks")
	files, _ := filepath.Glob(filepath.Join(dir, "*.owner.json"))
	type owner struct {
		thread string
		mtime  time.Time
	}
	best := map[int]owner{}
	for _, f := range files {
		st, err := os.Stat(f)
		if err != nil {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var o struct {
			PID int `json:"pid"`
		}
		if json.Unmarshal(b, &o) != nil || o.PID <= 0 || !alive(o.PID) {
			continue
		}
		thread := strings.TrimSuffix(filepath.Base(f), ".owner.json")
		if cur, ok := best[o.PID]; !ok || st.ModTime().After(cur.mtime) {
			best[o.PID] = owner{thread, st.ModTime()}
		}
	}
	out := make(map[int]string, len(best))
	for pid, o := range best {
		out[pid] = o.thread
	}
	return out
}

func alive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

var herdrPaneEnvRe = regexp.MustCompile(`(?:^|[\x00\s])HERDR_PANE_ID=([^\x00\s]+)`)

// paneOfPID reads HERDR_PANE_ID from a process's environment (Linux: /proc; macOS: ps -E). Cached per pid.
func paneOfPID(pid int) string {
	traexCache.Lock()
	p, ok := traexCache.panes[pid]
	traexCache.Unlock()
	if ok {
		return p
	}
	var env []byte
	if runtime.GOOS == "linux" {
		env, _ = os.ReadFile("/proc/" + strconv.Itoa(pid) + "/environ")
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		env, _ = exec.CommandContext(ctx, "ps", "-E", "-ww", "-o", "command=", "-p", strconv.Itoa(pid)).Output()
		cancel()
	}
	if m := herdrPaneEnvRe.FindSubmatch(env); m != nil {
		p = string(m[1])
	}
	traexCache.Lock()
	traexCache.panes[pid] = p
	traexCache.Unlock()
	return p
}

// traexAgents turns the panes running TRAEX into agents, with their session id and a state of our own.
func (n *Node) traexAgents(ctx context.Context, panes []herdr.Agent, agents []herdr.Agent) []herdr.Agent {
	owners := n.traexOwners()
	if len(owners) == 0 {
		return nil
	}
	isAgent := map[string]bool{}
	for _, a := range agents {
		isAgent[a.PaneID] = true
	}
	byPane := map[string]herdr.Agent{}
	for _, p := range panes {
		byPane[p.PaneID] = p
	}
	var out []herdr.Agent
	for pid, thread := range owners {
		pane := paneOfPID(pid)
		p, ok := byPane[pane]
		if !ok || isAgent[pane] {
			continue
		}
		p.Agent = traexAgent
		p.AgentSession = &herdr.AgentSession{Kind: "id", Value: thread}
		p.AgentStatus = n.traexStatus(ctx, p)
		out = append(out, p)
	}
	return out
}

var traexChoiceRe = regexp.MustCompile(`(?m)^\s*[›❯>]\s*1\.\s+\S`)

// traexStatus: working from the title's spinner at once; otherwise the screen, read at most every 2s.
func (n *Node) traexStatus(ctx context.Context, p herdr.Agent) string {
	if r := []rune(strings.TrimSpace(p.RawTitle)); len(r) > 0 && r[0] >= 0x2800 && r[0] <= 0x28FF {
		return proto.StatusWorking // braille spinner: a turn is running
	}
	traexCache.Lock()
	s, ok := traexCache.status[p.PaneID]
	traexCache.Unlock()
	if ok && time.Since(s.at) < 2*time.Second {
		return s.status
	}
	status := proto.StatusDone
	rctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	text, err := n.herdr.PaneRead(rctx, p.PaneID, "visible", 40)
	cancel()
	switch {
	case err != nil:
		status = s.status // keep the last known
		if status == "" {
			status = proto.StatusUnknown
		}
	case strings.Contains(text, "esc to interrupt"):
		status = proto.StatusWorking
	case traexChoiceRe.MatchString(text):
		status = proto.StatusBlocked
	}
	traexCache.Lock()
	traexCache.status[p.PaneID] = traexState{status, time.Now()}
	traexCache.Unlock()
	return status
}

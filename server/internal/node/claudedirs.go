package node

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Claude Code keeps everything (projects/<cwd>/<session>.jsonl, sessions/<pid>.json, settings.json) under its config
// directory: $CLAUDE_CONFIG_DIR, else ~/.claude. Launchers for several accounts or profiles set CLAUDE_CONFIG_DIR, so one
// machine can have many. The node looks in all of them:
//   - the node's own default ($CLAUDE_CONFIG_DIR of the node, else ~/.claude);
//   - every running claude process's CLAUDE_CONFIG_DIR (scanned every 30 s; also tells which directory a pid uses);
//   - the hub's list: hub.json machines[].claude_config_dirs plus directories scanned earlier (node.configure), so a
//     profile's past sessions stay listed when none of its claudes runs.
// Directories are compared by their real path: ~/.claude/projects symlinked into a profile is the same directory.

// claudeScanEvery is how often running claude processes are scanned for their config directory.
const claudeScanEvery = 30 * time.Second

type claudeDirs struct {
	mu      sync.Mutex
	known   []string       // from the hub: configured and remembered
	found   []string       // seen in running claude processes, in order of discovery
	byPID   map[int]string // live claude pid -> its config directory
	scanned time.Time
	report  func(dirs []string) // tells the hub about newly found directories
	scan    func() map[int]string
}

func newClaudeDirs() *claudeDirs {
	return &claudeDirs{byPID: map[int]string{}, scan: scanClaudeProcesses}
}

// defaultClaudeDir is where a claude started without CLAUDE_CONFIG_DIR keeps its files.
func (n *Node) defaultClaudeDir() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	return filepath.Join(n.home, ".claude")
}

// claudeConfigDirs lists every config directory in use, the default first, each once (by real path), existing only.
func (n *Node) claudeConfigDirs() []string {
	d := n.cdirs
	n.refreshClaudeDirs()
	cands := []string{n.defaultClaudeDir()}
	if d != nil {
		d.mu.Lock()
		cands = append(append(cands, d.found...), d.known...)
		d.mu.Unlock()
	}
	var out, seen []string
	for _, c := range cands {
		c = expandHome(c, n.home)
		real, err := filepath.EvalSymlinks(c)
		if err != nil {
			continue // missing
		}
		if !slices.Contains(seen, real) {
			seen = append(seen, real)
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		out = []string{n.defaultClaudeDir()}
	}
	return out
}

// projectDirs: the projects directory of each config directory. Two config directories may share one through a
// symlink; that one is listed once.
func (n *Node) projectDirs() []string {
	var out, seen []string
	for _, d := range n.claudeConfigDirs() {
		p := filepath.Join(d, "projects")
		real, err := filepath.EvalSymlinks(p)
		if err != nil {
			continue
		}
		if !slices.Contains(seen, real) {
			seen = append(seen, real)
			out = append(out, p)
		}
	}
	return out
}

// globProjects globs pattern (relative to a projects directory) in every projects directory.
func (n *Node) globProjects(pattern ...string) []string {
	var out []string
	for _, p := range n.projectDirs() {
		m, _ := filepath.Glob(filepath.Join(append([]string{p}, pattern...)...))
		out = append(out, m...)
	}
	return out
}

// claudeSessionFile is sessions/<pid>.json of that claude: in the directory its environment names when known, else
// the newest one among the config directories (a stale file of a reused pid may sit in another).
func (n *Node) claudeSessionFile(pid int) string {
	name := filepath.Join("sessions", strconv.Itoa(pid)+".json")
	if d := n.cdirs; d != nil {
		n.refreshClaudeDirs()
		d.mu.Lock()
		dir, ok := d.byPID[pid]
		d.mu.Unlock()
		if ok {
			return filepath.Join(expandHome(dir, n.home), name)
		}
	}
	best, bestT := filepath.Join(n.defaultClaudeDir(), name), time.Time{}
	for _, dir := range n.claudeConfigDirs() {
		p := filepath.Join(dir, name)
		if st, err := os.Stat(p); err == nil && st.ModTime().After(bestT) {
			best, bestT = p, st.ModTime()
		}
	}
	return best
}

// primaryClaudeDir is the config directory most running claudes use (the default when none runs): the one whose
// settings the machine settings page shows.
func (n *Node) primaryClaudeDir() string {
	dirs := n.claudeConfigDirs()
	d := n.cdirs
	if d == nil {
		return dirs[0]
	}
	count := map[string]int{}
	d.mu.Lock()
	for _, dir := range d.byPID {
		if real, err := filepath.EvalSymlinks(expandHome(dir, n.home)); err == nil {
			count[real]++
		}
	}
	d.mu.Unlock()
	best, bestN := dirs[0], -1
	for _, dir := range dirs {
		real, _ := filepath.EvalSymlinks(dir)
		if count[real] > bestN {
			best, bestN = dir, count[real]
		}
	}
	return best
}

// setKnownClaudeDirs takes the hub's list (node.configure).
func (n *Node) setKnownClaudeDirs(dirs []string) {
	if n.cdirs == nil {
		return
	}
	n.cdirs.mu.Lock()
	n.cdirs.known = dirs
	n.cdirs.mu.Unlock()
}

// refreshClaudeDirs rescans running claudes when the last scan is old; new directories are reported to the hub.
func (n *Node) refreshClaudeDirs() {
	d := n.cdirs
	if d == nil {
		return
	}
	d.mu.Lock()
	if time.Since(d.scanned) < claudeScanEvery {
		d.mu.Unlock()
		return
	}
	d.scanned = time.Now()
	scan := d.scan
	d.mu.Unlock()
	byPID := scan()
	d.mu.Lock()
	d.byPID = byPID
	added := false
	for _, dir := range byPID {
		if !slices.Contains(d.found, dir) {
			d.found = append(d.found, dir)
			added = true
		}
	}
	found, report := slices.Clone(d.found), d.report
	d.mu.Unlock()
	if added && report != nil {
		report(found)
	}
}

func expandHome(p, home string) string {
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, p[2:])
	}
	return p
}

var claudeConfigEnvRe = regexp.MustCompile(`(?:^|[\x00\s])CLAUDE_CONFIG_DIR=([^\x00\s]+)`)

// scanClaudeProcesses maps this user's running claude processes to the CLAUDE_CONFIG_DIR in their environment
// (processes without one are left out: they use the default). Linux reads /proc; macOS asks ps once for all.
func scanClaudeProcesses() map[int]string {
	out := map[int]string{}
	if runtime.GOOS == "linux" {
		entries, _ := os.ReadDir("/proc")
		for _, e := range entries {
			pid, err := strconv.Atoi(e.Name())
			if err != nil {
				continue
			}
			cmd, err := os.ReadFile("/proc/" + e.Name() + "/cmdline")
			if err != nil || !isClaudeCmd(strings.Split(string(cmd), "\x00")) {
				continue
			}
			env, _ := os.ReadFile("/proc/" + e.Name() + "/environ")
			if m := claudeConfigEnvRe.FindSubmatch(env); m != nil {
				out[pid] = string(m[1])
			}
		}
		return out
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	b, err := exec.CommandContext(ctx, "ps", "-E", "-ww", "-o", "pid=,command=", "-U", strconv.Itoa(os.Getuid())).Output()
	if err != nil {
		return out
	}
	return parsePSEnv(string(b))
}

// parsePSEnv reads `ps -E -o pid=,command=` output: the command line is followed by the environment.
func parsePSEnv(s string) map[int]string {
	out := map[int]string{}
	for _, line := range strings.Split(s, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		pid, err := strconv.Atoi(f[0])
		if err != nil || !isClaudeCmd(f[1:]) {
			continue
		}
		if m := claudeConfigEnvRe.FindStringSubmatch(line); m != nil {
			out[pid] = m[1]
		}
	}
	return out
}

// isClaudeCmd: the command line runs Claude Code — its native binary, or its script under node / bun.
func isClaudeCmd(argv []string) bool {
	if len(argv) == 0 {
		return false
	}
	switch filepath.Base(argv[0]) {
	case "claude":
		return true
	case "node", "bun":
		return len(argv) > 1 && strings.Contains(argv[1], "claude")
	}
	return false
}

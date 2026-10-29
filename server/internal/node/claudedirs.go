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
	mu        sync.Mutex
	known     []string           // from the hub: configured and remembered
	found     []string           // seen in running claude processes, in order of discovery
	byPID     map[int]string     // live claude pid -> its config directory
	bySession map[string]procEnv // session a live claude runs (its sessions/<pid>.json) -> that claude's environment
	scanned   time.Time
	report    func(dirs []string) // tells the hub about newly found directories
	scan      func() map[int]procEnv
}

// procEnv is what the scan keeps from a running claude's environment: its config directory, and the API endpoint
// and model a launcher function exported (used to tell which launcher started a claude Jarvis did not start).
type procEnv struct {
	ConfigDir string // CLAUDE_CONFIG_DIR
	BaseURL   string // ANTHROPIC_BASE_URL
	Model     string // ANTHROPIC_MODEL
}

func newClaudeDirs() *claudeDirs {
	return &claudeDirs{byPID: map[int]string{}, bySession: map[string]procEnv{}, scan: scanClaudeProcesses}
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
	envs := scan()
	byPID := map[int]string{}
	for pid, e := range envs {
		if e.ConfigDir != "" {
			byPID[pid] = e.ConfigDir
		}
	}
	d.mu.Lock()
	d.byPID = byPID
	d.mu.Unlock()
	// Which session each claude runs: its sessions/<pid>.json (found through the directory just scanned).
	bySession := map[string]procEnv{}
	for pid, e := range envs {
		if e.BaseURL == "" && e.Model == "" {
			continue
		}
		if id := readClaudeSessionID(n.claudeSessionFile(pid)); id != "" {
			bySession[id] = e
		}
	}
	d.mu.Lock()
	d.bySession = bySession
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

// sessionEnv is the scanned environment of the live claude running session id (false when none was seen or it set
// neither ANTHROPIC_BASE_URL nor ANTHROPIC_MODEL).
func (n *Node) sessionEnv(id string) (procEnv, bool) {
	d := n.cdirs
	if d == nil {
		return procEnv{}, false
	}
	n.refreshClaudeDirs()
	d.mu.Lock()
	defer d.mu.Unlock()
	e, ok := d.bySession[id]
	return e, ok
}

var (
	claudeConfigEnvRe = envVarRe("CLAUDE_CONFIG_DIR")
	anthropicBaseRe   = envVarRe("ANTHROPIC_BASE_URL")
	anthropicModelRe  = envVarRe("ANTHROPIC_MODEL")
)

// envVarRe matches NAME=value in a NUL- (/proc/<pid>/environ) or space-separated (ps -E) environment.
func envVarRe(name string) *regexp.Regexp {
	return regexp.MustCompile(`(?:^|[\x00\s])` + name + `=([^\x00\s]+)`)
}

// parseProcEnv picks the variables procEnv keeps out of an environment listing.
func parseProcEnv(env string) procEnv {
	get := func(re *regexp.Regexp) string {
		if m := re.FindStringSubmatch(env); m != nil {
			return m[1]
		}
		return ""
	}
	return procEnv{ConfigDir: get(claudeConfigEnvRe), BaseURL: get(anthropicBaseRe), Model: get(anthropicModelRe)}
}

// scanClaudeProcesses maps this user's running claude processes to the variables procEnv keeps from their
// environment (processes with none of them are left out: default directory, default endpoint). Linux reads /proc;
// macOS asks ps once for all.
func scanClaudeProcesses() map[int]procEnv {
	out := map[int]procEnv{}
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
			if pe := parseProcEnv(string(env)); pe != (procEnv{}) {
				out[pid] = pe
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
func parsePSEnv(s string) map[int]procEnv {
	out := map[int]procEnv{}
	for _, line := range strings.Split(s, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		pid, err := strconv.Atoi(f[0])
		if err != nil || !isClaudeCmd(f[1:]) {
			continue
		}
		if pe := parseProcEnv(line); pe != (procEnv{}) {
			out[pid] = pe
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

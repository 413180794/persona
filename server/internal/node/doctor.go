package node

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/assistantskills"
	"github.com/nwverse/persona/server/internal/herdr"
	"github.com/nwverse/persona/server/internal/identity"
	"github.com/nwverse/persona/server/internal/nodeclient"
)

// `personad node doctor` looks at what a node on this machine needs for the sessions Persona starts to reach the
// phone: the sockets hooks and MCP dial, the files claude loads (compared with what this binary would write now, the
// same bytes the node writes at startup) and the programs those files run. It only reads. --fix writes Persona's
// own generated files again (writeGeneratedConfig, the startup path) and nothing else: the user's own Claude Code
// settings are only ever looked at. Background: a redeploy once deleted node.sock and every hook and MCP call fell
// back to the terminal without a word (docs/lessons/server.md).

// Check statuses.
const (
	DoctorOK   = "ok"
	DoctorWarn = "warn"
	DoctorFail = "fail"
)

// DoctorCheck is one line of the report; the names are stable for scripts.
type DoctorCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	// State is current / stale / missing for a generated file or directory, "" for the other checks.
	State  string   `json:"state"`
	Detail string   `json:"detail"`
	Hint   string   `json:"hint"`
	Items  []string `json:"items"` // what differs, which commands, ...; never null
}

// DoctorReport is what `node doctor` prints (--json: as it is).
type DoctorReport struct {
	OK     bool          `json:"ok"` // no check failed
	Dir    string        `json:"dir"`
	Checks []DoctorCheck `json:"checks"`
	// Fixed names the generated files --fix wrote again.
	Fixed []string `json:"fixed"`
}

// DoctorOptions says where to look; DefaultDoctorOptions fills them in for this machine (tests give their own).
type DoctorOptions struct {
	Dir string // the node's directory (~/.persona)
	Bin string // the program the generated files should run: this binary
	// Sockets: node.sock, runtime.sock and herdr's API socket.
	NodeSock, RuntimeSock, HerdrSock string
	// ClaudeConfigDir is where the user's own Claude Code settings are (read, never written).
	ClaudeConfigDir string
	// ClaudeVersion asks claude's version ("" when unknown).
	ClaudeVersion func(context.Context) string
	// Fix writes the generated files again when they are stale or missing.
	Fix bool
}

// DefaultDoctorOptions is this machine's: the same places the node itself uses, without creating or moving any.
func DefaultDoctorOptions() (DoctorOptions, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return DoctorOptions{}, err
	}
	bin, err := os.Executable()
	if err != nil {
		return DoctorOptions{}, err
	}
	if p, err := filepath.EvalSymlinks(bin); err == nil {
		bin = p
	}
	return DoctorOptions{
		Dir: identity.DataDir(home), Bin: bin,
		NodeSock: nodeclient.SocketPath(), RuntimeSock: RuntimeSocketPath(), HerdrSock: herdr.SocketPath(),
		ClaudeConfigDir: claudeDirOf(home), ClaudeVersion: claudeVersion,
	}, nil
}

// generatedFile is a file the node writes for the sessions it starts: Group is the unit the doctor reports on, Rel
// the path under the node's directory.
type generatedFile struct {
	Group   string
	Rel     string
	Content []byte
}

// generatedGroups are the groups of generatedFiles, then the skills plugin (agent-plugin, assistantskills).
var generatedGroups = []string{"claude-settings.json", "claude-hooks", "assistant-settings.json", "assistant-hooks", "mcp.json"}

const agentPluginGroup = "agent-plugin"

// hookSettings is the settings with Persona's hooks for a claude whose program is bin: the agents' (assistant false)
// or the assistant's, which add the Feishu gate on Bash (larkgate.go).
func hookSettings(bin string, assistant bool) map[string]any {
	cmd := shellQuote(bin) + " hook"
	blocking := func(matcher string) map[string]any {
		return map[string]any{"matcher": matcher, "hooks": []any{map[string]any{"type": "command", "command": cmd, "timeout": 86400}}}
	}
	notify := map[string]any{"hooks": []any{map[string]any{"type": "command", "command": cmd, "timeout": 10}}}
	pre := []any{blocking("AskUserQuestion")}
	if assistant {
		pre = append(pre, blocking("Bash"))
	}
	return map[string]any{"hooks": map[string]any{
		"PreToolUse":        pre,
		"PermissionRequest": []any{blocking("*")},
		"Elicitation":       []any{blocking("*")},
		"Notification":      []any{notify},
		"Stop":              []any{notify},
		"UserPromptSubmit":  []any{notify},
		"SessionStart":      []any{notify},
	}}
}

// generatedFiles is every file writeGeneratedConfig writes under the node's directory (but the skills plugin),
// computed without writing. Each set of hooks is there twice: as a plugin for a claude that loads plugins, in a
// settings file for one that does not (hooksplugin.go).
func generatedFiles(bin string) ([]generatedFile, error) {
	var out []generatedFile
	for _, kind := range []string{"claude", "assistant"} {
		settings := hookSettings(bin, kind == "assistant")
		b, err := marshalJSON(settings)
		if err != nil {
			return nil, err
		}
		out = append(out, generatedFile{Group: kind + "-settings.json", Rel: kind + "-settings.json", Content: b})
		files, err := hooksPluginFiles(settings["hooks"])
		if err != nil {
			return nil, err
		}
		for _, rel := range slices.Sorted(maps.Keys(files)) {
			out = append(out, generatedFile{Group: kind + "-hooks", Rel: filepath.Join(kind+"-hooks", rel), Content: files[rel]})
		}
	}
	mcp, err := marshalJSON(map[string]any{"mcpServers": map[string]any{"jarvis": map[string]any{"command": bin, "args": []string{"mcp"}}}})
	if err != nil {
		return nil, err
	}
	return append(out, generatedFile{Group: "mcp.json", Rel: "mcp.json", Content: mcp}), nil
}

// writeGeneratedConfig writes the hooks, MCP config and skills plugin that sessions Persona starts load into dir,
// for a claude whose hook program is bin: what the node does at each start, and what `node doctor --fix` does.
func writeGeneratedConfig(dir, bin string) error {
	files, err := generatedFiles(bin)
	if err != nil {
		return err
	}
	for _, f := range files {
		path := filepath.Join(dir, f.Rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return err
		}
		if err := writeFileAtomic(path, f.Content); err != nil {
			return err
		}
	}
	return assistantskills.WritePlugin(filepath.Join(dir, agentPluginGroup)) // the skills the agents Persona starts load (agentplugin.go)
}

// RunDoctor runs the checks (and with opts.Fix, writes the generated files again first, then checks again).
func RunDoctor(ctx context.Context, opts DoctorOptions) DoctorReport {
	rep := doctorChecks(ctx, opts)
	if opts.Fix {
		var stale []string
		for _, c := range rep.Checks {
			if c.State == "stale" || c.State == "missing" {
				stale = append(stale, c.Name)
			}
		}
		if len(stale) > 0 {
			if err := writeGeneratedConfig(opts.Dir, opts.Bin); err != nil {
				rep = doctorChecks(ctx, opts)
				rep.Checks = append(rep.Checks, DoctorCheck{Name: "fix", Status: DoctorFail, Detail: "重新生成失败：" + err.Error(),
					Hint: "检查 " + opts.Dir + " 的权限和磁盘空间", Items: []string{}})
				rep.OK = false
				return rep
			}
			rep = doctorChecks(ctx, opts)
			rep.Fixed = stale
		}
	}
	return rep
}

func doctorChecks(ctx context.Context, opts DoctorOptions) DoctorReport {
	rep := DoctorReport{Dir: opts.Dir, Fixed: []string{}}
	add := func(c DoctorCheck) {
		if c.Items == nil {
			c.Items = []string{}
		}
		rep.Checks = append(rep.Checks, c)
	}
	add(checkNodeSocket(ctx, opts.NodeSock))
	add(checkRuntimeSocket(ctx, opts.RuntimeSock))
	for _, c := range checkGenerated(opts) {
		add(c)
	}
	add(checkCommandPaths(opts.Dir))
	add(checkClaudeVersion(ctx, opts.ClaudeVersion))
	add(checkHerdr(ctx, opts.HerdrSock))
	add(checkGlobalPermissionHooks(opts.ClaudeConfigDir))
	rep.OK = !slices.ContainsFunc(rep.Checks, func(c DoctorCheck) bool { return c.Status == DoctorFail })
	return rep
}

const doctorDialTimeout = 3 * time.Second

func checkNodeSocket(ctx context.Context, path string) DoctorCheck {
	c := DoctorCheck{Name: "node.sock"}
	if _, err := os.Stat(path); err != nil {
		c.Status, c.Detail = DoctorFail, "不存在"
		c.Hint = "没有 node 在监听：hook 和 MCP 会静默退回终端。确认 hub 已连上这台电脑；node 重启会重建"
		return c
	}
	ctx, cancel := context.WithTimeout(ctx, doctorDialTimeout)
	defer cancel()
	line, err := nodeclient.DialAt(ctx, path, sockRequest{Kind: "ping"})
	if err != nil {
		c.Status, c.Detail = DoctorFail, "文件在但连不上或没有应答（"+shorten(err.Error(), 80)+"）"
		c.Hint = "可能是旧 node 退出后留下的文件；重启这台电脑上的 node"
		return c
	}
	if !json.Valid(line) {
		c.Status, c.Detail = DoctorFail, "应答不是 JSON"
		c.Hint = "这个 socket 不是 node 的；重启 node"
		return c
	}
	c.Status, c.Detail = DoctorOK, "已应答"
	return c
}

func checkRuntimeSocket(ctx context.Context, path string) DoctorCheck {
	c := DoctorCheck{Name: "runtime.sock"}
	if _, err := os.Stat(path); err != nil {
		c.Status, c.Detail = DoctorWarn, "不存在"
		c.Hint = "没有 node 在监听：DSH、pi 这类 runtime 连不上；没用到可忽略"
		return c
	}
	ctx, cancel := context.WithTimeout(ctx, doctorDialTimeout)
	defer cancel()
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", path)
	if err != nil {
		c.Status, c.Detail = DoctorFail, "文件在但连不上（"+shorten(err.Error(), 80)+"）"
		c.Hint = "可能是旧 node 退出后留下的文件；重启这台电脑上的 node"
		return c
	}
	conn.Close()
	c.Status, c.Detail = DoctorOK, "可连接"
	return c
}

// checkGenerated compares each generated file with what this binary would write: current, stale (differs, or part
// of a plugin is missing) or missing.
func checkGenerated(opts DoctorOptions) []DoctorCheck {
	files, err := generatedFiles(opts.Bin)
	if err != nil {
		return []DoctorCheck{{Name: "generated-files", Status: DoctorFail, Detail: err.Error(), Items: []string{}}}
	}
	var out []DoctorCheck
	for _, g := range generatedGroups {
		var issues []string
		total := 0
		for _, f := range files {
			if f.Group != g {
				continue
			}
			total++
			got, err := os.ReadFile(filepath.Join(opts.Dir, f.Rel))
			switch {
			case err != nil:
				issues = append(issues, "missing "+filepath.ToSlash(f.Rel))
			case !bytes.Equal(got, f.Content):
				issues = append(issues, "differs "+filepath.ToSlash(f.Rel))
			}
		}
		allMissing := len(issues) == total && allPrefix(issues, "missing")
		out = append(out, generatedCheck(g, issues, allMissing, true))
	}
	issues := assistantskills.PluginIssues(filepath.Join(opts.Dir, agentPluginGroup))
	allMissing := len(issues) == 1 && issues[0] == "missing"
	return append(out, generatedCheck(agentPluginGroup, issues, allMissing, false))
}

// generatedCheck turns what differs in one generated file or directory into its check. critical: the sessions
// lose their hooks or MCP without it (missing fails), otherwise only the skills (missing warns).
func generatedCheck(name string, issues []string, allMissing, critical bool) DoctorCheck {
	c := DoctorCheck{Name: name, Items: issues}
	if c.Items == nil {
		c.Items = []string{}
	}
	switch {
	case len(issues) == 0:
		c.State, c.Status, c.Detail = "current", DoctorOK, "和当前二进制会写的一致"
	case allMissing:
		c.State, c.Status, c.Detail = "missing", DoctorWarn, "不存在"
		if critical {
			c.Status = DoctorFail
			c.Hint = "Persona 启动的会话收不到手机上的提问和审批；重启 node 或运行 personad node doctor --fix"
		} else {
			c.Hint = "Persona 启动的 Agent 没有 persona-office 技能；重启 node 或运行 personad node doctor --fix"
		}
	default:
		c.State, c.Status = "stale", DoctorWarn
		c.Detail = fmt.Sprintf("过期：%d 处不同（%s）", len(issues), shorten(issues[0], 60))
		c.Hint = "重启 node 会重写；或运行 personad node doctor --fix"
	}
	return c
}

func allPrefix(items []string, prefix string) bool {
	return !slices.ContainsFunc(items, func(s string) bool { return !strings.HasPrefix(s, prefix) })
}

// configCommands are the programs a settings file or hooks plugin (top-level "hooks") runs, as first words.
func configCommands(b []byte) []string {
	var f struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
		MCPServers map[string]struct {
			Command string `json:"command"`
		} `json:"mcpServers"`
	}
	if json.Unmarshal(b, &f) != nil {
		return nil
	}
	var out []string
	for _, matchers := range f.Hooks {
		for _, m := range matchers {
			for _, h := range m.Hooks {
				if w := shellFirstWord(h.Command); w != "" {
					out = append(out, w)
				}
			}
		}
	}
	for _, s := range f.MCPServers {
		if s.Command != "" {
			out = append(out, s.Command)
		}
	}
	return out
}

// shellFirstWord is the first word of a command line written with shellQuote: bare, or single-quoted with each
// embedded quote closed, backslash-escaped and reopened.
func shellFirstWord(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if !strings.HasPrefix(cmd, "'") {
		if i := strings.IndexAny(cmd, " \t"); i >= 0 {
			return cmd[:i]
		}
		return cmd
	}
	var w strings.Builder
	for i := 0; i < len(cmd); {
		switch {
		case cmd[i] == '\'':
			j := strings.IndexByte(cmd[i+1:], '\'')
			if j < 0 {
				return w.String()
			}
			w.WriteString(cmd[i+1 : i+1+j])
			i += j + 2
		case strings.HasPrefix(cmd[i:], `\'`):
			w.WriteByte('\'')
			i += 2
		default:
			return w.String()
		}
	}
	return w.String()
}

// checkCommandPaths: the program each generated file runs exists and can be run. A file moved or removed under a
// running node (a redeploy to another path) leaves hooks that fail on every call.
func checkCommandPaths(dir string) DoctorCheck {
	c := DoctorCheck{Name: "command-paths", Items: []string{}}
	files := []string{"claude-settings.json", "assistant-settings.json", "mcp.json"}
	for _, kind := range []string{"claude", "assistant"} {
		files = append(files, filepath.Join(kind+"-hooks", "hooks", "hooks.json"))
	}
	seen := map[string]bool{}
	var bad []string
	read := 0
	for _, rel := range files {
		b, err := os.ReadFile(filepath.Join(dir, rel))
		if err != nil {
			continue
		}
		read++
		for _, p := range configCommands(b) {
			if seen[p] {
				continue
			}
			seen[p] = true
			if err := runnable(p); err != nil {
				bad = append(bad, fmt.Sprintf("%s: %v", p, err))
			}
		}
	}
	switch {
	case len(bad) > 0:
		slices.Sort(bad)
		c.Status, c.Items = DoctorFail, bad
		c.Detail = fmt.Sprintf("%d 个命令路径不可用（%s）", len(bad), shorten(bad[0], 80))
		c.Hint = "hook / MCP 会失败；用 node 现在的二进制运行 personad node doctor --fix 重写"
	case read == 0:
		c.Status, c.Detail = DoctorWarn, "没有可检查的生成文件"
		c.Hint = "先让 node 启动一次，或运行 personad node doctor --fix"
	default:
		c.Status, c.Detail = DoctorOK, fmt.Sprintf("%d 个命令路径都存在且可执行", len(seen))
	}
	return c
}

func runnable(path string) error {
	if !filepath.IsAbs(path) { // a bare name: found on PATH like claude would
		if _, err := exec.LookPath(path); err != nil {
			return errors.New("not on PATH")
		}
		return nil
	}
	st, err := os.Stat(path)
	if err != nil {
		return errors.New("not found")
	}
	if st.IsDir() || (runtime.GOOS != "windows" && st.Mode()&0o111 == 0) {
		return errors.New("not executable")
	}
	return nil
}

func checkClaudeVersion(ctx context.Context, ask func(context.Context) string) DoctorCheck {
	c := DoctorCheck{Name: "claude-version"}
	v := ""
	if ask != nil {
		v = ask(ctx)
	}
	switch {
	case v == "":
		c.Status, c.Detail = DoctorWarn, "读不到 claude 的版本"
		c.Hint = "没装 Claude Code，或登录 shell 里找不到它；读不到时 Persona 按不支持 --plugin-dir 处理，hooks 走 --settings"
	case !versionAtLeast(v, pluginMinVersion):
		c.Status, c.Detail = DoctorWarn, fmt.Sprintf("%s（低于 %d.%d.%d，不支持 --plugin-dir）", v, pluginMinVersion[0], pluginMinVersion[1], pluginMinVersion[2])
		c.Hint = "hooks 退回 --settings，persona-office 技能不可用；升级 Claude Code"
	default:
		c.Status, c.Detail = DoctorOK, v+"，支持 --plugin-dir"
	}
	return c
}

// checkHerdr dials herdr's API socket. The engine is chosen by the hub when it connects and is not kept on this
// machine, so a socket that is not there is a warning: it only matters when herdr is this machine's engine.
func checkHerdr(ctx context.Context, path string) DoctorCheck {
	c := DoctorCheck{Name: "herdr"}
	if path == "" {
		c.Status, c.Detail = DoctorWarn, "找不到 herdr 的 socket 位置"
		c.Hint = "设了 HERDR_SOCKET_PATH 的话确认它指向 herdr；这台电脑的引擎不是 herdr 时可忽略"
		return c
	}
	ctx, cancel := context.WithTimeout(ctx, doctorDialTimeout)
	defer cancel()
	conn, err := herdr.Dial(ctx, path)
	if err != nil {
		c.Status, c.Detail = DoctorWarn, "连不上 herdr 的 socket"
		c.Hint = "引擎是 herdr 时先启动 herdr；这台电脑用别的引擎时可忽略"
		return c
	}
	conn.Close()
	c.Status, c.Detail = DoctorOK, "herdr 在监听"
	return c
}

// checkGlobalPermissionHooks looks at the user's own settings.json for synchronous PermissionRequest hooks (other
// tools put them there) and says how many; it never changes the file.
func checkGlobalPermissionHooks(configDir string) DoctorCheck {
	c := DoctorCheck{Name: "global-permission-hooks", Items: []string{}}
	b, err := os.ReadFile(filepath.Join(configDir, "settings.json"))
	if errors.Is(err, os.ErrNotExist) {
		c.Status, c.Detail = DoctorOK, "没有全局 settings.json"
		return c
	}
	if err != nil {
		c.Status, c.Detail = DoctorWarn, "读不了全局 settings.json（"+shorten(err.Error(), 60)+"）"
		return c
	}
	var f struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Type    string `json:"type"`
				Command string `json:"command"`
				Async   bool   `json:"async"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if json.Unmarshal(b, &f) != nil {
		c.Status, c.Detail = DoctorWarn, "全局 settings.json 不是合法 JSON"
		c.Hint = "Claude Code 自己也读不了它；Persona 不会改这个文件"
		return c
	}
	for _, m := range f.Hooks["PermissionRequest"] {
		for _, h := range m.Hooks {
			if !h.Async && (h.Type == "command" || h.Type == "") {
				c.Items = append(c.Items, shorten(h.Command, 80))
			}
		}
	}
	if len(c.Items) == 0 {
		c.Status, c.Detail = DoctorOK, "没有同步的 PermissionRequest 钩子"
		return c
	}
	c.Status = DoctorWarn
	c.Detail = fmt.Sprintf("%d 个同步的 PermissionRequest 钩子（%s）", len(c.Items), c.Items[0])
	c.Hint = "它们和 Persona 的手机审批同时拦着权限请求，可能互相影响；Persona 不会改这个文件，需要时自己检查"
	return c
}

// shorten cuts s to at most n runes, with … when it cut.
func shorten(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// DoctorCommand is `personad node doctor [--json] [--fix]`: it prints the report to out and says whether a check
// failed (the caller exits non-zero then).
func DoctorCommand(ctx context.Context, args []string, out io.Writer) (failed bool, err error) {
	flags := flag.NewFlagSet("node doctor", flag.ContinueOnError)
	asJSON := flags.Bool("json", false, "以 JSON 输出")
	fix := flags.Bool("fix", false, "重新生成 Persona 自己生成的文件（不碰用户的全局配置）")
	if err := flags.Parse(args); err != nil {
		return false, err
	}
	if flags.NArg() != 0 {
		return false, errors.New("node doctor 不接受位置参数")
	}
	opts, err := DefaultDoctorOptions()
	if err != nil {
		return false, err
	}
	opts.Fix = *fix
	rep := RunDoctor(ctx, opts)
	if *asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return !rep.OK, enc.Encode(rep)
	}
	PrintDoctor(out, rep)
	return !rep.OK, nil
}

// doctorLabels are the line prefixes of the text report, the same words `hub doctor` uses (hub/doctor.go).
var doctorLabels = map[string]string{DoctorOK: "正常", DoctorWarn: "注意", DoctorFail: "有问题"}

// PrintDoctor writes rep, one line per check: label | name: what was found; what to do about it (full-width separators).
func PrintDoctor(out io.Writer, rep DoctorReport) {
	fmt.Fprintf(out, "node 自检（%s）\n", rep.Dir)
	counts := map[string]int{}
	for _, c := range rep.Checks {
		counts[c.Status]++
		line := fmt.Sprintf("%s｜%s：%s", doctorLabels[c.Status], c.Name, c.Detail)
		if c.Hint != "" && c.Status != DoctorOK {
			line += "；" + c.Hint
		}
		fmt.Fprintln(out, line)
	}
	if len(rep.Fixed) > 0 {
		fmt.Fprintf(out, "已重新生成：%s\n", strings.Join(rep.Fixed, "、"))
	}
	fmt.Fprintf(out, "共 %d 项：%d 正常，%d 注意，%d 有问题\n", len(rep.Checks), counts[DoctorOK], counts[DoctorWarn], counts[DoctorFail])
}

package node

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/chyroc/jarvis/server/internal/assistantskills"
	"github.com/chyroc/jarvis/server/internal/proto"
)

// The assistant drives Feishu through lark-connector's own skill (lark-connector, in the user's ~/.jarvis/skills): Bash runs `ol METHOD PATH
// [BODY]` against the lark-connector agent API as the user. Every Bash call of the assistant passes a PreToolUse hook that
// reads the command (docs/assistant.md「飞书：lark-connector skill」):
//
//   - only `ol` calls (and a few output filters): reads run at once; writes that others would see as the user (send,
//     reply, recall, react, docs, calendar, mail, approvals, tasks for others…) are approved on the phone first,
//     with method, path and body on the card; writes that only touch the user (marking read, completing their own
//     todo) run at once. Rules: hub.json lark_gate first, then defaultLarkRules; the first match decides.
//   - anything naming the token, curl / wget at lark-connector, or curl flags that would print or redirect the token: denied.
//   - anything else: no decision, the usual permission flow (the phone asks about the whole command).
var defaultLarkRules = []proto.LarkRule{
	// GETs that act as the user or dump the account.
	{Method: "GET", Path: "/links/open", Action: "confirm"},
	{Method: "GET", Path: "/conversations/*/qr-code", Action: "confirm"},
	{Method: "GET", Path: "/maintenance/export", Action: "confirm"},
	{Method: "GET", Path: "**", Action: "allow"},
	{Method: "HEAD", Path: "**", Action: "allow"},
	// POSTs that only read (lark-connector's read-only token allows the same ones; references/platform.md), and translation.
	{Method: "POST", Path: "/context/harvest", Action: "allow"},
	{Method: "POST", Path: "/context/read", Action: "allow"},
	{Method: "POST", Path: "/context/jobs", Action: "allow"},
	{Method: "POST", Path: "/context/jobs/*/cancel", Action: "allow"},
	{Method: "POST", Path: "/context/jobs/*/query", Action: "allow"},
	{Method: "POST", Path: "/context/jobs/*/read", Action: "allow"},
	{Method: "POST", Path: "/search/page", Action: "allow"},
	{Method: "POST", Path: "/search/mention-history", Action: "allow"},
	{Method: "POST", Path: "/wiki/spaces/*/search", Action: "allow"},
	{Method: "POST", Path: "/documents/reader/export", Action: "allow"},
	{Method: "POST", Path: "/documents/share/export", Action: "allow"},
	{Method: "POST", Path: "/documents/base/attachments/export", Action: "allow"},
	{Method: "POST", Path: "/messages/read-states", Action: "allow"},
	{Method: "POST", Path: "/translate/text", Action: "allow"},
	{Method: "POST", Path: "/messages/*/translate", Action: "allow"},
	{Method: "POST", Path: "/messages/*/audio-text", Action: "allow"},
	// Finding the DM with someone (an existing one comes back with created:false; a new one is empty, nobody sees it).
	{Method: "POST", Path: "/conversations", Body: map[string]any{"type": "p2p"}, Action: "allow"},
	// Writes nobody else sees: the user's own read state, their own todo.
	{Method: "POST", Path: "/conversations/*/read-position", Action: "allow"},
	{Method: "POST", Path: "/conversations/*/read-thread-position", Action: "allow"},
	{Method: "POST", Path: "/conversations/*/local-read-position", Action: "allow"},
	{Method: "POST", Path: "/conversations/*/threads/*/read-position", Action: "allow"},
	{Method: "PUT", Path: "/conversations/*/unread", Action: "allow"},
	{Method: "PUT", Path: "/conversations/*/local-unread", Action: "allow"},
	{Method: "POST", Path: "/todos/*/complete", Body: map[string]any{"scope": "self"}, Action: "allow"},
	// Everything else acts as the user where others see it.
	{Method: "*", Path: "**", Action: "confirm"},
}

// larkCall is one `ol` invocation read off a Bash command.
type larkCall struct {
	Method string
	Path   string // without /agent/v1 and the query string
	Query  string
	Body   string // JSON text, a file's content (@file, cut), or "上传文件 X"
}

// larkVerdict: what the gate makes of a whole Bash command.
type larkVerdict struct {
	calls  []larkCall
	deny   string // why the command is refused
	other  bool   // something besides `ol` calls and filters: the usual permission flow decides
	unsure bool   // command substitution / unknown variables: fine for writes (the user sees them), not for reads
}

var (
	olScript    = regexp.MustCompile(`(^|/)lark-connector/scripts/ol\.sh$`)
	larkFilters = map[string]bool{"jq": true, "head": true, "tail": true, "grep": true, "egrep": true, "wc": true, "sort": true, "uniq": true, "cut": true, "tr": true, "column": true,
		// and commands that only print
		"echo": true, "printf": true, "true": true, "date": true, "which": true, "type": true, "cd": true}
	olEnvAllowed = map[string]bool{"lark-connector_ACCOUNT": true, "OL_TIMEOUT": true}
	// curl flags ol passes on that cannot leak the token or reach another host, with whether they take a value.
	olCurlFlags = map[string]bool{"--url-query": true, "-G": false, "--get": false, "--data-urlencode": true, "-d": true, "--data": true,
		"--data-raw": true, "-o": true, "--output": true, "-i": false, "--include": false, "-m": true, "--max-time": true, "-s": false,
		"-S": false, "-f": false, "--compressed": false, "-H": true, "--header": true, "--upload": true, "-X": true, "--request": true}
	olHeaders = map[string]bool{"content-type": true, "accept": true, "x-lark-connector-account": true}
)

// classifyLark reads a Bash command; host is lark-connector's host (curl / wget aimed at it is refused), cwd resolves @file bodies.
func classifyLark(cmd, host, cwd string) larkVerdict {
	var v larkVerdict
	if strings.Contains(cmd, "lark-connector_AGENT_TOKEN") || strings.Contains(cmd, "/proc/") && strings.Contains(cmd, "environ") {
		v.deny = "不要读取、打印或直接使用 lark-connector_AGENT_TOKEN：飞书一律用 ol（lark-connector skill），它自己带 token。"
		return v
	}
	segs, ok := shellSegments(cmd)
	if !ok {
		v.other = true
		return v
	}
	vars := map[string]string{}
	for _, s := range segs {
		if s.subst {
			v.unsure = true
		}
		words := s.words
		// Leading assignments: alone they set a variable for later words; before ol only the account / timeout.
		var env []string
		for len(words) > 0 && isAssignment(words[0]) {
			env = append(env, words[0])
			words = words[1:]
		}
		if len(words) == 0 {
			for _, a := range env {
				k, val, _ := strings.Cut(a, "=")
				vars[k] = val
			}
			continue
		}
		// `timeout 20 ol …` (the prompt asks for timeouts), `command -v ol`.
		if len(words) > 2 && words[0] == "timeout" && regexp.MustCompile(`^[0-9.]+[smh]?$`).MatchString(words[1]) {
			words = words[2:]
		}
		if len(words) > 1 && words[0] == "command" && (words[1] == "-v" || words[1] == "-V") {
			words = []string{"which"}
		}
		name := words[0]
		if name == "bash" || name == "sh" {
			if len(words) > 1 && olScript.MatchString(words[1]) {
				name, words = "ol", words[1:]
			}
		}
		if olScript.MatchString(name) {
			name = "ol"
		}
		switch {
		case name == "ol":
			for _, a := range env {
				if k, _, _ := strings.Cut(a, "="); !olEnvAllowed[k] {
					v.other = true
				}
			}
			c, deny, ok := parseOl(words[1:], vars, cwd)
			if deny != "" {
				v.deny = deny
				return v
			}
			if !ok {
				v.other = true
				continue
			}
			if s.redirect != "" && !safeRedirect(s.redirect) {
				v.other = true
			}
			v.calls = append(v.calls, c)
		case (name == "curl" || name == "wget" || name == "http" || name == "xh") && (host != "" && strings.Contains(s.text, host) || strings.Contains(s.text, "/agent/v1")):
			v.deny = "不要直接 curl lark-connector：用 ol（lark-connector skill），写操作要先在手机上确认。"
			return v
		case larkFilters[name] && len(env) == 0 && (s.redirect == "" || safeRedirect(s.redirect)):
		default:
			v.other = true
		}
	}
	if len(v.calls) == 0 {
		v.other = true
	}
	return v
}

func isAssignment(w string) bool {
	k, _, ok := strings.Cut(w, "=")
	return ok && k != "" && regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`).MatchString(k)
}

// safeRedirect: output into /tmp, or stderr folded or dropped.
func safeRedirect(r string) bool {
	for _, t := range strings.Fields(r) {
		t = strings.TrimLeft(t, "0123456789&>")
		if t == "" || t == "/dev/null" || strings.HasPrefix(t, "/tmp/") {
			continue
		}
		return false
	}
	return true
}

// parseOl reads `METHOD PATH [BODY] [curl flags]`.
func parseOl(args []string, vars map[string]string, cwd string) (larkCall, string, bool) {
	expand := func(s string) (string, bool) {
		if !strings.Contains(s, "$") {
			return s, true
		}
		out := regexp.MustCompile(`\$\{?([A-Za-z_][A-Za-z0-9_]*)\}?`).ReplaceAllStringFunc(s, func(m string) string {
			k := strings.Trim(m, "${}")
			if val, ok := vars[k]; ok {
				return val
			}
			return m
		})
		return out, !strings.Contains(out, "$")
	}
	if len(args) < 2 || args[0] == "events" {
		return larkCall{}, "", false
	}
	method, ok1 := expand(strings.ToUpper(args[0]))
	path, ok2 := expand(args[1])
	if !ok1 || !ok2 {
		return larkCall{}, "", false
	}
	c := larkCall{Method: method}
	path = strings.TrimPrefix(path, "/agent/v1")
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	c.Path, c.Query, _ = strings.Cut(path, "?")
	rest := args[2:]
	if len(rest) > 0 && (strings.HasPrefix(rest[0], "{") || strings.HasPrefix(rest[0], "[")) {
		c.Body, rest = rest[0], rest[1:]
	} else if len(rest) > 0 && strings.HasPrefix(rest[0], "@") {
		c.Body, rest = readBodyFile(rest[0][1:], cwd), rest[1:]
	}
	for i := 0; i < len(rest); i++ {
		f := rest[i]
		switch f {
		case "-v", "--verbose", "--trace", "--trace-ascii", "--trace-time", "-K", "--config", "--libcurl", "-w", "--write-out", "-D", "--dump-header":
			return larkCall{}, "不要给 ol 加会打印请求头（含 token）或改写请求的 curl 参数（" + f + "）。", false
		}
		name, val, inline := strings.Cut(f, "=")
		if strings.HasPrefix(f, "-X") && len(f) > 2 { // -XPOST
			name, val, inline = "-X", f[2:], true
		}
		takes, known := olCurlFlags[name]
		if !known {
			return larkCall{}, "", false // a positional word would be another URL curl sends the token to
		}
		if takes && !inline {
			if i+1 >= len(rest) {
				return larkCall{}, "", false
			}
			i++
			val = rest[i]
		}
		switch name {
		case "-X", "--request":
			c.Method = strings.ToUpper(val) // curl takes the last -X
		case "-H", "--header":
			if h, _, _ := strings.Cut(val, ":"); !olHeaders[strings.ToLower(strings.TrimSpace(h))] {
				return larkCall{}, "", false
			}
		case "--upload":
			c.Body = "上传文件 " + val
		case "-d", "--data", "--data-raw", "--data-urlencode":
			if c.Body == "" {
				c.Body = val
			}
		}
	}
	return c, "", true
}

func readBodyFile(p, cwd string) string {
	if !filepath.IsAbs(p) {
		p = filepath.Join(cwd, p)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return "（读不到 " + p + "）"
	}
	if len(b) > 4000 {
		b = append(b[:4000], "…"...)
	}
	return string(b)
}

// decideLark: the action for a call, from the rules in order (the first match).
func decideLark(rules []proto.LarkRule, c larkCall) string {
	for _, r := range rules {
		if (r.Method == "*" || strings.EqualFold(r.Method, c.Method)) && pathMatches(r.Path, c.Path) && bodyMatches(r.Body, c.Body) {
			return r.Action
		}
	}
	return "confirm"
}

func pathMatches(pattern, path string) bool {
	p := strings.Split(strings.Trim(pattern, "/"), "/")
	s := strings.Split(strings.Trim(path, "/"), "/")
	for i, seg := range p {
		if seg == "**" {
			return true
		}
		if i >= len(s) || (seg != "*" && seg != s[i]) {
			return false
		}
	}
	return len(p) == len(s)
}

func bodyMatches(want map[string]any, body string) bool {
	if len(want) == 0 {
		return true
	}
	var got map[string]any
	if json.Unmarshal([]byte(body), &got) != nil {
		return false
	}
	for k, v := range want {
		if fmt.Sprint(got[k]) != fmt.Sprint(v) {
			return false
		}
	}
	return true
}

// larkPattern: the call's route with its ids blanked, what 本会话总是允许 remembers.
func larkPattern(c larkCall) string {
	segs := strings.Split(strings.Trim(c.Path, "/"), "/")
	for i, s := range segs {
		if regexp.MustCompile(`[0-9]`).MatchString(s) || len(s) > 24 {
			segs[i] = "*"
		}
	}
	return c.Method + " /" + strings.Join(segs, "/")
}

var larkActions = []struct {
	re   *regexp.Regexp
	what string
}{
	{regexp.MustCompile(`^POST /conversations/[^/]+/(messages|files|images|videos|audios|stickers)`), "发消息"},
	{regexp.MustCompile(`^POST /messages/[^/]+/replies`), "回复消息"},
	{regexp.MustCompile(`^POST /messages/[^/]+/(group-)?recall`), "撤回消息"},
	{regexp.MustCompile(`^(POST|DELETE) /messages/[^/]+/reactions`), "表情回复"},
	{regexp.MustCompile(`^POST /messages/([^/]+/)?(forward|merge-forward|forward-many)`), "转发消息"},
	{regexp.MustCompile(`^POST /messages/[^/]+/urgent`), "加急"},
	{regexp.MustCompile(`^\S+ /calendar`), "建 / 改日程"},
	{regexp.MustCompile(`^\S+ /(documents|docs|sheets|wiki|drive|base)`), "写文档"},
	{regexp.MustCompile(`^\S+ /mail`), "发 / 改邮件"},
	{regexp.MustCompile(`^\S+ /todos`), "改待办"},
	{regexp.MustCompile(`^\S+ /approvals`), "处理审批"},
	{regexp.MustCompile(`^\S+ /(groups|conversations)`), "改群 / 会话"},
}

// larkSummary is the phone card's text: what each write does, then its method, path and body.
func larkSummary(calls []larkCall) string {
	var b strings.Builder
	for i, c := range calls {
		if i > 0 {
			b.WriteString("\n\n")
		}
		what := "写操作"
		for _, a := range larkActions {
			if a.re.MatchString(c.Method + " " + c.Path) {
				what = a.what
				break
			}
		}
		b.WriteString("以你的身份在飞书上" + what + "\n" + c.Method + " " + c.Path)
		if c.Query != "" {
			b.WriteString("?" + c.Query)
		}
		if body := prettyBody(c.Body); body != "" {
			b.WriteString("\n" + body)
		}
	}
	return b.String()
}

func prettyBody(body string) string {
	var v any
	if json.Unmarshal([]byte(body), &v) == nil {
		if b, err := json.MarshalIndent(v, "", "  "); err == nil {
			body = string(b)
		}
	}
	if r := []rune(body); len(r) > 2000 {
		body = string(r[:2000]) + "…"
	}
	return body
}

// larkGate decides an assistant Bash call (see defaultLarkRules); nil leaves it to the usual permission flow.
func (n *Node) larkGate(ctx context.Context, in hookInput, ix proto.Interaction) (any, error) {
	var a struct {
		Command string `json:"command"`
	}
	if json.Unmarshal(in.ToolInput, &a) != nil || a.Command == "" {
		return nil, nil
	}
	decision, reason, err := n.larkCheck(ctx, in.SessionID, in.Cwd, a.Command, ix)
	if err != nil || decision == "" {
		return nil, err
	}
	return preToolDecision(decision, reason), nil
}

// larkCheck is the gate itself, for any agent: "allow" / "deny" with the reason to give the model, or "" when the
// command is not the gate's to decide (the usual permission flow). Writes are confirmed on the phone here, so a
// "deny" may carry the user's own words. Claude reaches it through the Bash PreToolUse hook (larkGate), Codex through
// the app-server's command approvals (assistant_codex.go).
func (n *Node) larkCheck(ctx context.Context, session, cwd, command string, ix proto.Interaction) (string, string, error) {
	cfg := n.assistantLark(ctx)
	v := classifyLark(command, larkHost(cfg.Base), cwd)
	if v.deny != "" {
		return "deny", v.deny, nil
	}
	if v.other {
		// A command the gate cannot follow (loops, $(…), scripts) that still calls ol with a write: the phone confirms
		// it here, whole, whatever the permission mode (bypassPermissions would otherwise run it unasked).
		if m := olWriteRe.FindString(command); m != "" {
			ix.Kind, ix.ToolName = proto.IxPermission, "飞书"
			ix.Summary = "以你的身份在飞书上写（命令较复杂，整条显示）\n" + prettyBody(command)
			return n.confirmLark(ctx, session, ix, nil)
		}
		return "", "", nil
	}
	rules := append(append([]proto.LarkRule(nil), cfg.Rules...), defaultLarkRules...)
	var ask []larkCall
	for _, c := range v.calls {
		switch decideLark(rules, c) {
		case "deny":
			return "deny", "这个飞书操作按配置不允许（" + c.Method + " " + c.Path + "）。", nil
		case "allow":
		default:
			if !n.larkAlways(session, larkPattern(c)) {
				ask = append(ask, c)
			}
		}
	}
	if len(ask) == 0 {
		if v.unsure {
			return "", "", nil // reads, but $(…) could run anything: the usual flow asks about the whole command
		}
		return "allow", "lark-connector 读操作", nil
	}
	ix.Kind, ix.ToolName, ix.Summary = proto.IxPermission, "飞书", larkSummary(ask)
	return n.confirmLark(ctx, session, ix, ask)
}

// olWriteRe: an ol call (or ol.sh) whose method is not a read, anywhere in a command.
var olWriteRe = regexp.MustCompile("(?i)(^|[\\s;&|(`])(ol|\\S*/ol\\.sh)\\s+(POST|PUT|PATCH|DELETE|\"?\\$)")

// confirmLark asks the phone; "本会话总是允许" remembers the calls' routes (not for whole commands).
func (n *Node) confirmLark(ctx context.Context, session string, ix proto.Interaction, ask []larkCall) (string, string, error) {
	res, ok, err := n.ask(ctx, ix)
	if err != nil {
		return "", "", err
	}
	if !ok || res.Allow == nil || !*res.Allow {
		msg := res.Message
		if msg == "" {
			msg = "用户没有同意这个飞书写操作，没有执行。"
		}
		return "deny", msg, nil
	}
	if res.Always {
		for _, c := range ask {
			n.rememberLarkAlways(session, larkPattern(c))
		}
	}
	return "allow", "用户在手机上同意了", nil
}

func preToolDecision(decision, reason string) any {
	return map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName": "PreToolUse", "permissionDecision": decision, "permissionDecisionReason": reason,
	}}
}

func larkHost(base string) string {
	h := base
	if i := strings.Index(h, "://"); i >= 0 {
		h = h[i+3:]
	}
	h, _, _ = strings.Cut(h, "/")
	return h
}

func (n *Node) larkAlways(session, pattern string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.larkAllowed[session+" "+pattern]
}

func (n *Node) rememberLarkAlways(session, pattern string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.larkAllowed == nil {
		n.larkAllowed = map[string]bool{}
	}
	n.larkAllowed[session+" "+pattern] = true
}

// assistantLark asks the hub for lark-connector's base, token and gate rules, at most once a minute (the token may
// rotate); an empty config when the hub has none or is unreachable (no env, ol fails with a clear error).
func (n *Node) assistantLark(ctx context.Context) proto.AssistantLark {
	n.mu.Lock()
	cached, at := n.larkCfg, n.larkCfgAt
	n.mu.Unlock()
	if time.Since(at) < time.Minute || n.peer == nil {
		return cached
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var cfg proto.AssistantLark
	if err := n.peer.Call(ctx, "assistant.lark", map[string]any{}, &cfg); err != nil {
		n.logger.Printf("assistant: lark-connector config from hub: %v", err) // never the config itself
		return cached
	}
	n.mu.Lock()
	n.larkCfg, n.larkCfgAt = cfg, time.Now()
	n.mu.Unlock()
	return cfg
}

// assistantLarkEnv puts the skills in the assistant's directory (the user's from ~/.jarvis/skills, linked, and the
// built-in ones, assistantskills) and gives its process what ol.sh needs when the lark-connector skill is among them:
// lark-connector's base and token (from the hub, only in this process's environment) and `ol` on PATH. Without a config
// from the hub, ol says the agent API is not reachable.
func (n *Node) assistantLarkEnv(cwd string) []string {
	skills := filepath.Join(cwd, ".claude", "skills")
	if err := assistantskills.Sync(skills, assistantskills.UserDir(), n.logger.Printf); err != nil {
		n.logger.Printf("assistant: skills: %v", err)
	}
	bin := filepath.Join(n.dir, "assistant-bin")
	_ = os.MkdirAll(bin, 0o755)
	ol := filepath.Join(bin, "ol")
	if _, err := os.Stat(assistantskills.OlScript(skills)); err == nil {
		wrapper := "#!/bin/sh\n# lark-connector's helper (the lark-connector skill), on the assistant's PATH (node/larkgate.go).\nexec bash " +
			shellQuote(assistantskills.OlScript(skills)) + " \"$@\"\n"
		_ = os.WriteFile(ol, []byte(wrapper), 0o755)
	} else {
		_ = os.Remove(ol) // no lark-connector skill on this machine: no ol to call
	}
	env := []string{"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"), "JARVIS_ASSISTANT_BIN=" + bin}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if cfg := n.assistantLark(ctx); cfg.Base != "" && cfg.Token != "" {
		env = append(env, "lark-connector_AGENT_BASE="+cfg.Base, "lark-connector_AGENT_TOKEN="+cfg.Token)
		if cfg.Account > 0 {
			env = append(env, fmt.Sprintf("lark-connector_ACCOUNT=%d", cfg.Account))
		}
	}
	return env
}

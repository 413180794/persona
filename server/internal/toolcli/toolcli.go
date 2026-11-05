// Package toolcli is `personad tool`: a connector's tools from the assistant's shell, through the node on this machine
// (node/clicall.go lets only the assistant's processes through and only the tools of the connectors signed in), which
// asks the hub, whose gate decides each write (connector_gate: by default the user allows it on the phone).
//
//	personad tool <tool> [<json args> | -]
//	    one call; its answer on stdout
//	personad tool --cli <connector> [--format json|text] <words…>
//	    the connector's command line (Telegram's tg, on the assistant's PATH: node/clicall.go): the connector's
//	    <connector>_cli tool reads the words and names the tool to call; that tool is called (through the gate, as by
//	    name); with --format text the connector shows its answer for people (docs/connector-protocol.md「命令行」)
//
// Output is for a program: one line of JSON on stdout, or text for a person. A failure is one line of JSON on stderr
// ({error, kind, retry_after}), or "<name>: …" with --format text, and an exit code by its kind.
package toolcli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/node"
	"github.com/nwverse/persona/server/internal/proto"
)

// Exit codes, by what went wrong (the same as Telegram's tg had).
const (
	exitOK          = 0
	exitFailed      = 1 // the call failed (error says why)
	exitUsage       = 2 // the command line is wrong
	exitUnavailable = 3 // the node, the hub or the connector cannot be reached or is not signed in
	exitRateLimited = 4 // the connector waits out a rate limit (retry_after seconds)
	exitDeclined    = 5 // the user (or connector_gate) did not allow the write
)

var kinds = map[int]string{exitFailed: "failed", exitUsage: "usage", exitUnavailable: "unavailable", exitRateLimited: "rate_limited",
	exitDeclined: "declined"}

// callTimeout covers a write waiting for the user on the phone (the card lasts 15 minutes).
const callTimeout = 30 * time.Minute

// stdinMost is the most a command line reads from stdin (a message's text).
const stdinMost = 1 << 20

// dial reaches the node; it hangs up when ctx is cancelled (Ctrl-C, SIGTERM), so a write waiting for the user ends on
// the node too and the hub takes its card off the phone (rpc.Peer.Cancels), instead of a card nobody waits for until
// it expires (replaced by tests).
var dial node.Dialer = node.DialSocketInterruptible

// call asks the node to run a tool as the assistant's (replaced by tests).
var call = func(ctx context.Context, name string, args any) (proto.ToolResult, error) {
	line, err := dial(ctx, map[string]any{"kind": "cli.call", "name": name, "args": args})
	if err != nil {
		return proto.ToolResult{}, err
	}
	var res proto.ToolResult
	if err := json.Unmarshal(line, &res); err != nil {
		return proto.ToolResult{}, fmt.Errorf("bad answer from the Persona node: %w", err)
	}
	return res, nil
}

const usage = `personad tool: a connector's tools from the assistant's shell, through the Persona hub
  personad tool <tool> [<json args> | -]
  personad tool --cli <connector> [--format json|text] <words…>`

// Main runs one command line and returns its exit code.
func Main(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprintln(stdout, usage)
		if len(args) == 0 {
			return exitUsage
		}
		return exitOK
	}
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	if args[0] == "--cli" {
		if len(args) < 2 || args[1] == "" {
			fmt.Fprintln(stderr, usage)
			return exitUsage
		}
		return commandLine(ctx, args[1], args[2:], stdin, stdout, stderr)
	}
	out := failer{name: "personad tool", stderr: stderr}
	var toolArgs json.RawMessage = json.RawMessage(`{}`)
	switch {
	case len(args) > 2:
		return out.fail(exitUsage, "one tool and one JSON object of its arguments", 0)
	case len(args) == 2 && args[1] == "-":
		b, err := io.ReadAll(io.LimitReader(stdin, stdinMost))
		if err != nil {
			return out.fail(exitUsage, "stdin: "+err.Error(), 0)
		}
		toolArgs = b
	case len(args) == 2:
		toolArgs = json.RawMessage(args[1])
	}
	if !json.Valid(toolArgs) {
		return out.fail(exitUsage, "the arguments are not JSON", 0)
	}
	res, err := call(ctx, args[0], toolArgs)
	if err != nil {
		return out.fail(exitUnavailable, fmt.Sprintf("the Persona node on this machine is not reachable (%v): is the hub connected?", err), 0)
	}
	text := firstText(res)
	if res.IsError {
		code, retry := classify(text)
		return out.fail(code, text, retry)
	}
	fmt.Fprintln(stdout, text)
	return exitOK
}

// plan is what a connector's <id>_cli tool makes of a command line: the call it means, or what to say right away
// (its usage, a mistake in the words) with an exit code.
type plan struct {
	Name   string `json:"name"` // the command, for messages ("tg")
	Exit   *int   `json:"exit"`
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
	Call   *struct {
		Tool string          `json:"tool"`
		Args json.RawMessage `json:"args"`
	} `json:"call"`
	Render string `json:"render"` // how --format text asks the connector to show the answer ("" prints it as it is)
}

// commandLine runs the connector's command line: the words planned by <connector>_cli, the tool it names called,
// the answer shown.
func commandLine(ctx context.Context, conn string, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	format := "json"
	var words []string
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--format" && i+1 < len(args):
			format = args[i+1]
			i++
		case strings.HasPrefix(a, "--format="):
			format = strings.TrimPrefix(a, "--format=")
		default:
			words = append(words, a)
		}
	}
	out := failer{name: conn, text: format == "text", stderr: stderr}
	if format != "json" && format != "text" {
		return out.fail(exitUsage, fmt.Sprintf("--format %s: json or text", format), 0)
	}
	in := map[string]any{"argv": append([]string{}, words...)}
	if slices.Contains(words, "-") { // the text comes from stdin (tg send <chat> -)
		b, err := io.ReadAll(io.LimitReader(stdin, stdinMost))
		if err != nil {
			return out.fail(exitUsage, "stdin: "+err.Error(), 0)
		}
		in["stdin"] = string(b)
	}
	res, err := call(ctx, conn+"_cli", in)
	if err != nil {
		return out.fail(exitUnavailable, fmt.Sprintf("the Persona node on this machine is not reachable (%v): is the hub connected?", err), 0)
	}
	if res.IsError {
		code, retry := classify(firstText(res))
		return out.fail(code, firstText(res), retry)
	}
	var p plan
	if err := json.Unmarshal([]byte(firstText(res)), &p); err != nil {
		return out.fail(exitFailed, conn+"_cli's answer is not a plan: "+err.Error(), 0)
	}
	if p.Name != "" {
		out.name = p.Name
	}
	if p.Exit != nil {
		if p.Stdout != "" {
			fmt.Fprintln(stdout, strings.TrimRight(p.Stdout, "\n"))
		}
		if p.Stderr != "" {
			if *p.Exit == exitOK {
				fmt.Fprintln(stderr, strings.TrimRight(p.Stderr, "\n"))
			} else {
				return out.fail(*p.Exit, p.Stderr, 0)
			}
		}
		return *p.Exit
	}
	if p.Call == nil || p.Call.Tool == "" {
		return out.fail(exitFailed, conn+"_cli named no tool to call", 0)
	}
	toolArgs := p.Call.Args
	if len(toolArgs) == 0 {
		toolArgs = json.RawMessage(`{}`)
	}
	res, err = call(ctx, p.Call.Tool, toolArgs)
	if err != nil {
		return out.fail(exitUnavailable, fmt.Sprintf("the Persona node on this machine is not reachable (%v): is the hub connected?", err), 0)
	}
	text := firstText(res)
	if res.IsError {
		code, retry := classify(text)
		return out.fail(code, text, retry)
	}
	if format != "text" || p.Render == "" {
		fmt.Fprintln(stdout, text)
		return exitOK
	}
	shown, err := call(ctx, conn+"_cli", map[string]any{"render": p.Render, "result": text})
	if err != nil || shown.IsError {
		why := firstText(shown)
		if err != nil {
			why = err.Error()
		}
		return out.fail(exitFailed, "the answer could not be shown as text: "+why, 0)
	}
	var page struct {
		Stdout string `json:"stdout"`
	}
	if err := json.Unmarshal([]byte(firstText(shown)), &page); err != nil {
		return out.fail(exitFailed, "the answer could not be shown as text: "+err.Error(), 0)
	}
	fmt.Fprint(stdout, page.Stdout)
	if !strings.HasSuffix(page.Stdout, "\n") && page.Stdout != "" {
		fmt.Fprintln(stdout)
	}
	return exitOK
}

// failer writes a failure as the format says: "<name>: …" for people, one line of JSON for a program.
type failer struct {
	name   string
	text   bool
	stderr io.Writer
}

func (f failer) fail(code int, msg string, retry int) int {
	if f.text {
		fmt.Fprintln(f.stderr, f.name+": "+msg)
		return code
	}
	out := map[string]any{"error": msg, "kind": kinds[code]}
	if retry > 0 {
		out["retry_after"] = retry
	}
	b, _ := json.Marshal(out)
	fmt.Fprintln(f.stderr, string(b))
	return code
}

func firstText(res proto.ToolResult) string {
	if len(res.Content) > 0 {
		return res.Content[0].Text
	}
	return ""
}

// waitFor is how the hub and the connectors say how long a rate limit lasts.
var waitFor = regexp.MustCompile(`(?:还要 |限流中，)(\d+) 秒`)

// classify is a failure's exit code by what the node, the hub and the connector say, and how long a rate limit lasts.
func classify(text string) (int, int) {
	if m := waitFor.FindStringSubmatch(text); m != nil {
		n, _ := strconv.Atoi(m[1])
		return exitRateLimited, n
	}
	switch {
	case strings.Contains(text, "is not available"), strings.Contains(text, "只能在助手的对话里用"), strings.HasPrefix(text, "Persona hub error"):
		return exitUnavailable, 0
	case strings.HasPrefix(text, "The user declined"), strings.Contains(text, "(connector_gate) do not allow"):
		return exitDeclined, 0
	}
	return exitFailed, 0
}

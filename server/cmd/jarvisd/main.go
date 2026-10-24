// Command jarvisd is the Jarvis server: hub, node, hook and mcp modes in one binary.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/chyroc/jarvis/server/internal/hub"
	"github.com/chyroc/jarvis/server/internal/mcpserver"
	"github.com/chyroc/jarvis/server/internal/node"
)

var version = "dev"

const usage = `usage:
  jarvisd hub [run]        run the hub (config ~/.jarvis/hub.json)
  jarvisd hub pair         print the pairing URL, token and QR code
  jarvisd node --stdio     run the node protocol on stdin/stdout (started by the hub over ssh)
  jarvisd hook             Claude Code hook entry (reads hook JSON on stdin)
  jarvisd mcp              Claude Code MCP server (stdio)
  jarvisd version`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var err error
	switch os.Args[1] {
	case "version", "--version", "-v":
		fmt.Println(version)
	case "hub":
		sub := "run"
		if len(os.Args) > 2 {
			sub = os.Args[2]
		}
		switch sub {
		case "run":
			err = hub.Run(ctx, version)
		case "pair":
			err = hub.Pair()
		default:
			err = fmt.Errorf("unknown hub command %q", sub)
		}
	case "node":
		var n *node.Node
		if n, err = node.New(version); err == nil {
			err = n.ServeStdio(ctx)
			if err == io.EOF {
				err = nil
			}
		}
	case "hook":
		runHook(ctx)
	case "mcp":
		err = mcpserver.Serve(ctx, version, os.Stdin, os.Stdout)
	default:
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		log.Fatal(err)
	}
}

// runHook forwards a Claude Code hook event to the local node. Any failure exits 0 silently so claude carries on as usual.
func runHook(ctx context.Context) {
	input, err := io.ReadAll(os.Stdin)
	if err != nil || !json.Valid(input) {
		return
	}
	line, err := node.DialSocket(ctx, map[string]any{"kind": "hook", "input": json.RawMessage(input), "pane": os.Getenv("HERDR_PANE_ID")})
	if err != nil {
		return
	}
	var resp struct {
		Output json.RawMessage `json:"output"`
	}
	if json.Unmarshal(line, &resp) != nil || len(resp.Output) == 0 || string(resp.Output) == "null" {
		return
	}
	os.Stdout.Write(resp.Output)
	os.Stdout.Write([]byte("\n"))
}

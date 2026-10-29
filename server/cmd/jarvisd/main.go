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
	"strconv"
	"syscall"
	"time"

	"github.com/chyroc/jarvis/server/internal/hub"
	"github.com/chyroc/jarvis/server/internal/mcpserver"
	"github.com/chyroc/jarvis/server/internal/node"
	"github.com/chyroc/jarvis/server/internal/sysmon"
)

var version = "dev"

const usage = `usage:
  jarvisd hub [run]        run the hub (config ~/.jarvis/hub.json)
  jarvisd hub pair         print the pairing URL, token and QR code
  jarvisd node --stdio     run the node protocol on stdin/stdout (started by the hub over ssh)
  jarvisd hook             Claude Code hook entry (reads hook JSON on stdin)
  jarvisd mcp              Claude Code MCP server (stdio)
  jarvisd sysmon [n]       print this machine's system info and n metric samples, with what they cost
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
	case "sysmon":
		n := 3
		if len(os.Args) > 2 {
			n, _ = strconv.Atoi(os.Args[2])
		}
		runSysmon(ctx, n)
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
	out := node.Hook(ctx, input, os.Getenv("HERDR_PANE_ID"))
	if len(out) == 0 {
		return
	}
	os.Stdout.Write(out)
	os.Stdout.Write([]byte("\n"))
}

// runSysmon shows what a node would report (docs/machines.md), and the CPU time it took: how the collection cost
// in the docs was measured.
func runSysmon(ctx context.Context, n int) {
	enc := json.NewEncoder(os.Stdout)
	start := time.Now()
	_ = enc.Encode(sysmon.ReadSystem(ctx))
	fmt.Printf("system info: %v\n", time.Since(start).Round(time.Millisecond))
	c := sysmon.NewCollector(ctx)
	defer c.Close()
	var ru0, ru1 syscall.Rusage
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &ru0)
	began := time.Now()
	var took time.Duration
	for i := 0; i <= n && ctx.Err() == nil; i++ {
		if i > 0 {
			time.Sleep(10 * time.Second)
		}
		t := time.Now()
		s := c.Sample(t)
		took += time.Since(t)
		_ = enc.Encode(s)
	}
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &ru1)
	cpu := time.Duration(ru1.Utime.Nano()+ru1.Stime.Nano()-ru0.Utime.Nano()-ru0.Stime.Nano()) * time.Nanosecond
	wall := time.Since(began)
	fmt.Printf("%d samples: %v each; jarvisd CPU %v over %v (%.3f%%), not counting iostat / vm_stat on macOS\n",
		n+1, (took / time.Duration(n+1)).Round(time.Microsecond), cpu.Round(time.Millisecond), wall.Round(time.Second),
		100*cpu.Seconds()/wall.Seconds())
}

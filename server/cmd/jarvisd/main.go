// Command jarvisd is the Jarvis server: hub, node, hook and mcp modes in one binary.
package main

import (
	"context"
	"encoding/json"
	"flag"
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
	"github.com/chyroc/jarvis/server/internal/tunnel"
)

var version = "dev"

const usage = `usage:
  jarvisd launch <name> …   run claude with a launcher kept in 设置 › 模型 (from the local node)
  jarvisd hub [run]        run the hub (config ~/.jarvis/hub.json)
  jarvisd hub pair         print the pairing URL, token and QR code
  jarvisd hub tailcat-key  print the hub's tailcat key (created the first time), for "node tailcat init --hub"
  jarvisd node --stdio     run the node protocol on stdin/stdout (started by the hub over ssh); with a resident
                           node running on this machine, relay to it instead
  jarvisd node --attach    resident node (launchd, in the login session): serves what --stdio relays, so what it
                           starts can use the login keychain (docs/server.md「常驻 node」); set up for tailcat,
                           also reachable by the hub over tailcat
  jarvisd node tailcat init --hub <key> [--derp-map-url <url>]
                           let the hub reach this machine over tailcat when it cannot ssh here; prints the address
                           for hub.json (docs/server.md「tailcat 打洞」)
  jarvisd node tailcat addr
                           print this machine's tailcat address again
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
		case "tailcat-key":
			var k *tunnel.HubKey
			if k, err = tunnel.LoadOrCreateHubKey(tunnel.HubKeyPath()); err == nil {
				fmt.Println(k.Public())
			}
		default:
			err = fmt.Errorf("unknown hub command %q", sub)
		}
	case "node":
		if len(os.Args) > 2 && os.Args[2] == "--attach" {
			err = runResident(ctx)
			break
		}
		if len(os.Args) > 2 && os.Args[2] == "tailcat" {
			err = runNodeTailcat(ctx, os.Args[3:])
			break
		}
		var relayed bool
		if relayed, err = node.Relay(ctx, os.Stdin, os.Stdout); relayed {
			if err == io.EOF {
				err = nil
			}
			break
		}
		var n *node.Node
		if n, err = node.New(version); err == nil {
			err = n.ServeStdio(ctx)
			if err == io.EOF {
				err = nil
			}
		}
	case "hook":
		runHook(ctx)
	case "launch": // a launcher kept in 设置 › 模型: its settings from the local node, then claude (docs/launchers.md)
		if len(os.Args) < 3 {
			err = fmt.Errorf("usage: jarvisd launch <launcher> [claude args…]")
			break
		}
		err = node.Launch(ctx, os.Args[2], os.Args[3:])
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

// runResident is `jarvisd node --attach`: relays over ssh, and the hub over tailcat when this machine is set up for
// it. A new binary arriving over tailcat ends it (exit 0): its supervisor starts the new one.
func runResident(ctx context.Context) error {
	// tailcat stops only after Attach has closed the hub's connection: torn down first, the close would never reach
	// the hub, which would then take the dead connection for a live one until its pings went unanswered.
	tctx, tstop := context.WithCancel(context.Background())
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	logger := log.New(os.Stderr, "", log.LstdFlags)
	conns, stopped, err := node.ServeTailcat(tctx, logger, func() {
		logger.Printf("tailcat: a new jarvisd is installed; exiting so it starts")
		cancel()
	})
	if err != nil {
		logger.Printf("tailcat: %v (serving relays over ssh only)", err) // a broken key file must not stop the node
	}
	err = node.Attach(ctx, func() (*node.Node, error) { return node.New(version) }, conns)
	tstop()
	<-stopped
	return err
}

// runNodeTailcat is `jarvisd node tailcat init|addr`.
func runNodeTailcat(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: jarvisd node tailcat init --hub <key> [--derp-map-url <url>] | addr")
	}
	switch args[0] {
	case "addr":
		addr, err := node.TailcatAddr()
		if err != nil {
			return err
		}
		fmt.Println(addr)
		return nil
	case "init":
		fs := flag.NewFlagSet("tailcat init", flag.ContinueOnError)
		hubKey := fs.String("hub", "", "the hub's key: `jarvisd hub tailcat-key` on the hub's machine")
		derpMap := fs.String("derp-map-url", "", "a DERP map of your own relays instead of Tailscale's public ones")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *hubKey == "" {
			return fmt.Errorf("--hub is required: run `jarvisd hub tailcat-key` on the hub's machine")
		}
		ictx, cancel := context.WithTimeout(ctx, time.Minute)
		defer cancel()
		k, err := tunnel.InitNodeKey(ictx, tunnel.NodeKeyPath(), *hubKey, *derpMap)
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "set up %s (DERP region %d). In the hub's hub.json, this machine's entry gets\n"+
			"  \"tailcat_addr\": the line below (a secret: it lets the hub's key in)\n"+
			"then restart the resident node here (scripts/node-launchd.sh install) and the hub.\n", tunnel.NodeKeyPath(), k.RegionID)
		fmt.Println(k.Addr())
		return nil
	}
	return fmt.Errorf("unknown tailcat command %q", args[0])
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

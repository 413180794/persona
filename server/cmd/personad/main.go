// Command jarvisd is the Persona server: hub, node, hook and mcp modes in one binary.
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
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/nwverse/persona/server/internal/identity"
	"github.com/nwverse/persona/server/internal/osproc"

	"github.com/nwverse/persona/server/internal/assistant"
	"github.com/nwverse/persona/server/internal/hub"
	"github.com/nwverse/persona/server/internal/mcpserver"
	"github.com/nwverse/persona/server/internal/node"
	"github.com/nwverse/persona/server/internal/nodeupdate"
	"github.com/nwverse/persona/server/internal/office"
	"github.com/nwverse/persona/server/internal/relay"
	"github.com/nwverse/persona/server/internal/sysmon"
	"github.com/nwverse/persona/server/internal/tgcli"
	"github.com/nwverse/persona/server/internal/toolcli"
	"github.com/nwverse/persona/server/internal/tunnel"
	"github.com/nwverse/persona/server/internal/update"
	"github.com/nwverse/persona/server/internal/voice"
)

// What the build wrote in (-X): the version for people, and for updates (docs/hub-update.md) the channel
// (release / docker / dev), the build number (UTC YYYYMMDDHHMM), where the publisher's dl/ is, the commit it was
// built from ("-dirty" after it with changes not committed: a release has to contain it to be installed by itself),
// and for the Docker image the image it was built as (where to look for newer ones).
var (
	version   = "dev"
	channel   = ""
	build     = ""
	updateURL = ""
	commit    = ""
	image     = ""
)

func buildInfo() update.Build {
	b := update.Make(version, channel, build, updateURL)
	b.Commit, b.Image = commit, image
	return b
}

const usage = `usage:
  personad launch <name> …   run claude with a launcher kept in 设置 › 模型 (from the local node)
  personad hub [run]        run the hub (config ~/.persona/hub.json)
  personad hub pair [--legacy] [--android] print a pairing QR code and optional Android download link
  personad hub pair --machine <id>          print a pairing link for that link machine's jarvis-link (docs/link.md)
  personad hub pair --peer  print a code another hub pairs with this one by (hub mesh, docs/mesh.md)
  personad hub peer add <code>|-   pair this hub with the hub whose code it is
  personad hub peer list           the paired hubs and the ones found on the LAN
  personad hub peer remove <hub_id>
  personad hub peer center [<hub_id>] [--confirm]   make that hub (this one by default) the group's center
                           unpair a hub (its credential is revoked on both sides)
  personad hub doctor       只读检查 hub 配置、网络和证书
  personad hub admin …      the admin UI on this machine: access <scope>, reset-password (docs/admin.md)
  personad hub tailcat-key  print the hub's tailcat key (created the first time), for "node tailcat init --hub"
  personad hub assistant-token  print a new bus token for an assistant run elsewhere (hub.json assistant.external)
  personad assistant        the assistant (the Persona tab's) as its own process on the hub's bus; the hub starts it
                           unless hub.json says assistant.external (token: stdin or PERSONA_ASSISTANT_TOKEN)
  personad node --stdio     run the node protocol on stdin/stdout (started by the hub over ssh); with a resident
                           node running on this machine, relay to it instead
  personad node --attach    resident node (launchd, in the login session): serves what --stdio relays, so what it
                           starts can use the login keychain (docs/server.md「常驻 node」); set up for tailcat,
                           also reachable by the hub over tailcat
  personad node tailcat init --hub <key> [--derp-map-url <url>]
                           let the hub reach this machine over tailcat when it cannot ssh here; prints the address
                           for hub.json (docs/server.md「tailcat 打洞」)
  personad node tailcat addr
                           print this machine's tailcat address again
  personad node reset-uid   give this computer a new machine uid (~/.persona/machine-uid): for a virtual machine cloned
                           with another's home directory, which hubs would take for that one (docs/mesh.md)
  personad hook             Claude Code hook entry (reads hook JSON on stdin)
  personad mcp              Claude Code MCP server (stdio)
  personad tg …             the user's Telegram through the hub, for the assistant (the telegram skill): personad tg --help
  personad office docx|pptx|xlsx|check …
                           write a Word document or a deck from Markdown, a workbook from JSON / CSV, or say what is in
                           one (the office skill): personad office --help
  personad codex-start <file>
                           run the Codex launcher in file (written by the node, removed once read) with the first
                           message as its last argument, no shell in between (docs/codex.md「从手机新建」)
  personad relay [config]   run the push relay (config ~/.persona/relay.json): pushes for hubs without the app's APNs
                           key, payloads sealed to the phone (docs/push-relay.md)
  personad voice            the built-in voice plugin: the hub runs it (hub.json plugins, docs/plugins.md「内置语音插件」)
  personad plugin …         plugins on the hub's machine: list, install, upgrade, rollback, enable, disable, config,
                           secret, uninstall (docs/plugins.md「命令行」; personad plugin for the details)
  personad sysmon [n]       print this machine's system info and n metric samples, with what they cost
  personad release manifest|check-next …
                           the publisher's release manifest and checks (scripts/release/publish.sh, docs/hub-update.md「发布」)
  personad update-watch <pending>
                           the watchdog the hub starts over its own update (docs/hub-update.md「回滚」)
  personad version [-v]     the version; -v adds the channel and build number`

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
		if len(os.Args) > 2 && os.Args[2] == "-v" {
			b := buildInfo()
			fmt.Printf("channel %s, build %d\n", b.Channel, b.Number)
		}
	case "release":
		err = runRelease(os.Args[2:])
	case "node-update-check":
		err = json.NewEncoder(os.Stdout).Encode(nodeupdate.Check{Version: version, OS: runtime.GOOS, Arch: runtime.GOARCH})
	case "node-update-watch":
		err = runNodeUpdateWatch(ctx, os.Args[2:])
	case "update-watch": // the old binary watching an update come up (hub/selfupdate.go)
		err = runUpdateWatch(ctx, os.Args[2:])
	case "hub":
		sub := "run"
		if len(os.Args) > 2 {
			sub = os.Args[2]
		}
		switch sub {
		case "run":
			err = hub.Run(ctx, buildInfo())
		case "doctor":
			if len(os.Args) != 3 {
				err = fmt.Errorf("hub doctor 不接受额外参数")
			} else {
				err = hub.Doctor(ctx, os.Stdout)
			}
		case "pair":
			flags := flag.NewFlagSet("hub pair", flag.ContinueOnError)
			legacy := flags.Bool("legacy", false, "使用主 token 打印旧格式二维码")
			android := flags.Bool("android", false, "同时打印 Android 安装包的临时下载链接")
			machine := flags.String("machine", "", "给 hub.json 里这台 kind 为 link 的电脑的 jarvis-link 配对")
			peer := flags.Bool("peer", false, "生成和另一台 hub 互联的互联码（docs/mesh.md）")
			if err = flags.Parse(os.Args[3:]); err == nil {
				switch {
				case flags.NArg() != 0:
					err = fmt.Errorf("hub pair 不接受位置参数")
				case *peer && (*machine != "" || *legacy || *android):
					err = fmt.Errorf("--peer 不能和 --machine、--legacy、--android 一起用")
				case *peer:
					err = hub.PairPeer()
				case *machine != "" && (*legacy || *android):
					err = fmt.Errorf("--machine 不能和 --legacy、--android 一起用")
				case *machine != "":
					err = hub.PairMachine(*machine)
				default:
					err = hub.Pair(*legacy, *android)
				}
			}
		case "admin":
			err = hub.AdminCommand(ctx, os.Args[3:], os.Stdin, os.Stdout, os.Stderr)
		case "peer":
			err = hub.PeerCommand(os.Args[3:], os.Stdin, os.Stdout)
		case "assistant-token": // a token for an assistant run elsewhere (hub.json assistant.external, docs/assistant.md)
			if len(os.Args) != 3 {
				err = fmt.Errorf("hub assistant-token 不接受额外参数")
			} else {
				err = hub.AssistantToken(ctx, os.Stdout)
			}
		case "tailcat-key":
			var k *tunnel.HubKey
			if k, err = tunnel.LoadOrCreateHubKey(tunnel.HubKeyPath()); err == nil {
				fmt.Println(k.Public())
			}
		default:
			err = fmt.Errorf("unknown hub command %q", sub)
		}
	case "assistant": // the assistant as its own process on the hub's bus (the hub starts it, docs/assistant.md)
		err = assistant.Main(ctx, buildInfo().Version, os.Stdin)
	case "node":
		if len(os.Args) > 2 && os.Args[2] == "--attach" {
			err = runResident(ctx)
			break
		}
		if len(os.Args) > 2 && os.Args[2] == "tailcat" {
			err = runNodeTailcat(ctx, os.Args[3:])
			break
		}
		if len(os.Args) > 2 && os.Args[2] == "reset-uid" {
			err = resetMachineUID()
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
	case "codex-start": // a Codex launcher with the phone's first message, read from a file (node/codex_start.go)
		err = node.CodexStart(os.Args[2:])
	case "tab-start": // a command a runtime gave to resume its session in a new tab (`pi --session …`, node/pi.go)
		err = node.CodexStart(os.Args[2:])
	case "relay":
		path := filepath.Join(identity.HomeDir(), "relay.json")
		if len(os.Args) > 2 {
			path = os.Args[2]
		}
		err = relay.Run(ctx, path)
	case "mcp":
		err = mcpserver.Serve(ctx, version, os.Stdin, os.Stdout)
	case "tg":
		os.Exit(tgcli.Main(ctx, os.Args[2:], os.Stdin, os.Stdout, os.Stderr))
	case "tool": // a connector's tools and command line from the assistant's shell (internal/toolcli)
		os.Exit(toolcli.Main(ctx, os.Args[2:], os.Stdin, os.Stdout, os.Stderr))
	case "office":
		os.Exit(office.Main(os.Args[2:], os.Stdout, os.Stderr))
	case "voice":
		err = voice.Run(ctx)
	case "plugin":
		err = hub.PluginCommand(ctx, os.Args[2:], os.Stdin, os.Stdout, os.Stderr)
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
	defer tstop()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	restart := make(chan struct{})
	updater := nodeupdate.New(version, func() { close(restart); cancel() })
	defer updater.Close()
	if updater.RestartRequired() {
		return syscall.Exec(updater.Executable(), os.Args, os.Environ())
	}
	logger := log.New(os.Stderr, "", log.LstdFlags)
	conns, stopped, err := node.ServeTailcat(tctx, logger, func() {
		logger.Printf("tailcat: a new jarvisd is installed; exiting so it starts")
		cancel()
	})
	if err != nil {
		logger.Printf("tailcat: %v (serving relays over ssh only)", err) // a broken key file must not stop the node
	}
	// SIGHUP (a deploy: scripts/deploy/node-launchd.sh restart-idle) restarts on the binary now installed, once no
	// conversation is answering or running background tasks: they go on across hub restarts, not across this one.
	restartIdle := make(chan struct{})
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	defer signal.Stop(hup)
	go func() {
		select {
		case <-hup:
		case <-ctx.Done():
			return
		}
		logger.Printf("restart asked for: restarting once no conversation is busy")
		for node.AssistantsBusy() {
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
		}
		close(restartIdle)
		cancel()
	}()
	// The runtime socket is the resident node's, not one connection's: runtimes (DSH…) stay connected while the hub
	// reconnects, and connect before it does (node/runtimehost.go).
	err = node.Attach(ctx, func() (*node.Node, error) {
		n, err := node.New(version)
		if err == nil {
			n.SetUpdater(updater)
		}
		return n, err
	}, conns, node.NewRuntimeHost(version, ""))
	tstop()
	<-stopped
	select {
	case <-restart:
		return syscall.Exec(updater.Executable(), os.Args, os.Environ())
	case <-restartIdle:
		logger.Printf("restarting on %s", updater.Executable())
		return syscall.Exec(updater.Executable(), os.Args, os.Environ())
	default:
		return err
	}
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
			"then restart the resident node here (scripts/deploy/node-launchd.sh install) and the hub.\n", tunnel.NodeKeyPath(), k.RegionID)
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
	out := node.Hook(ctx, input, node.HookPane(os.Getenv))
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
	cpu0 := osproc.CPUTime()
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
	cpu := osproc.CPUTime() - cpu0
	wall := time.Since(began)
	fmt.Printf("%d samples: %v each; jarvisd CPU %v over %v (%.3f%%), not counting iostat / vm_stat on macOS\n",
		n+1, (took / time.Duration(n+1)).Round(time.Microsecond), cpu.Round(time.Millisecond), wall.Round(time.Second),
		100*cpu.Seconds()/wall.Seconds())
}

// resetMachineUID makes a new ~/.jarvis/machine-uid; the node picks it up when it starts again.
func resetMachineUID() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	uid, err := node.ResetMachineUID(identity.DataDir(home))
	if err != nil {
		return err
	}
	fmt.Println(uid)
	fmt.Fprintln(os.Stderr, "已换成新的机器 uid。重启这台电脑上的常驻 node（scripts/node-launchd.sh install 会重启它），hub 下次连上时就用新的。")
	return nil
}

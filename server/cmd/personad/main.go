// Command jarvisd is the Persona server: hub, node, hook and mcp modes in one binary.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/nwverse/persona/server/internal/extmgr"
	"github.com/nwverse/persona/server/internal/identity"
	"github.com/nwverse/persona/server/internal/managerconf"
	"github.com/nwverse/persona/server/internal/nodecmd"

	"github.com/nwverse/persona/server/internal/assistant"
	"github.com/nwverse/persona/server/internal/extension/sandbox"
	"github.com/nwverse/persona/server/internal/hub"
	"github.com/nwverse/persona/server/internal/manager"
	"github.com/nwverse/persona/server/internal/relay"
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
` + nodecmd.LaunchUsage + `
  personad hub [run]        run the hub (config ~/.persona/hub.json); behind its edge on Linux and macOS, which
                           holds the ports and switches to a new binary with no restart (docs/hub-restart.md「蓝绿切换」)
  personad hub switch [--binary <path>] [--prev <path>] [--status]
                           have the running edge start <path> (default: the hub's binary) and switch to it once it is up;
                           exit 0 switched, 1 it did not (the hub serves on), 3 no edge here (restart the service instead)
  personad hub setup [--yes] [--listen :18660] [--tls :18662] [--outside] [--no-service] [--json]
                                           set this machine up as the hub: hub.json, database, service, pairing (docs/onboarding.md)
  personad hub pair [--legacy] [--android] print a pairing QR code and optional Android download link
  personad hub pair --machine <id> [--add [--name <name>]]
                                           print a pairing link for that link machine's jarvis-link (docs/link.md);
                                           --add adds it to hub.json first when it is not there
  personad hub pair --peer  print a code another hub pairs with this one by (hub mesh, docs/mesh.md)
  personad hub peer add <code>|-   pair this hub with the hub whose code it is
  personad hub peer list           the paired hubs and the ones found on the LAN
  personad hub peer remove <hub_id>
                           unpair a hub (its credential is revoked on both sides)
  personad hub peer center [<hub_id>] [--confirm]   make that hub (this one by default) the group's center
  personad hub doctor       只读检查 hub 配置、网络和证书
  personad hub admin …      the admin UI on this machine: access <scope>, reset-password (docs/admin.md)
  personad hub tailcat-key  print the built-in manager's tailcat key (created the first time), for "node tailcat init --hub"
  personad hub assistant-token  print a new bus token for an assistant run elsewhere (hub.json assistant.external)
  personad assistant        the assistant (the Persona tab's) as its own process on the hub's bus; the hub starts it
                           unless hub.json says assistant.external (token: stdin or PERSONA_ASSISTANT_TOKEN)
  personad manager join|run|install|uninstall|status
                           another manager made in the app: trade its install code, run it, install it as a service
                           (docs/managers.md; personad manager for the details)
` + nodecmd.NodeUsage + `
` + nodecmd.Usage + `
  personad relay [config]   run the push relay (config ~/.persona/relay.json): pushes for hubs without the app's APNs
                           key, payloads sealed to the phone (docs/push-relay.md)
  personad voice            the built-in voice extension: the hub runs it (hub.json extensions, docs/extensions.md「内置语音扩展」)
  personad extension …      extensions on the hub's machine: list, install, upgrade, rollback, enable, disable, config,
                           secret, uninstall, markets (docs/extensions.md「命令行」; personad extension for the details;
                           also personad ext …, and personad plugin … as before)
` + nodecmd.SysmonUsage + `
  personad release manifest|check-next …
                           the publisher's release manifest and checks (scripts/release/publish.sh, docs/hub-update.md「发布」)
  personad update-watch <pending>
                           the watchdog the hub starts over its own update (docs/hub-update.md「回滚」)
  personad version [-v]     the version; -v adds the channel and build number`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, named(usage))
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if os.Args[1] == sandbox.RunCommand { // an extension's sandbox (docs/extensions.md "Sandbox"); it passes signals on itself
		stop()
		os.Exit(sandbox.Run(os.Args[2:], os.Stderr))
	}
	var err error
	if handled, nerr := nodecmd.Run(ctx, stop, os.Args[1:], nodecmd.Build{Version: version}); handled {
		finish(nerr)
		return
	}
	switch os.Args[1] {
	case "version", "--version", "-v":
		fmt.Println(version)
		if len(os.Args) > 2 && os.Args[2] == "-v" {
			b := buildInfo()
			fmt.Printf("channel %s, build %d\n", b.Channel, b.Number)
		}
	case "release":
		err = runRelease(os.Args[2:])
	case "update-watch": // the old binary watching an update come up (hub/selfupdate.go)
		err = runUpdateWatch(ctx, os.Args[2:])
	case "hub":
		sub := "run"
		if len(os.Args) > 2 {
			sub = os.Args[2]
		}
		switch sub {
		case "run":
			if len(os.Args) > 3 { // a flag such as --listen would otherwise be dropped and a hub started on hub.json's port
				err = usageError("usage: " + commandName() + " hub [run]")
			} else {
				err = hub.Serve(ctx, buildInfo())
			}
		case "slot": // the hub as its edge runs it (hub_edge.go): not for people
			if len(os.Args) > 3 {
				err = usageError(commandName() + " hub slot takes no arguments")
			} else {
				err = hub.RunSlot(ctx, buildInfo())
			}
		case "switch":
			err = runHubSwitch(os.Args[3:])
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
			add := flags.Bool("add", false, "和 --machine 一起用：hub.json 里没有这台时先添加（电脑来连，直接是已确认的）")
			name := flags.String("name", "", "和 --add 一起用：新电脑的名字（可以是中文，默认同 id）")
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
				case (*add || *name != "") && *machine == "":
					err = fmt.Errorf("--add、--name 要和 --machine 一起用")
				case *name != "" && !*add:
					err = fmt.Errorf("--name 要和 --add 一起用")
				case *machine != "":
					err = hub.PairMachine(*machine, *add, *name)
				default:
					err = hub.Pair(*legacy, *android)
				}
			}
		case "setup": // docs/onboarding.md 4.1
			if err = hub.SetupCommand(ctx, os.Args[3:], os.Stdout, os.Stderr); errors.Is(err, hub.ErrSetupStopped) {
				os.Exit(1) // the step that stopped said why
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
			if len(os.Args) > 3 {
				err = usageError(commandName() + " hub tailcat-key takes no arguments")
				break
			}
			// The key is the built-in manager's: it dials the computers (managerconf.OwnFile, docs/managers.md).
			var k *tunnel.HubKey
			if k, err = tunnel.LoadOrCreateHubKey(managerconf.OwnFile(identity.HomeDir(), managerconf.TailcatKey)); err == nil {
				fmt.Println(k.Public())
			}
		default:
			err = fmt.Errorf("unknown hub command %q", sub)
		}
	case "assistant": // the assistant as its own process on the hub's bus (the hub starts it, docs/assistant.md)
		err = assistant.Main(ctx, buildInfo(), os.Stdin)
	case "manager": // a manager made in the app: join, run, install as a service (docs/managers.md)
		err = manager.Main(ctx, buildInfo(), os.Args[2:], os.Stdout)
		var ce manager.ConfigError
		switch {
		case errors.Is(err, manager.ErrUsage):
			fmt.Fprint(os.Stderr, named(manager.Usage))
			os.Exit(2)
		case errors.Is(err, manager.ErrHelp):
			os.Exit(0)
		case errors.As(err, &ce): // restarting cannot fix it: its service does not (RestartPreventExitStatus=2)
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	case "relay":
		if len(os.Args) > 3 { // only the config path: anything after it would be ignored
			err = usageError("usage: " + commandName() + " relay [config]")
			break
		}
		path := filepath.Join(identity.HomeDir(), "relay.json")
		if len(os.Args) > 2 {
			path = os.Args[2]
		}
		err = relay.Run(ctx, path)
	case "voice":
		err = voice.Run(ctx)
	case "extension", "ext", "plugin": // plugin: its name before #1844
		err = extmgr.ExtensionCommand(ctx, os.Args[2:], os.Stdin, os.Stdout, os.Stderr)
	default:
		fmt.Fprintln(os.Stderr, named(usage))
		os.Exit(2)
	}
	finish(err)
}

// finish reports how a command ended: a usage mistake with exit 2, -h with 0, any other error fatally.
func finish(err error) {
	var ue usageError
	if errors.As(err, &ue) { // a usage mistake: what to type instead, exit 2 like the other usage mistakes
		fmt.Fprintln(os.Stderr, string(ue))
		os.Exit(2)
	}
	if errors.Is(err, flag.ErrHelp) { // -h on a subcommand's flags: the flag package printed its help, nothing failed
		os.Exit(0)
	}
	if err != nil {
		log.Fatal(err)
	}
}

// The usage helpers are the device commands' (nodecmd): one name for the program in all it prints.
type usageError = nodecmd.UsageError

func commandName() string      { return nodecmd.CommandName() }
func named(text string) string { return nodecmd.Named(text) }
func isHelpArg(a string) bool  { return nodecmd.IsHelpArg(a) }

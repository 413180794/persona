package wechat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/nwverse/persona/sdk/go/ext"
	"github.com/nwverse/persona/sdk/go/wire"
	"golang.org/x/term"
)

// Capabilities are its connector's: a status (bound to whose WeChat, or not) and a sign-in (a QR code to scan in
// WeChat). Nothing goes to the inbox or the knowledge base: iLink only carries the chat with the bot.
var Capabilities = []string{wire.ConnectorCapabilityStatus, wire.ConnectorCapabilityLogin}

// Item is the connector it contributes, as initialize answers it.
func Item() wire.ConnectorItem {
	return wire.ConnectorItem{ID: ID, Name: Name, Capabilities: Capabilities}
}

// MaxText is how many characters one WeChat message takes (ilink.MaxText).
const MaxText = 3500

// Network is where it goes: iLink and WeChat's media CDN.
var Network = []string{"*.weixin.qq.com"}

// Channel is the channel it contributes (docs/extensions/channels.md): pictures and voice (WeChat's own transcript) come in, plain
// text goes out in pieces WeChat takes, what agents ask is answered in text, "typing…" while a turn goes on.
func Channel() wire.ChannelItem {
	return wire.ChannelItem{ID: ID, Name: Name, Kind: "im", Connector: ID, Bind: "qr",
		Description: "在个人微信里私聊机器人：扫码绑定，消息进主对话，回答以纯文字发回", // i18n-ignore: as the hub registers it
		Capabilities: wire.ChannelCapabilities{ImagesIn: true, VoiceIn: true, Format: "plain", MaxText: MaxText,
			Asks: "text", Push: true, Typing: true}}
}

// Manifest is the plugin's manifest at version, as the hub registers the built-in (command: the program's file name).
func Manifest(version, command string) wire.Manifest {
	return wire.Manifest{ManifestVersion: 1, ID: ID, Name: Name, Version: version,
		Description: "微信机器人：在个人微信里和 Persona 对话（personad 自带）", // i18n-ignore: as the hub registers it
		Engines:     &wire.Engines{ExtensionProtocol: ext.Protocol},
		Runtime:     &wire.Runtime{Command: mustJSON(command)},
		Permissions: &wire.Permissions{Network: Network},
		Contributes: &wire.Contributes{Connectors: []wire.ConnectorItem{Item()}, Channel: []wire.ChannelItem{Channel()}}}
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// Build is what the build wrote into the program (-X main.version, main.channel, main.build, main.updateURL), the
// same as personad's: the hub runs the program only when `version -v` says what its own does.
type Build struct {
	Version, Channel, Number, UpdateURL string
}

// Line is `version -v`'s second line: the channel and the build number, as personad works them out (an unknown
// channel is dev, a release without a valid build number is dev too).
func (b Build) Line() string {
	ch, n := "dev", int64(0)
	if len(b.Number) == 12 {
		if _, err := time.Parse("200601021504", b.Number); err == nil {
			n, _ = strconv.ParseInt(b.Number, 10, 64)
		}
	}
	switch b.Channel {
	case "release":
		if n > 0 {
			ch = "release"
		}
	case "docker":
		ch = "docker"
	}
	return fmt.Sprintf("channel %s, build %d", ch, n)
}

// Program is the plugin's program for the SDK (ext.Serve, ext.Main): the bot starts at initialize, with the user's
// language and the connector's secrets; channel.send, channel.typing and the connector's calls go to it.
func Program(version string) ext.Program {
	p := &program{agent: "Persona/" + version}
	return ext.Program{ID: ID, Version: version, Init: p.init,
		Namespaces: map[string]ext.Handler{ext.ConnectorNamespace: p.connector, ext.ChannelNamespace: p.channel}}
}

type program struct {
	agent string
	// options adjusts the bot's options before it starts (tests: a fake iLink, a clock).
	options func(*Options)

	mu  sync.Mutex
	bot *Bot
	// sent are the refs of the latest channel.send done or still running, and what each answered: the hub sends one
	// again when it did not hear back.
	sent sentRefs
}

func (p *program) init(ctx context.Context, in *ext.Init, hub *ext.Hub) (map[string]any, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.bot == nil {
		opt := Options{Hub: channelHub{hub}, Agent: p.agent, Lang: in.Hub.Locale}
		if p.options != nil {
			p.options(&opt)
		}
		p.bot = New(opt, hubSecrets{hub})
		go p.bot.Run(ctx)
	}
	return map[string]any{"connectors": []wire.ConnectorItem{Item()}}, nil
}

func (p *program) theBot() (*Bot, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.bot == nil {
		return nil, &ext.Error{Code: wire.ErrorCodeNotInitialized, Message: "not initialized"}
	}
	return p.bot, nil
}

// connector answers the connector namespace: status, login.step, logout; tools.list and actions.list are empty (the
// hub asks every connector for them); another of the protocol's methods is no_capability and one it does not have
// unknown_method, as connector/serve answers them.
func (p *program) connector(ctx context.Context, method string, params json.RawMessage) (any, error) {
	if c := ext.Contribution(params); c != "" && c != ID {
		return nil, ext.NoContribution(c)
	}
	b, err := p.theBot()
	if err != nil {
		return nil, err
	}
	switch method {
	case "manifest":
		return wire.ConnectorManifest{Protocol: 2, ID: ID, Name: Name, Capabilities: Capabilities}, nil
	case "status":
		return b.Status(ctx), nil
	case "login.step":
		var lp wire.ConnectorLoginParams
		if err := json.Unmarshal(params, &lp); err != nil {
			return nil, err
		}
		return b.LoginStep(ctx, lp.Step, lp.Values)
	case "logout":
		return ext.Empty{}, b.Logout(ctx)
	case "tools.list", "actions.list":
		return wire.ConnectorTools{}, nil
	}
	if c, ok := wire.Calls[ext.ConnectorNamespace+"."+method]; ok && c.From == "hub" {
		return nil, &ext.Error{Code: wire.ErrorCodeNoCapability, Message: fmt.Sprintf("%s has no %s", Name, method)}
	}
	return nil, ext.UnknownMethod(ext.ConnectorNamespace + "." + method)
}

// channel answers the channel namespace: channel.send (once per ref) and channel.typing.
func (p *program) channel(ctx context.Context, method string, params json.RawMessage) (any, error) {
	if c := ext.Contribution(params); c != "" && c != ID {
		return nil, &ext.Error{Code: wire.ErrorCodeNoContribution, Message: "no channel " + c}
	}
	b, err := p.theBot()
	if err != nil {
		return nil, err
	}
	switch method {
	case "send":
		var sp wire.ChannelSendParams
		if err := json.Unmarshal(params, &sp); err != nil {
			return nil, err
		}
		if sp.Ref == "" {
			return nil, errors.New("channel.send without a ref")
		}
		res, err := p.sent.do(sp.Ref, func() (wire.ChannelSendResult, error) { return b.Send(ctx, sp) })
		if errors.Is(err, errCannotSend) {
			return nil, ext.CannotSend(err.Error())
		}
		return res, err
	case "typing":
		var tp wire.ChannelTypingParams
		if err := json.Unmarshal(params, &tp); err != nil {
			return nil, err
		}
		return ext.Empty{}, b.Typing(ctx, tp.On)
	}
	return nil, ext.UnknownMethod("channel." + method)
}

// channelHub is the hub as the bot hands it the owner's messages.
type channelHub struct{ hub *ext.Hub }

func (c channelHub) Receive(ctx context.Context, m wire.ChannelMessage) error {
	return c.hub.ChannelReceive(ctx, ID, m)
}

// hubSecrets is the connector's sealed blob in the hub.
type hubSecrets struct{ hub *ext.Hub }

func (s hubSecrets) Load(ctx context.Context) ([]byte, error) { return s.hub.LoadSecret(ctx, ID) }
func (s hubSecrets) Save(ctx context.Context, data []byte) error {
	return s.hub.SaveSecret(ctx, ID, data)
}
func (s hubSecrets) Delete(ctx context.Context) error { return s.hub.DeleteSecret(ctx, ID) }

// doneMost is how many sent refs the program remembers.
const doneMost = 256

// sentRefs runs each channel.send once by its ref (a failed one may go again). A send still running holds its ref,
// so the hub's retry of it (it timed out waiting) waits for that send instead of sending the message again.
type sentRefs struct {
	mu    sync.Mutex
	sent  map[string]*sentRef
	order []string // the refs sent successfully, oldest first
}

type sentRef struct {
	done chan struct{}
	res  wire.ChannelSendResult
	err  error
}

func (r *sentRefs) do(ref string, send func() (wire.ChannelSendResult, error)) (wire.ChannelSendResult, error) {
	r.mu.Lock()
	if prev, ok := r.sent[ref]; ok {
		r.mu.Unlock()
		<-prev.done
		return prev.res, prev.err
	}
	if r.sent == nil {
		r.sent = map[string]*sentRef{}
	}
	cur := &sentRef{done: make(chan struct{})}
	r.sent[ref] = cur
	r.mu.Unlock()
	cur.res, cur.err = send()
	r.mu.Lock()
	if cur.err != nil {
		delete(r.sent, ref) // forget a failed send before waking its waiters, so the ref may be sent again
	} else {
		r.order = append(r.order, ref)
		if len(r.order) > doneMost {
			delete(r.sent, r.order[0])
			r.order = r.order[1:]
		}
	}
	r.mu.Unlock()
	close(cur.done)
	return cur.res, cur.err
}

// Main runs the program as the hub starts it: no arguments and a pipe on stdin. What a person types gets an answer
// instead (CommandLine). It returns only to exit.
func Main(b Build) {
	name := filepath.Base(os.Args[0])
	if code, done := CommandLine(name, b, os.Args[1:], stdinIsTerminal(), os.Stdout, os.Stderr); done {
		os.Exit(code)
	}
	log.SetFlags(0) // the hub prefixes each line it keeps
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	err := ext.Serve(ctx, os.Stdin, os.Stdout, Program(b.Version))
	if err != nil && ctx.Err() == nil && !errors.Is(err, io.EOF) { // EOF: the hub went away
		log.Printf("stopped: %v", err)
		os.Exit(1)
	}
}

// CommandLine handles what a person types: the hub starts the program with no arguments and a pipe on stdin. done
// says to exit with code instead of serving. Run by hand in a terminal with no arguments it would wait on stdin
// without a word, so it says what it is instead.
func CommandLine(name string, b Build, args []string, terminal bool, stdout, stderr io.Writer) (code int, done bool) {
	usage := fmt.Sprintf("usage: %s [version [-v]]\n"+
		"The WeChat plugin's program (docs/extensions/wechat.md): the hub starts it and talks to it over stdin / stdout, so there is\n"+
		"nothing to run by hand. To check a build, run: %s version -v", name, name)
	if len(args) == 0 {
		if terminal {
			fmt.Fprintln(stderr, usage)
			return 2, true
		}
		return 0, false
	}
	switch args[0] {
	case "version", "--version", "-v":
		fmt.Fprintln(stdout, b.Version)
		if len(args) > 1 && args[1] == "-v" {
			fmt.Fprintln(stdout, b.Line())
		}
		return 0, true
	case "help", "-h", "-help", "--help":
		fmt.Fprintln(stdout, usage)
		return 0, true
	default:
		fmt.Fprintf(stderr, "unknown argument %q\n%s\n", args[0], usage)
		return 2, true
	}
}

// stdinIsTerminal says whether stdin is a terminal rather than the hub's pipe.
func stdinIsTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

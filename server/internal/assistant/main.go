package assistant

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/nwverse/persona/sdk/go/wire"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/bus"
	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/devices"
	"github.com/nwverse/persona/server/internal/devicewire"
	"github.com/nwverse/persona/server/internal/errreport"
	"github.com/nwverse/persona/server/internal/extmgr"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/identity"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/update"
)

// Main is `personad assistant`: the assistant as its own process, on the hub's bus. The hub starts one beside itself
// (hub/assistant_process.go) and hands it its token on stdin; one run elsewhere (hub.json assistant.external) gets it
// from PERSONA_ASSISTANT_TOKEN (`personad hub assistant-token` makes one). It keeps its state in the hub's Postgres,
// found as the hub finds it: hub.json's database_url, PERSONA_DATABASE_URL over it.
func Main(ctx context.Context, build update.Build, stdin io.Reader) error {
	errs := newErrorTee(build) // its log's error lines become error reports (docs/extensions.md "Error reports")
	logger := log.New(errs, "assistant: ", log.LstdFlags)
	cfg, err := readHubConfig()
	if err != nil {
		return err
	}
	if cfg.DatabaseURL == "" {
		return errors.New("no database_url in hub.json (or PERSONA_DATABASE_URL)")
	}
	token := strings.TrimSpace(identity.Getenv("JARVIS_ASSISTANT_TOKEN"))
	if token == "" && stdin != nil {
		r := bufio.NewReader(stdin)
		line, _ := r.ReadString('\n')
		token = strings.TrimSpace(line)
		// Handed the token on stdin (by the hub that started it): it stops when stdin closes, the hub gone.
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(ctx)
		defer cancel()
		go func() {
			_, _ = io.Copy(io.Discard, r)
			logger.Printf("stdin closed (the hub that started it is gone): stopping")
			cancel()
		}()
	}
	if token == "" {
		return errors.New("no bus token: set PERSONA_ASSISTANT_TOKEN (personad hub assistant-token prints one)")
	}
	hub := strings.TrimSpace(identity.Getenv("JARVIS_ASSISTANT_HUB"))
	if hub == "" {
		hub = localHubURL(cfg.Listen)
	}
	deploy := identity.Getenv("JARVIS_ASSISTANT_DEPLOY")
	if deploy == "" {
		deploy = "external"
	}
	return run(ctx, build, runOptions{ID: assistantwire.AssistantID, Hub: hub, Token: token, Deploy: deploy, Errors: errs,
		Dir: identity.HomeDir(), DatabaseURL: cfg.DatabaseURL, Logf: logger.Printf})
}

// runOptions are who a manager is and where its things are: the assistant the hub starts (Main), or a manager made
// in the app with a manager.json of its own (RunManager, manager.go).
type runOptions struct {
	ID, Hub, Token, Deploy string
	// Fingerprint pins the hub's TLS key (its public port's self-signed certificate); "" trusts the system's CAs.
	Fingerprint string
	// Dir is its data directory: the knowledge base (data/), secret.key, the extensions, hub.json's usage section.
	Dir         string
	DatabaseURL string
	// StopWhenRevoked: once the hub refuses the token, stop (bus.ErrRevoked) rather than try it every minute.
	StopWhenRevoked bool
	Logf            func(format string, args ...any)
	// Errors is the process's log writer that reports its error lines (newErrorTee); nil: run makes one.
	Errors *errorTee
}

// run is a manager's process: its database, the computers, the extensions and the assistant itself on the hub's
// bus, until ctx ends (or the token is refused, with StopWhenRevoked: bus.ErrRevoked).
func run(ctx context.Context, build update.Build, o runOptions) error {
	if o.Errors == nil {
		o.Errors = newErrorTee(build)
	}
	stderr := o.Errors
	version := build.Version
	hub, token, deploy, home := o.Hub, o.Token, o.Deploy, o.Dir
	logf := o.Logf
	var httpClient *http.Client
	if o.Fingerprint != "" {
		httpClient = pinnedHTTPClient(o.Fingerprint)
	}
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	database, err := db.Open(ctx, o.DatabaseURL, db.ManagerSet)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer database.Close()
	// The user's computers are the manager's: it connects them itself (internal/devices, docs/machines.md "Who connects the computers").
	var bh *BusHub
	var svc *Service
	var ext *extmgr.Manager
	// Dir: hub.json's usage section (the account-pool gateways and their keys) is the devices', read and written there.
	devs := devices.New(devices.Options{Store: database, Dir: home, Logf: log.New(stderr, "devices: ", log.LstdFlags).Printf,
		Answer: func(ctx context.Context, machine, method string, params json.RawMessage) (any, bool, error) {
			if err := svc.machineTrust(ctx, machine); err != nil { // not confirmed yet: nothing of the assistant's (machine_trust.go)
				return nil, true, err
			}
			if out, ok, err := svc.NodeAnswer(ctx, machine, method, params); ok {
				return out, ok, err
			}
			return svc.NodeRequest(ctx, machine, method, params) // the interactions, the MCP tools (fleet.go)
		},
		Event:    func(machine, event string, params json.RawMessage) bool { return svc.NodeEvent(machine, event, params) },
		Detached: func(machine string) { svc.NodeDetached(machine) },
		Location: func() *time.Location { return svc.location(context.Background()) },
		// The extensions' part of a node's connection and its events (internal/extmgr).
		Own:         func(ctx context.Context, machine string) map[string]json.RawMessage { return ext.Own(ctx, machine) },
		OnConnected: func(machine string) { ext.NodeConnected(machine) },
		OnEvent:     func(machine, event string, params json.RawMessage) bool { return ext.NodeEvent(machine, event, params) },
		// A node's new connection gets the launchers added in Persona, with their keys (models_launchers.go).
		// Not one waiting to be confirmed: it gets them when it is (trustChanged, machine_trust.go).
		NodeCalls: func(ctx context.Context, machine string) []devicewire.NodeCall {
			if svc.machinePending(ctx, machine) {
				return nil
			}
			b, err := json.Marshal(map[string]any{"launchers": svc.managedForNodes(ctx)})
			if err != nil {
				return nil
			}
			return []devicewire.NodeCall{{MachineID: machine, Method: DeviceLaunchersManaged, Params: b}}
		},
		DialRelay: func(ctx context.Context, query string) (*websocket.Conn, error) {
			return bus.DialAt(ctx, hub, token, httpClient, devicewire.RelayPath, query)
		},
		Connected: func() bool { return bh.Client().Peer() != nil },
		Hub:       lazyHost{func() *bus.Client { return bh.Client() }}})
	// The extensions, connectors and bots (internal/extmgr): the hub.json and secret.key of the data directory here.
	ext = extmgr.New(extmgr.Options{Dir: home, Store: database, Build: build,
		Logf: log.New(stderr, "extensions: ", log.LstdFlags).Printf,
		Hub:  lazyHost{func() *bus.Client { return bh.Client() }}, Devices: devs,
		Assistant: func(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
			return bh.Answer(ctx, method, params)
		},
		AssistantEvent:   func(event string, params json.RawMessage) { bh.TellLocal(event, params) },
		AssistantTools:   func() []proto.Tool { return svc.toolsNow() },
		AssistantMachine: func() string { return svc.mainMachine(context.Background()) },
		Ask:              func(ctx context.Context, ix proto.Interaction) (proto.Resolution, error) { return svc.AskCard(ctx, ix) },
		DialRelay: func(ctx context.Context, query string) (*websocket.Conn, error) {
			return bus.DialAt(ctx, hub, token, httpClient, devicewire.RelayPath, query)
		},
		Connected: func() bool { return bh.Client().Peer() != nil },
		Lang:      func() i18n.Lang { return svc.lang(context.Background()) },
		Timezone:  func() string { return svc.userTZ() }})
	o.Errors.ext.Store(ext)
	bh = NewBusHub(BusOptions{ID: o.ID, Hub: hub, Token: token, HTTPClient: httpClient, Version: version, Deploy: deploy,
		Logf: logf, Devices: devs, Extensions: ext, StopWhenRevoked: o.StopWhenRevoked})
	// The knowledge base in the data directory here (the hub's own beside it), kept as the hub's hub.json says (hello).
	// The model providers' keys are sealed with secret.key beside hub.json (the hub's; one elsewhere needs a copy).
	svc = New(Options{Hub: bh, Store: func() Store { return database }, Logf: logf, Devices: devs,
		KB: &KBOptions{Dir: filepath.Join(home, "data"), KeyDir: home}, SecretKey: filepath.Join(home, "secret.key"),
		Trust: devs})
	svc.SetExtensions(ext) // the connectors', Cua's and ego's tools, in this process (fleet_tools.go)
	logf("connecting to %s as %s", hub, o.ID)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = devs.Run(ctx) // after ctx: hands the machines over to the next manager, then lets them go
	}()
	extDone := make(chan struct{})
	go func() {
		defer close(extDone)
		_ = ext.Run(ctx) // after ctx: stops the extensions' programs (they are the manager's children)
	}()
	err = bh.Run(ctx, svc)
	stop() // a refused token ends Run before ctx: the computers and the extensions stop with it
	<-done
	<-extDone
	if err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

// lazyHost is the bus client, made after the devices that use it.
type lazyHost struct{ c func() *bus.Client }

func (l lazyHost) Call(ctx context.Context, method string, params, out any) error {
	return l.c().Call(ctx, method, params, out)
}
func (l lazyHost) Notify(event string, params any) error { return l.c().Notify(event, params) }

// hubConfig is what the assistant reads of the hub's hub.json.
type hubConfig struct {
	DatabaseURL string `json:"database_url"`
	Listen      string `json:"listen"`
}

// readHubConfig reads hub.json in the data directory (~/.persona, or ~/.jarvis) with the environment over it, as the
// hub does (its envOverrides); a missing file is no error.
func readHubConfig() (hubConfig, error) {
	cfg := hubConfig{Listen: ":18660"}
	b, err := os.ReadFile(filepath.Join(identity.HomeDir(), "hub.json"))
	switch {
	case err == nil:
		if err := json.Unmarshal(b, &cfg); err != nil {
			return cfg, fmt.Errorf("hub.json: %w", err)
		}
	case !os.IsNotExist(err):
		return cfg, err
	}
	if v := identity.Getenv("JARVIS_DATABASE_URL"); v != "" {
		cfg.DatabaseURL = v
	}
	if v := identity.Getenv("JARVIS_LISTEN"); v != "" {
		cfg.Listen = v
	}
	return cfg, nil
}

// localHubURL is the hub on this machine, from its listen address.
func localHubURL(listen string) string {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return "http://127.0.0.1:18660"
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}

// errorTee is the manager process's log writer: lines go to stderr, and its error lines become the manager's error
// reports while an extension takes them (ext is set once the manager exists).
type errorTee struct {
	*errreport.Tee
	ext *atomic.Pointer[extmgr.Manager]
}

func newErrorTee(build update.Build) *errorTee {
	ref := &atomic.Pointer[extmgr.Manager]{}
	return &errorTee{ext: ref, Tee: &errreport.Tee{W: os.Stderr, Build: managerErrorBuild(build),
		On:   func() bool { return ref.Load().ErrorReportsAsked() },
		Sink: func(r wire.ErrorReport) { ref.Load().ReportOwn(r) }}}
}

// managerErrorBuild is what the manager says of itself in its error reports.
func managerErrorBuild(b update.Build) errreport.Build {
	out := errreport.Build{Source: wire.ErrorSourceManager, Release: b.Version, Commit: b.Commit}
	if b.Number > 0 {
		out.Build = strconv.FormatInt(b.Number, 10)
	}
	return out
}

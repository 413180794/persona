// jarvisd-wechat is the WeChat plugin as a program of its own (docs/wechat.md): the hub starts it as a built-in plugin
// and talks to it over stdin / stdout; it reaches the hub's API with the address and token the hub hands it (hub_api).
// It is built, released and updated together with jarvisd, and the hub runs it only when its build is the hub's own
// (`version -v`).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/connector/serve"
	"github.com/nwverse/persona/server/internal/identity"
	"github.com/nwverse/persona/server/internal/update"
	"github.com/nwverse/persona/server/internal/wechat"
)

// What the build wrote in (-X), as for jarvisd: they must say the same.
var (
	version   = "dev"
	channel   = ""
	build     = ""
	updateURL = ""
	commit    = ""
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version", "--version", "-v":
			fmt.Println(version)
			if len(os.Args) > 2 && os.Args[2] == "-v" {
				b := update.Make(version, channel, build, updateURL)
				fmt.Printf("channel %s, build %d\n", b.Channel, b.Number)
			}
			return
		default:
			fmt.Fprintln(os.Stderr, "usage: jarvisd-wechat [version [-v]]: started by the hub, which talks to it over stdin / stdout")
			os.Exit(2)
		}
	}
	_ = commit
	log.SetFlags(0) // the hub prefixes each line it keeps
	hubURL, token := identity.Getenv("PERSONA_HUB_URL"), identity.Getenv("PERSONA_HUB_TOKEN")
	if hubURL == "" || token == "" {
		log.Printf("no hub address or token (the plugin needs hub_api)")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	hub := wechat.NewHub(hubURL, token)
	err := serve.Main(ctx, os.Stdin, os.Stdout, func(_ json.RawMessage, secrets connector.Secrets) (connector.Connector, error) {
		return wechat.New(wechat.Options{Hub: hub, Agent: "Persona/" + version}, secrets), nil
	})
	if err != nil && ctx.Err() == nil && !errors.Is(err, io.EOF) { // EOF: the hub went away
		log.Printf("stopped: %v", err)
		os.Exit(1)
	}
}

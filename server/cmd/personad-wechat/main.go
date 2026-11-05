// jarvisd-wechat is the WeChat plugin as a program of its own (docs/wechat.md): the hub starts it as a built-in plugin
// and talks to it over stdin / stdout, as its connector and its channel (docs/channels.md).
// It is built, released and updated together with jarvisd, and the hub runs it only when its build is the hub's own
// (`version -v`).
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/connector/serve"
	"github.com/nwverse/persona/server/internal/i18n"
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	err := serve.MainWith(ctx, os.Stdin, os.Stdout, func(init serve.Init, secrets connector.Secrets) (connector.Connector, error) {
		return wechat.New(wechat.Options{Hub: init.Channel, Agent: "Persona/" + version, Lang: i18n.Lang(init.Locale)}, secrets), nil
	})
	if err != nil && ctx.Err() == nil && !errors.Is(err, io.EOF) { // EOF: the hub went away
		log.Printf("stopped: %v", err)
		os.Exit(1)
	}
}

// jarvisd-telegram is the Telegram connector as a program of its own (docs/plan-telegram.md): the hub starts it
// (connector/proc) and talks to it over stdin / stdout. It lives apart from jarvisd because gotd would double
// jarvisd's size; it is built, released and updated together with jarvisd, and the hub runs it only when its build
// is the hub's own (`version -v`).
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
	"time"

	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/connector/serve"
	"github.com/nwverse/persona/server/internal/connector/telegram"
	"github.com/nwverse/persona/server/internal/update"
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
			fmt.Fprintln(os.Stderr, "usage: jarvisd-telegram [version [-v]]: started by the hub, which talks to it over stdin / stdout")
			os.Exit(2)
		}
	}
	_ = commit
	log.SetFlags(0) // the hub prefixes each line it keeps
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	err := serve.Main(ctx, os.Stdin, os.Stdout, func(raw json.RawMessage, secrets connector.Secrets) (connector.Connector, error) {
		var cfg struct {
			Zone string `json:"zone"` // the user's time zone ("Asia/Shanghai"), from the hub; "" while it does not know it
		}
		_ = json.Unmarshal(raw, &cfg)
		return telegram.New(secrets, userZone(cfg.Zone)), nil
	})
	if err != nil && ctx.Err() == nil && !errors.Is(err, io.EOF) { // EOF: the hub went away
		log.Printf("stopped: %v", err)
		os.Exit(1)
	}
}

// userZone is the user's time zone by name, nil when there is none or it does not load (the tools then say UTC): never
// the machine's, which is the hub's (UTC), not the user's.
func userZone(name string) *time.Location {
	if name == "" {
		return nil
	}
	zone, err := time.LoadLocation(name)
	if err != nil {
		log.Printf("time zone %q: %v", name, err)
		return nil
	}
	return zone
}

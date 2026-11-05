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
	"path/filepath"
	"syscall"

	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/connector/serve"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/update"
	"github.com/nwverse/persona/server/internal/wechat"
	"golang.org/x/term"
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
	name := filepath.Base(os.Args[0])
	if code, done := commandLine(name, os.Args[1:], stdinIsTerminal(), os.Stdout, os.Stderr); done {
		os.Exit(code)
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

// commandLine handles what a person types: the hub starts the program with no arguments and a pipe on stdin. done
// says to exit with code instead of serving. Run by hand in a terminal with no arguments it would wait on stdin
// without a word, so it says what it is instead.
func commandLine(name string, args []string, terminal bool, stdout, stderr io.Writer) (code int, done bool) {
	usage := fmt.Sprintf("usage: %s [version [-v]]\n"+
		"The WeChat plugin's program (docs/wechat.md): the hub starts it and talks to it over stdin / stdout, so there is\n"+
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
		fmt.Fprintln(stdout, version)
		if len(args) > 1 && args[1] == "-v" {
			b := update.Make(version, channel, build, updateURL)
			fmt.Fprintf(stdout, "channel %s, build %d\n", b.Channel, b.Number)
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

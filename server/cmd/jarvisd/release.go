package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/chyroc/jarvis/server/internal/proto"
	"github.com/chyroc/jarvis/server/internal/update"
)

// runRelease is `jarvisd release …`, what scripts/publish.sh runs to sign a release (docs/hub-update.md「发布」). The
// private key stays on the publisher's machine; nothing here prints it.
func runRelease(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: jarvisd release keygen <key file> | manifest … | sign <key file> <latest.json> | verify <latest.json> | check-next <served latest.json> --build N --commit C")
	}
	switch args[0] {
	case "keygen":
		if len(args) != 2 {
			return errors.New("usage: jarvisd release keygen <key file>  (it is never overwritten)")
		}
		pub, err := update.Keygen(args[1])
		if err != nil {
			return err
		}
		fmt.Printf("wrote the private key to %s (0600): back it up somewhere only you can read, never commit it.\n", args[1])
		fmt.Printf("key id %s; add this line to trustedKeys in server/internal/update/keys.go:\n", update.KeyID(pub))
		fmt.Printf("\t%q: %q,\n", update.KeyID(pub), base64.StdEncoding.EncodeToString(pub))
		return nil
	case "manifest":
		fs := flag.NewFlagSet("release manifest", flag.ContinueOnError)
		dir := fs.String("dir", "", "where the jarvisd-<os>-<arch>.gz are")
		ver := fs.String("version", "", "the version")
		num := fs.String("build", "", "the build number (UTC YYYYMMDDHHMM)")
		commit := fs.String("commit", "", "the commit")
		notes := fs.String("notes", "", "what is new, for the app")
		minApp := fs.Int("min-app", 0, "the oldest app build that works with it")
		minNode := fs.String("min-node", "", "the oldest node build that works with it")
		out := fs.String("out", "", "where to write latest.json")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		n, err := update.ParseNumber(*num)
		if err != nil {
			return err
		}
		var mn int64
		if *minNode != "" {
			if mn, err = update.ParseNumber(*minNode); err != nil {
				return err
			}
		}
		m, err := update.BuildManifest(*dir, update.Manifest{Version: *ver, Build: n, Commit: *commit, Released: time.Now().UTC().Truncate(time.Second),
			MinAppBuild: *minApp, MinNodeBuild: mn, Protocol: proto.Protocol, Notes: *notes})
		if err != nil {
			return err
		}
		b, _ := json.MarshalIndent(m, "", "  ")
		return os.WriteFile(*out, append(b, '\n'), 0o644)
	case "sign":
		if len(args) != 3 {
			return errors.New("usage: jarvisd release sign <key file> <latest.json>")
		}
		priv, err := update.LoadPrivate(args[1])
		if err != nil {
			return err
		}
		data, err := os.ReadFile(args[2])
		if err != nil {
			return err
		}
		b, _ := json.Marshal(update.Sign(priv, data))
		return os.WriteFile(args[2]+".sig", append(b, '\n'), 0o644)
	case "verify":
		if len(args) != 2 {
			return errors.New("usage: jarvisd release verify <latest.json>  (with the keys this jarvisd trusts)")
		}
		data, err := os.ReadFile(args[1])
		if err != nil {
			return err
		}
		sig, _ := os.ReadFile(args[1] + ".sig")
		if err := update.Verify(data, sig, update.TrustedKeys()); err != nil {
			if errors.Is(err, update.ErrUnknownKey) || errors.Is(err, update.ErrNoTrustedKey) {
				return fmt.Errorf("%w: is the key's line in server/internal/update/keys.go, and this jarvisd built after it went in?", err)
			}
			return err
		}
		fmt.Println("signature ok")
		return nil
	case "check-next":
		fs := flag.NewFlagSet("release check-next", flag.ContinueOnError)
		num := fs.String("build", "", "this release's build number")
		commit := fs.String("commit", "", "this release's commit")
		if len(args) < 2 {
			return errors.New("usage: jarvisd release check-next <served latest.json> --build N --commit C")
		}
		served := args[1]
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		n, err := update.ParseNumber(*num)
		if err != nil {
			return err
		}
		var m *update.Manifest
		if b, err := os.ReadFile(served); err == nil && len(strings.TrimSpace(string(b))) > 0 {
			if m, err = update.ParseManifest(b); err != nil {
				return err
			}
		}
		return update.CheckNext(m, n, *commit, func(older, newer string) (bool, error) {
			err := exec.Command("git", "merge-base", "--is-ancestor", older, newer).Run()
			var exit *exec.ExitError
			if errors.As(err, &exit) && exit.ExitCode() == 1 {
				return false, nil
			}
			return err == nil, err
		})
	}
	return fmt.Errorf("unknown release command %q", args[0])
}

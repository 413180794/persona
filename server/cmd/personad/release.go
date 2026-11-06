package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/update"
)

// runRelease is `jarvisd release …`, what scripts/release/publish.sh runs to describe a release (docs/hub-update.md "Publishing").
func runRelease(args []string) error {
	if len(args) == 0 {
		return usageError(releaseUsage())
	}
	switch args[0] {
	case "manifest":
		fs := flag.NewFlagSet("release manifest", flag.ContinueOnError)
		dir := fs.String("dir", "", "where the jarvisd-<os>-<arch>.gz are")
		ver := fs.String("version", "", "the version")
		num := fs.String("build", "", "the build number (UTC YYYYMMDDHHMM)")
		commit := fs.String("commit", "", "the commit (full id; run in its repository: its ancestors go in too)")
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
		// Every commit the release contains, so a hub deployed from newer code than this does not take it by itself.
		revs, err := exec.Command("git", "rev-list", *commit).Output()
		if err != nil {
			return fmt.Errorf("git rev-list %s: %w", *commit, err)
		}
		ancestors, err := update.EncodeAncestors(strings.Split(string(revs), "\n"))
		if err != nil {
			return err
		}
		m, err := update.BuildManifest(*dir, update.Manifest{Version: *ver, Build: n, Commit: *commit, Released: time.Now().UTC().Truncate(time.Second),
			MinAppBuild: *minApp, MinNodeBuild: mn, Protocol: proto.Protocol, Notes: *notes, Ancestors: ancestors})
		if err != nil {
			return err
		}
		b, _ := json.MarshalIndent(m, "", "  ")
		return os.WriteFile(*out, append(b, '\n'), 0o644)
	case "check-next":
		fs := flag.NewFlagSet("release check-next", flag.ContinueOnError)
		num := fs.String("build", "", "this release's build number")
		commit := fs.String("commit", "", "this release's commit")
		if len(args) >= 2 && (args[1] == "-h" || args[1] == "-help" || args[1] == "--help") {
			return fs.Parse(args[1:]) // prints the help; main exits 0 on flag.ErrHelp
		}
		if len(args) < 2 {
			return usageError("usage: " + commandName() + " release check-next <served latest.json> --build N --commit C")
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
	return usageError(fmt.Sprintf("unknown release command %q\n%s", args[0], releaseUsage()))
}

// releaseUsage is what `release` without a known subcommand prints.
func releaseUsage() string {
	return "usage: " + commandName() + " release manifest --dir D --version V --build N --commit C --out latest.json […]\n" +
		"       " + commandName() + " release check-next <served latest.json> --build N --commit C\n" +
		"(" + commandName() + " release manifest -h for its flags)"
}

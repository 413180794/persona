// Package testhome keeps a test binary off the developer's real ~/.persona.
//
// Code that finds its state through the home directory (the assistant process a hub test starts, the hub's own
// setup) writes there when a test does not point PERSONA_HOME somewhere else. Run from a developer machine that has a
// hub on it, such a test rewrote the real hub's manager.json and secret.key, and the next run's assistant child read
// the poisoned file and never came up. scripts/test/test-server.sh already runs with a throw-away HOME; a plain
// `go test` did not.
//
// A package whose tests start Persona processes calls Main from its TestMain: the tests run with HOME set to an empty
// directory and PERSONA_HOME unset, and the run fails if anything created <HOME>/.persona. A test that needs state
// sets PERSONA_HOME (t.Setenv) to a directory of its own.
package testhome

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// isolated names the throw-away home once Main made it: a test binary a test starts again inherits the variable and
// the HOME and PERSONA_HOME its parent's test set, and must not replace them.
const isolated = "PERSONA_TEST_HOME_ISOLATED"

// Active says whether this test binary runs through Main (or is started by one that does), for a test of a package's
// TestMain that needs no database.
func Active() bool { return os.Getenv(isolated) != "" }

// Main runs run (os.Exit(testhome.Main(m.Run)), or around dbtest.Main) with a throw-away HOME and returns its exit
// code, or 1 when the run left <HOME>/.persona behind.
func Main(run func() int) int { return runIn(run, os.Stderr) }

func runIn(run func() int, stderr io.Writer) int {
	if os.Getenv(isolated) != "" {
		return run()
	}
	dir := "" // a short path: the tests make Unix sockets under it
	if _, err := os.Stat("/tmp"); err == nil && runtime.GOOS != "windows" {
		dir = "/tmp"
	}
	home, err := os.MkdirTemp(dir, "persona-test-home-")
	if err != nil {
		fmt.Fprintf(stderr, "testhome: %v\n", err)
		return 1
	}
	if resolved, err := filepath.EvalSymlinks(home); err == nil {
		home = resolved
	}
	defer os.RemoveAll(home)
	restore := set(map[string]string{"HOME": home, "USERPROFILE": home, "PERSONA_HOME": "", isolated: home})
	defer restore()
	code := run()
	if left := leftBehind(home); len(left) > 0 {
		fmt.Fprintf(stderr, "testhome: the tests wrote to the default state directory ~/.persona (%d files, e.g. %s).\n"+
			"A test that needs state sets PERSONA_HOME to a directory of its own (t.Setenv).\n",
			len(left), strings.Join(left[:min(len(left), 5)], ", "))
		if code == 0 {
			code = 1
		}
	}
	return code
}

// set changes the variables (an empty value unsets) and returns how to put them back.
func set(vars map[string]string) func() {
	type old struct {
		value string
		had   bool
	}
	saved := map[string]old{}
	for k, v := range vars {
		prev, had := os.LookupEnv(k)
		saved[k] = old{prev, had}
		if v == "" {
			_ = os.Unsetenv(k)
		} else {
			_ = os.Setenv(k, v)
		}
	}
	return func() {
		for k, o := range saved {
			if o.had {
				_ = os.Setenv(k, o.value)
			} else {
				_ = os.Unsetenv(k)
			}
		}
	}
}

// leftBehind lists the files under <home>/.persona, relative to home.
func leftBehind(home string) []string {
	var files []string
	root := filepath.Join(home, ".persona")
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(home, path)
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	if _, err := os.Lstat(root); err == nil && len(files) == 0 {
		files = append(files, ".persona") // an empty directory is still a write
	}
	return files
}

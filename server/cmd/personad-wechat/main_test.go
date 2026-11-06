package main

import (
	"testing"

	"github.com/nwverse/persona/server/plugins/wechat"
)

// The -X variables are the ones personad's build writes (scripts, Dockerfile): the program reads them as its Build.
func TestBuildVariables(t *testing.T) {
	b := wechat.Build{Version: version, Channel: channel, Number: build, UpdateURL: updateURL}
	if b.Version != "dev" || b.Line() != "channel dev, build 0" {
		t.Fatalf("%+v: %s", b, b.Line())
	}
}

// personad-wechat (also built as personad-wechat) is the WeChat plugin's program (docs/extensions/wechat.md,
// server/plugins/wechat): the hub starts it as a built-in plugin and talks to it over stdin / stdout, as its connector
// and its channel (docs/extensions/channels.md). It is built, released and updated together with personad, and the hub runs it
// only when its build is the hub's own (`version -v`).
package main

import "github.com/nwverse/persona/server/plugins/wechat"

// What the build wrote in (-X), as for personad: they must say the same.
var (
	version   = "dev"
	channel   = ""
	build     = ""
	updateURL = ""
	commit    = ""
)

func main() {
	_ = commit
	wechat.Main(wechat.Build{Version: version, Channel: channel, Number: build, UpdateURL: updateURL})
}

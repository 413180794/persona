// {{name}}: a Persona extension (docs/extensions.md「写扩展：SDK、模板和命令行」). A page on the main navigation with a
// list you can search and open, and a card on the overview. Replace the sample items with your service's.
package main

import (
	_ "embed"
	"encoding/json"

	"github.com/nwverse/persona/sdk/go/ext"
)

//go:embed persona-extension.json
var manifest []byte

func main() { ext.Main(program()) }

func program() ext.Program {
	var m struct{ ID, Name, Version string }
	_ = json.Unmarshal(manifest, &m)
	a := &app{name: m.Name, greeting: "你好"}
	return ext.Program{
		ID: m.ID, Version: m.Version, Manifest: manifest,
		Init: a.init,
		UI:   map[string]ext.Page{"home": {Render: a.home, Action: a.homeAction}, "today": {Render: a.today}},
	}
}

package extmgr

import (
	"testing"

	"github.com/nwverse/persona/server/internal/assistantwire"
)

// The `board` switch (#6052) is off by default and for settings saved before it; an app that does not know it leaves it
// as it was; the assistant's feishu.status says it and feishu.configure sets it.
func TestBotBoardSwitch(t *testing.T) {
	w := newBotWorld(t)
	w.pair(t)
	feats := func() map[string]any {
		f, _ := w.feishu(t, assistantwire.FeishuStatusGet, nil)["features"].(map[string]any)
		return f
	}
	if f := feats(); f["board"] != false {
		t.Fatalf("default features %v: the board must be off", f)
	}
	if code, _ := w.do(t, "PUT", "/api/v1/bots/feishu", `{"features":{"main":true,"cards":true,"board":true}}`); code != 200 {
		t.Fatal(code)
	}
	if f := feats(); f["board"] != true || f["reports"] != true {
		t.Fatalf("board on: %v", f)
	}
	// An app from before the switch sends the others only, and leaves it as it was.
	if code, _ := w.do(t, "PUT", "/api/v1/bots/feishu", `{"features":{"main":true,"cards":true,"agents":true}}`); code != 200 {
		t.Fatal(code)
	}
	if f := feats(); f["board"] != true || f["agents"] != true {
		t.Fatalf("an older app's PUT: %v", f)
	}
	w.feishu(t, assistantwire.FeishuConfigure, map[string]any{"features": map[string]bool{"board": false}})
	if f := feats(); f["board"] != false || f["agents"] != true {
		t.Fatalf("board off by the assistant: %v", f)
	}
}

// The Lark bot's channel page draws its switches from the list the manager gives: the board is one of them, off.
func TestChannelListHasTheBoardSwitch(t *testing.T) {
	w := newChannelWorld(t)
	out, err := w.channels(t, assistantwire.ChannelsList, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range out["channels"].([]any) {
		m := c.(map[string]any)
		if m["id"] != "feishu" {
			continue
		}
		for _, f := range m["features"].([]any) {
			if fm := f.(map[string]any); fm["id"] == "board" {
				if fm["on"] == true || fm["title"] == "" || fm["description"] == "" {
					t.Fatalf("the board switch %v", fm)
				}
				return
			}
		}
		t.Fatalf("no board switch in %v", m["features"])
	}
	t.Fatal("no Lark bot channel")
}

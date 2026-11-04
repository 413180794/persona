package hub

import (
	"reflect"
	"testing"

	"github.com/nwverse/persona/server/internal/lark"
)

func TestLarkEventBatchKeepsSettingsAlongsideMessages(t *testing.T) {
	settings := lark.Event{Type: "local.settings.updated"}
	messages := lark.Event{Type: "message.updated", FeedIDs: []string{"b", "a", "b"}}
	for _, events := range [][]lark.Event{{settings, messages}, {messages, settings}, {settings, messages, settings}} {
		var batch larkEventBatch
		for _, ev := range events {
			batch.add(ev)
		}
		got := batch.drain()
		if got["settings_changed"] != true || !reflect.DeepEqual(got["feed_ids"], []string{"a", "b"}) {
			t.Fatalf("lost invalidation: %#v", got)
		}
		batch.add(lark.Event{Type: "message.updated", FeedIDs: []string{"c"}})
		got = batch.drain()
		if got["settings_changed"] != false || !reflect.DeepEqual(got["feed_ids"], []string{"c"}) {
			t.Fatalf("leaked earlier batch: %#v", got)
		}
		batch.add(settings)
		got = batch.drain()
		if got["settings_changed"] != true || !reflect.DeepEqual(got["feed_ids"], []string{}) {
			t.Fatalf("lost settings-only change: %#v", got)
		}
	}
}

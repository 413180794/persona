package uistream

import (
	"encoding/json"
	"errors"

	"github.com/nwverse/persona/sdk/go/wire"
)

// EventAction validates the exact displayed session/epoch/version and the saved
// callback's node/event slot, returning a detached native Action for the caller's
// confirmation/write gates. It does not authorize, deduplicate, consume an event
// or execute anything. The caller must resolve accepted retries before this stale
// check. Managers consume an empty-delta revision after every entered/ambiguous
// callback, so bounded receipt eviction cannot repeat no-UI-change writes.
func (t *Tree) EventAction(event wire.UIStreamEvent) (*wire.Action, error) {
	if t == nil || t.root == nil {
		return nil, errors.New("uninitialized tree")
	}
	raw, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}
	if _, err := DecodeEvent(raw); err != nil {
		return nil, err
	}
	if event.Session != t.Session() || event.Epoch != t.Epoch() || event.Version != t.Version() {
		return nil, errors.New("stale event or identity mismatch")
	}
	n, _, _ := t.root.find(event.Key)
	if n == nil {
		return nil, errors.New("event node missing")
	}
	var view wire.View
	switch n.kind {
	case "view":
		// Root callbacks only; descendants have their own keys.
		_ = decodeDTO(n.json(), &view)
		view.Sections = nil
	case "section":
		var section wire.Section
		_ = decodeDTO(n.json(), &section)
		section.Items = nil
		view.Sections = []wire.Section{section}
	case "item":
		var item wire.Component
		_ = decodeDTO(n.json(), &item)
		item.Tiles = nil
		view.Sections = []wire.Section{{Items: []wire.Component{item}}}
	case "tile":
		var tile wire.Tile
		_ = decodeDTO(n.json(), &tile)
		view.Sections = []wire.Section{{Items: []wire.Component{{Type: "grid", Tiles: []wire.Tile{tile}}}}}
	}
	for _, action := range wire.Actions(&view) {
		if action.ID != event.Action {
			continue
		}
		slot := "press"
		if n.kind == "view" {
			if view.Search != nil && view.Search.Action.ID == action.ID {
				slot = "change"
			}
			if view.Filter != nil && view.Filter.Action.ID == action.ID || view.Tabs != nil && view.Tabs.Action.ID == action.ID {
				slot = "change"
			}
			if view.Review != nil {
				for _, button := range view.Review.Buttons {
					if button.Action.ID == action.ID {
						slot = "submit"
					}
				}
			}
		} else if n.kind == "item" {
			item := view.Sections[0].Items[0]
			if item.Type == "segmented" && item.Action != nil && item.Action.ID == action.ID {
				slot = "change"
			}
			if item.Control != nil && item.Control.Action.ID == action.ID {
				if item.Control.Disabled {
					return nil, errors.New("callback control is disabled")
				}
				slot = "change"
			}
			if item.Type == "composer" {
				for _, button := range item.Buttons {
					if button.Action.ID == action.ID {
						slot = "submit"
					}
				}
			}
		} else if n.kind == "tile" {
			control := view.Sections[0].Items[0].Tiles[0].Control
			if control != nil && control.Action.ID == action.ID {
				if control.Disabled {
					return nil, errors.New("callback control is disabled")
				}
				slot = "change"
			}
		}
		if event.Event != slot {
			return nil, errors.New("callback event slot mismatch")
		}
		return action, nil
	}
	return nil, errors.New("callback does not belong to node")
}

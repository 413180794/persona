package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"unicode/utf8"
)

// Declarative UI (docs/plugins.md「声明式 UI」): a plugin never runs code in the apps. It answers ui.render with a View
// — a few fixed kinds of items — and the apps draw it with their own components (each style's, in light and dark);
// a button's action goes back to the plugin (ui.action). The same View is drawn by iOS, Android and the web.

// UIItem is a contribution to ui: a place in the apps a plugin draws.
type UIItem struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Placement: plugin_page (a page opened from the plugin's settings) or launcher_section (a section on the page of
	// a launcher whose gateway is one of Hosts, or of any launcher when there are none: the plugin then answers
	// unsupported for gateways not its own; it may send requests there with the key, as quota does).
	Placement string   `json:"placement"`
	Hosts     []string `json:"hosts,omitempty"`
}

// EventsItem is a contribution to events: the hub's events a plugin is told about. It is only told; it changes
// nothing (docs/plugins.md「事件」).
type EventsItem struct {
	ID        string   `json:"id"`
	Subscribe []string `json:"subscribe"`
}

// Placements and events this hub has.
var (
	UIPlacements = []string{"plugin_page", "launcher_section"}
	EventTopics  = map[string]string{
		"inbox.changed":   "收件箱里有新的或更新了的条目（来源、种类、标题）",
		"agent.needs_you": "有 Agent 在等你回答或批准（哪台电脑、哪个会话、问的什么）",
		"agent.finished":  "有 Agent 做完了（哪台电脑、哪个会话、标题）",
	}
)

// View is what a plugin draws (ui.render, ui.action).
type View struct {
	Title    string    `json:"title,omitempty"`
	Sections []Section `json:"sections"`
	// State is the plugin's own, sent back with the next render or action (a page, a tab); at most StateMost bytes.
	State string `json:"state,omitempty"`
}

// Section is a group of items.
type Section struct {
	Title  string   `json:"title,omitempty"`
	Footer string   `json:"footer,omitempty"`
	Items  []UIElem `json:"items"`
}

// UIElem is one item. Type says which fields mean something:
//
//	text      Text, Style (body | secondary | caption | code)
//	metric    Label, Value, Detail, Tone
//	progress  Label, Ratio (0…1), Detail, Tone
//	row       Title, Subtitle, Value, Badge, Action
//	button    Title, Action, Role (normal | destructive)
//	link      Title, URL (https; opened in the app)
//	field     Name, Label, Kind (text | secret | number | toggle | choice), Value, Choices, Placeholder
//	divider
type UIElem struct {
	Type        string     `json:"type"`
	Text        string     `json:"text,omitempty"`
	Style       string     `json:"style,omitempty"`
	Label       string     `json:"label,omitempty"`
	Value       string     `json:"value,omitempty"`
	Detail      string     `json:"detail,omitempty"`
	Tone        string     `json:"tone,omitempty"` // neutral | positive | caution | danger
	Ratio       *float64   `json:"ratio,omitempty"`
	Title       string     `json:"title,omitempty"`
	Subtitle    string     `json:"subtitle,omitempty"`
	Badge       *Badge     `json:"badge,omitempty"`
	Action      *UIAction  `json:"action,omitempty"`
	Role        string     `json:"role,omitempty"`
	URL         string     `json:"url,omitempty"`
	Name        string     `json:"name,omitempty"`
	Kind        string     `json:"kind,omitempty"`
	Choices     []UIChoice `json:"choices,omitempty"`
	Placeholder string     `json:"placeholder,omitempty"`
}

type Badge struct {
	Text string `json:"text"`
	Tone string `json:"tone,omitempty"`
}

// UIAction is what a row or a button does: ui.action with its ID (and the fields' values when Submit).
type UIAction struct {
	ID string `json:"id"`
	// Confirm asks the user first, in these words.
	Confirm string `json:"confirm,omitempty"`
	Submit  bool   `json:"submit,omitempty"`
}

type UIChoice struct {
	Value string `json:"value"`
	Title string `json:"title,omitempty"`
}

// ActionResult is what ui.action answers: a new View, a short message, a link to open, or nothing (draw again).
type ActionResult struct {
	View    *View  `json:"view,omitempty"`
	Toast   string `json:"toast,omitempty"`
	OpenURL string `json:"open_url,omitempty"`
}

// Limits of a View.
const (
	ItemsMost  = 300
	StringMost = 2000
	StateMost  = 16 << 10
)

var (
	elemTypes  = []string{"text", "metric", "progress", "row", "button", "link", "field", "divider"}
	tones      = []string{"", "neutral", "positive", "caution", "danger"}
	textStyles = []string{"", "body", "secondary", "caption", "code"}
	fieldKinds = []string{"text", "secret", "number", "toggle", "choice"}
)

// CheckView holds a View to what the apps draw: known item types with what they need, short strings, https links,
// tones and styles from the fixed lists. A View that does not fit is refused, not half drawn.
func CheckView(v *View) error {
	if v == nil {
		return errors.New("no view")
	}
	if len(v.State) > StateMost {
		return fmt.Errorf("state: more than %d bytes", StateMost)
	}
	long := func(s string) bool { return utf8.RuneCountInString(s) > StringMost }
	if long(v.Title) {
		return errors.New("title too long")
	}
	n := 0
	names := map[string]bool{}
	for i, s := range v.Sections {
		if long(s.Title) || long(s.Footer) {
			return fmt.Errorf("section %d: title or footer too long", i)
		}
		for j, e := range s.Items {
			if n++; n > ItemsMost {
				return fmt.Errorf("more than %d items", ItemsMost)
			}
			at := fmt.Sprintf("section %d item %d (%s)", i, j, e.Type)
			for _, str := range []string{e.Text, e.Label, e.Value, e.Detail, e.Title, e.Subtitle, e.Placeholder} {
				if long(str) {
					return fmt.Errorf("%s: a string longer than %d", at, StringMost)
				}
			}
			if !slices.Contains(elemTypes, e.Type) {
				return fmt.Errorf("%s: unknown type", at)
			}
			if !slices.Contains(tones, e.Tone) || (e.Badge != nil && !slices.Contains(tones, e.Badge.Tone)) {
				return fmt.Errorf("%s: tone is neutral, positive, caution or danger", at)
			}
			if e.Action != nil && (e.Action.ID == "" || len(e.Action.ID) > 128) {
				return fmt.Errorf("%s: an action needs an id", at)
			}
			var err error
			switch e.Type {
			case "text":
				if e.Text == "" || !slices.Contains(textStyles, e.Style) {
					err = errors.New("text needs text; style is body, secondary, caption or code")
				}
			case "metric":
				if e.Label == "" || e.Value == "" {
					err = errors.New("metric needs label and value")
				}
			case "progress":
				if e.Label == "" || e.Ratio == nil || *e.Ratio < 0 || *e.Ratio > 1 {
					err = errors.New("progress needs label and a ratio 0…1")
				}
			case "row":
				if e.Title == "" {
					err = errors.New("row needs title")
				}
			case "button":
				if e.Title == "" || e.Action == nil || (e.Role != "" && e.Role != "normal" && e.Role != "destructive") {
					err = errors.New("button needs title and action; role is normal or destructive")
				}
			case "link":
				if u, perr := url.Parse(e.URL); e.Title == "" || perr != nil || u.Scheme != "https" || u.Host == "" {
					err = errors.New("link needs title and an https url")
				}
			case "field":
				switch {
				case e.Name == "" || e.Label == "" || !slices.Contains(fieldKinds, e.Kind):
					err = errors.New("field needs name, label and kind text | secret | number | toggle | choice")
				case names[e.Name]:
					err = fmt.Errorf("field %q twice", e.Name)
				case e.Kind == "choice" && len(e.Choices) == 0:
					err = errors.New("a choice field needs choices")
				case e.Kind == "secret" && e.Value != "":
					err = errors.New("a secret field is never filled in")
				}
				names[e.Name] = true
			}
			if err != nil {
				return fmt.Errorf("%s: %v", at, err)
			}
		}
	}
	return nil
}

// ParseView reads and checks a View a plugin answered with.
func ParseView(raw json.RawMessage) (*View, error) {
	var v View
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	if v.Sections == nil {
		v.Sections = []Section{}
	}
	return &v, CheckView(&v)
}

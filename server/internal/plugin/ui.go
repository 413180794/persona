package plugin

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
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
	// Placement is one of UISlots: plugin_page (a page opened from the plugin's settings), launcher_section (a section
	// on the page of a launcher whose gateway is one of Hosts, or of any launcher when there are none: the plugin then
	// answers unsupported for gateways not its own; it may send requests there with the key, as quota does), nav_page,
	// overview_card, machine_section.
	Placement string   `json:"placement"`
	Hosts     []string `json:"hosts,omitempty"`
	// Icon is a nav_page's icon, from UIIcons (each app draws its own); plugin when empty.
	Icon string `json:"icon,omitempty"`
	// Opens is the ui (a nav_page or plugin_page of the same plugin) an overview_card opens when tapped.
	Opens string `json:"opens,omitempty"`
}

// UISlot is a place in the apps plugins may draw (docs/plugins.md「UI 插槽」).
type UISlot struct {
	Name string
	// Size is what the plugin is asked to draw there: compact (a card) or full (a page, a section).
	Size string
	// Context is what the apps say about where it is drawn: launcher, machine.
	Context string
	// Badge: the plugin may put a badge (ui.badge) on its entry.
	Badge bool
	// Shown: the user is shown before installing that the plugin draws here, and agrees again when an upgrade adds it
	// (Explain, Digest): it sits among the app's own pages, outside the plugin's settings.
	Shown string
	// Since is the UI version that brought it.
	Since int
}

// UISlots are the places this hub lets plugins draw, in the order the apps list them.
var UISlots = []UISlot{
	{Name: "plugin_page", Size: SizeFull, Since: 1},
	{Name: "launcher_section", Size: SizeFull, Context: "launcher", Since: 1},
	{Name: "nav_page", Size: SizeFull, Badge: true, Shown: "主导航上的入口", Since: 2},
	{Name: "overview_card", Size: SizeCompact, Shown: "总览上的卡片", Since: 2},
	{Name: "machine_section", Size: SizeFull, Context: "machine", Shown: "机器详情页里的一节（会知道是哪台机器）", Since: 2},
}

// Slot is the slot named name.
func Slot(name string) (UISlot, bool) {
	i := slices.IndexFunc(UISlots, func(s UISlot) bool { return s.Name == name })
	if i < 0 {
		return UISlot{}, false
	}
	return UISlots[i], true
}

// Sizes, platforms and icons of the UI.
const (
	SizeCompact = "compact"
	SizeFull    = "full"
)

var (
	UIPlatforms = []string{"ios", "mac", "android", "web"}
	UIIcons     = []string{"plugin", "chart", "gauge", "list", "bell", "doc", "calendar", "money"}
)

// UIVersion is the version of the UI this hub knows: 1 the first eight items and two slots; 2 the slots of UISlots,
// chart and stat_grid, refresh_after and the render context. An app says which it draws (context.ui_version, 1 when
// it says nothing); the hub leaves out of a View what the app does not know (Fit).
const UIVersion = 2

// UIMachine is the machine a machine_section is drawn for.
type UIMachine struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	OS     string `json:"os,omitempty"`
	Online bool   `json:"online"`
}

// UILauncher is the launcher a launcher_section is drawn for; QueryID lets the plugin reach its gateway
// (gateway.http) while it draws.
type UILauncher struct {
	Name    string `json:"name"`
	BaseURL string `json:"base_url"`
	QueryID string `json:"query_id"`
}

// UIParams are ui.render's and ui.action's parameters: what to draw and where, for whom.
type UIParams struct {
	Contribution string            `json:"contribution"`
	State        string            `json:"state"`
	Action       string            `json:"action,omitempty"`
	Values       map[string]string `json:"values,omitempty"`
	Placement    string            `json:"placement"`
	Size         string            `json:"size"`
	// UIVersion is what the app that asks draws (UIVersion at most): a plugin may draw an older app something it knows.
	UIVersion int         `json:"ui_version"`
	TZ        string      `json:"tz,omitempty"`     // the user's time zone ("Asia/Shanghai"); "" when unknown
	Locale    string      `json:"locale,omitempty"` // the app's language ("zh-Hans-CN")
	Platform  string      `json:"platform,omitempty"`
	Machine   *UIMachine  `json:"machine,omitempty"`
	Launcher  *UILauncher `json:"launcher,omitempty"`
}

// BadgeEvent is ui.badge, sent by a plugin: the badge on its nav_page's entry (no text clears it).
type BadgeEvent struct {
	Contribution string `json:"contribution"`
	Text         string `json:"text,omitempty"`
	Tone         string `json:"tone,omitempty"`
}

// BadgeMost is how long a badge is, in characters.
const BadgeMost = 8

// CheckBadge holds a badge to what the apps draw.
func CheckBadge(text, tone string) error {
	if utf8.RuneCountInString(text) > BadgeMost {
		return fmt.Errorf("a badge has at most %d characters", BadgeMost)
	}
	if !slices.Contains(tones, tone) {
		return errors.New("tone is neutral, positive, caution or danger")
	}
	return nil
}

// EventsItem is a contribution to events: the hub's events a plugin is told about. It is only told; it changes
// nothing (docs/plugins.md「事件」).
type EventsItem struct {
	ID        string   `json:"id"`
	Subscribe []string `json:"subscribe"`
}

// Events this hub has.
var (
	EventTopics = map[string]string{
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
	// RefreshAfter: the apps draw it again after this many seconds while it is on screen (RefreshLeast at least, the
	// hub raises a smaller one; 0 never).
	RefreshAfter int `json:"refresh_after,omitempty"`
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
//	chart     Kind (bar | line), Series, Title, Unit, Stacked, Detail                     (UI version 2)
//	stat_grid Stats                                                                     (UI version 2)
//	segmented Name, Choices (2…5), Value, Action: one of a few, chosen at once          (UI version 2)
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
	// A chart's: its series (all with the same labels, in the same order), the unit of their values, bars stacked.
	Series  []ChartSeries `json:"series,omitempty"`
	Unit    string        `json:"unit,omitempty"`
	Stacked bool          `json:"stacked,omitempty"`
	// A stat_grid's: a few metrics side by side.
	Stats []Stat `json:"stats,omitempty"`
}

// ChartSeries is one line, or one color of bars. Tone empty: the app's own colors for series, in order.
type ChartSeries struct {
	Name   string       `json:"name,omitempty"`
	Tone   string       `json:"tone,omitempty"`
	Points []ChartPoint `json:"points"`
}

// ChartPoint is a value at a label (a day, a model). Display is how to write the value ("¥1.20", "3.4M"); the app
// writes Value with the chart's Unit when it is empty.
type ChartPoint struct {
	Label   string  `json:"label"`
	Value   float64 `json:"value"`
	Display string  `json:"display,omitempty"`
}

// Stat is one metric of a stat_grid.
type Stat struct {
	Label  string `json:"label"`
	Value  string `json:"value"`
	Detail string `json:"detail,omitempty"`
	Tone   string `json:"tone,omitempty"`
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
	// A chart has at most SeriesMost series of PointsMost points; a stat_grid at most StatsMost stats.
	SeriesMost = 4
	PointsMost = 120
	StatsMost  = 6
	// A segmented has at most SegmentsMost choices.
	SegmentsMost = 5
	// A compact view (an overview_card) has at most CompactSections sections and CompactItems items, and no fields.
	CompactSections = 2
	CompactItems    = 12
	// RefreshLeast and RefreshMost bound refresh_after, in seconds.
	RefreshLeast = 30
	RefreshMost  = 24 * 3600
)

var (
	// elemSince is each item's type and the UI version that brought it.
	elemSince = map[string]int{"text": 1, "metric": 1, "progress": 1, "row": 1, "button": 1, "link": 1, "field": 1, "divider": 1,
		"chart": 2, "stat_grid": 2, "segmented": 2}
	elemTypes  = slices.Sorted(maps.Keys(elemSince))
	chartKinds = []string{"bar", "line"}
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
			for _, str := range []string{e.Text, e.Label, e.Value, e.Detail, e.Title, e.Subtitle, e.Placeholder, e.Unit} {
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
			case "segmented":
				switch {
				case e.Name == "" || e.Action == nil || len(e.Choices) < 2 || len(e.Choices) > SegmentsMost:
					err = fmt.Errorf("segmented needs name, action and 2 to %d choices", SegmentsMost)
				case names[e.Name]:
					err = fmt.Errorf("field %q twice", e.Name)
				case !slices.ContainsFunc(e.Choices, func(c UIChoice) bool { return c.Value == e.Value }):
					err = errors.New("segmented: value is one of its choices")
				case slices.ContainsFunc(e.Choices, func(c UIChoice) bool { return utf8.RuneCountInString(cmp.Or(c.Title, c.Value)) > 12 }):
					err = errors.New("segmented: a choice has at most 12 characters")
				}
				names[e.Name] = true
			case "chart":
				err = checkChart(e)
			case "stat_grid":
				err = checkStats(e.Stats)
			}
			if err != nil {
				return fmt.Errorf("%s: %v", at, err)
			}
		}
	}
	return nil
}

func checkChart(e UIElem) error {
	if !slices.Contains(chartKinds, e.Kind) {
		return errors.New("chart kind is bar or line")
	}
	if len(e.Series) == 0 || len(e.Series) > SeriesMost {
		return fmt.Errorf("a chart has 1 to %d series", SeriesMost)
	}
	first := e.Series[0].Points
	if len(first) == 0 || len(first) > PointsMost {
		return fmt.Errorf("a series has 1 to %d points", PointsMost)
	}
	for k, s := range e.Series {
		if utf8.RuneCountInString(s.Name) > 40 || !slices.Contains(tones, s.Tone) {
			return fmt.Errorf("series %d: a name of at most 40 characters; tone is neutral, positive, caution or danger", k)
		}
		if len(e.Series) > 1 && s.Name == "" {
			return fmt.Errorf("series %d: series of a chart with more than one are named", k)
		}
		if len(s.Points) != len(first) {
			return fmt.Errorf("series %d: every series has the same labels", k)
		}
		for n, pt := range s.Points {
			if pt.Label != first[n].Label {
				return fmt.Errorf("series %d point %d: every series has the same labels, in the same order", k, n)
			}
			if utf8.RuneCountInString(pt.Label) > 40 || utf8.RuneCountInString(pt.Display) > 40 {
				return fmt.Errorf("series %d point %d: label and display have at most 40 characters", k, n)
			}
			if math.IsNaN(pt.Value) || math.IsInf(pt.Value, 0) {
				return fmt.Errorf("series %d point %d: a number", k, n)
			}
		}
	}
	return nil
}

func checkStats(stats []Stat) error {
	if len(stats) == 0 || len(stats) > StatsMost {
		return fmt.Errorf("a stat_grid has 1 to %d stats", StatsMost)
	}
	for k, st := range stats {
		if st.Label == "" || st.Value == "" {
			return fmt.Errorf("stat %d: label and value", k)
		}
		if utf8.RuneCountInString(st.Label)+utf8.RuneCountInString(st.Value)+utf8.RuneCountInString(st.Detail) > StringMost {
			return fmt.Errorf("stat %d: too long", k)
		}
		if !slices.Contains(tones, st.Tone) {
			return fmt.Errorf("stat %d: tone is neutral, positive, caution or danger", k)
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

// Fit makes a View a plugin drew into one an app that draws UI version uiVersion can draw at slot: items of types
// this hub does not know (a plugin written for a newer hub) and those newer than the app are left out, a section they
// emptied with them; refresh_after is held to its bounds. Then it is checked (CheckView), a compact one to its limits.
// It says what types it left out.
func Fit(v *View, slot UISlot, uiVersion int) (dropped []string, err error) {
	if v == nil {
		return nil, errors.New("no view")
	}
	if v.Sections == nil {
		v.Sections = []Section{}
	}
	sections := v.Sections[:0]
	for _, s := range v.Sections {
		items := s.Items[:0]
		for _, e := range s.Items {
			since, known := elemSince[e.Type]
			if !known || since > uiVersion {
				if !slices.Contains(dropped, e.Type) {
					dropped = append(dropped, e.Type)
				}
				continue
			}
			items = append(items, e)
		}
		if len(items) == 0 && len(s.Items) > 0 && s.Footer == "" {
			continue
		}
		if s.Items = items; s.Items == nil {
			s.Items = []UIElem{}
		}
		sections = append(sections, s)
	}
	v.Sections = sections
	switch {
	case v.RefreshAfter < 0:
		v.RefreshAfter = 0
	case v.RefreshAfter > 0 && v.RefreshAfter < RefreshLeast:
		v.RefreshAfter = RefreshLeast
	case v.RefreshAfter > RefreshMost:
		v.RefreshAfter = RefreshMost
	}
	if err := CheckView(v); err != nil {
		return dropped, err
	}
	if slot.Size == SizeCompact {
		n := 0
		for _, s := range v.Sections {
			n += len(s.Items)
			for _, e := range s.Items {
				if e.Type == "field" {
					return dropped, errors.New("a card has no fields: open a page for them")
				}
			}
		}
		if len(v.Sections) > CompactSections || n > CompactItems {
			return dropped, fmt.Errorf("a card has at most %d sections and %d items", CompactSections, CompactItems)
		}
	}
	return dropped, nil
}

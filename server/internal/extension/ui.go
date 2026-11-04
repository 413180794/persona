package extension

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nwverse/persona/sdk/go/wire"
)

// Declarative UI (docs/extensions.md「声明式 UI」): a plugin never runs code in the apps. It answers ui.render with a View
// — a few fixed kinds of items — and the apps draw it with their own components (each style's, in light and dark);
// a button's action goes back to the plugin (ui.action). The same View is drawn by iOS, Android and the web.

// UIItem is a contribution to ui: a place in the apps a plugin draws.
type UIItem struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Placement is one of UISlots: plugin_page (a page opened from the plugin's settings), launcher_section (a section
	// on the page of a launcher whose gateway is one of Hosts, or of any launcher when there are none: the plugin then
	// answers unsupported for gateways not its own; it may send requests there with the key, as quota does), nav_page,
	// overview_card, machine_section, inbox_item.
	Placement string   `json:"placement"`
	Hosts     []string `json:"hosts,omitempty"`
	// Icon is a nav_page's icon, from UIIcons (each app draws its own); plugin when empty.
	Icon string `json:"icon,omitempty"`
	// Opens is the ui (a nav_page or plugin_page of the same plugin) an overview_card opens when tapped.
	Opens string `json:"opens,omitempty"`
	// Connector is the connector an inbox_item draws the items of: one of the plugin's own connectors.
	Connector string `json:"connector,omitempty"`
}

// UISlot is a place in the apps plugins may draw (docs/extensions.md「UI 插槽」).
type UISlot struct {
	Name string
	// Size is what the plugin is asked to draw there: compact (a card) or full (a page, a section).
	Size string
	// Context is what the apps say about where it is drawn: launcher, machine, item.
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
	// inbox_item draws the body of an inbox item from the plugin's connector (its own header stays the app's): the
	// same items a plugin's connector brought in, with the hub's triage on them. Drawn with UI version 2's items.
	{Name: "inbox_item", Size: SizeFull, Context: "item", Shown: "收件箱里它的连接器的条目详情（会读到那一条的标题、总结和分级）", Since: 2},
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

// UIInboxItem is the inbox item an inbox_item is drawn for: the row as the hub keeps it. Meta is the connector's own
// record of what the item gathers, passed as it is stored (null when it has none): the hub never reads it. Extra are
// the item's source blocks as the apps get them (lark, doc, moment, connector…), for a plugin drawing an item that has
// no meta.
type UIInboxItem struct {
	ID      string                     `json:"id"`
	Title   string                     `json:"title"`
	Summary string                     `json:"summary,omitempty"`
	Tier    string                     `json:"tier,omitempty"`
	Reason  string                     `json:"reason,omitempty"`
	Done    bool                       `json:"done"`
	Meta    json.RawMessage            `json:"meta"`
	Extra   map[string]json.RawMessage `json:"extra,omitempty"`
	// Count is how many messages the item holds (for an event, the hub's own count); StartedAt is when it began.
	Count     int       `json:"count,omitempty"`
	StartedAt time.Time `json:"started_at,omitzero"`
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
	UIVersion int          `json:"ui_version"`
	TZ        string       `json:"tz,omitempty"`     // the user's time zone ("Asia/Shanghai"); "" when unknown
	Locale    string       `json:"locale,omitempty"` // the app's language ("zh-Hans-CN")
	Platform  string       `json:"platform,omitempty"`
	Machine   *UIMachine   `json:"machine,omitempty"`
	Launcher  *UILauncher  `json:"launcher,omitempty"`
	Item      *UIInboxItem `json:"item,omitempty"`
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
// nothing (docs/extensions.md「事件」).
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

// The View and its components are the wire types generated from schema/extension/view.schema.json (sdk/go/wire):
// the schema is the one truth of what an extension may draw, and the same types are every app's.
type (
	// View is what an extension draws (ui.render, ui.action): sections of components, a state of its own the apps send
	// back (at most StateMost bytes), refresh_after.
	View    = wire.View
	Section = wire.Section
	// UIElem is one component; Type says which fields mean something (the schema's Component, one flat struct).
	UIElem      = wire.Component
	ChartSeries = wire.ChartSeries
	ChartPoint  = wire.ChartPoint
	Stat        = wire.Stat
	Badge       = wire.Badge
	// UIAction is what a row, a button or a segmented does: ui.action with its ID (and the fields' values when Submit).
	UIAction = wire.Action
	UIChoice = wire.Choice
	// ActionResult is what ui.action answers: a new View, a short message, a link to open, or nothing (draw again).
	ActionResult = wire.ActionResult
)

// InboxSourceURL is the one link besides https an action may open, and only on an inbox_item, for the item it is
// drawn for: the apps open the item's own source page there (what opening the item showed before the slot: a Feishu
// chat at the item's first message). The id is escaped as one path segment ("lark:F1:1051" stays as it is).
func InboxSourceURL(itemID string) string {
	return "persona://inbox/" + url.PathEscape(itemID) + "/source"
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

// elemSince is each component's type and the UI version that brought it (the schema's x-since).
var elemSince = wire.ComponentSince

// tones are the tones a component may have; empty is the app's own.
var tones = append([]string{""}, wire.Tones...)

// CheckView holds a View to what the apps draw: first to the schema (known component types with what they need, short
// strings, https links, tones and styles from the fixed lists), then to what the schema cannot say (a page's size, a
// field's name once, a chart's series alike). A View that does not fit is refused, not half drawn.
func CheckView(v *View) error {
	if v == nil {
		return errors.New("no view")
	}
	if err := wire.Validate("View", v); err != nil {
		var ve *wire.ValidationError
		if errors.As(err, &ve) {
			return errors.New(where(v, ve.Errors[0]))
		}
		return err
	}
	if len(v.State) > StateMost {
		return fmt.Errorf("state: more than %d bytes", StateMost)
	}
	n := 0
	names := map[string]bool{}
	for i, s := range v.Sections {
		for j, e := range s.Items {
			if n++; n > ItemsMost {
				return fmt.Errorf("more than %d items", ItemsMost)
			}
			var err error
			switch e.Type {
			case "link":
				if u, perr := url.Parse(e.URL); perr != nil || u.Scheme != "https" || u.Host == "" {
					err = errors.New("link needs an https url")
				}
			case "field":
				switch {
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
				return fmt.Errorf("section %d item %d (%s): %v", i, j, e.Type, err)
			}
		}
	}
	return nil
}

// where turns the schema's "$.sections[1].items[2].tone: …" into "section 1 item 2 (metric): tone: …".
func where(v *View, msg string) string {
	var i, j int
	if k, err := fmt.Sscanf(msg, "$.sections[%d].items[%d]", &i, &j); err == nil && k == 2 && i < len(v.Sections) && j < len(v.Sections[i].Items) {
		rest := msg[len(fmt.Sprintf("$.sections[%d].items[%d]", i, j)):]
		rest = strings.TrimPrefix(strings.TrimPrefix(rest, "."), ": ")
		return fmt.Sprintf("section %d item %d (%s): %s", i, j, v.Sections[i].Items[j].Type, rest)
	}
	return strings.TrimPrefix(strings.TrimPrefix(msg, "$."), "$: ")
}

func checkChart(e UIElem) error {
	if !slices.Contains(wire.ChartKinds, e.Kind) {
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
	for i := range v.Sections {
		if v.Sections[i].Items == nil {
			v.Sections[i].Items = []UIElem{}
		}
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

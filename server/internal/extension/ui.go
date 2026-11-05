package extension

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"regexp"
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
	// Placement is one of UISlots: extension_page (a page opened from the extension's settings; plugin_page is its old
	// name, the extension is told the one it wrote), launcher_section (a section
	// on the page of a launcher whose gateway is one of Hosts, or of any launcher when there are none: the plugin then
	// answers unsupported for gateways not its own; it may send requests there with the key, as quota does), nav_page,
	// overview_card, capsule_card, machine_section, inbox_item.
	Placement string   `json:"placement"`
	Hosts     []string `json:"hosts,omitempty"`
	// Icon is a nav_page's icon, from UIIcons (each app draws its own); the default when empty.
	Icon string `json:"icon,omitempty"`
	// Opens is the ui (a nav_page or extension_page of the same extension) a compact card or widget opens when
	// tapped.
	Opens string `json:"opens,omitempty"`
	// Connector is the connector an inbox_item draws the items of: one of the plugin's own connectors.
	Connector string `json:"connector,omitempty"`
	// Families are the sizes a widget can be drawn at (WidgetFamilies); RefreshMinutes how often it is drawn again on
	// its own (WidgetRefreshDefault when 0). A widget's only.
	Families       []string `json:"families,omitempty"`
	RefreshMinutes int      `json:"refresh_minutes,omitempty"`
}

// Refresh is how often widget u is drawn again on its own.
func (u UIItem) Refresh() time.Duration {
	return time.Duration(cmp.Or(u.RefreshMinutes, WidgetRefreshDefault)) * time.Minute
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
	// extension_page is plugin_page's new name: the same slot (PageSlot).
	{Name: "extension_page", Size: SizeFull, Since: 3},
	{Name: "launcher_section", Size: SizeFull, Context: "launcher", Since: 1},
	{Name: "nav_page", Size: SizeFull, Badge: true, Shown: "主导航上的入口", Since: 2},
	{Name: "overview_card", Size: SizeCompact, Shown: "总览上的卡片", Since: 2},
	{Name: "capsule_card", Size: SizeCompact, Shown: "Android 跨 App 悬浮胶囊里的卡片（在 App 外显示）", Since: 3},
	{Name: "machine_section", Size: SizeFull, Context: "machine", Shown: "机器详情页里的一节（会知道是哪台机器）", Since: 2},
	// inbox_item draws the body of an inbox item from the plugin's connector (its own header stays the app's): the
	// same items a plugin's connector brought in, with the hub's triage on them. Drawn with UI version 2's items.
	{Name: "inbox_item", Size: SizeFull, Context: "item", Shown: "收件箱里它的连接器的条目详情（会读到那一条的标题、总结和分级）", Since: 2},
	// widget is drawn outside the app (docs/extensions.md「系统级位置」): a home screen widget, a Lock Screen or watch
	// face complication. The size it is asked for is the family (FitWidget holds the view to it); the apps list
	// widgets on their own (GET /api/v1/extensions/-/widgets), not with the slots drawn in the app.
	{Name: "widget", Size: WidgetFamilySmall, Shown: "主屏幕小组件和表盘（在 App 外显示）", Since: 3},
}

// Widgets: the families a widget is drawn at and how often it is drawn again (docs/extensions.md「系统级位置」).
const (
	WidgetFamilySmall    = wire.WidgetFamilySmall
	WidgetRefreshDefault = 30 // minutes
	WidgetRefreshLeast   = 15
	WidgetRefreshMost    = 24 * 60
)

// WidgetFamilies are the sizes a widget can be drawn at: small / medium / large on the home screen, accessory_* on the
// Lock Screen and watch faces.
var WidgetFamilies = wire.WidgetFamilies

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
	// UIIcons is the fixed icon set (a nav_page's, a row's, an empty state's): the schema's NavIcon.
	UIIcons = wire.NavIcons
)

// UIVersion is the version of the UI this hub knows: 1 the first eight items and two slots; 2 the slots of UISlots,
// chart and stat_grid, refresh_after and the render context; 3 markdown, table, empty and loading, a row's icon, avatar
// and thumbnail, components' keys, a view's search and filter, a section's more, pushing and popping pages,
// ui.invalidate. An app says which it draws (context.ui_version, 1 when it says nothing); the hub leaves out of a View
// what the app does not know, or draws it with what it knows (Fit).
const UIVersion = 3

// PageSlot says whether placement is the extension's own page: extension_page, or plugin_page as it was called.
func PageSlot(placement string) bool {
	return placement == "extension_page" || placement == "plugin_page"
}

// FullPage says whether placement is a whole page a push may open: an extension_page or a nav_page.
func FullPage(placement string) bool { return PageSlot(placement) || placement == "nav_page" }

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
		"kb.changed":      "知识库里的条目有变化（类型、标识、变化部分；需读知识库权限）",
		"kb.forgotten":    "知识库删除了一个来源的数据（来源；需读知识库权限）",
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
	// ActionResult is what ui.action answers: a new View, a short message, a link to open, a page to push or pop, or
	// nothing (draw again).
	ActionResult = wire.ActionResult
	// Push is a page an action opens on top: one of the extension's whole pages, with a state.
	Push = wire.Push
	// InvalidateEvent is ui.invalidate, sent by an extension: what it draws changed (one ui, or all).
	InvalidateEvent = wire.InvalidateEvent
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
	// A view has at most StackMost pages pushed on its own (the apps keep no more).
	StackMost = 8
	// A markdown has at most MarkdownMost characters; a table at most TableColumnsMost columns and TableRowsMost rows.
	MarkdownMost     = 20000
	TableColumnsMost = 8
	TableRowsMost    = 100
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
	for _, a := range Actions(v) {
		if a.Push != nil && len(a.Push.State) > StateMost {
			return fmt.Errorf("action %s: the state it pushes is more than %d bytes", a.ID, StateMost)
		}
	}
	n := 0
	names := map[string]bool{}
	keys := map[string]bool{}
	if v.Search != nil {
		names["q"] = true
	}
	if f := v.Filter; f != nil {
		if names[f.Name] {
			return fmt.Errorf("filter: field %q twice (q is the search's)", f.Name)
		}
		names[f.Name] = true
		if !slices.ContainsFunc(f.Choices, func(c UIChoice) bool { return c.Value == f.Value }) {
			return errors.New("filter: value is one of its choices")
		}
	}
	for i, s := range v.Sections {
		for j, e := range s.Items {
			if n++; n > ItemsMost {
				return fmt.Errorf("more than %d items", ItemsMost)
			}
			var err error
			if e.Key != "" {
				if keys[e.Key] {
					err = fmt.Errorf("key %q twice", e.Key)
				}
				keys[e.Key] = true
			}
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
			case "row":
				if (e.Icon != "") && (e.Avatar != nil || e.Thumbnail != nil) || e.Avatar != nil && e.Thumbnail != nil {
					err = errors.New("a row has at most one of icon, avatar and thumbnail")
				} else if a := e.Avatar; a != nil && a.URL == "" && a.Text == "" {
					err = errors.New("an avatar has a url or a text")
				}
			case "table":
				for k, r := range e.Rows {
					if len(r) != len(e.Columns) {
						err = fmt.Errorf("row %d has %d cells for %d columns", k, len(r), len(e.Columns))
						break
					}
				}
			case "markdown":
				err = checkMarkdown(e.Text)
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

// mdImage is a Markdown image: ![text](target.
var mdImage = regexp.MustCompile(`!\[[^\]]*\]\(`)

// mdLink is a Markdown link's or image's target: [text](target "title").
var mdLink = regexp.MustCompile(`\]\(\s*<?([^)\s>]*)`)

// mdCode is Markdown's code, a block or a span: what looks like a link in it is text.
var mdCode = regexp.MustCompile("(?s)```.*?(```|$)|`[^`\n]*`")

// checkMarkdown holds a markdown to its subset where it matters: links are https (the apps open nothing else), no
// images. HTML is drawn as the text it is.
func checkMarkdown(text string) error {
	text = mdCode.ReplaceAllString(text, "")
	if mdImage.MatchString(text) {
		return errors.New("markdown has no images")
	}
	for _, m := range mdLink.FindAllStringSubmatch(text, -1) {
		if u, err := url.Parse(m[1]); err != nil || u.Scheme != "https" || u.Host == "" {
			return fmt.Errorf("markdown links are https: %q", m[1])
		}
	}
	return nil
}

// Actions are a view's actions (its components', its search's, filter's and sections' more) and where each is.
func Actions(v *View) []*UIAction {
	var out []*UIAction
	if v == nil {
		return nil
	}
	if v.Search != nil {
		out = append(out, &v.Search.Action)
	}
	if v.Filter != nil {
		out = append(out, &v.Filter.Action)
	}
	for i := range v.Sections {
		s := &v.Sections[i]
		for j := range s.Items {
			e := &s.Items[j]
			if e.Action != nil {
				out = append(out, e.Action)
			}
			if e.Button != nil {
				out = append(out, &e.Button.Action)
			}
		}
		if s.More != nil {
			out = append(out, &s.More.Action)
		}
	}
	return out
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
// this hub does not know (a plugin written for a newer hub) are left out, a section they emptied with them; those
// newer than the app are drawn with what it knows where that says the same (a markdown as text, a table as rows, an
// empty state as a text and a button, a section's more as a button, a filter as a segmented), left out where it does
// not; refresh_after is held to its bounds. Then it is checked (CheckView), a compact one to its limits. It says what
// types it left out.
func Fit(v *View, slot UISlot, uiVersion int) (dropped []string, err error) {
	if v == nil {
		return nil, errors.New("no view")
	}
	if v.Sections == nil {
		v.Sections = []Section{}
	}
	if uiVersion < 3 {
		downgrade(v, uiVersion)
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
		if v.Search != nil || v.Filter != nil || slices.ContainsFunc(v.Sections, func(s Section) bool { return s.More != nil }) {
			return dropped, errors.New("a card has no search, filter or more: open a page for them")
		}
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

// downgrade draws v3's components and fields with those of uiVersion (1 or 2), in place: what an older app gets.
func downgrade(v *View, uiVersion int) {
	var filter []UIElem
	if f := v.Filter; f != nil && uiVersion >= 2 && len(f.Choices) <= SegmentsMost &&
		!slices.ContainsFunc(f.Choices, func(c UIChoice) bool { return utf8.RuneCountInString(cmp.Or(c.Title, c.Value)) > 12 }) {
		filter = []UIElem{{Type: "segmented", Name: f.Name, Choices: f.Choices, Value: f.Value, Action: &f.Action}}
	}
	v.Search, v.Filter = nil, nil
	if len(filter) > 0 {
		v.Sections = append([]Section{{Items: filter}}, v.Sections...)
	}
	for i := range v.Sections {
		s := &v.Sections[i]
		items := make([]UIElem, 0, len(s.Items))
		for _, e := range s.Items {
			e.Key, e.Icon, e.Avatar, e.Thumbnail = "", "", nil, nil
			if e.Action != nil {
				a := *e.Action
				a.Push = nil
				e.Action = &a
			}
			switch e.Type {
			case "markdown":
				items = append(items, UIElem{Type: "text", Text: clip(e.Text, StringMost)})
			case "table":
				for _, r := range e.Rows {
					if len(r) == 0 {
						continue
					}
					items = append(items, UIElem{Type: "row", Title: cmp.Or(clip(r[0], StringMost), "—"), Value: clip(strings.Join(r[1:], " · "), StringMost)})
				}
				if e.Caption != "" {
					items = append(items, UIElem{Type: "text", Text: e.Caption, Style: "caption"})
				}
			case "empty":
				items = append(items, UIElem{Type: "text", Text: clip(strings.TrimSpace(e.Title+"\n"+e.Detail), StringMost), Style: "secondary"})
				if b := e.Button; b != nil {
					a := b.Action
					a.Push = nil
					items = append(items, UIElem{Type: "button", Title: b.Title, Action: &a})
				}
			case "loading":
				items = append(items, UIElem{Type: "text", Text: cmp.Or(e.Title, "加载中…"), Style: "secondary"})
			default:
				items = append(items, e)
			}
		}
		if m := s.More; m != nil {
			a := m.Action
			a.Push = nil
			items = append(items, UIElem{Type: "button", Title: cmp.Or(m.Title, "加载更多"), Action: &a})
		}
		s.More, s.Items = nil, items
	}
}

// Limits of a widget (FitWidget): the items of each family, the stats of a stat_grid, the rows, the lines of a text.
var widgetItems = map[string]int{"small": 4, "medium": 6, "large": 12}

const (
	widgetSections   = 2
	widgetStats      = 4
	widgetStatsSmall = 2
	widgetRows       = 4
	widgetLines      = 3
	widgetText       = 200
)

// widgetKinds are the components a widget draws; the rest are left out, or drawn as text (markdown, empty, loading).
var widgetKinds = []string{"metric", "stat_grid", "progress", "chart", "text", "row"}

// FitWidget makes a View an extension drew for a widget into one the family draws (docs/extensions.md「系统级位置」):
// first as any view (Fit), then held to a widget's subset — a metric, a stat_grid, a progress, a chart, a few lines of
// text, a few rows, nothing to tap (a tap opens the widget's page), no search, filter or more — cutting what is too
// long rather than refusing it. A complication (accessory_*) draws one metric, progress or line of text, the first.
// It says what it left out.
func FitWidget(v *View, family string, uiVersion int) (dropped []string, err error) {
	if !slices.Contains(WidgetFamilies, family) {
		return nil, fmt.Errorf("a widget is drawn at one of %s", strings.Join(WidgetFamilies, ", "))
	}
	slot, _ := Slot("widget")
	if dropped, err = Fit(v, slot, uiVersion); err != nil {
		return dropped, err
	}
	drop := func(what string) {
		if !slices.Contains(dropped, what) {
			dropped = append(dropped, what)
		}
	}
	if v.Search != nil || v.Filter != nil {
		drop("search")
	}
	v.Search, v.Filter, v.RefreshAfter = nil, nil, 0
	complication := strings.HasPrefix(family, "accessory_")
	stats := widgetStats
	if family == "small" {
		stats = widgetStatsSmall
	}
	sections := []Section{}
	n, rows := 0, 0
	for _, s := range v.Sections {
		if s.More != nil {
			drop("more")
		}
		var kept []UIElem
		for _, e := range s.Items {
			switch e.Type {
			case "markdown":
				e = UIElem{Type: "text", Text: e.Text}
			case "empty":
				e = UIElem{Type: "text", Text: strings.TrimSpace(e.Title + "\n" + e.Detail), Style: "secondary"}
			case "loading":
				e = UIElem{Type: "text", Text: cmp.Or(e.Title, "加载中…"), Style: "secondary"}
			}
			if !slices.Contains(widgetKinds, e.Type) {
				drop(e.Type)
				continue
			}
			e.Action, e.Avatar, e.Thumbnail = nil, nil, nil
			if complication {
				if e.Type == "metric" || e.Type == "text" || e.Type == "progress" && family != "accessory_inline" {
					if e.Type == "text" {
						e.Text = widgetLinesOf(e.Text, map[bool]int{true: widgetLines, false: 1}[family == "accessory_rectangular"])
					}
					v.Sections = []Section{{Items: []UIElem{e}}}
					return dropped, nil
				}
				continue
			}
			switch e.Type {
			case "text":
				e.Text = widgetLinesOf(e.Text, widgetLines)
			case "stat_grid":
				if len(e.Stats) > stats {
					e.Stats = e.Stats[:stats]
				}
			case "row":
				if rows++; rows > widgetRows {
					continue
				}
			}
			if n < widgetItems[family] {
				kept = append(kept, e)
				n++
			}
		}
		if len(kept) > 0 && len(sections) < widgetSections {
			sections = append(sections, Section{Title: s.Title, Items: kept})
		}
	}
	if complication {
		return dropped, errors.New("a complication draws a metric, a progress or a line of text: the view has none")
	}
	if len(sections) == 0 {
		return dropped, errors.New("a widget draws a metric, a stat_grid, a progress, a chart, text or rows: the view has none")
	}
	v.Sections = sections
	return dropped, nil
}

// widgetLinesOf is text cut to at most lines lines and widgetText characters.
func widgetLinesOf(text string, lines int) string {
	parts := strings.Split(text, "\n")
	if len(parts) > lines {
		text = strings.Join(parts[:lines], "\n") + "…"
	}
	return clip(text, widgetText)
}

// clip is s cut to at most n characters, an ellipsis ending one that was longer.
func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

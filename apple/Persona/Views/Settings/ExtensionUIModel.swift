import Foundation
import Observation

// A plugin's declarative UI (docs/extensions.md「声明式 UI」): the plugin sends a View made of a few fixed kinds of items, the
// app draws them with its own design system (ExtensionViewRenderer) and sends the buttons back. Nothing of the plugin
// runs here; its tones become the app's role colors, it gives no colors, sizes or icons.

/// One of GET /api/v1/plugins/-/ui (and a plugin detail's `ui`): something a plugin draws, and where.
nonisolated struct PluginUIPlace: Codable, Hashable, Sendable, Identifiable {
    var plugin: String
    var id: String
    var title: String
    /// plugin_page | launcher_section | nav_page | overview_card | machine_section | inbox_item (docs/extensions.md「UI 插槽」);
    /// one the app does not know is left out where it lists them.
    var placement: String?
    /// The plugin's name (its entries say whose they are).
    var pluginName: String?
    /// compact (a card) | full.
    var size: String?
    /// A nav_page's: plugin | chart | gauge | list | bell | doc | calendar | money; anything else draws as plugin.
    var icon: String?
    /// An overview_card's: the id of the plugin's nav_page / plugin_page that tapping the card opens.
    var opens: String?
    /// A nav_page entry's badge, set by the plugin (`plugin_ui` events change it).
    var badge: PluginBadge?
    /// An inbox_item's: the connector whose inbox items it draws (an item's `source`).
    var connector: String?

    enum CodingKeys: String, CodingKey {
        case plugin, id, title, placement, size, icon, opens, badge, connector
        case pluginName = "plugin_name"
    }

    private enum NewKeys: String, CodingKey {
        case `extension`
        case extensionName = "extension_name"
    }

    /// `extension` / `extension_name` under /api/v1/extensions, `plugin` / `plugin_name` under the old path (#1844).
    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        let n = try decoder.container(keyedBy: NewKeys.self)
        plugin = try n.decodeIfPresent(String.self, forKey: .extension) ?? c.decode(String.self, forKey: .plugin)
        pluginName = try n.decodeIfPresent(String.self, forKey: .extensionName) ?? c.decodeIfPresent(String.self, forKey: .pluginName)
        id = try c.decode(String.self, forKey: .id)
        title = try c.decode(String.self, forKey: .title)
        placement = try c.decodeIfPresent(String.self, forKey: .placement)
        size = try c.decodeIfPresent(String.self, forKey: .size)
        icon = try c.decodeIfPresent(String.self, forKey: .icon)
        opens = try c.decodeIfPresent(String.self, forKey: .opens)
        badge = try c.decodeIfPresent(PluginBadge.self, forKey: .badge)
        connector = try c.decodeIfPresent(String.self, forKey: .connector)
    }

    /// The slots this app draws (anything else from a newer hub is ignored).
    static let slots: Set<String> = ["plugin_page", "launcher_section", "nav_page", "overview_card", "machine_section", "inbox_item"]
}

/// Where and for whom a plugin's View is drawn (docs/extensions.md「UI 插槽」上下文): the hub checks it and hands it on.
nonisolated struct PluginUIContext: Encodable, Hashable, Sendable {
    /// launcher_section's launcher name.
    var launcher: String?
    /// machine_section's machine id.
    var machine: String?
    /// inbox_item's: the id of the inbox item drawn.
    var item: String?
    /// compact (a card) | full; nil: the slot's own.
    var size: String?
    var tz: String? = TimeZone.current.identifier
    var locale: String? = PluginUIContext.appLocale
    var platform: String? = PluginUIContext.platform
    /// The UI version this app draws: 2 has the slots, chart, stat_grid and refresh_after.
    var uiVersion: Int = PluginUIContext.version

    static let version = 2

    enum CodingKeys: String, CodingKey {
        case launcher, machine, item, size, tz, locale, platform
        case uiVersion = "ui_version"
    }

    static var platform: String {
        #if os(macOS)
        "mac"
        #else
        "ios"
        #endif
    }

    /// The app's language as a BCP 47 tag ("zh-Hans-CN"): the language the app is shown in, with the region.
    static var appLocale: String {
        let lang = Bundle.main.preferredLocalizations.first ?? Locale.current.language.languageCode?.identifier ?? "zh-Hans"
        guard let region = Locale.current.region?.identifier, !lang.contains("-\(region)") else { return lang }
        return lang + "-" + region
    }
}

/// A View: sections of items, and the plugin's own state, sent back as it was.
nonisolated struct ExtensionUIView: Codable, Hashable, Sendable {
    var title: String?
    var sections: [ExtensionUISection]
    var state: String?
    /// Seconds after which the app draws it again while it is on screen (the hub keeps it at 30 or more); nil or 0: never.
    var refreshAfter: Int?

    enum CodingKeys: String, CodingKey {
        case title, sections, state
        case refreshAfter = "refresh_after"
    }
}

nonisolated struct ExtensionUISection: Codable, Hashable, Sendable {
    var title: String?
    var footer: String?
    var items: [PluginUIItem]
}

/// One item: text | metric | progress | row | button | link | field | divider, with the fields of its kind.
nonisolated struct PluginUIItem: Codable, Hashable, Sendable {
    var type: String
    var text: String?
    /// text's: body | secondary | caption | code
    var style: String?
    var label: String?
    var value: String?
    var detail: String?
    /// neutral | positive | caution | danger
    var tone: String?
    var ratio: Double?
    var title: String?
    var subtitle: String?
    var badge: Badge?
    var action: ExtensionUIAction?
    /// button's: normal | destructive
    var role: String?
    var url: String?
    /// field's
    var name: String?
    /// field's: text | secret | number | toggle | choice
    var kind: String?
    var choices: [Choice]?
    var placeholder: String?
    /// chart's (its `kind` is bar | line, its `title` above it, its `detail` below).
    var series: [ChartSeries]?
    var unit: String?
    var stacked: Bool?
    /// stat_grid's.
    var stats: [Stat]?

    nonisolated struct ChartSeries: Codable, Hashable, Sendable {
        var name: String?
        var tone: String?
        var points: [ChartPoint]
    }

    nonisolated struct ChartPoint: Codable, Hashable, Sendable {
        var label: String
        var value: Double
        /// How the value is written ("¥1.20"); nil: a compact number with the unit.
        var display: String?
    }

    nonisolated struct Stat: Codable, Hashable, Sendable {
        var label: String
        var value: String
        var detail: String?
        var tone: String?
    }

    nonisolated struct Badge: Codable, Hashable, Sendable {
        var text: String
        var tone: String?
    }

    nonisolated struct Choice: Codable, Hashable, Sendable, Identifiable {
        var value: String
        var title: String?
        var id: String { value }
        var label: String { title?.isEmpty == false ? title! : value }
    }
}

/// What a button or a row does: `confirm` is asked first; `submit` sends the page's fields along.
nonisolated struct ExtensionUIAction: Codable, Hashable, Sendable {
    var id: String
    var confirm: String?
    var submit: Bool?
}

/// `persona://inbox/<item id>/source` (the id path-escaped): an inbox_item page asking to open its item's own source
/// page, what opening the item showed before the slot (a Feishu chat at the item's first message). The only `open_url`
/// that is not https, and only for the item the page is drawn for (docs/extensions.md「收件箱条目」).
nonisolated enum InboxSourceLink {
    /// The item `url` opens the source of, nil when it is no such link.
    static func item(_ url: URL) -> String? {
        guard url.scheme?.lowercased() == "persona",
              let c = URLComponents(url: url, resolvingAgainstBaseURL: false), c.host?.lowercased() == "inbox" else { return nil }
        let parts = c.percentEncodedPath.split(separator: "/", omittingEmptySubsequences: false)
        guard parts.count == 3, parts[0].isEmpty, parts[2] == "source",
              let id = String(parts[1]).removingPercentEncoding, !id.isEmpty else { return nil }
        return id
    }
}

/// The answer to a button: a new view, a word to show, a link to open (none: draw again).
nonisolated struct ExtensionUIResult: Decodable, Sendable {
    var view: ExtensionUIView?
    var toast: String?
    var openURL: String?

    enum CodingKeys: String, CodingKey {
        case view, toast
        case openURL = "open_url"
    }
}

extension APIClient {
    /// What enabled plugins draw at a placement (a launcher's section: only the plugins whose gateway it is).
    /// No placement: every slot's but launchers' sections (docs/extensions.md「UI 插槽」). Placements this app does not draw
    /// are left out.
    func pluginUI(placement: String?, launcher: String? = nil, machine: String? = nil) async throws -> [PluginUIPlace] {
        struct List: Decodable { var ui: [PluginUIPlace]? }
        let query = ["placement": placement, "launcher": launcher, "machine": machine].compactMapValues { $0?.isEmpty == false ? $0 : nil }
        let list: List = try await get(extensionPath("/-/ui"), query: query)
        return (list.ui ?? []).filter { $0.placement.map(PluginUIPlace.slots.contains) ?? true }
    }

    /// Draws (no action) or presses a button (an action, with the fields' values when it submits).
    func pluginUI(_ plugin: String, _ ui: String, context: PluginUIContext, state: String?, action: String? = nil,
                  values: [String: String]? = nil) async throws -> ExtensionUIResult {
        struct Body: Encodable {
            var context: PluginUIContext
            var state: String?
            var action: String?
            var values: [String: String]?
        }
        let body = Body(context: context, state: state, action: action, values: values)
        return try JSON.decoder.decode(ExtensionUIResult.self, from: try await request("POST", extensionPath("/\(plugin)/-/ui/\(ui)"), body: body))
    }
}

/// One plugin view on a page: drawing it, the fields' values as typed, pressing its buttons.
@Observable final class ExtensionViewModel {
    struct Deps {
        var api: () -> APIClient?
        static var live: Deps { .init(api: { AppModel.shared.api }) }
    }

    /// What pressing a button came to, for the page to show: a word (a toast), a link to open in the app, or why it failed.
    struct Outcome: Equatable {
        var toast: String?
        var url: URL?
        var error: String?
    }

    let plugin: String
    let ui: String
    /// Where it is drawn: the launcher or machine, the size, and the app's time zone, language and platform.
    let context: PluginUIContext
    private(set) var view: ExtensionUIView?
    private(set) var loading = false
    private(set) var error: String?
    /// The hub's status when the last draw failed (403, 404, 502…); nil when it did not fail or never got an answer.
    private(set) var failedStatus: Int?
    /// The plugin says this is not its business (a launcher whose gateway is not its own: 404 `unsupported`): the
    /// section is not drawn at all, and that is no error.
    private(set) var unsupported = false
    /// The button being pressed (its spinner), by action id.
    private(set) var running: String?
    /// The fields' values by name (a toggle "true" / "false"); a secret starts empty, whatever the plugin sends.
    var values: [String: String] = [:]
    @ObservationIgnored private let deps: Deps

    /// When the view on screen was last drawn (its `refresh_after` counts from here).
    private(set) var drawnAt: Date?

    init(plugin: String, ui: String, context: PluginUIContext = .init(), deps: Deps = .live) {
        self.plugin = plugin
        self.ui = ui
        self.context = context
        self.deps = deps
    }

    func load() async {
        guard !loading else { return }
        loading = true
        defer { loading = false }
        do {
            let r = try await call(nil, values: nil)
            failedStatus = nil
            show(r.view)
            if r.view == nil { error = "扩展没有返回内容" }
        } catch let e as APIError where Self.isUnsupported(e) || (context.machine != nil && e.status == 404) {
            // A machine's section the hub says is not there (404: no such machine, or not the plugin's business) is
            // not drawn at all.
            unsupported = true
            error = nil
            failedStatus = e.status
        } catch {
            self.error = error.localizedDescription
            failedStatus = (error as? APIError)?.status
        }
    }

    /// Presses a button (asked first by the page when it has `confirm`): a new view replaces this one, nothing in the
    /// answer draws it again.
    func perform(_ action: ExtensionUIAction, with chosen: [String: String] = [:]) async -> Outcome {
        guard running == nil else { return Outcome() }
        running = action.id
        defer { running = nil }
        do {
            let sent = (action.submit == true ? submitted : [:]).merging(chosen) { _, c in c }
            let r = try await call(action.id, values: sent.isEmpty ? nil : sent)
            if let v = r.view { show(v) } else if r.toast == nil && r.openURL == nil { await redraw() }
            let url = r.openURL.flatMap(URL.init(string:)).flatMap { Self.opens($0, item: context.item) ? $0 : nil }
            return Outcome(toast: r.toast?.isEmpty == false ? r.toast : nil, url: url)
        } catch let e as APIError where Self.isUnsupported(e) {
            unsupported = true
            return Outcome()
        } catch {
            return Outcome(error: error.localizedDescription)
        }
    }

    /// A `segmented` picked: shown picked at once, and its action sent with `{name: value}` (and the page's fields when it
    /// submits); the plugin answers with the view for it. Picking the one already picked does nothing.
    func choose(_ item: PluginUIItem, _ value: String) async -> Outcome {
        guard let name = item.name, let action = item.action, values[name] != value else { return Outcome() }
        let before = values[name]
        values[name] = value
        let o = await perform(action, with: [name: value])
        if o.error != nil { values[name] = before }
        return o
    }

    /// The fields' values as the plugin gets them: every field of the view, by name.
    var submitted: [String: String] {
        var out: [String: String] = [:]
        for item in fields { if let n = item.name { out[n] = values[n] ?? "" } }
        return out
    }

    private var fields: [PluginUIItem] { (view?.sections ?? []).flatMap(\.items).filter { $0.type == "field" && $0.name != nil } }

    private func redraw() async {
        if let r = try? await call(nil, values: nil), let v = r.view { show(v) }
    }

    /// When the view asks to be drawn again (`refresh_after`), nil: it does not.
    var refreshDue: Date? {
        guard let s = view?.refreshAfter, s > 0, let at = drawnAt else { return nil }
        return at.addingTimeInterval(TimeInterval(max(s, Self.refreshFloor)))
    }

    /// The hub keeps refresh_after at 30 s or more; so does the app, against a hub that does not.
    static let refreshFloor = 30

    /// Draws it again because its `refresh_after` is up: what the user is typing in its fields stays, a failure keeps the
    /// view that is there (the next one tries again).
    func refresh() async {
        guard !loading, running == nil else { return }
        loading = true
        defer { loading = false }
        guard let r = try? await call(nil, values: nil), let v = r.view else {
            drawnAt = .now
            return
        }
        show(v, keeping: values)
    }

    private func show(_ v: ExtensionUIView?, keeping typed: [String: String] = [:]) {
        guard let v else { return }
        view = v
        drawnAt = .now
        error = nil
        unsupported = false
        var next: [String: String] = [:]
        for item in (v.sections).flatMap(\.items) {
            guard let n = item.name else { continue }
            switch item.type {
            case "field": next[n] = typed[n] ?? (item.kind == "secret" ? "" : (item.value ?? (item.kind == "toggle" ? "false" : "")))
            // What the plugin says is picked (its answer to a pick, or its own state).
            case "segmented": next[n] = item.value ?? ""
            default: break
            }
        }
        values = next
    }

    private func call(_ action: String?, values: [String: String]?) async throws -> ExtensionUIResult {
        guard let api = deps.api() else { throw APIError(status: 0, message: "还没连接 hub") }
        return try await api.pluginUI(plugin, ui, context: context, state: view?.state, action: action, values: values)
    }

    static func isUnsupported(_ e: APIError) -> Bool { e.status == 404 && e.code == "unsupported" }

    /// What a button's `open_url` may open: an https page; on an inbox item's page (`item`), also that item's own
    /// source (`InboxSourceLink`), never another item's.
    static func opens(_ url: URL, item: String?) -> Bool {
        if url.scheme?.lowercased() == "https" { return true }
        guard let item, let source = InboxSourceLink.item(url) else { return false }
        return source == item
    }
}

/// The sections a launcher's page shows from plugins (placement launcher_section).
@Observable final class ExtensionLauncherModel {
    let launcher: String
    private(set) var places: [PluginUIPlace] = []
    private(set) var models: [String: ExtensionViewModel] = [:]
    @ObservationIgnored private let deps: ExtensionViewModel.Deps

    init(launcher: String, deps: ExtensionViewModel.Deps = .live) {
        self.launcher = launcher
        self.deps = deps
    }

    func load() async {
        guard let api = deps.api(), let list = try? await api.pluginUI(placement: "launcher_section", launcher: launcher) else { return }
        places = list
        for p in list where models[key(p)] == nil {
            models[key(p)] = ExtensionViewModel(plugin: p.plugin, ui: p.id, context: PluginUIContext(launcher: launcher), deps: deps)
        }
        // Each draws on its own: a slow plugin does not hold the others' sections.
        let pending = list.compactMap { models[key($0)] }
        await withTaskGroup(of: Void.self) { group in
            for m in pending { group.addTask { await m.load() } }
        }
    }

    struct Entry: Identifiable {
        var place: PluginUIPlace
        var model: ExtensionViewModel
        var id: String { place.plugin + "/" + place.id }
    }

    /// The sections to draw: a plugin that said 404 unsupported is left out.
    var shown: [Entry] {
        places.compactMap { p in models[key(p)].flatMap { $0.unsupported ? nil : Entry(place: p, model: $0) } }
    }

    private func key(_ p: PluginUIPlace) -> String { p.plugin + "/" + p.id }
}

import Foundation
import Observation

// A plugin's declarative UI (docs/extensions.md「声明式 UI」): the plugin sends a View made of a few fixed kinds of items, the
// app draws them with its own design system (ExtensionViewRenderer) and sends the buttons back. Nothing of the plugin
// runs here; its tones become the app's role colors, it gives no colors, sizes or icons.

// The View, its components, actions and places are the schema's (apple/Shared/Generated/ExtensionWire.swift,
// schema/extension/view.schema.json); what follows is how the app reads them.

nonisolated extension ExtensionUIPlace: Identifiable {
    /// The extension that draws it: `extension` under /api/v1/extensions, `plugin` under the old path (#1844).
    var owner: String { self.extension?.isEmpty == false ? self.extension! : (plugin ?? "") }
    /// The extension's name (its entries say whose they are).
    var ownerName: String? { extensionName?.isEmpty == false ? extensionName : pluginName }

    /// The slots this app draws (anything else from a newer hub is ignored). `extension_page` is UI version 3's name for
    /// `plugin_page` (docs/extensions.md「UI 版本 3」兼容).
    static let slots: Set<String> = ["plugin_page", "extension_page", "launcher_section", "nav_page", "overview_card",
                                     "machine_section", "inbox_item"]

    /// An extension's own page in 设置 › 扩展 › it.
    var isExtensionPage: Bool { placement.isEmpty || placement == "plugin_page" || placement == "extension_page" }
}

nonisolated extension ExtensionUIContext {
    /// The UI version a whole page of the app draws (`ExtensionPageView`): 4 adds the code-hosting parts (header,
    /// breadcrumb, file_tree, code, diff, comment_thread, composer, tabs, the split layout) and actions' own values
    /// (#2961). Cards, widgets, the watch and the extensions keep `version` (Shared, 3): they draw none of them.
    static let pageVersion = 4

    /// This context for a whole page: the page's UI version.
    func forPage() -> Self {
        var c = self
        if c.size == nil || c.size == "full" { c.uiVersion = Self.pageVersion }
        return c
    }
}

nonisolated extension ExtensionChoice: Identifiable {
    var id: String { value }
    var label: String { title?.isEmpty == false ? title! : value }
}

nonisolated extension ExtensionComponent {
    /// The name a field or a segmented sends its value under.
    var valueName: String? {
        switch self {
        case .field(let f): f.name.isEmpty ? nil : f.name
        case .segmented(let s): s.name.isEmpty ? nil : s.name
        default: nil
        }
    }

    /// The component's `key` (unique on the page): what keeps its place when the page is drawn again.
    var key: String? {
        switch self {
        case .text(let c): c.key
        case .metric(let c): c.key
        case .progress(let c): c.key
        case .row(let c): c.key
        case .button(let c): c.key
        case .link(let c): c.key
        case .field(let c): c.key
        case .divider(let c): c.key
        case .chart(let c): c.key
        case .statGrid(let c): c.key
        case .segmented(let c): c.key
        case .markdown(let c): c.key
        case .table(let c): c.key
        case .empty(let c): c.key
        case .loading(let c): c.key
        case .html(let c): c.key
        case .header(let c): c.key
        case .breadcrumb(let c): c.key
        case .fileTree(let c): c.key
        case .code(let c): c.key
        case .diff(let c): c.key
        case .commentThread(let c): c.key
        case .composer(let c): c.key
        case .unknown: nil
        }
    }
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

extension APIClient {
    /// What enabled extensions draw at a placement (a launcher's section: only the extensions whose gateway it is).
    /// No placement: every slot's but launchers' sections (docs/extensions.md「UI 插槽」). Placements this app does not
    /// draw are left out.
    func pluginUI(placement: String?, launcher: String? = nil, machine: String? = nil) async throws -> [ExtensionUIPlace] {
        let query = ["placement": placement, "launcher": launcher, "machine": machine].compactMapValues { $0?.isEmpty == false ? $0 : nil }
        let list: ExtensionUIPlaces = try await get(extensionPath("/-/ui"), query: query)
        return list.ui.filter { $0.placement.isEmpty || ExtensionUIPlace.slots.contains($0.placement) }
    }

    /// Draws (no action) or uses a control (an action, with the fields' values it sends).
    func pluginUI(_ ext: String, _ ui: String, context: ExtensionUIContext, state: String?, action: String? = nil,
                  values: [String: String]? = nil) async throws -> ExtensionActionResult {
        let body = ExtensionUIRequest(context: context, state: state, action: action, values: values)
        return try JSON.decoder.decode(ExtensionActionResult.self, from: try await request("POST", extensionPath("/\(ext)/-/ui/\(ui)"), body: body))
    }
}

/// The extension models alive now, so that `ui.invalidate` reaches the ones drawing that extension (docs/extensions.md
/// 「ui.invalidate」). A model on screen draws again at once, one that is not when it is next shown (`ExtensionRefresh`).
@MainActor final class ExtensionUIRegistry {
    static let shared = ExtensionUIRegistry()
    private var models: [WeakModel] = []

    private struct WeakModel { weak var model: ExtensionViewModel? }

    func add(_ model: ExtensionViewModel) {
        models.removeAll { $0.model == nil }
        models.append(WeakModel(model: model))
    }

    /// The extension's data changed: `ui` the one of its interfaces, nil all of them.
    func invalidate(extension ext: String, ui: String?, group: String = "") {
        models.removeAll { $0.model == nil }
        for case let m? in models.map(\.model)
            where m.plugin == ext && (ui == nil || ui == m.ui) && m.group == group {
            m.invalidate()
        }
    }
}

/// One extension view on a page: drawing it, the fields' values as typed, using its controls (buttons, rows, search,
/// filter, load more), the page it asks to push or to pop back from.
@Observable final class ExtensionViewModel {
    struct Deps {
        var api: () -> APIClient?
        static var live: Deps { .init(api: { AppModel.shared.api }) }
    }

    /// What using a control came to, for the page to show: a word (a toast), a link to open in the app, or why it failed.
    struct Outcome: Equatable {
        var toast: String?
        var url: URL?
        var error: String?
    }

    let plugin: String
    let ui: String
    /// Where it is drawn: the launcher or machine, the size, and the app's time zone, language, platform and UI version.
    let context: ExtensionUIContext
    /// How deep in its navigation stack the page is (the first is 1): no push past `maxDepth`.
    let depth: Int
    private(set) var view: ExtensionView?
    private(set) var loading = false
    private(set) var error: String?
    /// The hub's status when the last draw failed (403, 404, 502…); nil when it did not fail or never got an answer.
    private(set) var failedStatus: Int?
    /// The extension says this is not its business (a launcher whose gateway is not its own: 404 `unsupported`): the
    /// section is not drawn at all, and that is no error.
    private(set) var unsupported = false
    /// The control being used (its spinner), by action id.
    private(set) var running: String?
    /// The fields' and segmented's values by name (a toggle "true" / "false"); a secret starts empty, whatever the
    /// extension sends.
    var values: [String: String] = [:]
    /// What is typed in the page's search field.
    var query = ""
    /// The search the extension last answered or was last asked (the View's `search.value`): what is typed and not yet
    /// asked is not this, and a View drawn meanwhile leaves it alone.
    private(set) var searchedQuery = ""
    /// The page the extension asks to show on top of this one (`push`), shown by the page that hosts this view.
    var pushed: ExtensionPush?
    /// The extension asked to go back (`pop: true`): the page that hosts this view closes itself.
    var popped = false
    /// `ui.invalidate` came for it: drawn again at once while on screen, else when it is next shown.
    private(set) var stale = false
    /// 每次失效都推进版本，合并请求期间到达的第二次事件不会被第一次刷新吞掉。
    private(set) var invalidationVersion = 0
    /// 忙碌期间暂缓的刷新，在当前操作结束后重试；开始请求时不改变任务键。
    private(set) var completedOperations = 0
    var group: String { deps.api()?.group ?? "" }

    func invalidate() {
        invalidationVersion += 1
        stale = true
    }
    /// It pushed a page by value (`splitPush`): drawn again when it is shown again.
    @ObservationIgnored var returning = false
    /// A section's `more` being loaded, by its action id: not asked again until it answers.
    private(set) var loadingMore: String?
    @ObservationIgnored private let deps: Deps
    /// The state the page starts with (a pushed page's `push.state`).
    @ObservationIgnored private let initialState: String?

    /// When the view on screen was last drawn (its `refresh_after` counts from here).
    private(set) var drawnAt: Date?

    static let maxDepth = 8

    init(plugin: String, ui: String, context: ExtensionUIContext = .here(), state: String? = nil, depth: Int = 1,
         deps: Deps = .live) {
        self.plugin = plugin
        self.ui = ui
        self.context = context
        self.initialState = state
        self.depth = depth
        self.deps = deps
        ExtensionUIRegistry.shared.add(self)
    }

    func load() async {
        guard !loading else { return }
        loading = true
        defer { loading = false; completedOperations += 1 }
        do {
            let r = try await call(nil, values: nil)
            failedStatus = nil
            show(r.view)
            if r.view == nil { error = String(localized: "扩展没有返回内容") }
        } catch let e as APIError where Self.isUnsupported(e) || (context.machine != nil && e.status == 404) {
            // A machine's section the hub says is not there (404: no such machine, or not the extension's business)
            // is not drawn at all.
            unsupported = true
            error = nil
            failedStatus = e.status
        } catch {
            self.error = error.localizedDescription
            failedStatus = (error as? APIError)?.status
        }
    }

    /// Uses a button or row (asked first by the page when it has `confirm`). A `push` opens its page without asking the
    /// extension; otherwise a new view replaces this one, and nothing in the answer draws it again.
    func perform(_ action: ExtensionAction, with chosen: [String: String] = [:]) async -> Outcome {
        if let push = action.push {
            if depth < Self.maxDepth { pushed = push }
            return Outcome()
        }
        guard running == nil else { return Outcome() }
        running = action.id
        defer { running = nil; completedOperations += 1 }
        do {
            // The page's fields (submit), then the component's own values (a path, a line, what was written), then the
            // action's own (UI version 4): the later wins (docs/extensions.md「UI 版本 4」).
            let sent = (action.submit == true ? submitted : [:]).merging(chosen) { _, c in c }
                .merging(action.values ?? [:]) { _, own in own }
            let r = try await call(action.id, values: sent.isEmpty ? nil : sent)
            return await take(r)
        } catch let e as APIError where Self.isUnsupported(e) {
            unsupported = true
            return Outcome()
        } catch {
            return Outcome(error: error.localizedDescription)
        }
    }

    /// An answer to a control: its view, its page to push or the way back, its word and link.
    /// `searched`: the answer to the search, which need not say again what was searched for (its `q` is in its state).
    private func take(_ r: ExtensionActionResult, searched: Bool = false) async -> Outcome {
        if let v = r.view { show(v, keeping: [:], searched: searched) }
        if let p = r.push, depth < Self.maxDepth { pushed = p }
        if r.pop == true { popped = true }
        if r.view == nil && r.toast == nil && r.openUrl == nil && r.push == nil && r.pop != true { await redraw() }
        let url = r.openUrl.flatMap(URL.init(string:)).flatMap { Self.opens($0, item: context.item) ? $0 : nil }
        return Outcome(toast: r.toast?.isEmpty == false ? r.toast : nil, url: url)
    }

    /// A `segmented` picked: shown picked at once, and its action sent with `{name: value}` (and the page's fields when it
    /// submits); the extension answers with the view for it. Picking the one already picked does nothing.
    func choose(_ name: String, _ value: String, action: ExtensionAction) async -> Outcome {
        guard values[name] != value else { return Outcome() }
        let before = values[name]
        values[name] = value
        let o = await perform(action, with: [name: value])
        if o.error != nil { values[name] = before }
        return o
    }

    /// The page's tabs (UI version 4): picked at once, its action sent with `{name: value}` and the search.
    func pickTab(_ value: String) async -> Outcome {
        guard let t = view?.tabs, values[t.name] != value else { return Outcome() }
        let before = values[t.name]
        values[t.name] = value
        var sent = [t.name: value]
        if view?.search != nil { sent["q"] = query }
        let o = await perform(t.action, with: sent)
        if o.error != nil { values[t.name] = before }
        return o
    }

    /// The page's search: its action with `{q}` (and the filter's value), once the typing pauses (the page waits). A
    /// newer search makes an older one's answer moot: only the last one's is shown.
    func search() async -> Outcome {
        guard let s = view?.search else { return Outcome() }
        searches += 1
        let mine = searches
        defer { completedOperations += 1 }
        searchedQuery = query
        var sent = ["q": query]
        if let f = view?.filter { sent[f.name] = values[f.name] ?? f.value }
        do {
            let r = try await call(s.action.id, values: sent)
            guard mine == searches else { return Outcome() }
            return await take(r, searched: true)
        } catch {
            return mine == searches ? Outcome(error: error.localizedDescription) : Outcome()
        }
    }

    @ObservationIgnored private var searches = 0

    /// The page's filter picked: shown picked at once, its action sent with `{name: value}` and what is in the search
    /// field.
    func filter(_ value: String) async -> Outcome {
        guard let f = view?.filter, values[f.name] != value else { return Outcome() }
        let before = values[f.name]
        values[f.name] = value
        var sent = [f.name: value]
        if view?.search != nil { sent["q"] = query }
        let o = await perform(f.action, with: sent)
        if o.error != nil { values[f.name] = before }
        return o
    }

    /// A section's `more` (scrolled to its end, or tapped): asked once until it answers; the extension answers with the
    /// whole new view.
    func more(_ m: ExtensionMore) async -> Outcome {
        guard loadingMore == nil, running == nil else { return Outcome() }
        loadingMore = m.action.id
        defer { loadingMore = nil; completedOperations += 1 }
        do {
            var sent: [String: String] = [:]
            if view?.search != nil { sent["q"] = query }
            if let f = view?.filter { sent[f.name] = values[f.name] ?? f.value }
            let r = try await call(m.action.id, values: sent.isEmpty ? nil : sent)
            return await take(r)
        } catch {
            return Outcome(error: error.localizedDescription)
        }
    }

    /// The fields' values as the extension gets them: every field of the view, by name.
    var submitted: [String: String] {
        var out: [String: String] = [:]
        for item in components {
            if case .field(let f) = item, !f.name.isEmpty { out[f.name] = values[f.name] ?? "" }
            if case .composer(let c) = item, !c.name.isEmpty { out[c.name] = values[c.name] ?? "" }
        }
        return out
    }

    private var components: [ExtensionComponent] { (view?.sections ?? []).flatMap(\.items) }

    private func redraw() async {
        if let r = try? await call(nil, values: nil), let v = r.view { show(v, keeping: values) }
    }

    /// When the view asks to be drawn again (`refresh_after`), nil: it does not.
    var refreshDue: Date? {
        guard let s = view?.refreshAfter, s > 0, let at = drawnAt else { return nil }
        return at.addingTimeInterval(TimeInterval(max(s, Self.refreshFloor)))
    }

    /// The hub keeps refresh_after at 30 s or more; so does the app, against a hub that does not.
    static let refreshFloor = 30

    /// Draws it again because its `refresh_after` is up, `ui.invalidate` came or the page on top of it closed: what the
    /// user is typing in its fields stays, a failure keeps the view that is there (the next one tries again).
    func refresh() async {
        guard !loading, running == nil, loadingMore == nil else { return }
        let version = invalidationVersion
        loading = true
        defer { loading = false; completedOperations += 1 }
        let result = try? await call(nil, values: nil)
        guard !Task.isCancelled else { return }
        // 请求结束后才处理失效标记，避免改变 SwiftUI 任务键而取消自己的网络请求。
        if version == invalidationVersion { stale = false }
        guard let v = result?.view else {
            drawnAt = .now
            return
        }
        show(v, keeping: values)
    }

    private func show(_ v: ExtensionView?, keeping typed: [String: String] = [:], searched: Bool = false) {
        guard let v else { return }
        view = v
        drawnAt = .now
        error = nil
        unsupported = false
        var next: [String: String] = [:]
        for item in v.sections.flatMap(\.items) {
            switch item {
            case .field(let f) where !f.name.isEmpty:
                next[f.name] = typed[f.name] ?? (f.kind == "secret" ? "" : (f.value ?? (f.kind == "toggle" ? "false" : "")))
            // What the extension says is picked (its answer to a pick, or its own state).
            case .segmented(let s) where !s.name.isEmpty: next[s.name] = s.value
            // A composer keeps what is being typed in it; what the extension puts there, otherwise.
            case .composer(let c) where !c.name.isEmpty: next[c.name] = typed[c.name] ?? c.value ?? ""
            default: break
            }
        }
        if let f = v.filter, !f.name.isEmpty { next[f.name] = f.value }
        if let t = v.tabs, !t.name.isEmpty { next[t.name] = t.value }
        values = next
        // The search field says what the View was drawn for (none: nothing), unless something else is being typed in it
        // or this is the search's own answer.
        if !searched, query == searchedQuery {
            query = v.search?.value ?? ""
            searchedQuery = query
        }
    }

    private func call(_ action: String?, values: [String: String]?) async throws -> ExtensionActionResult {
        guard let api = deps.api() else { throw APIError(status: 0, message: String(localized: "还没连接 hub")) }
        return try await api.pluginUI(plugin, ui, context: context, state: view?.state ?? initialState, action: action, values: values)
    }

    static func isUnsupported(_ e: APIError) -> Bool { e.status == 404 && e.code == "unsupported" }

    /// What a control's `open_url` may open: an https page; on an inbox item's page (`item`), also that item's own
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
    private(set) var places: [ExtensionUIPlace] = []
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
            models[key(p)] = ExtensionViewModel(plugin: p.owner, ui: p.id, context: .here(launcher: launcher), deps: deps)
        }
        // Each draws on its own: a slow plugin does not hold the others' sections.
        let pending = list.compactMap { models[key($0)] }
        await withTaskGroup(of: Void.self) { group in
            for m in pending { group.addTask { await m.load() } }
        }
    }

    struct Entry: Identifiable {
        var place: ExtensionUIPlace
        var model: ExtensionViewModel
        var id: String { place.owner + "/" + place.id }
    }

    /// The sections to draw: a plugin that said 404 unsupported is left out.
    var shown: [Entry] {
        places.compactMap { p in models[key(p)].flatMap { $0.unsupported ? nil : Entry(place: p, model: $0) } }
    }

    private func key(_ p: ExtensionUIPlace) -> String { p.owner + "/" + p.id }
}

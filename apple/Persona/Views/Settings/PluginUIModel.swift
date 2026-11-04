import Foundation
import Observation

// A plugin's declarative UI (docs/plugins.md「声明式 UI」): the plugin sends a View made of a few fixed kinds of items, the
// app draws them with its own design system (PluginViewRenderer) and sends the buttons back. Nothing of the plugin
// runs here; its tones become the app's role colors, it gives no colors, sizes or icons.

/// One of GET /api/v1/plugins/-/ui (and a plugin detail's `ui`): something a plugin draws, and where.
nonisolated struct PluginUIPlace: Codable, Hashable, Sendable, Identifiable {
    var plugin: String
    var id: String
    var title: String
    /// plugin_page | launcher_section
    var placement: String?
}

/// A View: sections of items, and the plugin's own state, sent back as it was.
nonisolated struct PluginUIView: Codable, Hashable, Sendable {
    var title: String?
    var sections: [PluginUISection]
    var state: String?
}

nonisolated struct PluginUISection: Codable, Hashable, Sendable {
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
    var action: PluginUIAction?
    /// button's: normal | destructive
    var role: String?
    var url: String?
    /// field's
    var name: String?
    /// field's: text | secret | number | toggle | choice
    var kind: String?
    var choices: [Choice]?
    var placeholder: String?

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
nonisolated struct PluginUIAction: Codable, Hashable, Sendable {
    var id: String
    var confirm: String?
    var submit: Bool?
}

/// The answer to a button: a new view, a word to show, a link to open (none: draw again).
nonisolated struct PluginUIResult: Decodable, Sendable {
    var view: PluginUIView?
    var toast: String?
    var openURL: String?

    enum CodingKeys: String, CodingKey {
        case view, toast
        case openURL = "open_url"
    }
}

extension APIClient {
    /// What enabled plugins draw at a placement (a launcher's section: only the plugins whose gateway it is).
    func pluginUI(placement: String, launcher: String? = nil, machine: String? = nil) async throws -> [PluginUIPlace] {
        struct List: Decodable { var ui: [PluginUIPlace]? }
        let list: List = try await get("/api/v1/plugins/-/ui",
                                       query: ["placement": placement, "launcher": launcher ?? "", "machine": machine ?? ""])
        return list.ui ?? []
    }

    /// Draws (no action) or presses a button (an action, with the fields' values when it submits).
    func pluginUI(_ plugin: String, _ ui: String, context: [String: String]?, state: String?, action: String? = nil,
                  values: [String: String]? = nil) async throws -> PluginUIResult {
        struct Body: Encodable {
            var context: [String: String]?
            var state: String?
            var action: String?
            var values: [String: String]?
        }
        let body = Body(context: context?.isEmpty == false ? context : nil, state: state, action: action, values: values)
        return try JSON.decoder.decode(PluginUIResult.self, from: try await request("POST", "/api/v1/plugins/\(plugin)/-/ui/\(ui)", body: body))
    }
}

/// One plugin view on a page: drawing it, the fields' values as typed, pressing its buttons.
@Observable final class PluginViewModel {
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
    /// launcher / machine, for a launcher's section.
    let context: [String: String]
    private(set) var view: PluginUIView?
    private(set) var loading = false
    private(set) var error: String?
    /// The plugin says this is not its business (a launcher whose gateway is not its own: 404 `unsupported`): the
    /// section is not drawn at all, and that is no error.
    private(set) var unsupported = false
    /// The button being pressed (its spinner), by action id.
    private(set) var running: String?
    /// The fields' values by name (a toggle "true" / "false"); a secret starts empty, whatever the plugin sends.
    var values: [String: String] = [:]
    @ObservationIgnored private let deps: Deps

    init(plugin: String, ui: String, context: [String: String] = [:], deps: Deps = .live) {
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
            show(r.view)
            if r.view == nil { error = "插件没有返回内容" }
        } catch let e as APIError where Self.isUnsupported(e) {
            unsupported = true
            error = nil
        } catch {
            self.error = error.localizedDescription
        }
    }

    /// Presses a button (asked first by the page when it has `confirm`): a new view replaces this one, nothing in the
    /// answer draws it again.
    func perform(_ action: PluginUIAction) async -> Outcome {
        guard running == nil else { return Outcome() }
        running = action.id
        defer { running = nil }
        do {
            let r = try await call(action.id, values: action.submit == true ? submitted : nil)
            if let v = r.view { show(v) } else if r.toast == nil && r.openURL == nil { await redraw() }
            let url = r.openURL.flatMap(URL.init(string:)).flatMap { $0.scheme == "https" ? $0 : nil }
            return Outcome(toast: r.toast?.isEmpty == false ? r.toast : nil, url: url)
        } catch let e as APIError where Self.isUnsupported(e) {
            unsupported = true
            return Outcome()
        } catch {
            return Outcome(error: error.localizedDescription)
        }
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

    private func show(_ v: PluginUIView?) {
        guard let v else { return }
        view = v
        error = nil
        unsupported = false
        var next: [String: String] = [:]
        for item in (v.sections).flatMap(\.items) where item.type == "field" {
            guard let n = item.name else { continue }
            next[n] = item.kind == "secret" ? "" : (item.value ?? (item.kind == "toggle" ? "false" : ""))
        }
        values = next
    }

    private func call(_ action: String?, values: [String: String]?) async throws -> PluginUIResult {
        guard let api = deps.api() else { throw APIError(status: 0, message: "还没连接 hub") }
        return try await api.pluginUI(plugin, ui, context: context, state: view?.state, action: action, values: values)
    }

    static func isUnsupported(_ e: APIError) -> Bool { e.status == 404 && e.code == "unsupported" }
}

/// The sections a launcher's page shows from plugins (placement launcher_section).
@Observable final class PluginLauncherModel {
    let launcher: String
    private(set) var places: [PluginUIPlace] = []
    private(set) var models: [String: PluginViewModel] = [:]
    @ObservationIgnored private let deps: PluginViewModel.Deps

    init(launcher: String, deps: PluginViewModel.Deps = .live) {
        self.launcher = launcher
        self.deps = deps
    }

    func load() async {
        guard let api = deps.api(), let list = try? await api.pluginUI(placement: "launcher_section", launcher: launcher) else { return }
        places = list
        for p in list where models[key(p)] == nil {
            models[key(p)] = PluginViewModel(plugin: p.plugin, ui: p.id, context: ["launcher": launcher], deps: deps)
        }
        // Each draws on its own: a slow plugin does not hold the others' sections.
        let pending = list.compactMap { models[key($0)] }
        await withTaskGroup(of: Void.self) { group in
            for m in pending { group.addTask { await m.load() } }
        }
    }

    struct Entry: Identifiable {
        var place: PluginUIPlace
        var model: PluginViewModel
        var id: String { place.plugin + "/" + place.id }
    }

    /// The sections to draw: a plugin that said 404 unsupported is left out.
    var shown: [Entry] {
        places.compactMap { p in models[key(p)].flatMap { $0.unsupported ? nil : Entry(place: p, model: $0) } }
    }

    private func key(_ p: PluginUIPlace) -> String { p.plugin + "/" + p.id }
}

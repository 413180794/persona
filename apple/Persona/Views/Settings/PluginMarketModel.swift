import Foundation
import Observation

// 设置 › 插件 › 插件市场 (docs/plugins.md「插件市场」): the plugins of the markets the hub has, searched and filtered by
// market; one plugin's page with what it may do, installed or upgraded after its preview (the permissions agreed to
// are the preview's digest) and removed when it came from that market; and the markets themselves (added with an
// optional token that is never shown again, refreshed, removed). Only with a hub that says `markets: true`.

/// One of GET /api/v1/plugins/-/markets.
nonisolated struct PluginMarket: Codable, Hashable, Sendable, Identifiable {
    var id: String
    /// What was added: https://… or github:owner/repo.
    var spec: String
    var name: String
    var description: String?
    var homepage: String?
    var hasToken: Bool
    /// How many plugins its index lists.
    var plugins: Int?
    var warnings: [String]?
    /// Why its index could not be read the last time.
    var error: String?
    var fetchedAt: Date?
    var addedAt: Date?

    var title: String { name.isEmpty ? spec : name }

    enum CodingKeys: String, CodingKey {
        case id, spec, name, description, homepage, plugins, warnings, error
        case hasToken = "has_token", fetchedAt = "fetched_at", addedAt = "added_at"
    }
}

/// One plugin of the catalog (`CatalogItem`): what a market says of it, and how it stands on this hub.
nonisolated struct PluginCatalogItem: Codable, Hashable, Sendable, Identifiable {
    var market: String
    var marketName: String?
    var pluginID: String
    var name: String
    var version: String
    var description: String?
    var publisher: String?
    var homepage: String?
    var tags: [String]?
    var contributes: [String: [String]]?
    var permissions: [PluginPermission]?
    /// Why this hub cannot install it (no package for its platform, a newer hub needed).
    var problem: String?
    /// The version installed, if it is.
    var installed: String?
    var installedSource: String?
    var fromThisMarket: Bool?
    var upgradable: Bool?

    var id: String { market + "/" + pluginID }

    enum CodingKeys: String, CodingKey {
        case market, name, version, description, publisher, homepage, tags, contributes, permissions, problem, installed, upgradable
        case pluginID = "id", marketName = "market_name", installedSource = "installed_source", fromThisMarket = "from_this_market"
    }
}

/// GET /api/v1/plugins/-/catalog.
nonisolated struct PluginCatalog: Decodable, Sendable {
    var markets: [PluginMarket]
    var plugins: [PluginCatalogItem]
    var platform: String?

    private enum CodingKeys: String, CodingKey { case markets, plugins, platform }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        markets = try c.decodeIfPresent([PluginMarket].self, forKey: .markets) ?? []
        plugins = try c.decodeIfPresent([PluginCatalogItem].self, forKey: .plugins) ?? []
        platform = try c.decodeIfPresent(String.self, forKey: .platform)
    }
}

/// What installing (or upgrading to) a plugin of a market would be: downloaded and checked, its permissions to agree to.
nonisolated struct PluginPreview: Decodable, Hashable, Sendable, Identifiable {
    struct Manifest: Decodable, Hashable, Sendable {
        var id: String
        var name: String?
        var version: String?
        var description: String?
    }

    var previewID: String
    var manifest: Manifest
    var permissions: [PluginPermission]
    var permissionsDigest: String
    var warnings: [String]?
    var expiresAt: Date?
    var upgrade: Bool?
    /// The version it upgrades from.
    var from: String?
    var morePermissions: Bool?
    /// What the new version may do that the installed one may not.
    var addedPermissions: [PluginPermission]?
    var market: String?

    var id: String { previewID }

    enum CodingKeys: String, CodingKey {
        case manifest, permissions, warnings, upgrade, from, market
        case previewID = "preview_id", permissionsDigest = "permissions_digest", expiresAt = "expires_at"
        case morePermissions = "more_permissions", addedPermissions = "added_permissions"
    }
}

extension APIClient {
    func pluginMarkets() async throws -> [PluginMarket] {
        struct List: Decodable { var markets: [PluginMarket]? }
        let list: List = try await get("/api/v1/plugins/-/markets")
        return list.markets ?? []
    }

    func addPluginMarket(spec: String, token: String?) async throws -> PluginMarket {
        struct Body: Encodable { var spec: String; var token: String? }
        return try JSON.decoder.decode(PluginMarket.self, from: try await request("POST", "/api/v1/plugins/-/markets",
                                                                                   body: Body(spec: spec, token: token), timeout: 60))
    }

    func deletePluginMarket(_ id: String) async throws {
        _ = try await request("DELETE", "/api/v1/plugins/-/markets/\(id)")
    }

    func refreshPluginMarket(_ id: String) async throws -> PluginMarket {
        try JSON.decoder.decode(PluginMarket.self, from: try await request("POST", "/api/v1/plugins/-/markets/\(id)/refresh", timeout: 60))
    }

    func setPluginMarket(_ id: String, token: String) async throws -> PluginMarket {
        try JSON.decoder.decode(PluginMarket.self, from: try await request("PUT", "/api/v1/plugins/-/markets/\(id)/token",
                                                                            body: ["token": token], timeout: 60))
    }

    func pluginCatalog(query: String = "", market: String? = nil, refresh: Bool = false) async throws -> PluginCatalog {
        try await get("/api/v1/plugins/-/catalog", query: ["q": query, "market": market ?? "", "refresh": refresh ? "1" : ""])
    }

    func previewCatalogPlugin(market: String, plugin: String) async throws -> PluginPreview {
        try JSON.decoder.decode(PluginPreview.self, from: try await request("POST", "/api/v1/plugins/-/catalog/preview",
                                                                             body: ["market": market, "plugin": plugin], timeout: 120))
    }

    func installPlugin(preview: PluginPreview) async throws -> HubPluginDetail {
        try JSON.decoder.decode(HubPluginDetail.self, from: try await request(
            "POST", "/api/v1/plugins/-/install", body: ["preview_id": preview.previewID, "permissions_digest": preview.permissionsDigest], timeout: 120))
    }

    func upgradePlugin(_ id: String, preview: PluginPreview) async throws -> HubPluginDetail {
        try JSON.decoder.decode(HubPluginDetail.self, from: try await request(
            "POST", "/api/v1/plugins/\(id)/-/upgrade", body: ["preview_id": preview.previewID, "permissions_digest": preview.permissionsDigest], timeout: 120))
    }

    func uninstallPlugin(_ id: String) async throws {
        _ = try await request("DELETE", "/api/v1/plugins/\(id)/-/", timeout: 60)
    }
}

/// How a catalog plugin stands on this hub, in words and as the one thing to do.
enum PluginMarketText {
    enum Step: Equatable { case install, upgrade, upToDate, unavailable }

    static func step(_ p: PluginCatalogItem) -> Step {
        if p.problem?.isEmpty == false { return p.installed == nil ? .unavailable : .upToDate }
        if p.installed == nil { return .install }
        return p.upgradable == true ? .upgrade : .upToDate
    }

    /// The row's word on it: 可安装 / 可升级到 1.1.0 / 已安装 1.2.0 / 装不了.
    static func state(_ p: PluginCatalogItem) -> String {
        switch step(p) {
        case .install: "可安装"
        case .upgrade: "可升级 · \(p.installed ?? "") → \(p.version)"
        case .upToDate: "已安装 · \(p.installed ?? p.version)"
        case .unavailable: "装不了"
        }
    }

    /// A market's line: how many plugins, when it was read.
    static func marketSummary(_ m: PluginMarket) -> String {
        var parts = ["\(m.plugins ?? 0) 个插件"]
        if m.hasToken { parts.append("有 token") }
        if let t = m.fetchedAt { parts.append(Fmt.relative(t) + "读取") }
        return parts.joined(separator: " · ")
    }
}

/// 插件市场: the catalog (searched, filtered by market) and the markets (added, refreshed, given a token, removed).
@Observable final class PluginMarketModel {
    struct Deps {
        var api: () -> APIClient?
        static var live: Deps { .init(api: { AppModel.shared.api }) }
    }

    private(set) var items: [PluginCatalogItem]?
    private(set) var markets: [PluginMarket] = []
    private(set) var platform: String?
    private(set) var loading = false
    private(set) var error: String?
    /// The words searched for (the hub matches id, name, description, publisher and tags).
    var query = ""
    /// The market the catalog is filtered to (nil: all).
    var market: String?
    /// A market being refreshed or removed (its spinner), by id.
    private(set) var busyMarket: String?
    @ObservationIgnored private let deps: Deps

    init(deps: Deps = .live) { self.deps = deps }

    /// Reads the catalog for the query and the market chosen (`refresh`: every index read again first).
    func load(refresh: Bool = false) async {
        loading = true
        defer { loading = false }
        do {
            let c = try await api().pluginCatalog(query: query.trimmingCharacters(in: .whitespaces), market: market, refresh: refresh)
            items = c.plugins
            markets = c.markets
            platform = c.platform
            if let m = market, !c.markets.contains(where: { $0.id == m }) { market = nil }
            error = nil
        } catch {
            self.error = error.localizedDescription
        }
    }

    /// Just the markets (their page).
    func loadMarkets() async {
        loading = true
        defer { loading = false }
        do {
            markets = try await api().pluginMarkets()
            error = nil
        } catch {
            self.error = error.localizedDescription
        }
    }

    /// Adds a market: the hub reads its index first and refuses one it cannot read (the reason comes back). nil: added.
    func addMarket(spec: String, token: String) async -> String? {
        let spec = spec.trimmingCharacters(in: .whitespacesAndNewlines)
        let token = token.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !spec.isEmpty else { return "填市场的地址" }
        do {
            let m = try await api().addPluginMarket(spec: spec, token: token.isEmpty ? nil : token)
            markets.removeAll { $0.id == m.id }
            markets.append(m)
            return nil
        } catch {
            return error.localizedDescription
        }
    }

    func removeMarket(_ id: String) async {
        await busy(id) {
            try await $0.deletePluginMarket(id)
            self.markets.removeAll { $0.id == id }
            if self.market == id { self.market = nil }
        }
    }

    func refreshMarket(_ id: String) async {
        await busy(id) { self.replace(try await $0.refreshPluginMarket(id)) }
    }

    /// A new token (empty: none); the hub reads the index again with it. nil: done.
    func setToken(_ id: String, token: String) async -> String? {
        do {
            replace(try await api().setPluginMarket(id, token: token.trimmingCharacters(in: .whitespacesAndNewlines)))
            return nil
        } catch {
            return error.localizedDescription
        }
    }

    private func replace(_ m: PluginMarket) {
        if let i = markets.firstIndex(where: { $0.id == m.id }) { markets[i] = m } else { markets.append(m) }
    }

    private func busy(_ id: String, _ run: (APIClient) async throws -> Void) async {
        busyMarket = id
        defer { busyMarket = nil }
        do {
            try await run(try api())
            error = nil
        } catch {
            self.error = error.localizedDescription
        }
    }

    private func api() throws -> APIClient {
        guard let api = deps.api() else { throw APIError(status: 0, message: "还没连接 hub") }
        return api
    }
}

/// 插件市场 › one plugin: what it is and may do; installing or upgrading goes preview → agree → install, removing is
/// only for one installed from this market.
@Observable final class MarketPluginModel {
    let market: String
    let pluginID: String
    private(set) var item: PluginCatalogItem?
    private(set) var loading = false
    private(set) var error: String?
    /// The preview waiting for the user's yes (the sheet with the permissions).
    var preview: PluginPreview?
    /// Previewing, installing, upgrading or removing.
    private(set) var working = false
    @ObservationIgnored private let deps: PluginMarketModel.Deps

    init(market: String, id: String, deps: PluginMarketModel.Deps = .live) {
        self.market = market
        self.pluginID = id
        self.deps = deps
    }

    var step: PluginMarketText.Step? { item.map(PluginMarketText.step) }
    var canUninstall: Bool { item?.fromThisMarket == true && item?.installed != nil }

    func load() async {
        loading = true
        defer { loading = false }
        do {
            let c = try await api().pluginCatalog(market: market)
            guard let it = c.plugins.first(where: { $0.pluginID == pluginID }) else {
                throw APIError(status: 404, message: "这个市场里已经没有这个插件了")
            }
            item = it
            error = nil
        } catch {
            self.error = error.localizedDescription
        }
    }

    /// Downloads and checks the package, for the permissions to agree to (the sheet opens with `preview`).
    func prepare() async {
        await work { self.preview = try await $0.previewCatalogPlugin(market: self.market, plugin: self.pluginID) }
    }

    /// Installs or upgrades with the preview agreed to. true: done (the catalog read again).
    func confirm() async -> Bool {
        guard let pv = preview else { return false }
        let ok = await work {
            if pv.upgrade == true { _ = try await $0.upgradePlugin(self.pluginID, preview: pv) } else { _ = try await $0.installPlugin(preview: pv) }
        }
        preview = nil
        if ok { await load() }
        return ok
    }

    func uninstall() async -> Bool {
        guard canUninstall else { return false }
        let ok = await work { try await $0.uninstallPlugin(self.pluginID) }
        if ok { await load() }
        return ok
    }

    @discardableResult private func work(_ run: (APIClient) async throws -> Void) async -> Bool {
        working = true
        defer { working = false }
        do {
            try await run(try api())
            error = nil
            return true
        } catch {
            self.error = error.localizedDescription
            return false
        }
    }

    private func api() throws -> APIClient {
        guard let api = deps.api() else { throw APIError(status: 0, message: "还没连接 hub") }
        return api
    }
}

import Foundation
import Observation

// Installing, upgrading and removing extensions, with what the hub is doing meanwhile (docs/protocol.md「扩展的安装、
// 升级、卸载进度」, #3277, #3307).
//
// A hub with `extension_operations` runs each as an operation and pushes it in `extension_operation` events (to a
// socket that declared the feature) as it goes through its steps: an install previews in the background (download,
// verify, unpack, check), stops at needs_consent for the user to agree to the permissions, and goes on with the same
// operation once they do. This store keeps them for the whole app, so the page that started one can be left and come
// back to it, and the market's list marks the row. An older hub answers when it is done: the store then keeps a local
// operation with no steps, so the page still says what is going on and for how long instead of a button that spins.

/// What the hub answers a request made with `async`.
nonisolated struct ExtensionOperationReply: Decodable, Sendable {
    var operation: ExtensionOperation?
}

/// One operation read on its own: while it waits for consent, the preview to agree to.
nonisolated struct ExtensionOperationDetail: Decodable, Sendable {
    var operation: ExtensionOperation
    var preview: ExtensionPreview?

    private enum CodingKeys: String, CodingKey { case preview }

    init(from decoder: Decoder) throws {
        operation = try ExtensionOperation(from: decoder)
        preview = try? decoder.container(keyedBy: CodingKeys.self).decodeIfPresent(ExtensionPreview.self, forKey: .preview)
    }
}

extension APIClient {
    /// The operations going on, waiting for consent, and the ones ended in the last minutes.
    func extensionOperations() async throws -> [ExtensionOperation] {
        struct List: Decodable { var operations: [ExtensionOperation]? }
        let list: List = try await get(extensionPath("/-/operations"))
        return list.operations ?? []
    }

    func extensionOperation(_ id: String) async throws -> ExtensionOperationDetail {
        try await get(extensionPath("/-/operations/\(id)"))
    }

    /// Starts it again from the beginning (the package is in the hub's cache); a new operation.
    func retryExtensionOperation(_ id: String) async throws -> ExtensionOperation {
        try Self.operation(try await request("POST", extensionPath("/-/operations/\(id)/retry"), timeout: 60))
    }

    /// Cancels one that downloads, checks or waits for consent; removes one that ended from the list.
    func deleteExtensionOperation(_ id: String) async throws {
        _ = try await request("DELETE", extensionPath("/-/operations/\(id)"))
    }

    /// Previews a market's extension in the background: download and checks, then it waits for consent.
    func startCatalogPreview(market: String, extension id: String) async throws -> ExtensionOperation {
        struct Body: Encodable {
            var market: String
            var extensionID: String
            var plugin: String
            var background = true
            enum CodingKeys: String, CodingKey { case market, plugin, extensionID = "extension", background = "async" }
        }
        return try Self.operation(try await request("POST", extensionPath("/-/catalog/preview"),
                                                    body: Body(market: market, extensionID: id, plugin: id), timeout: 60))
    }

    /// The user agreed to the preview: the operation it came from goes on to install (or upgrade) it.
    func agreeExtensionInstall(_ id: String, preview: ExtensionPreview) async throws -> ExtensionOperation {
        let path = preview.upgrade == true ? extensionPath("/\(id)/-/upgrade") : extensionPath("/-/install")
        return try Self.operation(try await request("POST", path, body: ExtensionInstallRequest(preview: preview, background: true), timeout: 60))
    }

    private static func operation(_ data: Data) throws -> ExtensionOperation {
        if let op = try? JSON.decoder.decode(ExtensionOperationReply.self, from: data).operation { return op }
        return try JSON.decoder.decode(ExtensionOperation.self, from: data)
    }
}

/// The app's extension operations, from the hub's events and lists and from the requests this app makes.
@MainActor @Observable final class ExtensionOperations {
    static let shared = ExtensionOperations()

    struct Deps {
        var api: () -> APIClient?
        var features: () -> Set<String>
        /// The hub the operations are of (another one: the list starts over).
        var hub: () -> String?
        /// How often an operation being waited for is read again when no event came.
        var poll: Duration = .seconds(2)
        /// How long to wait for one before saying so.
        var patience: TimeInterval = 15 * 60
        static var live: Deps {
            .init(api: { AppModel.shared.api }, features: { AppModel.shared.hubFeatures },
                  hub: { AppModel.shared.config?.url.absoluteString })
        }
    }

    /// By operation id.
    private(set) var operations: [String: ExtensionOperation] = [:]
    @ObservationIgnored private var lastHub: String?
    @ObservationIgnored private let deps: Deps

    init(deps: Deps = .live) { self.deps = deps }

    /// The hub runs them in steps and pushes them.
    var stepped: Bool { deps.features().hasExtensionFeature("operations") }

    /// The one to show for an extension (of a market, when given): one going on, else the latest.
    func operation(for extensionID: String, market: String? = nil) -> ExtensionOperation? {
        let mine = operations.values.filter {
            $0.extensionID == extensionID && (market == nil || $0.market == nil || $0.market == market)
        }
        return mine.first(where: \.isActive) ?? mine.max { ($0.startedAt ?? .distantPast) < ($1.startedAt ?? .distantPast) }
    }

    /// From an `extension_operation` event or a list: a newer state of the same operation replaces the one kept.
    func received(_ op: ExtensionOperation) {
        resetIfAnotherHub()
        if let old = operations[op.id], let was = old.updatedAt, let now = op.updatedAt, now < was { return }
        if operations[op.id] != op { operations[op.id] = op }
    }

    /// Reads what the hub has going on (a page opening, the connection back: events missed are not sent again).
    func reload() async {
        resetIfAnotherHub()
        guard stepped, let api = deps.api(), let list = try? await api.extensionOperations() else { return }
        // The hub's list is the truth for its operations; this app's own (an older hub's) stay.
        operations = operations.filter { $0.value.isLocal }
        list.forEach(received)
    }

    /// Put away: the hub forgets one that ended, cancels one that waits for consent or still checks.
    func dismiss(_ id: String) async {
        guard let op = operations[id] else { return }
        operations[id] = nil
        if !op.isLocal { try? await deps.api()?.deleteExtensionOperation(id) }
    }

    // MARK: - stepped (a hub with operations)

    /// Downloads and checks a market's extension in the background; the result is the operation once it waits for
    /// consent, failed or was canceled.
    func preview(market: String, extension id: String) async throws -> ExtensionOperation {
        guard let api = deps.api() else { throw APIError(status: 0, message: String(localized: "还没连接 hub")) }
        let op = try await api.startCatalogPreview(market: market, extension: id)
        received(op)
        return await follow(op.id) { $0.state != .running }
    }

    /// The preview the operation waits on.
    func consentPreview(_ id: String) async throws -> ExtensionPreview? {
        guard let api = deps.api() else { throw APIError(status: 0, message: String(localized: "还没连接 hub")) }
        let detail = try await api.extensionOperation(id)
        received(detail.operation)
        return detail.preview
    }

    /// The user agreed: the operation installs and the result is how it ended.
    func agree(_ id: String, preview: ExtensionPreview) async throws -> ExtensionOperation {
        guard let api = deps.api() else { throw APIError(status: 0, message: String(localized: "还没连接 hub")) }
        let op = try await api.agreeExtensionInstall(id, preview: preview)
        received(op)
        return await follow(op.id) { !$0.isRunning }
    }

    /// Starts a failed one again; the result once it waits for consent again or ended (a new operation).
    func retry(_ op: ExtensionOperation) async throws -> ExtensionOperation {
        guard let api = deps.api() else { throw APIError(status: 0, message: String(localized: "还没连接 hub")) }
        let next = try await api.retryExtensionOperation(op.id)
        operations[op.id] = nil
        received(next)
        return await follow(next.id) { !$0.isRunning }
    }

    // MARK: - an older hub

    /// Installs or upgrades with the preview agreed to, as the hub answers when it is done; a local operation says
    /// what is going on meanwhile. The result is how it ended.
    func installWithoutSteps(_ id: String, name: String?, preview: ExtensionPreview) async -> ExtensionOperation {
        let upgrade = preview.upgrade == true
        let local = ExtensionOperation(id: Self.localID(), kind: upgrade ? .upgrade : .install, extensionID: id, name: name,
                                       version: preview.manifest.version, from: preview.from, market: preview.market,
                                       startedAt: .now, updatedAt: .now)
        received(local)
        do {
            guard let api = deps.api() else { throw APIError(status: 0, message: String(localized: "还没连接 hub")) }
            if upgrade { _ = try await api.upgradeExtension(id, preview: preview) } else { _ = try await api.installExtension(preview: preview) }
            return finish(local, error: nil)
        } catch {
            return finish(local, error: error)
        }
    }

    /// Removes, as the hub answers when it is done (a hub with operations pushes the steps meanwhile; an older one
    /// gets a local operation). A refusal before anything started (has_dependents) is thrown for the caller to ask.
    func uninstall(_ id: String, name: String?, cascade: Bool, members: Bool) async throws -> ExtensionRemovalResult {
        let local = stepped ? nil : ExtensionOperation(id: Self.localID(), kind: .uninstall, extensionID: id, name: name,
                                                       startedAt: .now, updatedAt: .now)
        if let local { received(local) }
        do {
            guard let api = deps.api() else { throw APIError(status: 0, message: String(localized: "还没连接 hub")) }
            let result = try await api.uninstallExtension(id, cascade: cascade, members: members)
            if let local { _ = finish(local, error: nil) }
            return result
        } catch let e as APIError where e.code == "has_dependents" {
            if let local { operations[local.id] = nil }
            throw e
        } catch {
            if let local { _ = finish(local, error: error) }
            throw error
        }
    }

    // MARK: -

    private func finish(_ local: ExtensionOperation, error: Error?) -> ExtensionOperation {
        var op = local
        op.state = error == nil ? .succeeded : .failed
        op.error = error?.localizedDescription
        op.errorCode = (error as? APIError)?.code
        op.endedAt = .now
        op.updatedAt = .now
        operations[op.id] = op
        return op
    }

    /// Waits until `done` says the hub's operation got where it was going: its events move it on, and it is read
    /// again now and then in case one was missed.
    private func follow(_ id: String, until done: (ExtensionOperation) -> Bool) async -> ExtensionOperation {
        let started = Date.now
        var lastRead = Date.now
        while true {
            if let op = operations[id], done(op) { return op }
            if operations[id] == nil, let fresh = try? await deps.api()?.extensionOperation(id).operation {
                received(fresh)
                continue
            }
            if Date.now.timeIntervalSince(started) > deps.patience || Task.isCancelled {
                var late = operations[id] ?? ExtensionOperation(id: id, kind: .install, extensionID: "")
                late.state = .failed
                late.error = String(localized: "等了很久 hub 还没说做完没有，下拉刷新看看。")
                late.retryable = false
                return late
            }
            try? await Task.sleep(for: .milliseconds(200))
            let seconds = Double(deps.poll.components.seconds) + Double(deps.poll.components.attoseconds) / 1e18
            if Date.now.timeIntervalSince(lastRead) >= seconds, let api = deps.api() {
                lastRead = .now
                if let fresh = try? await api.extensionOperation(id).operation { received(fresh) }
            }
        }
    }

    private func resetIfAnotherHub() {
        let hub = deps.hub()
        guard hub != lastHub else { return }
        if lastHub != nil { operations = [:] }
        lastHub = hub
    }

    private static func localID() -> String { "local-" + UUID().uuidString }
}

extension ExtensionOperation {
    /// Kept by this app for a hub that does not run operations.
    var isLocal: Bool { id.hasPrefix("local-") }
}

/// The words for an operation and its steps (the hub sends ids; the words are the app's, docs/protocol.md).
enum ExtensionOperationText {
    static func verb(_ kind: ExtensionOperation.Kind) -> String {
        switch kind {
        case .install: String(localized: "安装")
        case .upgrade: String(localized: "升级")
        case .uninstall: String(localized: "卸载")
        }
    }

    /// A step's name; one this app does not know shows as its id.
    static func step(_ id: String, kind: ExtensionOperation.Kind) -> String {
        switch id {
        case "download": String(localized: "下载扩展包")
        case "verify": String(localized: "核对校验和")
        case "unpack": String(localized: "解包")
        case "check": String(localized: "检查权限和依赖")
        case "consent": String(localized: "等你同意权限")
        case "install": kind == .upgrade ? String(localized: "换上新版本") : String(localized: "装进 hub")
        case "start": String(localized: "启动")
        case "nodes": String(localized: "同步到各台电脑")
        case "stop": String(localized: "停下扩展")
        case "remove": String(localized: "删除文件和记录")
        default: id
        }
    }

    /// What a step says under its name: a download's bytes, the computers done, the cache.
    static func detail(_ s: ExtensionOperationStep) -> String? {
        if s.cached == true { return String(localized: "用了 hub 上缓存的包") }
        if let bytes = s.bytes, bytes > 0 {
            let done = ByteCountFormatter.string(fromByteCount: bytes, countStyle: .file)
            if let total = s.total, total > 0, s.state == .running {
                let all = ByteCountFormatter.string(fromByteCount: total, countStyle: .file)
                return String(localized: "\(done) / \(all)")
            }
            return done
        }
        if let m = s.machines, m.total > 0 {
            var line = String(localized: "\(m.done) / \(m.total) 台电脑")
            if let errors = m.errors, !errors.isEmpty {
                line += " · " + errors.sorted { $0.key < $1.key }.map { "\($0.key)：\($0.value)" }.joined(separator: "；") // i18n-ignore
            }
            return line
        }
        return nil
    }

    /// 正在安装 / 等你同意权限 / 安装没有完成 / 已安装 / 已取消.
    static func title(_ op: ExtensionOperation) -> String {
        switch (op.state, op.kind) {
        case (.running, .install): String(localized: "正在安装")
        case (.running, .upgrade): String(localized: "正在升级")
        case (.running, .uninstall): String(localized: "正在卸载")
        case (.needsConsent, _): String(localized: "等你同意权限")
        case (.failed, .install): String(localized: "安装没有完成")
        case (.failed, .upgrade): String(localized: "升级没有完成")
        case (.failed, .uninstall): String(localized: "卸载没有完成")
        case (.succeeded, .install): String(localized: "已安装")
        case (.succeeded, .upgrade): String(localized: "已升级")
        case (.succeeded, .uninstall): String(localized: "已卸载")
        case (.canceled, _): String(localized: "已取消")
        }
    }

    /// The row's chip in the market's list: 安装中 / 等你同意 / 安装失败.
    static func chip(_ op: ExtensionOperation) -> String? {
        switch (op.state, op.kind) {
        case (.running, .install): String(localized: "安装中")
        case (.running, .upgrade): String(localized: "升级中")
        case (.running, .uninstall): String(localized: "卸载中")
        case (.needsConsent, _): String(localized: "等你同意")
        case (.failed, .install): String(localized: "安装失败")
        case (.failed, .upgrade): String(localized: "升级失败")
        case (.failed, .uninstall): String(localized: "卸载失败")
        case (.succeeded, _), (.canceled, _): nil
        }
    }

    /// 1.0.0 → 1.1.0, or the version installed.
    static func versions(_ op: ExtensionOperation) -> String? {
        if op.kind == .upgrade, let from = op.from, let to = op.version { return "\(from) → \(to)" }
        return op.kind == .uninstall ? nil : op.version
    }

    /// 第 2 步，共 6 步 (the steps it skips not counted).
    static func progress(_ op: ExtensionOperation) -> String? {
        let shown = op.shownSteps
        guard !shown.isEmpty else { return nil }
        let done = shown.filter { $0.state == .done }.count
        let at = shown.firstIndex { $0.id == op.current?.id }.map { $0 + 1 } ?? done
        return String(localized: "第 \(max(at, 1)) 步，共 \(shown.count) 步")
    }

    /// 已过 12 秒 / 已过 1 分 5 秒.
    static func elapsed(since start: Date, now: Date) -> String {
        let s = max(0, Int(now.timeIntervalSince(start)))
        return s < 60 ? String(localized: "已过 \(s) 秒") : String(localized: "已过 \(s / 60) 分 \(s % 60) 秒")
    }

    /// What an older hub is doing while the request is out (no steps of its own).
    static func waitingNote(_ kind: ExtensionOperation.Kind) -> String {
        switch kind {
        case .install, .upgrade: String(localized: "hub 在下载、校验、解包并启动它，大的扩展要一两分钟。可以先离开这一页，回来还能看到。")
        case .uninstall: String(localized: "hub 在停下它、删掉它的文件和数据。可以先离开这一页，回来还能看到。")
        }
    }

    /// Who started it, when it was not this device's doing worth saying (the hub's automatic upgrade, the CLI).
    static func actor(_ op: ExtensionOperation) -> String? {
        guard let a = op.actor, !a.isEmpty else { return nil }
        return String(localized: "由 \(a) 发起")
    }
}

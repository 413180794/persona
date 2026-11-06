import Foundation
import Observation
import PersonaKit

// An extension's sign-in (docs/extensions.md「登录（auth）」, #4507): the hub asks the extension's program for its
// status and passes the steps of a sign-in on (the connectors' Prompt: fields, a device code, a QR code); what the
// sign-in yields stays with the program. Only with a hub that says `extension_auth`; an older hub's extension shows
// 「还没登录」 in its warnings and its own sign-in page (`auth.page`), which this app then lists as any other page.

/// One of the command-line programs a version of an extension runs with (`ExtensionView.tools`).
nonisolated struct ExtensionToolUse: Codable, Hashable, Sendable, Identifiable {
    var name: String
    var title: String?
    var version: String
    /// The hub machine's own copy, not one the hub installed.
    var reused: Bool?
    var id: String { name }
}

extension APIClient {
    func extensionAuth(_ id: String) async throws -> ExtensionAuth { try await get(extensionPath("/\(id)/-/auth")) }

    /// One step of the sign-in ("" starts over); the hub draws a QR code of the step's link.
    func extensionAuthStep(_ id: String, step: String, values: [String: String]) async throws -> ConnectorPrompt {
        struct Body: Encodable { var step: String; var values: [String: String] }
        return try JSON.decoder.decode(ConnectorPrompt.self, from: try await request(
            "POST", extensionPath("/\(id)/-/auth/step"), body: Body(step: step, values: values), timeout: 90))
    }

    func extensionAuthLogout(_ id: String) async throws -> ExtensionAuth {
        try JSON.decoder.decode(ExtensionAuth.self, from: try await request("POST", extensionPath("/\(id)/-/auth/logout"), timeout: 90))
    }
}

/// The extensions' sign-in states for the whole app: from the hub's lists and pages and its `extension_auth` events,
/// the latest wins, so the list's row and the open page follow a sign-in finished elsewhere at once.
@MainActor @Observable final class ExtensionAuthStore {
    static let shared = ExtensionAuthStore(hub: { AppModel.shared.config?.url.absoluteString })

    private(set) var byID: [String: ExtensionAuth] = [:]
    /// The extension whose sign-in should open when its page shows (an install that ended with `next: "auth"`).
    var openSignIn: String?
    @ObservationIgnored private let hub: () -> String?
    @ObservationIgnored private var lastHub: String?

    init(hub: @escaping () -> String? = { nil }) { self.hub = hub }

    /// The hub has sign-ins (`extension_auth`): the page draws its own section for them.
    static func available(_ features: Set<String>) -> Bool { features.hasExtensionFeature("auth") }

    func received(_ id: String, _ auth: ExtensionAuth?) {
        if lastHub != hub() {
            lastHub = hub()
            byID = [:]
        }
        if byID[id] != auth { byID[id] = auth }
    }

    /// The extensions of a list just read.
    func received(_ list: [HubExtension]) {
        for p in list { received(p.id, p.auth) }
    }

    /// What is known of id's sign-in, else what the hub said with the extension.
    func auth(_ id: String, else fallback: ExtensionAuth?) -> ExtensionAuth? {
        lastHub == hub() ? byID[id] ?? fallback : fallback
    }
}

/// Signing an extension in a step at a time on its page (the hub says what each step asks for), and signing out.
@MainActor @Observable final class ExtensionSignInModel {
    struct Deps {
        var api: () -> APIClient?
        var store: ExtensionAuthStore
        static var live: Deps { .init(api: { AppModel.shared.api }, store: .shared) }
    }

    let id: String
    /// The step on screen; nil when not signing in.
    private(set) var prompt: ConnectorPrompt?
    var values: [String: String] = [:]
    private(set) var busy = false
    /// What went wrong with the last request, for people.
    private(set) var error: String?
    @ObservationIgnored private let deps: Deps

    init(id: String, deps: Deps = .live) {
        self.id = id
        self.deps = deps
    }

    var signingIn: Bool { prompt != nil }

    /// Every field of the step on screen is filled in.
    var canSend: Bool {
        guard let fields = prompt?.fields, !fields.isEmpty, !busy else { return false }
        return fields.allSatisfy { !(values[$0.name] ?? "").trimmingCharacters(in: .whitespaces).isEmpty }
    }

    func start() async { await send(step: "", values: [:]) }

    /// From the first step again (a link or code that stopped working, a step this app cannot draw).
    func restart() async {
        cancel()
        await start()
    }

    /// Sends the step on screen with its fields; the answer is the next one, the same with what was wrong, or done.
    @discardableResult func next() async -> Bool {
        guard let step = prompt?.step else { return false }
        return await send(step: step, values: values)
    }

    /// A link, code or QR code on screen: asks the same step again every interval the hub gave until it is done (true)
    /// or the step changes; ends with the page.
    func waitForAuthorization() async -> Bool {
        while let p = prompt, ExtensionSignIn.polls(p), !Task.isCancelled {
            try? await Task.sleep(for: .seconds(max(p.interval ?? 5, 1)))
            if Task.isCancelled || prompt?.step != p.step { return false }
            if await send(step: p.step ?? "", values: [:]) { return true }
        }
        return false
    }

    func cancel() {
        prompt = nil
        values = [:]
    }

    func logout() async {
        guard let api = deps.api() else { return }
        busy = true
        defer { busy = false }
        do {
            deps.store.received(id, try await api.extensionAuthLogout(id))
            cancel()
            error = nil
        } catch {
            self.error = error.localizedDescription
        }
    }

    /// Asks the hub for the status again (after a sign-in, or the page opening).
    func refresh() async {
        guard let api = deps.api(), let auth = try? await api.extensionAuth(id) else { return }
        deps.store.received(id, auth)
    }

    /// True when signing in is done.
    @discardableResult private func send(step: String, values: [String: String]) async -> Bool {
        guard let api = deps.api() else { return false }
        busy = true
        defer { busy = false }
        do {
            let p = try await api.extensionAuthStep(id, step: step, values: values)
            error = nil
            if p.done == true {
                cancel()
                await refresh()
                return true
            }
            if p.step != prompt?.step { self.values = [:] } // a new step: its fields start empty
            prompt = p
            if step.isEmpty { await refreshIfPending() }
        } catch {
            self.error = error.localizedDescription
        }
        return false
    }

    /// The first step puts the status at Waiting for confirmation: the header says so too.
    private func refreshIfPending() async {
        if deps.store.byID[id]?.state != .pending { await refresh() }
    }
}

/// What a step of an extension's sign-in shows (the connectors' Prompt, with a device code's link and QR code).
enum ExtensionSignIn {
    /// A QR code to scan (the hub drew it).
    static func isQR(_ p: ConnectorPrompt) -> Bool { p.kind == "qr" && !(p.qr?.rows.isEmpty ?? true) }
    /// A page to open (and maybe a code to enter there).
    static func isDeviceCode(_ p: ConnectorPrompt) -> Bool { p.kind == "device_code" && link(p) != nil }
    /// Fields to fill.
    static func isForm(_ p: ConnectorPrompt) -> Bool { (p.kind ?? "").isEmpty && !(p.fields ?? []).isEmpty }
    /// The step is asked again on its own until the user has acted somewhere else.
    static func polls(_ p: ConnectorPrompt) -> Bool { isQR(p) || isDeviceCode(p) }
    /// A kind this app does not know, or one that came without what it needs.
    static func cannotDraw(_ p: ConnectorPrompt) -> Bool { !isQR(p) && !isDeviceCode(p) && !isForm(p) }
    /// The step's link: opened on this device (a Feishu link opens Feishu on the iPhone).
    static func link(_ p: ConnectorPrompt) -> URL? {
        guard let s = p.url, let url = URL(string: s), let scheme = url.scheme?.lowercased(), !scheme.isEmpty,
              !["javascript", "data", "file"].contains(scheme) else { return nil }
        return url
    }
    /// What the polling follows: a new code or link starts it over.
    static func pollKey(_ p: ConnectorPrompt) -> String? {
        polls(p) ? [p.step ?? "", p.url ?? "", p.userCode ?? "", p.qr?.rows.joined() ?? ""].joined(separator: "|") : nil
    }
}

/// The words of a sign-in.
enum ExtensionAuthText {
    /// Not signed in / Waiting for confirmation / Signed in: <account> / Expired / Error: <reason>.
    static func state(_ a: ExtensionAuth) -> String {
        switch a.state {
        case .signedOut: String(localized: "未登录")
        case .pending: String(localized: "等待确认")
        case .signedIn: a.account.flatMap { $0.isEmpty ? nil : $0 }.map { String(localized: "已登录：\($0)") } ?? String(localized: "已登录")
        case .expired: String(localized: "登录已过期")
        case .error: a.message.flatMap { $0.isEmpty ? nil : $0 }.map { String(localized: "出错：\($0)") } ?? String(localized: "出错了")
        case .unknown: String(localized: "扩展启动后才知道")
        }
    }

    /// The section's button: Sign In when not signed in, Sign In Again when signed in or expired.
    static func signInButton(_ a: ExtensionAuth) -> String {
        a.state == .signedIn || a.state == .expired ? String(localized: "重新登录") : String(localized: "登录")
    }

    /// The list's suffix for one that is not signed in.
    static func listSuffix(_ a: ExtensionAuth?) -> String? {
        guard let a, a.needsSignIn else { return nil }
        return a.state == .expired ? String(localized: "登录已过期") : String(localized: "未登录")
    }

    /// A program's name in About: Node.js (node).
    static func toolName(_ t: ExtensionToolUse) -> String {
        t.title.flatMap { $0.isEmpty || $0 == t.name ? nil : $0 }.map { "\($0)（\(t.name)）" } ?? t.name // i18n-ignore
    }

    /// Its version, the machine's own marked: 22.23.4 · already on the machine.
    static func toolVersion(_ t: ExtensionToolUse) -> String {
        let version = t.version
        return t.reused == true ? String(localized: "\(version) · 机器上已有") : version
    }

    /// The extension's warnings, without the hub's 「还没登录（<title>）…」: the Sign In section says how it is (and follows a
    /// sign-in at once, the warnings only when the page reads the extension again).
    static func warnings(_ all: [String]?, shown auth: ExtensionAuth?) -> [String] {
        guard let auth, !auth.title.isEmpty else { return all ?? [] }
        return (all ?? []).filter { !$0.contains("（\(auth.title)）") && !$0.contains("(\(auth.title))") } // i18n-ignore
    }

    /// Where a program comes from, as the install sheet says it: nodejs.org, npm: registry.example.com.
    static func source(_ t: ExtensionToolSpec) -> String {
        var hosts: [String] = []
        func add(_ raw: String?) {
            if let raw, let host = URL(string: raw)?.host()?.lowercased(), !host.isEmpty, !hosts.contains(host) { hosts.append(host) }
        }
        if let npm = t.npm {
            add(npm.registry?.isEmpty == false ? npm.registry : "https://registry.npmjs.org")
            let list = ExtensionText.list(hosts.sorted())
            return String(localized: "npm：\(list)")
        }
        for a in (t.archive ?? [:]).values {
            add(a.url)
            for m in a.mirrors ?? [] { add(m) }
        }
        return ExtensionText.list(hosts.sorted())
    }

    /// A program to install, for the install sheet: Node.js (node) 22.23.4 · nodejs.org.
    static func spec(_ t: ExtensionToolSpec) -> String {
        let name = t.title.flatMap { $0.isEmpty || $0 == t.name ? nil : $0 }.map { "\($0)（\(t.name)）" } ?? t.name // i18n-ignore
        let from = source(t)
        return from.isEmpty ? "\(name) \(t.version)" : "\(name) \(t.version) · \(from)" // i18n-ignore
    }

    /// Under the programs: where they go.
    static let toolsNote = String(localized: "装在扩展自己的目录，不用管理员权限；机器上已有能用的会直接用。")
}

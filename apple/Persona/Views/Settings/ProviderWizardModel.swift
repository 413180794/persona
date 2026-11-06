import Foundation
import Observation

/// A managed launcher's name as the hub takes it.
enum LauncherName {
    /// What is wrong with `name`, the way the hub would put it; nil while empty or fine.
    static func problem(_ name: String) -> String? {
        let n = name.trimmingCharacters(in: .whitespaces)
        guard !n.isEmpty else { return nil }
        if n.range(of: "^[A-Za-z_][A-Za-z0-9_-]*$", options: .regularExpression) == nil {
            return String(localized: "名字只能用字母、数字、_ 和 -，以字母或 _ 开头")
        }
        // The hub's reservedLauncherNames: claude itself and the /api/v1/launchers/<word> routes.
        if ["claude", "presets", "test", "discover", "import"].contains(n) { return String(localized: "不能叫 \(n)") }
        return nil
    }
}

/// Settings › Model Providers › Add / Edit (#2455, after cindy's AddProviderWizard): Choose Provider → Connect (name, address, auth, key;
/// Test Connection, Get Model List) → Choose Models (context window, thinking levels, the default), with the rest under Advanced.
@Observable final class ProviderWizardModel {
    struct Deps {
        var api: () -> APIClient?
        static var live: Deps { .init(api: { AppModel.shared.api }) }
    }

    enum Kind: String, CaseIterable, Hashable {
        /// api.anthropic.com with an x-api-key.
        case anthropic
        /// Any Anthropic-compatible gateway (a Coding Plan, a company's proxy).
        case custom
    }

    static let anthropicURL = "https://api.anthropic.com"

    let existing: LauncherView?
    var kind: Kind = .custom
    var name = ""
    var baseURL = ""
    /// api_key (x-api-key) | bearer.
    var auth = "api_key"
    var apiKey = ""
    /// Typed headers, "Name: value" a line. Empty keeps what the hub has unless `clearHeaders`.
    var headers = ""
    /// Clear Headers: the stored headers go when saved.
    var clearHeaders = false

    /// The models picked, in order; the default is `defaultModel`.
    private(set) var models: [ProviderModel] = []
    var defaultModel = ""
    /// What the gateway said it serves (Get Model List).
    private(set) var discovered: [ProviderModel] = []

    // Advanced
    var opusModel = ""
    var sonnetModel = ""
    var haikuModel = ""
    var fableModel = ""
    var smallModel = ""
    var subagentModel = ""
    var subagentForce = false
    var effort = ""
    var proxy = ""
    var noProxy = ""
    /// Numbers as typed; empty or 0 is the default.
    var timeoutMS = ""
    var compactWindow = ""
    var maxContext = ""

    private(set) var testing = false
    private(set) var test: LauncherProbeResult?
    private(set) var discovering = false
    /// Get Model List ran (even when it found none or failed): the models step shows what came of it.
    private(set) var discoveredOnce = false
    private(set) var discoverError: String?
    private(set) var saving = false
    private(set) var error: String?
    @ObservationIgnored private let deps: Deps

    init(existing: LauncherView? = nil, deps: Deps = .live) {
        self.existing = existing
        self.deps = deps
        guard let e = existing else { return }
        name = e.name
        let url = e.baseURL ?? ""
        kind = url.trimmingCharacters(in: CharacterSet(charactersIn: "/ ")) == Self.anthropicURL ? .anthropic : .custom
        baseURL = kind == .custom ? url : ""
        auth = e.bearerAuth ? "bearer" : "api_key"
        models = e.models ?? []
        defaultModel = e.model ?? ""
        if !defaultModel.isEmpty, !models.contains(where: { $0.id == defaultModel }) { models.insert(ProviderModel(id: defaultModel), at: 0) }
        opusModel = e.opusModel ?? ""
        sonnetModel = e.sonnetModel ?? ""
        haikuModel = e.haikuModel ?? ""
        fableModel = e.fableModel ?? ""
        smallModel = e.smallModel ?? ""
        subagentModel = e.subagentModel ?? ""
        subagentForce = e.subagentForce ?? false
        effort = e.effort ?? ""
        proxy = e.proxy ?? ""
        noProxy = e.noProxy ?? ""
        timeoutMS = Self.number(e.timeoutMS)
        compactWindow = Self.number(e.compactWindow)
        maxContext = Self.number(e.maxContext)
    }

    var isNew: Bool { existing == nil }
    var hasStoredKey: Bool { existing?.hasKey == true }
    var hasStoredHeaders: Bool { existing?.hasHeaders == true && !clearHeaders }

    var address: String { kind == .anthropic ? Self.anthropicURL : baseURL.trimmingCharacters(in: .whitespaces) }
    var authMode: String { kind == .anthropic ? "api_key" : auth }
    private var key: String { apiKey.trimmingCharacters(in: .whitespacesAndNewlines) }
    private var trimmedName: String { name.trimmingCharacters(in: .whitespaces) }
    var nameProblem: String? { LauncherName.problem(name) }

    /// nil keeps the stored headers, "" clears them.
    var headersValue: String? {
        let h = headers.trimmingCharacters(in: .whitespacesAndNewlines)
        if !h.isEmpty { return h }
        return clearHeaders ? "" : nil
    }

    /// Connect is filled in: the next step can come.
    var connectionReady: Bool {
        !trimmedName.isEmpty && nameProblem == nil && URL(string: address)?.host() != nil && (!key.isEmpty || hasStoredKey)
    }

    /// The model Test Connection asks for: the default, else the first picked or listed.
    var testModel: String { [defaultModel, models.first?.id ?? "", discovered.first?.id ?? ""].first { !$0.isEmpty } ?? "" }

    var canTest: Bool { connectionReady && !testModel.isEmpty && !testing }

    var canSave: Bool { connectionReady && models.contains { $0.id == defaultModel } && !saving }

    /// What the models step lists: the picked ones, then the rest of what the gateway serves.
    var choices: [ProviderModel] {
        models + discovered.filter { d in !models.contains { $0.id == d.id } }
    }

    func isPicked(_ id: String) -> Bool { models.contains { $0.id == id } }

    /// Ticks or unticks a model; the first one ticked becomes the default, and unticking the default moves it on.
    func toggle(_ m: ProviderModel) {
        if let i = models.firstIndex(where: { $0.id == m.id }) {
            models.remove(at: i)
            if defaultModel == m.id { defaultModel = models.first?.id ?? "" }
        } else {
            models.append(m)
            if defaultModel.isEmpty { defaultModel = m.id }
        }
    }

    /// Add: a model the list does not have, by its id; false when it is empty or already there.
    @discardableResult func addModel(_ id: String) -> Bool {
        let id = id.trimmingCharacters(in: .whitespaces)
        guard !id.isEmpty else { return false }
        if isPicked(id) { return false }
        toggle(discovered.first { $0.id == id } ?? ProviderModel(id: id))
        return true
    }

    func update(_ id: String, _ change: (inout ProviderModel) -> Void) {
        guard let i = models.firstIndex(where: { $0.id == id }) else { return }
        change(&models[i])
    }

    func model(_ id: String) -> ProviderModel? { models.first { $0.id == id } ?? discovered.first { $0.id == id } }

    private var probe: LauncherProbe {
        LauncherProbe(name: existing?.name, baseURL: address, auth: authMode, apiKey: key.isEmpty ? nil : key,
                      headers: headersValue, model: testModel.isEmpty ? nil : testModel)
    }

    func runTest() async {
        guard canTest, let api = deps.api() else { return }
        testing = true
        defer { testing = false }
        do {
            test = try await api.testLauncher(probe)
        } catch {
            test = LauncherProbeResult(ok: false, error: error.shown ?? error.localizedDescription)
        }
    }

    func discover() async {
        guard connectionReady, !discovering, let api = deps.api() else { return }
        discovering = true
        defer { discovering = false; discoveredOnce = true }
        do {
            let r = try await api.discoverLauncherModels(probe)
            discovered = r.models ?? []
            discoverError = r.error.flatMap { $0.isEmpty ? nil : $0 }
            // What the gateway says of a model already picked fills in what the picked one leaves out.
            for d in discovered {
                update(d.id) { m in
                    if m.name == nil { m.name = d.name }
                    if (m.contextWindow ?? 0) == 0 { m.contextWindow = d.contextWindow }
                    if (m.efforts ?? []).isEmpty { m.efforts = d.efforts }
                    if m.defaultEffort == nil { m.defaultEffort = d.defaultEffort }
                }
            }
        } catch {
            discoverError = error.shown ?? error.localizedDescription
        }
    }

    var input: LauncherInput {
        let e = existing
        let t = { (s: String) in s.trimmingCharacters(in: .whitespaces) }
        return LauncherInput(name: trimmedName, baseURL: address, model: defaultModel, smallModel: t(smallModel), subagentModel: t(subagentModel),
                             customModel: e?.customModel ?? "", customModelName: e?.customModelName ?? "",
                             customModelDescription: e?.customModelDescription ?? "", args: e?.args ?? [],
                             apiKey: key.isEmpty ? nil : key, kind: e?.kind ?? "launcher", api: "messages", auth: authMode,
                             models: models, opusModel: t(opusModel), sonnetModel: t(sonnetModel), haikuModel: t(haikuModel),
                             fableModel: t(fableModel), subagentForce: subagentForce, headers: headersValue, proxy: t(proxy),
                             noProxy: t(noProxy), timeoutMS: Int(t(timeoutMS)) ?? 0, compactWindow: Int(t(compactWindow)) ?? 0,
                             maxContext: Int(t(maxContext)) ?? 0, effort: t(effort))
    }

    /// Saves the provider; false (with `error`) when the hub refused it.
    func save() async -> Bool {
        guard canSave, let api = deps.api() else { return false }
        saving = true
        defer { saving = false }
        do {
            try await api.saveLauncher(input, isNew: isNew)
            error = nil
            return true
        } catch {
            self.error = error.shown ?? error.localizedDescription
            return false
        }
    }

    private static func number(_ n: Int?) -> String { n.map { $0 > 0 ? String($0) : "" } ?? "" }
}

import Foundation
import Observation

/// One source of model choices: the hub's purposes, bindings and effective values.
@Observable final class ModelConfigurationModel {
    struct Deps {
        var api: () -> APIClient?
        var updated: () async -> Void = {}

        static var live: Deps {
            let app = AppModel.shared
            // A saved use moves the pickers' 「默认：…」 too: they read the machines' launchers again.
            return .init(api: { app.api }, updated: { app.launcherDefaultsChanged(); await app.refreshAssistant() })
        }
    }

    private(set) var purposes: [ModelPurpose] = []
    private(set) var providers: [ModelProvider] = []
    private(set) var purpose: ModelPurpose?
    private(set) var loaded = false
    private(set) var loading = false
    private(set) var saving = false
    private(set) var error: String?
    private(set) var conflict = false
    var draft = ModelBinding()
    @ObservationIgnored private let deps: Deps

    init(deps: Deps = .live) { self.deps = deps }

    var dirty: Bool { purpose.map { draft != $0.override } ?? false }

    var categories: [ModelPurpose] { purposes.filter { $0.uses != nil } }

    func modelSummary(_ p: ModelPurpose) -> String {
        let model = p.effective.model ?? p.effective.provider.flatMap { id in providers.first { $0.id == id }?.name }
        let effort = p.effective.effort.map { value in SessionModes.effortLevels.first { $0.id == value }?.title ?? value }
        // Main Chat on the Agent SDK says so; the CLI is the default and goes unsaid.
        let runtime = p.effective.runtime == AssistantRuntime.agentSDK ? AssistantRuntime.title(AssistantRuntime.agentSDK) : nil
        let result = [model, effort, runtime].compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: " · ")
        return result.isEmpty ? String(localized: "Not set up") : result
    }

    func summary(_ p: ModelPurpose) -> String {
        let provider = p.effective.provider.flatMap { id in providers.first { $0.id == id }?.name ?? id }
        return [provider, p.effective.model, p.effective.effort].compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: " · ")
    }

    var candidates: [ModelProvider] {
        guard let purpose else { return [] }
        return providers.filter { p in
            if p.hidden == true && p.id != draft.provider { return false }
            if let ids = purpose.providerIDs { return ids.contains(p.id) || p.id == draft.provider }
            return p.kind == purpose.kind || p.id == draft.provider
        }
    }

    /// Main Chat's runtimes (How It Runs); empty for every other use and on an older hub.
    var runtimes: [String] {
        guard let purpose, purpose.supports("runtime") else { return [] }
        return purpose.runtimes ?? []
    }

    /// Another provider clears the model and the effort; the runtime is not the provider's and stays.
    func chooseProvider(_ id: String) {
        let selected = id.isEmpty ? nil : id
        guard selected != draft.provider else { return }
        draft = ModelBinding(provider: selected, runtime: draft.runtime)
    }

    func restoreInheritance() { draft = ModelBinding() }

    func load(purposeID: String? = nil) async {
        guard !loading && !saving else { return }
        loading = true
        defer { loading = false; loaded = true }
        do {
            guard let api = deps.api() else { throw APIError(status: 0, message: String(localized: "Not connected to the hub yet")) }
            if let purposeID {
                var result = try await api.modelPurpose(purposeID)
                if let category = result.purpose.categoryID, category != result.purpose.id {
                    result = try await api.modelPurpose(category)
                }
                guard result.purpose.kind == "plugin" || (result.purpose.categoryID == result.purpose.id && result.purpose.uses != nil) else {
                    purpose = nil
                    throw APIError(status: 0, message: String(localized: "This hub doesn't support model configuration by category yet. Update the server first."))
                }
                purpose = result.purpose
                providers = result.providers
                draft = result.purpose.override
            } else {
                let result = try await api.modelConfiguration()
                purposes = result.purposes
                providers = result.providers
                if !purposes.isEmpty && categories.isEmpty {
                    error = String(localized: "This hub doesn't support model configuration by category yet. Update the server first.")
                    return
                }
            }
            error = nil
            conflict = false
        } catch {
            self.error = Self.message(error, singlePurpose: purposeID != nil)
        }
    }

    func save() async -> Bool {
        guard !saving && !loading, let purpose, purpose.fields?.isEmpty != true else { return false }
        saving = true
        defer { saving = false }
        do {
            guard let api = deps.api() else { throw APIError(status: 0, message: String(localized: "Not connected to the hub yet")) }
            let binding = ModelBinding(provider: Self.trim(draft.provider), model: Self.trim(draft.model), effort: Self.trim(draft.effort),
                                       runtime: Self.trim(draft.runtime))
            let patch = ModelConfigurationPatch(revision: purpose.revision, binding: binding,
                                                fields: purpose.fields ?? ["provider", "model"])
            let result = try await api.saveModelPurpose(purpose.id, patch: patch)
            self.purpose = result.purpose
            providers = result.providers
            draft = result.purpose.override
            error = nil
            conflict = false
            await deps.updated()
            return true
        } catch {
            conflict = (error as? APIError)?.status == 409
            self.error = conflict ? String(localized: "This setting was changed elsewhere. Your edits are still here; reload, then save again.") : Self.message(error, singlePurpose: true)
            return false
        }
    }

    private static func trim(_ value: String?) -> String? {
        guard let value = value?.trimmingCharacters(in: .whitespacesAndNewlines), !value.isEmpty else { return nil }
        return value
    }

    private static func message(_ error: Error, singlePurpose: Bool = false) -> String {
        if (error as? APIError)?.status == 404 {
            return singlePurpose ? String(localized: "This use no longer exists, or the hub doesn't support unified model configuration yet. Refresh the list of uses or update the server.")
                : String(localized: "This hub doesn't support unified model configuration yet. Update the server first.")
        }
        return error.localizedDescription
    }
}

import Foundation
import Observation

/// One source of model choices: the hub's purposes, bindings and effective values.
@Observable final class ModelConfigurationModel {
    struct Deps {
        var api: () -> APIClient?
        var updated: () async -> Void = {}

        static var live: Deps {
            let app = AppModel.shared
            return .init(api: { app.api }, updated: { await app.refreshAssistant() })
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

    func filtered(_ query: String) -> [ModelPurpose] {
        let q = query.trimmingCharacters(in: .whitespacesAndNewlines)
        return q.isEmpty ? purposes : purposes.filter {
            [$0.name, $0.group, summary($0), $0.effective.model ?? ""].contains { $0.localizedCaseInsensitiveContains(q) }
        }
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

    func chooseProvider(_ id: String) {
        let selected = id.isEmpty ? nil : id
        guard selected != draft.provider else { return }
        draft = ModelBinding(provider: selected)
    }

    func restoreInheritance() { draft = ModelBinding() }

    func load(purposeID: String? = nil) async {
        guard !loading && !saving else { return }
        loading = true
        defer { loading = false; loaded = true }
        do {
            guard let api = deps.api() else { throw APIError(status: 0, message: "还没连接 hub") }
            if let purposeID {
                let result = try await api.modelPurpose(purposeID)
                purpose = result.purpose
                providers = result.providers
                draft = result.purpose.override
            } else {
                let result = try await api.modelConfiguration()
                purposes = result.purposes
                providers = result.providers
            }
            error = nil
            conflict = false
        } catch {
            self.error = Self.message(error, singlePurpose: purposeID != nil)
        }
    }

    func save() async -> Bool {
        guard !saving && !loading, let purpose else { return false }
        saving = true
        defer { saving = false }
        do {
            guard let api = deps.api() else { throw APIError(status: 0, message: "还没连接 hub") }
            let binding = ModelBinding(provider: Self.trim(draft.provider), model: Self.trim(draft.model), effort: Self.trim(draft.effort))
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
            self.error = conflict ? "这项配置已在别处修改。你的编辑仍在，重新加载后再保存。" : Self.message(error, singlePurpose: true)
            return false
        }
    }

    private static func trim(_ value: String?) -> String? {
        guard let value = value?.trimmingCharacters(in: .whitespacesAndNewlines), !value.isEmpty else { return nil }
        return value
    }

    private static func message(_ error: Error, singlePurpose: Bool = false) -> String {
        if (error as? APIError)?.status == 404 {
            return singlePurpose ? "此用途已不存在，或 hub 尚不支持统一模型配置。请刷新用途列表或更新服务器。"
                : "hub 尚不支持统一模型配置，请先更新服务器。"
        }
        return error.localizedDescription
    }
}

import SwiftUI

/// 新建 Agent's model: the launcher's list (`LauncherModels`) and what is picked from it. Picking nothing ("") is the
/// default — what the launcher runs, or 模型配置 › 新建 Agent's model when that applies — and sends no model.
nonisolated struct LauncherModelChoice: Equatable {
    var list: LauncherModels
    /// The picked model's id; "": the default.
    var picked = ""
    /// 模型配置 › 新建 Agent's model when it is what runs with no pick (the launcher is the one it names); "": none.
    var configured = ""
    /// How 默认's line names [configured]: 模型配置 (新建 Agent), 继承 (模型配置 itself, where empty inherits).
    var configuredLabel = "模型配置"

    /// A pick can reach the agent: there is a list and the launcher does not pass its own --model.
    var canPick: Bool { list.locked == nil && !list.models.isEmpty }
    /// The model the agent runs when it is given none.
    var defaultModel: AgentModel? { list.models.first(where: \.isDefault) }
    /// What can be picked besides the default.
    var choices: [AgentModel] { list.models.filter { !$0.isDefault } }
    var selected: AgentModel? { picked.isEmpty ? nil : list.models.first { $0.id == picked } }

    /// The `model` a start sends: nil for the default.
    var sent: String? { canPick && selected?.isDefault == false ? picked : nil }

    /// The row's title: the picked model's name, else 默认.
    var title: String { selected.map(\.name) ?? "默认" }
    /// The row's second line: the picked model's (`AgentModel.detail`), or what the default runs.
    var detail: String? {
        if let m = selected { return m.detail }
        return defaultDetail
    }
    /// What runs with no pick: 模型配置's model, or the one the agent defaults to.
    var defaultDetail: String? {
        if !configured.isEmpty { return "\(configuredLabel)：\(configured)" }
        return defaultModel.map { $0.resolved ?? $0.name }
    }

    /// The thinking efforts the model that will run takes; nil: not known (every level stays offered).
    var efforts: [String]? {
        let running = selected ?? (configured.isEmpty ? defaultModel : list.models.first { $0.id == configured })
        guard let running else { return nil }
        return running.efforts ?? []
    }

    /// What is typed, as an id to use when no model has exactly that id (模型配置 takes ids the list lacks); nil: none.
    func customModelID(_ query: String) -> String? {
        let id = query.trimmingCharacters(in: .whitespaces)
        guard !id.isEmpty, !id.contains(where: \.isWhitespace), !list.models.contains(where: { $0.id == id }) else { return nil }
        return id
    }

    /// The picked id when the list does not have it (模型配置's from before, or one typed in); nil: none.
    var unlisted: String? { !picked.isEmpty && selected == nil ? picked : nil }

    /// Models whose name, id, resolved model or description has the query.
    func matching(_ query: String) -> [AgentModel] {
        let q = query.trimmingCharacters(in: .whitespaces)
        guard !q.isEmpty else { return choices }
        return choices.filter { m in
            [m.name, m.id, m.resolved ?? "", m.description ?? ""].contains { $0.localizedCaseInsensitiveContains(q) }
        }
    }
}

extension AgentModel {
    /// Its second line: the id when the name is another word for it, and the model an alias stands for.
    nonisolated var detail: String? {
        var parts: [String] = []
        if id != name { parts.append(id) }
        if let r = resolved, r != name, r != id { parts.append("→ \(r)") }
        return parts.isEmpty ? nil : parts.joined(separator: " ")
    }
}

/// The list behind 新建 Agent's 模型 row: 默认 first, then every model the launcher's agent offers, searchable.
struct LauncherModelPicker: View {
    var choice: LauncherModelChoice
    @Binding var picked: String
    /// Asks the CLI again (pull to refresh).
    var refresh: () async -> Void
    /// 模型配置: an id the list does not have can be used (what is typed), and the current one stays shown.
    var allowsCustom = false
    /// Under 默认: what picking nothing means here.
    var defaultFooter = "不指定模型：按启动器自己的设置运行。"
    @Environment(\.dismiss) private var dismiss
    @State private var query = ""

    var body: some View {
        let shown = choice.matching(query)
        List {
            if query.isEmpty {
                Section {
                    row(title: "默认", detail: choice.defaultDetail, description: nil, id: "")
                    if allowsCustom, let id = choice.unlisted {
                        row(title: id, detail: "当前配置，列表里没有", description: nil, id: id)
                    }
                } footer: {
                    Text(defaultFooter)
                }
            }
            Section {
                ForEach(shown) { m in
                    row(title: m.name, detail: m.detail, description: m.description, id: m.id)
                }
            } header: {
                if !shown.isEmpty { Text("\(shown.count) 个模型") }
            }
            if allowsCustom, let id = choice.customModelID(query) {
                Section {
                    row(title: "使用「\(id)」", detail: nil, description: "列表里没有，按这个 ID 保存", id: id)
                }
            } else if shown.isEmpty && !query.isEmpty {
                ContentUnavailableView.search(text: query)
            }
        }
        .searchable(text: $query, prompt: allowsCustom ? "搜索，或输入模型 ID" : "名称或 ID")
        .refreshable { await refresh() }
        .themedPage()
        .navigationTitle("模型")
        #if os(iOS) || os(visionOS)
        .navigationBarTitleDisplayMode(.inline)
        #endif
    }

    private func row(title: String, detail: String?, description: String?, id: String) -> some View {
        Button {
            picked = id
            Haptics.tap()
            dismiss()
        } label: {
            HStack(spacing: Theme.Space.m) {
                VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                    Text(title).font(Theme.Font.heading).foregroundStyle(.primary).lineLimit(1)
                    if let detail {
                        Text(detail).font(Theme.Font.label.monospaced()).foregroundStyle(.secondary)
                            .lineLimit(1).truncationMode(.middle)
                    }
                    if let description, !description.isEmpty {
                        Text(description).font(Theme.Font.label).foregroundStyle(.tertiary).lineLimit(2)
                    }
                }
                Spacer(minLength: Theme.Space.s)
                if id == picked {
                    Image(systemName: Theme.Icon.done).foregroundStyle(Theme.accent).fontWeight(.semibold)
                }
            }
            .contentShape(.rect)
        }
        .accessibilityIdentifier("newChat.model.\(id.isEmpty ? "default" : id)")
    }
}

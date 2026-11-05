import SwiftUI

/// 设置 › 会话分诊 (`TriageSettingsModel`): the triage and its model, then the three things it may do.
struct TriageSettingsView: View {
    @Environment(AppModel.self) private var app
    @State private var model: TriageSettingsModel?

    var body: some View {
        SettingsForm {
            if let error = model?.error {
                Section {
                    Label(error, systemImage: Theme.Icon.error).font(Theme.Font.caption).foregroundStyle(Theme.danger)
                }
            }
            if model?.unsupported == true {
                Section {
                    Label("这台 hub 还不支持会话分诊，更新 hub 后再来打开", systemImage: Theme.Icon.info)
                        .font(Theme.Font.secondary).foregroundStyle(.secondary)
                }
            } else {
                Section {
                    toggle(\.enabled, String(localized: "会话分诊"), String(localized: "列表里显示在等什么；做完的不再算完成未看"), Theme.Icon.triage, id: "triage-enabled")
                    ModelConfigurationLink(title: String(localized: "分诊模型"), purposeID: "agent.triage")
                } footer: {
                    Text("Agent 每停下一轮，hub 用分诊模型读它最后的回复（每轮一次，用的是「模型配置 › 会话分诊」里选的模型）。")
                }
                Section {
                    toggle(\.decide, String(localized: "要你决定的开卡片"), String(localized: "Agent 在等你拍板时，问题和选项放进「需要你」"), Theme.Icon.askFirst, id: "triage-decide")
                    toggle(\.autoContinue, String(localized: "出错自动继续"), String(localized: "模型服务临时出错（503 等）后自动发「继续」，最多 3 次"), Theme.Icon.retry, id: "triage-continue")
                    toggle(\.wake, String(localized: "到点叫醒"), String(localized: "在等部署、CI 或另一个 Agent 的，到点叫醒它自己去查"), Theme.Icon.wakeUp, id: "triage-wake")
                } header: {
                    Text("分诊之后")
                } footer: {
                    Text("只在会话分诊打开时起作用。自动继续和到点叫醒会让 Agent 用它自己的模型再跑一轮。推代码、部署、给别人发消息这类事 hub 只会问你，不会自己做。")
                }
            }
        }
        .overlay { if model?.switches == nil && model?.error == nil && model?.unsupported != true { ProgressView() } }
        .themedGroupedPage()
        .detailTitle(String(localized: "会话分诊"))
        .navigationBarTitleDisplayMode(.inline)
        .task {
            if model == nil {
                let api = { [app] in app.api }
                model = TriageSettingsModel(deps: .init(
                    load: {
                        guard let api = api() else { throw APIError(status: 0, message: String(localized: "还没连接 hub")) }
                        return try await api.agentTriage()
                    },
                    save: { next in
                        guard let api = api() else { throw APIError(status: 0, message: String(localized: "还没连接 hub")) }
                        return try await api.setAgentTriage(next)
                    }))
            }
            await model?.load()
        }
    }

    private func toggle(_ key: WritableKeyPath<TriageSwitches, Bool>, _ title: String, _ hint: String, _ symbol: String,
                        id: String) -> some View {
        Toggle(isOn: Binding {
            model?.switches?[keyPath: key] ?? false
        } set: { on in
            Task { await model?.set(key, on) }
        }) {
            Label {
                VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                    Text(title).lineLimit(1)
                    Text(hint).font(Theme.Font.caption).foregroundStyle(.secondary)
                }
            } icon: {
                Image(systemName: symbol)
            }
        }
        .disabled(model?.canChange(key) != true)
        .accessibilityIdentifier(id)
    }
}

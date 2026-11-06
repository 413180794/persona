import SwiftUI

/// Settings › Session Triage (`TriageSettingsModel`): the triage and its model, then the three things it may do.
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
                    Label("This hub doesn't support session triage yet. Update the hub, then turn it on here.", systemImage: Theme.Icon.info)
                        .font(Theme.Font.secondary).foregroundStyle(.secondary)
                }
            } else {
                Section {
                    toggle(\.enabled, String(localized: "Session Triage"), String(localized: "Shows what each session is waiting for in the list; finished ones no longer count as Unread"), Theme.Icon.triage, id: "triage-enabled")
                    ModelConfigurationLink(title: String(localized: "Triage Model"), purposeID: "agent.triage")
                } footer: {
                    Text("Each time an agent stops a turn, the hub reads its last reply with the triage model (once per turn, using the model chosen in Model Configuration › Session Triage).")
                }
                Section {
                    toggle(\.decide, String(localized: "Cards for Your Decisions"), String(localized: "When an agent is waiting on your decision, its question and options go into Needs You"), Theme.Icon.askFirst, id: "triage-decide")
                    toggle(\.autoContinue, String(localized: "Continue After Errors"), String(localized: "After a temporary model service error (503 and the like), sends “continue” automatically, up to 3 times"), Theme.Icon.retry, id: "triage-continue")
                    toggle(\.wake, String(localized: "Wake on Time"), String(localized: "Agents waiting on a deploy, CI or another agent get woken up on time to check for themselves"), Theme.Icon.wakeUp, id: "triage-wake")
                } header: {
                    Text("After Triage")
                } footer: {
                    Text("Works only when session triage is on. Continuing and waking make the agent run another turn on its own model. For things like pushing code, deploying or messaging other people, the hub only asks you and never does them itself.")
                }
            }
        }
        .overlay { if model?.switches == nil && model?.error == nil && model?.unsupported != true { ProgressView() } }
        .themedGroupedPage()
        .detailTitle(String(localized: "Session Triage"))
        .navigationBarTitleDisplayMode(.inline)
        .task {
            if model == nil {
                let api = { [app] in app.api }
                model = TriageSettingsModel(deps: .init(
                    load: {
                        guard let api = api() else { throw APIError(status: 0, message: String(localized: "Not connected to the hub yet")) }
                        return try await api.agentTriage()
                    },
                    save: { next in
                        guard let api = api() else { throw APIError(status: 0, message: String(localized: "Not connected to the hub yet")) }
                        return try await api.setAgentTriage(next)
                    }))
            }
            await model?.load()
        }
        .refreshable { await model?.load() }
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

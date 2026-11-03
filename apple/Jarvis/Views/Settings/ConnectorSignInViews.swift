import SwiftUI

/// What the connector page needs from the app (tests: a FakeHub client).
struct ConnectorDeps {
    var api: () -> APIClient?
    var report: (Error) -> Void

    static var live: ConnectorDeps {
        let model = AppModel.shared
        return ConnectorDeps(api: { model.api }, report: { model.show($0) })
    }
}

/// 设置 › 连接器 › Telegram's own part (a connector the user signs in to here, `HubConnector.signsInHere`): its switch,
/// the sign-in a step at a time (the hub says what each step asks for: docs/plan-telegram.md), signing out and
/// deleting what it brought in.
@Observable final class ConnectorSignInModel {
    let id: String
    /// The step on screen; nil when not signing in.
    private(set) var prompt: ConnectorPrompt?
    var values: [String: String] = [:]
    private(set) var busy = false
    /// What the last delete removed, said for people.
    private(set) var deleted: String?
    @ObservationIgnored private let deps: ConnectorDeps

    init(id: String, deps: ConnectorDeps = .live) {
        self.id = id
        self.deps = deps
    }

    /// Every field of the step on screen is filled in.
    var canSend: Bool {
        guard let fields = prompt?.fields, !busy else { return false }
        return fields.allSatisfy { !(values[$0.name] ?? "").trimmingCharacters(in: .whitespaces).isEmpty }
    }

    func start() async { await send(step: "", values: [:]) }

    /// Sends the step on screen; the answer is the next one, the same one with what was wrong, or done.
    func next() async {
        guard let step = prompt?.step else { return }
        await send(step: step, values: values)
    }

    func cancel() {
        prompt = nil
        values = [:]
    }

    /// True when signing in is done (the page reloads the connector).
    @discardableResult
    private func send(step: String, values: [String: String]) async -> Bool {
        guard let api = deps.api() else { return false }
        busy = true
        defer { busy = false }
        do {
            let p = try await api.connectorLogin(id, step: step, values: values)
            if p.done == true {
                cancel()
                return true
            }
            if p.step != prompt?.step { self.values = [:] } // a new step: its fields start empty
            prompt = p
        } catch {
            deps.report(error)
        }
        return false
    }

    func setEnabled(_ on: Bool) async -> HubConnector? {
        guard let api = deps.api() else { return nil }
        do {
            return try await api.setConnector(id, enabled: on)
        } catch {
            deps.report(error)
            return nil
        }
    }

    func logout() async {
        guard let api = deps.api() else { return }
        busy = true
        defer { busy = false }
        do {
            try await api.connectorLogout(id)
            cancel()
        } catch {
            deps.report(error)
        }
    }

    func deleteData() async {
        guard let api = deps.api() else { return }
        busy = true
        defer { busy = false }
        do {
            let r = try await api.deleteConnectorData(id)
            deleted = "已删除：知识库里 \(r.chats ?? 0) 个会话、\(r.people ?? 0) 个人，收件箱里 \(r.inbox ?? 0) 条。"
        } catch {
            deps.report(error)
        }
    }
}

/// The sections of a connector the user signs in to here, under its status on `HubConnectorView`.
struct ConnectorSignInSections: View {
    var connector: HubConnector
    var reload: () async -> Void
    @State private var model: ConnectorSignInModel
    @State private var confirmLogout = false
    @State private var confirmDelete = false

    init(connector: HubConnector, reload: @escaping () async -> Void, deps: ConnectorDeps = .live) {
        self.connector = connector
        self.reload = reload
        _model = State(initialValue: ConnectorSignInModel(id: connector.id, deps: deps))
    }

    private var enabled: Bool { connector.enabled ?? true }
    private var signedIn: Bool { connector.status == "ok" || connector.status == "rate_limited" }

    var body: some View {
        Section {
            Toggle(isOn: Binding(get: { enabled }, set: { on in
                Task {
                    _ = await model.setEnabled(on)
                    await reload()
                }
            })) {
                Text("同步 \(connector.name)").lineLimit(1)
            }
            .accessibilityIdentifier("connector.enabled")
        } footer: {
            Text(enabled ? "关掉后不再读 \(connector.name)；登录状态和已经存下的数据都留着。"
                 : "打开后才能登录，登录后它的私聊和群进知识库和收件箱，助手也能查。")
        }
        if enabled && !signedIn {
            if connector.signsInHere {
                signInSection
            } else {
                // Just switched on: its program has not started yet (the page asks again every second).
                Section {
                    Text("\(connector.name) 启动后就能登录。").foregroundStyle(.secondary)
                        .accessibilityIdentifier("connector.starting")
                }
            }
        }
        Section {
            if signedIn || connector.via != nil {
                Button(role: .destructive) { confirmLogout = true } label: { Text("退出登录").lineLimit(1) }
                    .disabled(model.busy)
                    .accessibilityIdentifier("connector.logout")
            }
            Button(role: .destructive) { confirmDelete = true } label: { Text("删除已存的数据").lineLimit(1) }
                .disabled(model.busy)
                .accessibilityIdentifier("connector.deleteData")
        } footer: {
            if let deleted = model.deleted {
                Text(deleted)
            } else {
                Text("删除：知识库里从它来的会话和人、收件箱里它的条目，并关掉同步；登录状态不受影响。")
            }
        }
        .confirmationDialog("退出 \(connector.name) 登录？", isPresented: $confirmLogout, titleVisibility: .visible) {
            Button("退出登录", role: .destructive) {
                Task {
                    await model.logout()
                    await reload()
                }
            }
        } message: {
            Text("hub 上存的 API 凭据和登录状态都会删掉，\(connector.name) 那边这个设备会话也会结束。再用要重新登录。")
        }
        .confirmationDialog("删除 \(connector.name) 存下的数据？", isPresented: $confirmDelete, titleVisibility: .visible) {
            Button("删除", role: .destructive) {
                Task {
                    await model.deleteData()
                    await reload()
                }
            }
        } message: {
            Text("知识库里从 \(connector.name) 来的会话和人、收件箱里它的条目都会删掉，同步会关掉。项目的总结在下次更新时不再包含它们。")
        }
    }

    @ViewBuilder private var signInSection: some View {
        Section {
            if let p = model.prompt {
                if let hint = p.hint, !hint.isEmpty {
                    Text(hint).font(Theme.Font.secondary).foregroundStyle(.secondary)
                }
                // Above the fields: below them the keyboard covers it (the field keeps the focus).
                if let e = p.error, !e.isEmpty {
                    Text(e).font(Theme.Font.label).foregroundStyle(Theme.danger)
                        .accessibilityIdentifier("connector.signInError")
                }
                ForEach(p.fields ?? []) { f in
                    field(f)
                }
                Button {
                    Task {
                        await model.next()
                        await reload() // the status above follows (credentials saved: api_id shown; signed in)
                    }
                } label: {
                    Text(model.busy ? "正在验证…" : "下一步").lineLimit(1).frame(maxWidth: .infinity)
                }
                .themedButton(prominent: true)
                .disabled(!model.canSend)
                .listRowBackground(Color.clear)
                .accessibilityIdentifier("connector.next")
            } else {
                Button {
                    Task { await model.start() }
                } label: {
                    Text("登录 \(connector.name)").lineLimit(1).frame(maxWidth: .infinity)
                }
                .themedButton(prominent: true)
                .disabled(model.busy)
                .listRowBackground(Color.clear)
                .accessibilityIdentifier("connector.signIn")
            }
        } header: {
            SettingsHeading("登录")
        } footer: {
            Text("API 凭据和登录状态加密存在 hub 上，只显示 api_id；退出登录时一起删掉。")
        }
    }

    @ViewBuilder private func field(_ f: ConnectorPrompt.Field) -> some View {
        let text = Binding(get: { model.values[f.name] ?? "" }, set: { model.values[f.name] = $0 })
        let prompt = Text(f.placeholder ?? f.label)
        LabeledContent(f.label) {
            Group {
                if f.secret == true {
                    SecureField(f.label, text: text, prompt: prompt)
                } else {
                    TextField(f.label, text: text, prompt: prompt)
                        .keyboardType(Self.keyboard(f.name))
                }
            }
            .multilineTextAlignment(.trailing)
            .textInputAutocapitalization(.never)
            .autocorrectionDisabled()
            .accessibilityIdentifier("connector.field." + f.name)
        }
    }

    private static func keyboard(_ name: String) -> UIKeyboardType {
        switch name {
        case "phone": .phonePad
        case "code", "api_id": .numberPad
        default: .asciiCapable
        }
    }
}

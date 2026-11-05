import PersonaKit
import SwiftUI

/// What the connector page needs from the app (tests: a FakeHub client).
struct ConnectorDeps {
    var api: () -> APIClient?
    var report: (Error) -> Void
    /// One step of the sign-in: a connector's login (the default), or a channel's binding (`ChannelBinding`).
    var login: (APIClient, _ id: String, _ step: String, _ values: [String: String]) async throws -> ConnectorPrompt = { api, id, step, values in
        try await api.connectorLogin(id, step: step, values: values)
    }

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
    /// A QR step's code as a picture for 保存二维码, drawn once per code.
    private(set) var qrImage: UIImage?
    var values: [String: String] = [:]
    private(set) var busy = false
    /// What the last delete removed, said for people.
    private(set) var deleted: String?
    /// What the last 删除已存的知识库内容 removed, said for people.
    private(set) var archiveDeleted: String?
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

    /// From the first step again (a step this app cannot draw: maybe the hub sends another kind the next time).
    func restart() async {
        cancel()
        await start()
    }

    /// Sends the step on screen; the answer is the next one, the same one with what was wrong, or done.
    @discardableResult
    func next() async -> Bool {
        guard let step = prompt?.step else { return false }
        return await send(step: step, values: values)
    }

    /// A code on screen (GitHub's device flow, a QR code to scan): asks again every interval the hub gave until the user
    /// has entered or scanned it (true: signed in) or the step changes; cancelled with the page.
    func waitForAuthorization() async -> Bool {
        while let p = prompt, p.polls, !Task.isCancelled {
            try? await Task.sleep(for: .seconds(max(p.interval ?? 5, 1)))
            if Task.isCancelled || prompt?.step != p.step { return false }
            if await send(step: p.step ?? "", values: [:]) { return true }
        }
        return false
    }

    func cancel() {
        prompt = nil
        qrImage = nil
        values = [:]
    }

    /// True when signing in is done (the page reloads the connector).
    @discardableResult
    private func send(step: String, values: [String: String]) async -> Bool {
        guard let api = deps.api() else { return false }
        busy = true
        defer { busy = false }
        do {
            let p = try await deps.login(api, id, step, values)
            if p.done == true {
                cancel()
                return true
            }
            if p.step != prompt?.step { self.values = [:] } // a new step: its fields start empty
            if !p.isQRCode {
                qrImage = nil
            } else if let rows = p.qr?.rows, rows != prompt?.qr?.rows || qrImage == nil {
                qrImage = QRCodeView.image(rows)
            }
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
            deleted = String(localized: "已删除：知识库里 \(r.chats ?? 0) 个会话、\(r.people ?? 0) 个人，收件箱里 \(r.inbox ?? 0) 条。")
        } catch {
            deps.report(error)
        }
    }

    func setFeed(_ feed: ConnectorFeed, on: Bool) async -> HubConnector? {
        guard let api = deps.api() else { return nil }
        do {
            return try await api.setConnector(id, feed: feed, on: on)
        } catch {
            deps.report(error)
            return nil
        }
    }

    func deleteArchive() async {
        guard let api = deps.api() else { return }
        busy = true
        defer { busy = false }
        do {
            let r = try await api.deleteConnectorArchive(id)
            archiveDeleted = String(localized: "已删除：知识库里 \(r.chats ?? 0) 个会话、\(r.people ?? 0) 个人。")
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

    init(connector: HubConnector, reload: @escaping () async -> Void, deps: ConnectorDeps = .live) {
        self.connector = connector
        self.reload = reload
        _model = State(initialValue: ConnectorSignInModel(id: connector.id, deps: deps))
    }

    var body: some View {
        ConnectorSwitchSection(connector: connector, model: model, reload: reload)
        if connector.isEnabled && !connector.isSignedIn {
            if connector.signsInHere {
                ConnectorSignInSteps(name: connector.name, model: model, reload: reload,
                                     footer: ConnectorText.signInFooter(connector))
            } else {
                // Just switched on: its program has not started yet (the page asks again every second).
                Section {
                    Text("\(connector.name) 启动后就能登录。").foregroundStyle(.secondary)
                        .accessibilityIdentifier("connector.starting")
                }
            }
        }
        ConnectorFeedsSection(connector: connector, model: model, reload: reload)
        ConnectorDataSection(connector: connector, model: model, reload: reload, showsLogout: true)
    }
}

extension HubConnector {
    var isEnabled: Bool { enabled ?? true }
    var isSignedIn: Bool { status == "ok" || status == "rate_limited" }
}

/// 同步 <name>: a connector's switch.
struct ConnectorSwitchSection: View {
    var connector: HubConnector
    var model: ConnectorSignInModel
    var reload: () async -> Void

    var body: some View {
        Section {
            Toggle(isOn: Binding(get: { connector.isEnabled }, set: { on in
                Task {
                    _ = await model.setEnabled(on)
                    await reload()
                }
            })) {
                Text(connector.storesData ? "同步 \(connector.name)" : "启用 \(connector.name)").lineLimit(1)
            }
            .accessibilityIdentifier("connector.enabled")
        } footer: {
            Text(footer)
        }
    }

    private var footer: String {
        let reads = ConnectorText.profile(connector.id).reads ?? String(localized: "它的内容")
        return switch (connector.isEnabled, connector.storesData) {
        case (true, true): String(localized: "关掉后不再读 \(connector.name)；登录状态和已经存下的数据都留着。")
        case (true, false): String(localized: "关掉后 \(connector.name) 停下来；登录状态留着。")
        case (false, false): String(localized: "打开后才能登录。")
        case (false, true): String(localized: "打开后才能登录，登录后\(reads)进知识库和收件箱，助手也能查。")
        }
    }
}

/// 进知识库 and 进收件箱, for a connector that has them (the hub lists `archive`, `inbox`), and 删除已存的知识库内容.
struct ConnectorFeedsSection: View {
    var connector: HubConnector
    var model: ConnectorSignInModel
    var reload: () async -> Void
    @State private var confirmDelete = false

    var body: some View {
        if connector.archive != nil || connector.inbox != nil {
            Section {
                if let on = connector.archive {
                    toggle(String(localized: "进知识库"), on: on, feed: .archive).accessibilityIdentifier("connector.archive")
                }
                if let on = connector.inbox {
                    toggle(String(localized: "进收件箱"), on: on, feed: .inbox).accessibilityIdentifier("connector.inbox")
                }
                if connector.archive != nil {
                    Button(role: .destructive) { confirmDelete = true } label: { Text("删除已存的知识库内容").lineLimit(1) }
                        .disabled(model.busy)
                        .accessibilityIdentifier("connector.deleteArchive")
                }
            } footer: {
                Text(model.archiveDeleted ?? footer)
            }
            .confirmationDialog("删除 \(connector.name) 存进知识库的内容？", isPresented: $confirmDelete, titleVisibility: .visible) {
                Button("删除", role: .destructive) {
                    Task {
                        await model.deleteArchive()
                        await reload()
                    }
                }
            } message: {
                Text("知识库里从 \(connector.name) 来的会话和人都会删掉，「进知识库」会关掉。收件箱和助手查 \(connector.name) 不受影响。")
            }
        }
    }

    private var footer: String {
        var lines: [String] = []
        if connector.archive != nil {
            lines.append(connector.archive == true ? String(localized: "进知识库：\(connector.name) 的内容存进知识库，助手和 AI 分身都能查。关掉后不再存新的，已存的留着。")
                         : String(localized: "不进知识库：不再存新的内容。收件箱和助手查 \(connector.name) 照常。"))
        }
        if connector.inbox == false {
            lines.append(String(localized: "不进收件箱：不再收新的，已有的条目留着。"))
        }
        return lines.joined(separator: "\n")
    }

    private func toggle(_ title: String, on: Bool, feed: ConnectorFeed) -> some View {
        Toggle(isOn: Binding(get: { on }, set: { value in
            Task {
                _ = await model.setFeed(feed, on: value)
                await reload()
            }
        })) {
            Text(title).lineLimit(1)
        }
    }
}

/// 退出登录 (when it shows) and 删除已存的数据.
struct ConnectorDataSection: View {
    var connector: HubConnector
    var model: ConnectorSignInModel
    var reload: () async -> Void
    var showsLogout: Bool
    @State private var confirmLogout = false
    @State private var confirmDelete = false

    private var logout: Bool { showsLogout && (connector.isSignedIn || connector.via != nil) }

    var body: some View {
        if logout || connector.storesData {
            sections
        }
    }

    private var sections: some View {
        Section {
            if logout {
                Button(role: .destructive) { confirmLogout = true } label: { Text("退出登录").lineLimit(1) }
                    .disabled(model.busy)
                    .accessibilityIdentifier("connector.logout")
            }
            if connector.storesData {
                Button(role: .destructive) { confirmDelete = true } label: { Text("删除已存的数据").lineLimit(1) }
                    .disabled(model.busy)
                    .accessibilityIdentifier("connector.deleteData")
            }
        } footer: {
            if let deleted = model.deleted {
                Text(deleted)
            } else if connector.storesData {
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
            Text(ConnectorText.logoutMessage(connector))
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
}

/// Signing in a step at a time (the hub says what each step asks for): the step's card, then its button on its own
/// under the card (下一步, or the page to enter a code on), so the card keeps its rounded corners.
struct ConnectorSignInSteps: View {
    /// The connector's (or channel's) name, in the buttons and waiting lines.
    var name: String
    var model: ConnectorSignInModel
    var reload: () async -> Void
    var footer: String
    /// The section's heading and the first button: 登录 for a connector, 绑定 / 扫码绑定 for a channel.
    var heading = String(localized: "登录")
    var start: String?

    var body: some View {
        if let p = model.prompt {
            Section {
                if let hint = p.hint, !hint.isEmpty {
                    Text(hint).font(Theme.Font.secondary).foregroundStyle(.secondary)
                }
                // Above the fields: below them the keyboard covers it (the field keeps the focus).
                if let e = p.error, !e.isEmpty {
                    Text(e).font(Theme.Font.label).foregroundStyle(Theme.danger)
                        .accessibilityIdentifier("connector.signInError")
                }
                if p.isDeviceCode || p.isPairCode {
                    deviceCode(p)
                } else if p.isQRCode {
                    qrCode(p)
                } else if p.cannotDraw {
                    Text(p.cannotDrawNote(name: name))
                        .font(Theme.Font.secondary).foregroundStyle(Theme.cautionText)
                        .accessibilityIdentifier("connector.cannotDraw")
                } else {
                    ForEach(p.fields ?? []) { f in
                        field(f)
                    }
                }
            } header: {
                SettingsHeading(heading)
            }
            .task(id: p.pollKey) {
                if await model.waitForAuthorization() { await reload() }
            }
            Section {
                if p.isDeviceCode {
                    if let s = p.url, let url = URL(string: s) {
                        #if os(macOS)
                        // Standard buttons at the trailing end on the Mac, not bars the width of the form (#1221).
                        HStack {
                            Spacer(minLength: 0)
                            Link(destination: url) {
                                Label("打开 \(name) 输入验证码", systemImage: Theme.Icon.openExternal).lineLimit(1)
                            }
                            .formButton(prominent: true)
                            .accessibilityIdentifier("connector.openVerification")
                        }
                        .listRowBackground(Color.clear)
                        #else
                        Link(destination: url) {
                            Label("打开 \(name) 输入验证码", systemImage: Theme.Icon.openExternal).lineLimit(1).frame(maxWidth: .infinity)
                        }
                        .themedButton(prominent: true)
                        .listRowBackground(Color.clear)
                        .accessibilityIdentifier("connector.openVerification")
                        #endif
                    }
                } else if p.isPairCode {
                    EmptyView() // nothing to press: the code goes to the bot from the chat app
                } else if p.cannotDraw {
                    FormAction {
                        Button {
                            Task { await model.restart() }
                        } label: {
                            Text("重新开始").lineLimit(1).formActionWidth()
                        }
                        .formButton(prominent: true)
                        .disabled(model.busy)
                        .accessibilityIdentifier("connector.restart")
                    }
                } else if p.isQRCode {
                    if let image = model.qrImage {
                        FormAction {
                            ShareLink(item: Image(uiImage: image), preview: SharePreview("二维码", image: Image(uiImage: image))) {
                                Label("保存二维码", systemImage: Theme.Icon.share).lineLimit(1).formActionWidth()
                            }
                            .formButton(prominent: false)
                            .accessibilityIdentifier("connector.shareQR")
                        }
                    }
                } else {
                    FormAction {
                        Button {
                            Task {
                                await model.next()
                                await reload() // the status above follows (credentials saved: api_id shown; signed in)
                            }
                        } label: {
                            Text(model.busy ? "正在验证…" : "下一步").lineLimit(1).formActionWidth()
                        }
                        .formButton(prominent: true)
                        .disabled(!model.canSend)
                        .accessibilityIdentifier("connector.next")
                    }
                }
            } footer: {
                Text(footer)
            }
        } else {
            Section {
                FormAction {
                    Button {
                        Task { await model.start() }
                    } label: {
                        Text(start ?? String(localized: "登录 \(name)")).lineLimit(1).formActionWidth()
                    }
                    .formButton(prominent: true)
                    .disabled(model.busy)
                    .accessibilityIdentifier("connector.signIn")
                }
            } header: {
                SettingsHeading(heading)
            } footer: {
                Text(footer)
            }
        }
    }

    /// The code to enter on the sign-in page (GitHub's device flow): large, copyable; the page asks the hub again on
    /// its own until the user has entered it.
    @ViewBuilder private func deviceCode(_ p: ConnectorPrompt) -> some View {
        HStack {
            Text(p.userCode ?? "").font(Theme.Font.display).monospaced().textSelection(.enabled).lineLimit(1)
                .minimumScaleFactor(0.6)
                .accessibilityIdentifier(p.isPairCode ? "connector.pairCode" : "connector.userCode")
            Spacer(minLength: Theme.Space.s)
            Button {
                UIPasteboard.general.string = p.userCode
                Haptics.success()
            } label: {
                Label("复制", systemImage: Theme.Icon.copy).labelStyle(.iconOnly)
            }
            .buttonStyle(.borderless)
            .accessibilityLabel(p.isPairCode ? "复制配对码" : "复制验证码")
        }
        HStack(spacing: Theme.Space.s) {
            ProgressView()
            Text(p.isPairCode ? "等你把它发给机器人…" : "等你在 \(name) 上授权…").font(Theme.Font.secondary).foregroundStyle(.secondary).lineLimit(1)
        }
    }

    /// The QR code to scan with the other app: the page asks the hub again on its own until it is scanned and
    /// confirmed there. On the phone that runs the other app, 保存二维码 below puts it in the photo library to scan from.
    @ViewBuilder private func qrCode(_ p: ConnectorPrompt) -> some View {
        QRCodeView(rows: p.qr?.rows ?? [])
            .scaledFrame(width: Theme.Size.box200, height: Theme.Size.box200)
            .frame(maxWidth: .infinity)
            .accessibilityIdentifier("connector.qrCode")
        HStack(spacing: Theme.Space.s) {
            ProgressView()
            Text("等你扫码确认…").font(Theme.Font.secondary).foregroundStyle(.secondary).lineLimit(1)
        }
    }

    @ViewBuilder private func field(_ f: ConnectorPrompt.Field) -> some View {
        #if os(iOS) || os(visionOS)
        if Self.stacked(f) {
            // A label and a placeholder too long for one line: the title above, the field the row's width below. Left to
            // LabeledContent, the row wrapped on its own and the field in it never took the focus (a long token hint).
            VStack(alignment: .leading, spacing: Theme.Space.xs) {
                Text(f.label).lineLimit(1)
                input(f).multilineTextAlignment(.leading)
            }
        } else {
            LabeledContent(f.label) { input(f).multilineTextAlignment(.trailing) }
        }
        #else
        LabeledContent(f.label) { input(f).multilineTextAlignment(.trailing) }
        #endif
    }

    @ViewBuilder private func input(_ f: ConnectorPrompt.Field) -> some View {
        let text = Binding(get: { model.values[f.name] ?? "" }, set: { model.values[f.name] = $0 })
        let prompt = Text(f.placeholder ?? f.label)
        #if os(iOS) || os(visionOS)
        if f.secret == true {
            // A token, an API hash, another service's password: never offered to the system's passwords (#1598).
            SecretField(f.placeholder ?? f.label, filled: String(localized: "已填写"), text: text, identifier: "connector.field." + f.name)
        } else {
            TextField(f.label, text: text, prompt: prompt)
                .keyboardType(Self.keyboard(f.name))
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .accessibilityIdentifier("connector.field." + f.name)
        }
        #else
        Group {
            if f.secret == true {
                SecureField(f.label, text: text, prompt: prompt)
            } else {
                TextField(f.label, text: text, prompt: prompt)
                    .keyboardType(Self.keyboard(f.name))
            }
        }
        .textInputAutocapitalization(.never)
        .autocorrectionDisabled()
        .accessibilityIdentifier("connector.field." + f.name)
        #endif
    }

    /// Whether a field's label and placeholder together are wider than a phone's row holds on one line, in widths of a
    /// CJK character (a Latin one about half).
    static func stacked(_ f: ConnectorPrompt.Field) -> Bool {
        let width = (f.label + (f.placeholder ?? f.label)).reduce(0.0) { $0 + ($1.isASCII ? 0.55 : 1) }
        return width > 16
    }

    private static func keyboard(_ name: String) -> UIKeyboardType {
        switch name {
        case "phone": .phonePad
        case "code", "api_id": .numberPad
        case "base_url": .URL
        default: .asciiCapable
        }
    }
}


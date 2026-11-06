import SwiftUI

// 机器 › 添加电脑 (docs/onboarding.md 4.2「你的电脑 ↔ hub」, 5.3; docs/protocol.md「电脑的配对码」「电脑的信任」, #4196): a
// signed-in device adds a computer with POST /api/v1/auth/pair-codes {"add"}, hands the pairing link to that computer's
// Persona Link (a line for a Linux terminal, a link to share for a Mac, a line for Windows' PowerShell), counts the code
// down (a new one with {"machine"} once it expires) and waits for the computer to come online. It then asks, on the
// same screen, whether it is the user's: 是我的电脑 (POST /machines/{id}/confirm) or 不是 (…/reject). The same sheet
// confirms a computer that was added elsewhere (`AddComputerRequest.confirm`). After the web's (#3934) and Android's.

/// The systems there are instructions for; the hub takes the same words (`add.os`).
enum ComputerOS: String, CaseIterable, Identifiable, Sendable {
    case mac, linux, windows
    var id: String { rawValue }
    var title: String {
        switch self {
        case .mac: "Mac"
        case .linux: "Linux"
        case .windows: "Windows"
        }
    }
    /// The hub's `add.icon`.
    var icon: String { self == .linux ? "server" : self == .windows ? "desktop" : "laptop" }
    var symbol: String { self == .linux ? Theme.Icon.server : self == .windows ? Theme.Icon.machine : Theme.Icon.laptop }

    /// What a computer's node reported (`os`: darwin, linux, windows…); nil before it connected.
    init?(reported: String?) {
        let os = (reported ?? "").lowercased()
        if os.hasPrefix("linux") { self = .linux }
        else if os.hasPrefix("windows") { self = .windows }
        else if os.hasPrefix("darwin") || os.hasPrefix("mac") { self = .mac }
        else { return nil }
    }
}

/// What 添加电脑 opens on: the form, or one computer waiting to be confirmed (the readiness card's `confirm_computer`,
/// a pending machine's card).
enum AddComputerRequest: Identifiable, Hashable {
    case add
    case confirm(MachineRef)
    var id: String {
        switch self {
        case .add: "add"
        case .confirm(let ref): "confirm|" + ref.path
        }
    }
}

extension AppModel {
    /// The hub lets this device add a computer (`machines_add`); an older hub keeps the old pages.
    var canAddComputer: Bool { hubFeatures.contains(APIClient.machinesAddFeature) }
    /// The group's hub takes 是我的电脑 / 不是 (`machine_trust`).
    func canConfirmComputer(_ group: String) -> Bool { features(group).contains(APIClient.machineTrustFeature) }
    /// 添加电脑 over the whole app (nothing on a hub without `machines_add`).
    func openAddComputer() { if canAddComputer { addComputer = .add } }
    /// 确认电脑 for one computer; nil: the one the hub most likely means (`pendingToConfirm`), else the form.
    func openConfirmComputer(_ ref: MachineRef? = nil) {
        if let ref = ref ?? pendingToConfirm()?.ref { addComputer = .confirm(ref) } else { openAddComputer() }
    }
    /// The computer 确认电脑 is about: `preferred` while it waits, else the first one waiting that is online (it can be
    /// asked about now), else the first one waiting.
    func pendingToConfirm(preferred: String? = nil) -> Machine? { AddComputerText.pendingToConfirm(machines, preferred: preferred) }
}

/// Why the hub did not do it, in words, and what to offer.
struct AddComputerFailure: Equatable {
    var message: String
    var code: String?
}

/// The words, kept apart from the view to test.
enum AddComputerText {
    static let maxName = 40
    /// How to build and install Persona Link on a Mac until there is a download (docs/onboarding.md 拍板 1).
    static let macInstallDoc = URL(string: "https://github.com/nwverse/persona/blob/master/docs/persona-link-install.md")!
    /// How to install it on a Linux without the hub's install script (docs/link.md「Linux（headless）」).
    static let linuxInstallDoc = URL(string: "https://github.com/nwverse/persona/blob/master/docs/link.md#linuxheadless")!

    /// The name as sent: trimmed; nil when empty or too long.
    /// `AppModel.pendingToConfirm`: `preferred` while it waits, else the first one waiting that is online, else the first.
    static func pendingToConfirm(_ machines: [Machine], preferred: String? = nil) -> Machine? {
        let waiting = machines.filter(\.isPending)
        return waiting.first { preferred != nil && $0.id == preferred } ?? waiting.first(where: \.online) ?? waiting.first
    }

    static func validName(_ name: String) -> String? {
        let n = name.trimmingCharacters(in: .whitespacesAndNewlines)
        return n.isEmpty || n.count > maxName ? nil : n
    }

    /// Why the hub refused, by its code (docs/protocol.md「电脑的配对码」); the hub's own words for any other code.
    static func failure(_ error: Error) -> AddComputerFailure {
        let e = error as? APIError
        let code = e?.code
        let theirs = e?.message.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        let message: String
        switch code {
        case "forbidden": message = String(localized: "这个设备不能添加电脑。用已经登录好的手机或电脑上的 Persona 再试。")
        // The hub's words, in plain language and in the user's language (the acceptance of #4196).
        case "tls_required": message = theirs.isEmpty ? String(localized: "hub 还没打开安全连接，电脑接不进来。到 hub 的管理界面里打开安全连接后再试。") : theirs
        case "mesh_member": message = String(localized: "这台 hub 是组里的成员，到中心 hub 上添加电脑。")
        case "too_many_pending": message = String(localized: "已经加了 3 台电脑还没处理完（有的可能还没连上来过）。到「机器」页删掉用不上的，再添加。")
        case "not_pending": message = String(localized: "这台电脑已经确认过了。要给它重新配对，到 hub 的管理界面里做。")
        case "not_found": message = String(localized: "这台电脑已经不在了。")
        case "invalid_name": message = String(localized: "名字不能为空，最多 40 个字。")
        case "invalid_os": message = String(localized: "选一个系统：Mac、Linux 或 Windows。")
        default: message = error.shown ?? error.localizedDescription
        }
        return AddComputerFailure(message: message, code: code)
    }

    /// A machine's `error_code` in words (docs/onboarding.md 5.3); nil for a code this app does not know (show `error`).
    static func machineProblem(_ code: String?) -> String? {
        switch code {
        case "node_service_down": String(localized: "这台电脑上的 Persona 后台服务没在运行，打开 Persona Link 让它重新装一次。")
        case "link_offline": String(localized: "这台电脑离线了：Persona Link 没在运行，或者电脑睡着了。")
        case "never_connected": String(localized: "还没连上过：在那台电脑上用配对链接配对一次。")
        case "node_outdated": String(localized: "这台电脑上的 Persona 版本太旧，没法从这里更新。在那台电脑上重新安装一次。")
        default: nil
        }
    }

    /// What the machine's line says under its name: the known code in words, else the hub's own `error`.
    static func machineLine(_ m: Machine) -> String? {
        if let p = machineProblem(m.errorCode) { return p }
        if m.isPending { return m.online ? String(localized: "有一台电脑想加入，等你确认是不是你的。") : nil }
        return m.error.flatMap { $0.isEmpty ? nil : $0 }
    }

    /// The system the joining computer says it runs, for 「…（macOS）」.
    static func systemName(_ m: Machine) -> String? {
        if let s = m.system?.name, !s.isEmpty { return s }
        switch ComputerOS(reported: m.os) {
        case .mac: return "macOS"
        case .linux: return "Linux"
        case .windows: return "Windows"
        case nil: return nil
        }
    }

    /// The line that pairs an installed Persona Link; the hub's own when it gave one.
    static func pairCommand(_ code: APIClient.ComputerCode) -> String {
        code.command.flatMap { $0.isEmpty ? nil : $0 } ?? "jarvis-link pair " + quoted(code.pairLink)
    }

    /// What a Linux computer runs: the line that installs and pairs, or only the pairing when the hub has no site.
    static func linuxCommand(_ code: APIClient.ComputerCode) -> (line: String, installs: Bool) {
        if let line = code.install?.linux, !line.isEmpty { return (line, true) }
        return (pairCommand(code), false)
    }

    /// What Windows runs in PowerShell, in the folder with the two programs (docs/windows.md).
    static func windowsCommand(_ code: APIClient.ComputerCode) -> String {
        #".\jarvis-link.exe install --jarvisd .\personad.exe --pair "# + quoted(code.pairLink)
    }

    /// A pairing link in single quotes for sh and PowerShell; it has none of its own (it is URL-encoded).
    private static func quoted(_ s: String) -> String { "'" + s.replacingOccurrences(of: "'", with: "%27") + "'" }

    /// The code's end by this device's clock: the hub's, unless it is already past or more than ten minutes away
    /// (a clock that is off), then ten minutes from now.
    static func expiry(_ code: APIClient.ComputerCode, now: Date) -> Date {
        let left = code.expiresAt.timeIntervalSince(now)
        return left > 0 && left <= 600 ? code.expiresAt : now.addingTimeInterval(600)
    }

    static func namePlaceholder(_ os: ComputerOS) -> String {
        switch os {
        case .mac: String(localized: "比如：我的 MacBook")
        case .linux: String(localized: "比如：家里的服务器")
        case .windows: String(localized: "比如：办公室的台式机")
        }
    }
}

/// 添加电脑 / 确认电脑: the form, the code and its countdown, the computer joining, confirmed or rejected.
@Observable final class AddComputerModel {
    struct Deps {
        /// The client of a group's hub.
        var api: (String) -> APIClient?
        var now: () -> Date = { .now }
        /// The group's hub takes confirm / reject.
        var canConfirm: (String) -> Bool = { _ in true }
        /// After confirm / reject: the app's lists again.
        var refreshed: () async -> Void = {}
        /// The system a computer was added as, by machine id (the hub does not keep it).
        var rememberOS: (String, ComputerOS) -> Void = { _, _ in }
        var rememberedOS: (String) -> ComputerOS? = { _ in nil }

        static var live: Deps {
            .init(api: { AppModel.shared.api($0) }, canConfirm: { AppModel.shared.canConfirmComputer($0) },
                  refreshed: { await AppModel.shared.refresh() },
                  rememberOS: { id, os in UserDefaults.standard.set(os.rawValue, forKey: "addComputer.os." + id) },
                  rememberedOS: { id in UserDefaults.standard.string(forKey: "addComputer.os." + id).flatMap(ComputerOS.init(rawValue:)) })
        }
    }

    enum Stage: Equatable {
        /// Pick the system, give a name.
        case form
        /// The code is being made.
        case working
        /// The code is out, counting down (expired at 0), waiting for the computer.
        case waiting
        /// It is online and waits: 是你的吗？
        case joined
        /// 确认电脑 for one that is not online (no code made here).
        case offline
        case confirmed
        case rejected
        /// Removed elsewhere, or it waited too long.
        case gone
    }

    var os: ComputerOS = .mac
    var name = ""
    private(set) var stage: Stage = .form
    private(set) var code: APIClient.ComputerCode?
    /// The computer as the hub lists it now.
    private(set) var machine: Machine?
    /// The hub's id of the computer being added or confirmed.
    private(set) var machineID: String?
    private(set) var failure: AddComputerFailure?
    /// Confirming or rejecting.
    private(set) var busy = false
    private(set) var now: Date
    private(set) var expiresAt: Date?
    let group: String
    @ObservationIgnored private let deps: Deps
    /// The list has had it since the code was made: missing later means it is gone.
    @ObservationIgnored private var seen = false

    /// `request`: .add opens on the form, .confirm(ref) on that computer; `group` is where .add adds it.
    init(_ request: AddComputerRequest = .add, group: String = "", deps: Deps = .live) {
        self.deps = deps
        now = deps.now()
        switch request {
        case .add:
            self.group = group
        case .confirm(let ref):
            self.group = ref.group
            machineID = ref.id
            stage = .offline
            if let os = deps.rememberedOS(ref.id) { self.os = os }
        }
    }

    var validName: String? { AddComputerText.validName(name) }
    var secondsLeft: Int { expiresAt.map { ConnectDeviceText.secondsLeft($0, now: now) } ?? 0 }
    var expired: Bool { stage == .waiting && secondsLeft == 0 }
    /// What the screens call it.
    var computerName: String { machine?.name ?? code?.name ?? validName ?? machineID ?? "" }
    /// A new code is offered for one waiting to be confirmed that never connected (a new code voids the pairing a
    /// sleeping computer has).
    var canReissue: Bool { machine.map { $0.isPending && !$0.online && $0.neverConnected } ?? false }

    /// 下一步: adds the computer and shows its code.
    func submit() async {
        guard let n = validName else {
            failure = AddComputerFailure(message: String(localized: "名字不能为空，最多 40 个字。"), code: "invalid_name")
            return
        }
        guard let api = deps.api(group) else { failure = Self.disconnected; return }
        stage = .working
        failure = nil
        do {
            let c = try await api.computerCode(addNamed: n, os: os.rawValue, icon: os.icon)
            deps.rememberOS(c.machine, os)
            show(c)
        } catch {
            stage = .form
            failure = AddComputerText.failure(error)
        }
    }

    /// 重新生成 / 重新出链接: a new code for the same computer ({"machine": id}).
    func reissue() async {
        guard let id = machineID else { return }
        guard let api = deps.api(group) else { failure = Self.disconnected; return }
        let before = stage
        stage = .working
        failure = nil
        do {
            show(try await api.computerCode(machine: id))
        } catch {
            let f = AddComputerText.failure(error)
            switch f.code {
            case "not_found": stage = .gone
            case "not_pending": stage = .confirmed
            default: stage = before; failure = f
            }
        }
    }

    private func show(_ c: APIClient.ComputerCode) {
        now = deps.now()
        code = c
        machineID = c.machine
        expiresAt = AddComputerText.expiry(c, now: now)
        seen = false
        if c.trust == "confirmed" { stage = .confirmed } else { stage = .waiting }
    }

    func tick() { now = deps.now() }

    /// Reads the machine list once and moves on when the computer joined, was confirmed or went.
    func poll() async {
        guard let id = machineID, let api = deps.api(group) else { return }
        guard let list = try? await api.machines() else { return }
        update(list.first { $0.id == id })
    }

    /// Where it is, from what the list says of it now (as the web's addStage / confirmStage).
    func update(_ m: Machine?) {
        guard [.waiting, .joined, .offline].contains(stage) else { return }
        if let m { machine = m; seen = true }
        switch stage {
        case .waiting:
            guard let m else { if seen { stage = .gone }; return }
            if m.online { stage = m.isPending && deps.canConfirm(group) ? .joined : .confirmed }
        case .joined, .offline:
            guard let m else { stage = .gone; return }
            if !m.isPending { stage = .confirmed }
            else { stage = m.online ? .joined : .offline }
        default: break
        }
    }

    /// 是我的电脑.
    func confirm() async {
        guard let id = machineID, let api = deps.api(group) else { return }
        busy = true
        defer { busy = false }
        failure = nil
        do {
            try await api.confirmMachine(id)
            stage = .confirmed
            await deps.refreshed()
        } catch {
            let f = AddComputerText.failure(error)
            if f.code == "not_found" { stage = .gone } else { failure = f }
        }
    }

    /// 不是 / 删掉: the hub removes it.
    func reject() async {
        guard let id = machineID, let api = deps.api(group) else { return }
        busy = true
        defer { busy = false }
        failure = nil
        do {
            try await api.rejectMachine(id)
            stage = .rejected
            await deps.refreshed()
        } catch {
            let f = AddComputerText.failure(error)
            switch f.code {
            case "not_found": stage = .gone
            case "not_pending": stage = .confirmed
            default: failure = f
            }
        }
    }

    /// The countdown each second and the machine list every three, while there is something to wait for.
    func run() async {
        var n = 0
        if stage == .offline { await poll() }
        while !Task.isCancelled {
            try? await Task.sleep(for: .seconds(1))
            guard !Task.isCancelled else { return }
            tick()
            guard [.waiting, .joined, .offline].contains(stage) else { continue }
            n += 1
            if n % 3 == 0 { await poll() }
        }
    }

    private static var disconnected: AddComputerFailure {
        AddComputerFailure(message: String(localized: "还没连接 hub"), code: nil)
    }
}

extension View {
    /// 添加电脑 / 确认电脑 as a sheet while `request` is set.
    func addComputerSheet(_ request: Binding<AddComputerRequest?>) -> some View {
        modifier(AddComputerSheetModifier(request: request))
    }

    /// The app-wide one (`AppModel.addComputer`), on the root.
    func addComputerPresentation() -> some View { modifier(AppAddComputerPresentation()) }
}

private struct AddComputerSheetModifier: ViewModifier {
    @Environment(AppModel.self) private var model
    @Binding var request: AddComputerRequest?

    func body(content: Content) -> some View {
        content.sheet(item: $request) { r in
            AddComputerSheet(request: r)
                .environment(model)
                #if os(macOS)
                .macSheet()
                #endif
        }
    }
}

private struct AppAddComputerPresentation: ViewModifier {
    @Environment(AppModel.self) private var model

    func body(content: Content) -> some View {
        @Bindable var model = model
        content
            #if os(macOS)
            // On a view of its own: a second sheet on the Mac's root next to 新建 Agent's broke the window's layout.
            .background { Color.clear.addComputerSheet($model.addComputer) }
            #else
            .addComputerSheet($model.addComputer)
            #endif
            #if DEBUG
            // `-showAddComputer YES | add:<mac|linux|windows>:<name> | <machine id>`: 添加电脑 (filled in and sent with add:…),
            // or 确认电脑 for that computer, as the app opens: for screenshots.
            .task {
                guard let v = UserDefaults.standard.string(forKey: "showAddComputer"), !v.isEmpty else { return }
                try? await Task.sleep(for: .seconds(1.5))
                model.addComputer = v == "YES" || v.hasPrefix("add:") ? .add : .confirm(MachineRef(path: v))
            }
            #endif
    }
}

/// The sheet 添加电脑 / 确认电脑 open.
struct AddComputerSheet: View {
    @Environment(AppModel.self) private var app
    @Environment(\.dismiss) private var dismiss
    @State private var page: AddComputerModel
    @State private var docLink: LinkTarget?
    @FocusState private var nameFocused: Bool
    private let confirming: Bool

    init(request: AddComputerRequest, group: String? = nil) {
        _page = State(initialValue: AddComputerModel(request, group: group ?? AppModel.shared.hubGroup))
        if case .confirm = request { confirming = true } else { confirming = false }
    }

    var body: some View {
        NavigationStack {
            SettingsForm { content }
                .themedPage()
                .navigationTitle(confirming ? String(localized: "确认电脑") : String(localized: "添加电脑"))
                .navigationBarTitleDisplayMode(.inline)
                .toolbar { toolbar }
                .pluginLinks($docLink)
        }
        .presentationDetents([.large])
        .presentationDragIndicator(.visible)
        .interactiveDismissDisabled(page.stage == .working || page.busy)
        .task(id: page.stage == .form) {
            // Where the user starts typing: the name, once the sheet is up.
            if page.stage == .form { try? await Task.sleep(for: .seconds(0.5)); nameFocused = true }
        }
        .task {
            #if DEBUG
            if let v = UserDefaults.standard.string(forKey: "showAddComputer"), v.hasPrefix("add:"), page.stage == .form {
                let parts = v.split(separator: ":", maxSplits: 2).map(String.init)
                if parts.count == 3, let os = ComputerOS(rawValue: parts[1]) { page.os = os; page.name = parts[2]; await page.submit() }
            }
            #endif
            await page.run()
        }
        .accessibilityIdentifier("addComputer")
    }

    @ToolbarContentBuilder private var toolbar: some ToolbarContent {
        if page.stage == .form || page.stage == .working {
            ToolbarItem(placement: .cancellationAction) { Button { dismiss() } label: { Text("取消").lineLimit(1) } }
            ToolbarItem(placement: .confirmationAction) {
                Button { Task { await page.submit() } } label: { Text("下一步").lineLimit(1) }
                    .disabled(page.validName == nil || page.stage == .working)
                    .accessibilityIdentifier("addComputer.next")
            }
        } else {
            ToolbarItem(placement: .confirmationAction) {
                Button { dismiss() } label: { (isEnd ? Text("完成") : Text("关闭")).lineLimit(1) }
                    .accessibilityIdentifier("addComputer.done")
            }
        }
    }

    private var isEnd: Bool { [.confirmed, .rejected, .gone].contains(page.stage) }

    @ViewBuilder private var content: some View {
        if let f = page.failure { failure(f) }
        switch page.stage {
        case .form, .working: form
        case .waiting: waiting
        case .joined: joined
        case .offline: offline
        case .confirmed: outcome(symbol: Theme.Icon.successFilled, tone: Theme.positive, title: String(localized: "已添加：\(page.computerName)"),
                                 hint: String(localized: "它会出现在机器列表里，新建 Agent 时可以选它。"), id: "addComputer.confirmed")
        case .rejected: outcome(symbol: Theme.Icon.delete, tone: .secondary, title: String(localized: "已删掉「\(page.computerName)」"),
                                hint: String(localized: "它用过的配对链接也作废了。要加电脑，重新添加一次。"), id: "addComputer.rejected")
        case .gone: outcome(symbol: Theme.Icon.info, tone: .secondary, title: String(localized: "「\(page.computerName)」已经不在了"),
                            hint: String(localized: "它可能在别处被删掉了，或者等确认等得太久。要加电脑，重新添加一次。"), id: "addComputer.gone")
        }
    }

    private func failure(_ f: AddComputerFailure) -> some View {
        Section {
            Label(f.message, systemImage: Theme.Icon.errorFilled)
                .font(Theme.Font.secondary).foregroundStyle(Theme.danger)
                .accessibilityIdentifier("addComputer.error")
        }
    }

    // MARK: The form

    @ViewBuilder private var form: some View {
        @Bindable var page = page
        Section {
            Picker("这台电脑的系统", selection: $page.os) {
                ForEach(ComputerOS.allCases) { Text($0.title).lineLimit(1).tag($0) }
            }
            .pickerStyle(.segmented)
            .labelsHidden()
            .accessibilityIdentifier("addComputer.os")
        } header: {
            Text("这台电脑的系统").textCase(nil)
        }
        Section {
            TextField(AddComputerText.namePlaceholder(page.os), text: $page.name)
                .focused($nameFocused)
                .submitLabel(.next)
                .onSubmit { Task { await page.submit() } }
                .accessibilityIdentifier("addComputer.name")
        } header: {
            Text("名字").textCase(nil)
        } footer: {
            Text("在 App 和网页里显示，可以用中文，最多 40 个字。")
        }
        if page.stage == .working {
            Section { ProgressView().frame(maxWidth: .infinity) }
        }
    }

    // MARK: Waiting for it

    @ViewBuilder private var waiting: some View {
        if let code = page.code {
            if page.expired {
                Section {
                    Button { Task { await page.reissue() } } label: {
                        Label("重新生成", systemImage: Theme.Icon.reload).lineLimit(1)
                    }
                    .accessibilityIdentifier("addComputer.renew")
                } header: {
                    Text("这个链接已经过期，那台电脑还没连上来。").textCase(nil)
                }
            } else {
                instructions(code)
                Section {
                    HStack(spacing: Theme.Space.m) {
                        ProgressView()
                        VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                            Text("等「\(page.computerName)」连上来…").font(Theme.Font.body).lineLimit(2)
                            Text("\(ConnectDeviceText.clock(page.secondsLeft)) 后失效")
                                .font(Theme.Font.caption.monospacedDigit()).foregroundStyle(.secondary).lineLimit(1)
                                .accessibilityIdentifier("addComputer.countdown")
                        }
                    }
                    .accessibilityElement(children: .combine)
                    .accessibilityIdentifier("addComputer.waiting")
                }
            }
        }
    }

    @ViewBuilder private func instructions(_ code: APIClient.ComputerCode) -> some View {
        switch page.os {
        case .linux:
            let line = AddComputerText.linuxCommand(code)
            Section {
                step(line.installs ? String(localized: "在那台电脑上打开终端，粘贴下面这一行，回车。它会装好 Persona 并连上来。")
                                   : String(localized: "先在那台电脑上装好 Persona Link，再打开终端，粘贴下面这一行，回车。"))
                if !line.installs { docButton(String(localized: "怎么装"), AddComputerText.linuxInstallDoc) }
                command(line.line)
            } footer: {
                Text("看到「已配对」就好了，这一屏会自己往下走。")
            }
        case .mac:
            Section {
                step(String(localized: "在那台 Mac 上装好 Persona Link。目前还没有安装包，要自己从源码编译。"))
                docButton(String(localized: "安装说明"), AddComputerText.macInstallDoc)
            }
            Section {
                step(String(localized: "把配对链接发到那台 Mac，点菜单栏里的 Persona Link 图标 › 配对…，粘贴进去。"))
                if let url = URL(string: code.pairLink) {
                    ShareLink(item: url, subject: Text("Persona 配对链接")) {
                        Label("分享配对链接", systemImage: Theme.Icon.share).lineLimit(1)
                    }
                    .accessibilityIdentifier("addComputer.share")
                }
                copyButton(code.pairLink, title: String(localized: "拷贝配对链接"), said: String(localized: "已拷贝配对链接"))
            }
        case .windows:
            Section {
                step(String(localized: "在那台电脑上把 Persona 的两个程序放在同一个文件夹里。目前还没有安装包，要自己编译，托盘程序也还在做。"))
                step(String(localized: "在这个文件夹里打开 PowerShell，粘贴下面这一行，回车。"))
                command(AddComputerText.windowsCommand(code))
            } footer: {
                Text("它会装成登录时自动启动的后台任务，不弹窗口，也不用管理员权限。")
            }
        }
    }

    private func step(_ text: String) -> some View {
        Text(text).font(Theme.Font.body).fixedSize(horizontal: false, vertical: true)
    }

    @ViewBuilder private func command(_ line: String) -> some View {
        Text(line)
            .font(Theme.Font.code)
            .textSelection(.enabled)
            .lineLimit(4)
            .truncationMode(.middle)
            .accessibilityIdentifier("addComputer.command")
        copyButton(line, title: String(localized: "拷贝"), said: String(localized: "已拷贝"))
    }

    private func copyButton(_ text: String, title: String, said: String) -> some View {
        Button {
            UIPasteboard.general.string = text
            app.toast = Toast(said, style: .success)
        } label: {
            Label(title, systemImage: Theme.Icon.copy).lineLimit(1)
        }
        .accessibilityIdentifier("addComputer.copy")
    }

    private func docButton(_ title: String, _ url: URL) -> some View {
        Button { docLink = LinkTarget(url: url) } label: {
            Label(title, systemImage: Theme.Icon.openDocument).lineLimit(1)
        }
    }

    // MARK: It joined: is it yours?

    @ViewBuilder private var joined: some View {
        let system = page.machine.flatMap(AddComputerText.systemName)
        Section {
            VStack(spacing: Theme.Space.s) {
                Image(systemName: page.machine.map { ComputerOS(reported: $0.os)?.symbol ?? $0.symbol } ?? page.os.symbol)
                    .font(Theme.Font.display)
                    .foregroundStyle(Theme.accent)
                Text(system.map { String(localized: "有一台电脑想加入：\(page.computerName)（\($0)），是你的吗？") }
                     ?? String(localized: "有一台电脑想加入：\(page.computerName)，是你的吗？"))
                    .font(Theme.Font.heading).multilineTextAlignment(.center)
                Text("确认之后，Persona 才会在上面替你干活、把模型的 Key 给它。不认识就拒绝，它会被删掉。")
                    .font(Theme.Font.secondary).foregroundStyle(.secondary).multilineTextAlignment(.center)
            }
            .frame(maxWidth: .infinity)
            .padding(.vertical, Theme.Space.l)
            .accessibilityElement(children: .combine)
            .accessibilityIdentifier("addComputer.joined")
        }
        if app.canConfirmComputer(page.group) { decision }
    }

    private var decision: some View {
        Section {
            FormAction {
                Button { Task { await page.confirm() } } label: {
                    Group {
                        if page.busy { ProgressView() } else { Text("是我的电脑").lineLimit(1) }
                    }
                    .formActionWidth()
                }
                .themedButton(prominent: true)
                .disabled(page.busy)
                .accessibilityIdentifier("addComputer.confirm")
            }
            FormAction {
                Button(role: .destructive) { Task { await page.reject() } } label: {
                    Text("不是").lineLimit(1).formActionWidth()
                }
                .themedButton()
                .disabled(page.busy)
                .accessibilityIdentifier("addComputer.reject")
            }
        }
        #if !os(macOS)
        .listRowSeparator(.hidden) // two buttons on clear rows: no line between them
        #endif
    }

    // MARK: Confirming one that is not here

    @ViewBuilder private var offline: some View {
        Section {
            VStack(alignment: .leading, spacing: Theme.Space.s) {
                Text("「\(page.computerName)」还没连上来").font(Theme.Font.heading)
                (page.canReissue ? Text("它连上来以后，这里会问你是不是你的电脑。那台电脑还没拿到配对链接，就重新出一个。")
                                 : Text("它连上来以后，这里会问你是不是你的电脑。"))
                    .font(Theme.Font.secondary).foregroundStyle(.secondary)
            }
            .padding(.vertical, Theme.Space.xs)
            .accessibilityElement(children: .combine)
            .accessibilityIdentifier("addComputer.offline")
            if page.canReissue {
                Button { Task { await page.reissue() } } label: {
                    Label("重新出链接", systemImage: Theme.Icon.reload).lineLimit(1)
                }
                .accessibilityIdentifier("addComputer.reissue")
            }
            if app.canConfirmComputer(page.group) {
                Button(role: .destructive) { Task { await page.reject() } } label: {
                    Label("删掉", systemImage: Theme.Icon.delete).lineLimit(1)
                }
                .disabled(page.busy)
            }
        }
    }

    private func outcome(symbol: String, tone: Color, title: String, hint: String, id: String) -> some View {
        Section {
            VStack(spacing: Theme.Space.s) {
                Image(systemName: symbol).font(Theme.Font.display).foregroundStyle(tone)
                Text(title).font(Theme.Font.heading).multilineTextAlignment(.center)
                Text(hint).font(Theme.Font.secondary).foregroundStyle(.secondary).multilineTextAlignment(.center)
            }
            .frame(maxWidth: .infinity)
            .padding(.vertical, Theme.Space.l)
            .accessibilityElement(children: .combine)
            .accessibilityIdentifier(id)
        }
    }
}

import BeautifulUI
import SwiftUI

/// What the hub page needs from the app: the hub, error reporting, and where the status lives for the rest of the app
/// (AppModel.hubUpdate: the line over the Agents list, the connection title). Tests pass a FakeHub's client.
struct HubUpdateDeps {
    var api: () -> APIClient?
    var report: (Error) -> Void
    var publish: (HubUpdateStatus) -> Void

    static var live: HubUpdateDeps {
        let model = AppModel.shared
        return HubUpdateDeps(api: { model.api }, report: { model.show($0) }, publish: { s in if model.hubUpdate != s { model.hubUpdate = s } })
    }
}

/// 设置 › hub (docs/hub-update.md「App 里」): the hub's version and channel, checking for and installing a release,
/// what an update waits for, turning auto-update on and off, and the history.
@Observable final class HubUpdateModel {
    private(set) var status: HubUpdateStatus?
    private(set) var history: [HubUpdateRecord] = []
    private(set) var loaded = false
    /// The hub predates updates in the app (no /api/v1/hub/update).
    private(set) var unsupported = false
    private(set) var checking = false
    private(set) var error: String?
    @ObservationIgnored private let deps: HubUpdateDeps

    init(deps: HubUpdateDeps = .live) { self.deps = deps }

    func load() async {
        guard let api = deps.api() else { return }
        do {
            apply(try await api.hubUpdate())
            history = (try? await api.hubUpdateHistory()) ?? []
            error = nil
        } catch let e as APIError where e.status == 404 {
            unsupported = true
        } catch {
            self.error = error.shown ?? self.error
        }
        loaded = true
    }

    /// A newer status (from a request here, or the `hub_update` event).
    func apply(_ s: HubUpdateStatus) {
        let finished = status?.state != s.state && ["updated", "rolled_back", "failed"].contains(s.state)
        status = s
        deps.publish(s)
        if finished { Task { await reloadHistory() } }
    }

    private func reloadHistory() async {
        guard let api = deps.api() else { return }
        if let h = try? await api.hubUpdateHistory() { history = h }
    }

    func check() async {
        guard let api = deps.api() else { return }
        checking = true
        defer { checking = false }
        do { apply(try await api.checkHubUpdate()) } catch { deps.report(error) }
    }

    /// Starts the update; `now` is the second confirmation's 现在就更新 (it does not wait for idle).
    func update(now: Bool) async {
        guard let api = deps.api() else { return }
        do { apply(try await api.startHubUpdate(now: now)) } catch { deps.report(error) }
    }

    func setAuto(_ on: Bool) async {
        guard let api = deps.api() else { return }
        do { apply(try await api.setHubUpdate(auto: on)) } catch { deps.report(error) }
    }

    /// 「自动更新已开启」 seen: not shown again.
    func noticeSeen() async {
        guard let api = deps.api() else { return }
        do { apply(try await api.setHubUpdate(noticeSeen: true)) } catch { deps.report(error) }
    }
}

/// The page.
struct HubSettingsPage: View {
    @Environment(AppModel.self) private var app
    @State private var model: HubUpdateModel
    @State private var confirmNow = false

    init(model: HubUpdateModel = HubUpdateModel()) { _model = State(initialValue: model) }

    var body: some View {
        List {
            if model.unsupported {
                Section {
                    Text("这个 hub 的版本太旧，不支持在 App 里查看和更新版本。在 hub 所在的机器上用 install.sh 更新一次以后就可以了。")
                        .foregroundStyle(.secondary)
                }
            } else if let s = model.status {
                versionSection(s)
                updateSection(s)
                if s.canUpdate { autoSection(s) }
                nodesSection
                if !model.history.isEmpty { historySection }
            } else if let e = model.error {
                Section { Text(e).foregroundStyle(.secondary) }
            } else {
                Section { ProgressView().frame(maxWidth: .infinity) }
            }
        }
        .themedPage()
        .navigationTitle("hub")
        .task { await model.load() }
        .refreshable { await model.check() }
        .onChange(of: app.hubUpdate) { _, s in
            if let s, s != model.status { model.apply(s) }
        }
        .confirmationDialog("现在就更新？", isPresented: $confirmNow, titleVisibility: .visible) {
            Button("现在就更新", role: .destructive) { Task { await model.update(now: true) } }
        } message: {
            Text("不再等它们结束：会中断正在等 hub 的操作（助手这一轮、正在上传的文件、终端连接）。herdr 里的 Agent 不受影响，hub 起来后会自己连回来。")
        }
    }

    // MARK: Sections

    private func versionSection(_ s: HubUpdateStatus) -> some View {
        Section {
            Group {
                info("版本", s.version, code: true)
                info("渠道", s.channelTitle)
                if let at = s.lastCheck { info("上次检查", Fmt.relative(at)) }
                Button {
                    Task { await model.check() }
                } label: {
                    HStack {
                        Text(model.checking ? "正在检查…" : "检查更新").lineLimit(1)
                        if model.checking { Spacer(); ProgressView().controlSize(.small) }
                    }
                }
                .disabled(model.checking || s.working)
                .accessibilityIdentifier("hub.check")
            }
            .bareListRow()
        }
    }

    @ViewBuilder private func updateSection(_ s: HubUpdateStatus) -> some View {
        Section {
            if s.channel == "docker" {
                dockerRows(s)
            } else if !s.canUpdate {
                Text(s.whyNot.map { "这个 hub 不会自己更新：\($0)。" } ?? "这个 hub 不会自己更新。").foregroundStyle(.secondary)
            } else if let l = s.latest, l.newer {
                VStack(alignment: .leading, spacing: Theme.Space.xs) {
                    Text("新版本 \(l.version)").font(Theme.Font.bodyStrong).lineLimit(1)
                    if let notes = l.notes, !notes.isEmpty { Text(notes).font(Theme.Font.secondary).foregroundStyle(.secondary) }
                    if l.bad {
                        Label("这一版上次装完起不来，已经回滚过；自动更新不会再装它。", systemImage: Theme.Icon.error)
                            .font(Theme.Font.secondary).foregroundStyle(Theme.caution)
                    }
                }
                if s.working {
                    progressRows(s)
                } else {
                    Button {
                        Task { await model.update(now: false) }
                    } label: {
                        Text("更新 hub").lineLimit(1).frame(maxWidth: .infinity) // the version is right above
                    }
                    .themedButton(prominent: true)
                    .listRowBackground(Color.clear)
                    .accessibilityIdentifier("hub.update")
                }
            } else if s.working {
                progressRows(s)
            } else {
                Label("已是最新", systemImage: Theme.Icon.successFilled).foregroundStyle(Theme.positive)
            }
            if let e = s.error, !e.isEmpty, !s.working {
                Text(e).font(Theme.Font.secondary).foregroundStyle(s.state == "rolled_back" ? Theme.caution : Theme.danger)
            }
        } header: {
            SettingsHeading("更新")
        } footer: {
            if s.canUpdate {
                Text("更新会等助手和所有 Agent 都空闲时再重启 hub；新版本起不来会自动回到上一版。重启期间连接会断开一会儿。")
            }
        }
    }

    @ViewBuilder private func progressRows(_ s: HubUpdateStatus) -> some View {
        HStack(spacing: Theme.Space.s) {
            ProgressView().controlSize(.small)
            Text(s.stateTitle).lineLimit(1)
            if let p = s.progress, s.state == "downloading" { Text(p).font(Theme.Font.code).foregroundStyle(.secondary).lineLimit(1) }
        }
        .accessibilityIdentifier("hub.progress")
        if s.state == "waiting_idle", !s.busy.isEmpty {
            ForEach(s.busy) { b in
                Label {
                    Text(b.title).lineLimit(1)
                    Text(b.machine).font(Theme.Font.caption).foregroundStyle(.secondary).lineLimit(1)
                } icon: {
                    Image(systemName: Theme.Icon.inProgress).foregroundStyle(Theme.busy)
                }
            }
            Button(role: .destructive) { confirmNow = true } label: { Text("现在就更新…").lineLimit(1) }
                .accessibilityIdentifier("hub.updateNow")
        }
    }

    @ViewBuilder private func dockerRows(_ s: HubUpdateStatus) -> some View {
        if let l = s.latest, l.newer {
            Text("有新镜像 \(l.version)。在 hub 所在的机器（NAS）上，compose 文件所在的目录里运行：").foregroundStyle(.secondary)
        } else {
            Text("Docker 部署的 hub 由新镜像更新：在 compose 文件所在的目录里运行").foregroundStyle(.secondary)
        }
        HStack {
            Text(Self.dockerCommand).font(Theme.Font.code).lineLimit(1).minimumScaleFactor(0.7).textSelection(.enabled)
            Spacer(minLength: Theme.Space.s)
            Button { UIPasteboard.general.string = Self.dockerCommand; Haptics.success() } label: {
                Image(systemName: Theme.Icon.copy)
            }
            .buttonStyle(.borderless)
            .accessibilityLabel("复制")
        }
    }

    static let dockerCommand = "docker compose pull && docker compose up -d"

    private func autoSection(_ s: HubUpdateStatus) -> some View {
        Section {
            Toggle(isOn: Binding(get: { s.auto }, set: { on in Task { await model.setAuto(on) } })) {
                Text("自动更新 hub").lineLimit(1)
            }
            .accessibilityIdentifier("hub.auto")
        } footer: {
            Text("开着时，有新版本就在助手和所有 Agent 都空闲时自己装上，装完推一条通知。")
        }
    }

    private var nodesSection: some View {
        Section {
            ForEach(app.machines) { m in
                NavigationLink(value: MachineDetailRoute(id: m.id)) {
                    HStack(spacing: Theme.Space.s) {
                        Text(m.name).lineLimit(1)
                        Spacer(minLength: Theme.Space.s)
                        if m.nodeMismatch == true { Chip(text: "版本不同", look: .tone(StatusPalette.working)) }
                        Text(m.nodeVersion ?? (m.online ? "未知" : "离线"))
                            .font(Theme.Font.code).foregroundStyle(.secondary).lineLimit(1).truncationMode(.middle)
                    }
                }
            }
        } header: {
            SettingsHeading("各台电脑上的 jarvisd")
        } footer: {
            Text("hub 更新后会把各台电脑上的 jarvisd 也换成同一个版本；没换上的可以点进去「重新部署」。")
        }
    }

    private var historySection: some View {
        Section {
            ForEach(model.history.prefix(10)) { r in
                VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                    HStack(spacing: Theme.Space.s) {
                        Text(r.resultTitle).font(Theme.Font.bodyStrong)
                            .foregroundStyle(r.result == "updated" ? Theme.positive : r.result == "rolled_back" ? Theme.caution : Theme.danger)
                            .lineLimit(1)
                        if r.auto { Chip(text: "自动", look: .outline) }
                        Spacer(minLength: Theme.Space.s)
                        if let at = r.ended ?? r.started { Text(Fmt.relative(at)).font(Theme.Font.caption).foregroundStyle(.secondary).lineLimit(1) }
                    }
                    Text("\(r.fromVersion) → \(r.toVersion)").font(Theme.Font.code).foregroundStyle(.secondary).lineLimit(1).truncationMode(.middle)
                    if let why = r.reason, !why.isEmpty { Text(why).font(Theme.Font.caption).foregroundStyle(.secondary) }
                }
            }
        } header: {
            SettingsHeading("更新记录")
        }
    }

    @ViewBuilder private func info(_ title: String, _ value: String, code: Bool = false) -> some View {
        if AppStyle.current.usesBeautifulUI {
            BUIListRow(title, value: value)
        } else {
            LabeledContent(title) {
                Text(value).font(code ? Theme.Font.code : Theme.Font.body).lineLimit(1).truncationMode(.middle)
            }
        }
    }
}

/// The one quiet line about the hub over the Agents list, like 「Mac 上的 Jarvis 已登录」 (docs/hub-update.md「App 里」):
/// 自动更新已开启 (once), a newer version when auto-update is off, an update under way, one that went back, or that the
/// hub or the app is too old. No dialog; a tap opens the hub page.
struct HubNoticeRow: View {
    @Environment(AppModel.self) private var model
    @State private var page = false

    var body: some View {
        if let notice = model.hubNotice {
            HStack(spacing: Theme.Space.s) {
                Group {
                    if case .updating = notice { ProgressView().controlSize(.mini) } else { Image(systemName: symbol(notice)) }
                }
                .foregroundStyle(tint(notice))
                .scaledFrame(width: Theme.Size.iconS)
                Text(text(notice)).foregroundStyle(.secondary).lineLimit(2)
                Spacer(minLength: Theme.Space.s)
                actions(notice).buttonStyle(.borderless) // in a list row: each button takes only its own taps
            }
            .font(Theme.Font.secondary)
            .contentShape(.rect)
            .onTapGesture { page = true }
            .accessibilityElement(children: .contain) // or the row's id hides its buttons' (docs/lessons/ios.md)
            .accessibilityIdentifier("agents.hubNotice")
            .sheet(isPresented: $page) { NavigationStack { HubSettingsPage().toolbar { closeButton } } }
        }
    }

    private var closeButton: some ToolbarContent {
        ToolbarItem(placement: .cancellationAction) {
            Button { page = false } label: { Image(systemName: Theme.Icon.close) }.accessibilityLabel("关闭")
        }
    }

    private func text(_ n: HubNotice) -> String {
        switch n {
        case .appTooOld: "App 版本太旧，配不上这个 hub：请到 TestFlight 更新"
        case .hubTooOld: "hub 版本太旧，部分功能用不了"
        case .updating(let what): "hub 正在更新：" + what
        case .rolledBack(let why): why
        case .autoOn: "hub 会在空闲时自动更新"
        case .available(let v, _): "hub 有新版本 " + v
        }
    }

    private func symbol(_ n: HubNotice) -> String {
        switch n {
        case .appTooOld, .hubTooOld, .rolledBack: Theme.Icon.error
        default: Theme.Icon.hubUpdate
        }
    }

    private func tint(_ n: HubNotice) -> Color {
        switch n {
        case .appTooOld, .hubTooOld, .rolledBack: Theme.caution
        default: Theme.accent
        }
    }

    @ViewBuilder private func actions(_ n: HubNotice) -> some View {
        switch n {
        case .autoOn:
            Button("关闭") { Task { await HubUpdateModel().setAuto(false) } }.lineLimit(1).accessibilityIdentifier("hubNotice.autoOff")
            Button("知道了") { Task { await HubUpdateModel().noticeSeen() } }.lineLimit(1).accessibilityIdentifier("hubNotice.seen")
        case .available(_, let build):
            Button("更新") { Task { await HubUpdateModel().update(now: false) } }.lineLimit(1).accessibilityIdentifier("hubNotice.update")
            dismiss(build)
        case .rolledBack:
            dismiss(model.hubUpdate?.latest?.build ?? 0)
        case .hubTooOld:
            dismiss(HubNotice.hubTooOldDismissed)
        case .appTooOld, .updating:
            EmptyView()
        }
    }

    private func dismiss(_ value: Int64) -> some View {
        Button { model.hubNoticeDismissed = value } label: { Image(systemName: Theme.Icon.close) }
            .accessibilityLabel("不再提示")
            .accessibilityIdentifier("hubNotice.dismiss")
    }
}

/// 设置 › 助手 › Skills: the assistant's skills on its machine (GET /api/v1/skills), read only for now.
@Observable final class SkillsModel {
    private(set) var skills: [AgentSkill] = []
    private(set) var machine: String?
    private(set) var loaded = false
    private(set) var error: String?
    @ObservationIgnored private let api: () -> APIClient?

    init(api: @escaping () -> APIClient? = { AppModel.shared.api }) { self.api = api }

    func load() async {
        guard let api = api() else { return }
        do {
            let r = try await api.skills()
            (skills, machine, error) = (r.skills, r.machine, nil)
        } catch {
            self.error = error.shown ?? "读不到"
        }
        loaded = true
    }
}

struct SkillsPage: View {
    @State private var model: SkillsModel

    init(model: SkillsModel = SkillsModel()) { _model = State(initialValue: model) }

    var body: some View {
        List {
            if model.loaded && model.skills.isEmpty {
                Section {
                    EmptyStateView(symbol: Theme.Icon.skills, title: model.error == nil ? "还没有 skill" : "读不到 skill",
                                   message: model.error ?? "把 skill 放进跑助手的那台电脑的 ~/.jarvis/skills/，每个一个目录（里面有 SKILL.md）。")
                }
            } else {
                Section {
                    ForEach(model.skills) { s in
                        VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                            HStack(spacing: Theme.Space.s) {
                                Text(s.name).font(Theme.Font.bodyStrong).lineLimit(1)
                                Spacer(minLength: Theme.Space.s)
                                Chip(text: s.source == "builtin" ? "内置" : "我的", look: s.source == "builtin" ? .outline : .neutral)
                            }
                            if let d = s.description, !d.isEmpty {
                                Text(d).font(Theme.Font.secondary).foregroundStyle(.secondary).lineLimit(3)
                            }
                        }
                    }
                } footer: {
                    Text("放在\(model.machine.map { " \($0) " } ?? "跑助手的那台电脑")的 ~/.jarvis/skills/，每个一个目录（里面有 SKILL.md）。更新 jarvisd 不会动它们；改了以后，下一段新对话起生效。")
                }
            }
        }
        .themedPage()
        .navigationTitle("Skills")
        .task { await model.load() }
        .refreshable { await model.load() }
    }
}

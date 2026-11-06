import SwiftUI

/// Where a readiness button goes (docs/onboarding.md 5.1): a page of this app in a sheet, local help, or the hub's admin
/// page. Until adding a computer (M3) and the model wizard (M4) land, add / confirm open the machines page and
/// connect_model the provider wizard as it is.
enum ReadinessRoute: Identifiable, Hashable {
    /// Local help, never sent to the model: install / log in to Claude Code, wake a computer, what Persona does, how
    /// to connect a model. `machine`: the computer's name the words are about.
    case help(String, machine: String?)
    case machines
    /// The add-a-computer flow, and its confirm screen for the computer waiting (`AppModel.openAddComputer`).
    case addComputer
    case confirmComputer
    case providerWizard
    /// Settings › Model Providers, at one provider when the step names it.
    case provider(String?)
    case link(URL)

    var id: String {
        switch self {
        case .help(let t, let m): "help-\(t)-\(m ?? "")"
        case .machines: "machines"
        case .addComputer: "add-computer"
        case .confirmComputer: "confirm-computer"
        case .providerWizard: "wizard"
        case .provider(let n): "provider-\(n ?? "")"
        case .link(let u): "link-\(u.absoluteString)"
        }
    }

    /// The help topics this app has; any other id opens 「Persona 能做什么」.
    static let helpTopics: Set<String> = ["wake_computer", "install_claude", "login_claude", "what_can_you_do", "how_to_connect_model"]

    /// Where `action` goes; nil for test_assistant (a probe, run where the button is) and ids this app does not know.
    @MainActor static func route(for action: String, launcher: String?, machine: String?, model: AppModel) -> ReadinessRoute? {
        switch action {
        // Add a Computer / Confirm the Computer in place where the hub lets this device do it (#4196); else the machines page.
        case "add_computer": return model.canAddComputer ? .addComputer : .machines
        case "confirm_computer": return model.pendingToConfirm().map { model.canConfirmComputer($0.group) } == true ? .confirmComputer : .machines
        case "wake_computer", "install_claude", "login_claude": return .help(action, machine: machine)
        case "connect_model": return .providerWizard
        case "edit_provider": return .provider(launcher)
        case "enable_tls", "open_admin":
            return model.config.map { .link($0.url.appending(path: "admin")) }
        default: return nil
        }
    }

    /// The button's words for an action (5.1); an id this app does not know gets none (text only).
    static func title(_ action: String) -> String? {
        switch action {
        case "add_computer": String(localized: "添加电脑")
        case "confirm_computer": String(localized: "确认是我的电脑")
        case "wake_computer": String(localized: "怎么唤醒")
        case "install_claude": String(localized: "怎么安装")
        case "login_claude": String(localized: "怎么登录")
        case "connect_model": String(localized: "接上模型")
        case "edit_provider": String(localized: "换 Key")
        case "test_assistant": String(localized: "试一句")
        case "enable_tls", "open_admin": String(localized: "打开管理界面")
        default: nil
        }
    }

    /// What the send button says while the main conversation cannot answer (4.4).
    static func sendTitle(_ step: AssistantReadiness.Step) -> String {
        guard step.id == "computer" else { return String(localized: "先接上模型") }
        switch step.code {
        case "node_unreachable": return String(localized: "先唤醒电脑")
        case "machine_pending": return String(localized: "先确认电脑")
        case "node_outdated": return String(localized: "先更新电脑")
        default: return String(localized: "先添加电脑")
        }
    }
}

/// Runs readiness buttons and shows where they go, for a page that draws readiness cards: `run` from the card, the
/// sheets here. After a provider is saved or the machines page closes, the readiness is asked again.
struct ReadinessSheets: ViewModifier {
    @Environment(AppModel.self) private var model
    @Binding var route: ReadinessRoute?

    @State private var adding: AddComputerRequest?

    func body(content: Content) -> some View {
        content
            .onChange(of: route) { _, r in
                switch r {
                case .addComputer?: adding = .add
                case .confirmComputer?:
                    if let m = model.pendingToConfirm() { adding = .confirm(m.ref) } else { adding = .add }
                default: return
                }
                route = nil
            }
            .addComputerSheet($adding)
            .onChange(of: adding == nil) { _, closed in if closed { model.readinessChanged() } }
            .sheet(item: Binding(get: { route.flatMap { r -> ReadinessRoute? in
                switch r { case .link, .addComputer, .confirmComputer: nil; default: r } } }, set: { route = $0 }),
                   onDismiss: { model.readinessChanged() }) { r in
                sheet(r)
            }
            .sheet(item: Binding(get: { route.flatMap { if case .link(let u) = $0 { LinkTarget(url: u) } else { nil } } },
                                 set: { if $0 == nil { route = nil } })) { LinkSheet(target: $0) }
    }

    @ViewBuilder private func sheet(_ r: ReadinessRoute) -> some View {
        switch r {
        case .help(let topic, let machine):
            NavigationStack { ReadinessHelpView(topic: topic, machine: machine, route: $route) }
        case .machines:
            NavigationStack {
                MachinesView()
                    .navigationDestination(for: MachineDetailRoute.self) { MachineDetailView(ref: $0.ref) }
                    .toolbar { ToolbarItem(placement: .cancellationAction) { DoneButton { route = nil } } }
            }
        case .providerWizard:
            ProviderWizard(start: .pick) { model.readinessChanged() }
        case .provider(let name):
            NavigationStack {
                Group {
                    if let name, !name.isEmpty { ModelLauncherDetailView(name: name) } else { ModelLaunchersView() }
                }
                .toolbar { ToolbarItem(placement: .cancellationAction) { DoneButton { route = nil } } }
            }
        case .link, .addComputer, .confirmComputer:
            EmptyView()
        }
    }
}

/// Done on a sheet's own stack.
private struct DoneButton: View {
    var action: () -> Void
    var body: some View {
        Button(action: action) { Text("完成").lineLimit(1) }
    }
}

extension View {
    func readinessSheets(_ route: Binding<ReadinessRoute?>) -> some View { modifier(ReadinessSheets(route: route)) }
}

/// The card over a new conversation's composer (docs/onboarding.md 4.4): a refused start first (5.2), else the step
/// the readiness says is missing, else Try a Message's result. Nothing on a hub without readiness and nothing when all is well.
struct ReadinessHomeCard: View {
    @Environment(AppModel.self) private var model
    @Binding var route: ReadinessRoute?

    private var store: AssistantReadinessModel { model.readiness }

    var body: some View {
        if let f = store.failure {
            CodedFailureCard(failure: f, route: $route)
        } else if let step = store.current?.cardStep {
            stepCard(step)
        } else if let probe = store.probe {
            ReadinessCard(title: probe.ok == true ? String(localized: "试过了，能回答") : String(localized: "试了一句，没成功"),
                          detail: probe.ok == true ? probe.answer : probe.error, tone: probe.ok == true ? .caution : .danger)
        }
    }

    private func stepCard(_ step: AssistantReadiness.Step) -> some View {
        let primary = step.action?.id
        let alt = step.altActions?.first?.id
        return ReadinessCard(title: step.title ?? String(localized: "还差一步"),
                             detail: step.detail,
                             tone: step.state == "problem" ? .danger : .caution,
                             actionTitle: primary.flatMap(ReadinessRoute.title),
                             action: primary.flatMap { a in ReadinessRoute.title(a).map { _ in { run(a, launcher: step.launcher, machine: machineName(step)) } } },
                             altTitle: alt.flatMap(ReadinessRoute.title),
                             alt: alt.flatMap { a in ReadinessRoute.title(a).map { _ in { run(a, launcher: step.launcher, machine: machineName(step)) } } },
                             busy: store.probing)
    }

    private func machineName(_ step: AssistantReadiness.Step) -> String? {
        if let id = step.machine, let m = model.machines.first(where: { $0.id == id }) { return m.name }
        return store.current?.machine?.name
    }

    private func run(_ action: String, launcher: String?, machine: String?) {
        if action == "test_assistant" {
            Task { await store.refresh(api: model.api, probe: true) }
            return
        }
        route = ReadinessRoute.route(for: action, launcher: launcher, machine: machine, model: model)
    }
}

/// A start or a send the hub refused with a code (docs/onboarding.md 5.2): the hub's words, a button for what to do, the
/// original text under Details; closed with ×. A computer that is offline says so, not that the hub cannot be reached.
struct CodedFailureCard: View {
    @Environment(AppModel.self) private var model
    var failure: CodedFailure
    @Binding var route: ReadinessRoute?

    var body: some View {
        let f = failure
        let title = f.action.flatMap(ReadinessRoute.title)
        let name = f.machine ?? String(localized: "那台电脑")
        ReadinessCard(title: f.error,
                      detail: f.code == "node_unreachable" ? String(localized: "\(name) 现在不在线，可能睡着了") : nil,
                      tone: .danger,
                      actionTitle: title,
                      action: title == nil ? nil : { run(f.action ?? "") },
                      details: [f.detail, f.requestID.map { String(localized: "请求 ID：\($0)") }].compactMap { $0 },
                      dismiss: { withAnimation(Theme.Motion.state) { model.readiness.failure = nil } })
    }

    private func run(_ action: String) {
        if action == "test_assistant" {
            Task { await model.readiness.refresh(api: model.api, probe: true) }
            return
        }
        route = ReadinessRoute.route(for: action, launcher: failure.launcher, machine: failure.machine, model: model)
    }
}

/// Help that opens on this device instead of going to the model: the readiness's local example questions and the
/// install / log-in / wake buttons (docs/onboarding.md 4.4). The commands to paste are shown as they are typed.
struct ReadinessHelpView: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    var topic: String
    var machine: String?
    @Binding var route: ReadinessRoute?

    private var name: String { machine ?? String(localized: "那台电脑") }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: Theme.Space.l) {
                content
            }
            .padding(Theme.Space.page)
            .frame(maxWidth: ChatView.readableWidth, alignment: .leading)
            .frame(maxWidth: .infinity)
        }
        .themedGroupedPage()
        .navigationTitle(title)
        #if os(iOS)
        .navigationBarTitleDisplayMode(.inline)
        #endif
        .toolbar { ToolbarItem(placement: .confirmationAction) { Button { dismiss() } label: { Text("知道了").lineLimit(1) } } }
    }

    private var title: String {
        switch topic {
        case "wake_computer": String(localized: "叫醒\(name)")
        case "install_claude": String(localized: "安装 Claude Code")
        case "login_claude": String(localized: "登录 Claude Code")
        case "how_to_connect_model": String(localized: "怎么接模型")
        default: String(localized: "Persona 能做什么")
        }
    }

    @ViewBuilder private var content: some View {
        switch topic {
        case "wake_computer":
            para(String(localized: "\(name) 现在不在线，可能睡着了，或者上面的 Persona Link 没开。打开盖子或唤醒它，确认 Persona Link 在运行。"))
            para(String(localized: "连上以后这里会自己变好，不用刷新。"), secondary: true)
        case "install_claude":
            para(String(localized: "在 \(name) 上打开终端，粘贴下面这行，回车："))
            CommandLine(text: "curl -fsSL https://claude.ai/install.sh | bash")
            para(String(localized: "装好以后回到这里，过一会儿就会变好。"), secondary: true)
            useKey
        case "login_claude":
            para(String(localized: "在 \(name) 上打开终端，输入下面这个词，回车，按提示在浏览器里登录一次："))
            CommandLine(text: "claude")
            para(String(localized: "登录好以后回到这里，发一句试试。"), secondary: true)
            useKey
        case "how_to_connect_model":
            para(String(localized: "Persona 要一个能用的模型才能回答。两种方式选一种："))
            bullet(String(localized: "有 API Key：在「设置 › 模型供应商」里选一家，粘贴 Key，会自动测试。"))
            bullet(String(localized: "已经在用 Claude Code：在那台电脑上登录一次就行，不用再填什么。"))
            para(String(localized: "不管哪一种，现在主对话都还要在你的一台电脑上运行：只粘贴 API Key、不接电脑，还开始不了。"), secondary: true)
            useKey
        default:
            para(String(localized: "Persona 是你的私人助手。接好电脑和模型以后，它能："))
            bullet(String(localized: "在你的电脑上看文件、跑命令、替你动手（动手前会先问你）"))
            bullet(String(localized: "看你的日程、邮件、飞书和收件箱，帮你整理、草拟回复"))
            bullet(String(localized: "记住聊过的事，下次接着来"))
            if let step = model.readiness.current?.blockingStep, let t = step.title {
                para(String(localized: "现在还差一步：\(t)"), secondary: true)
            }
        }
    }

    private var useKey: some View {
        Button {
            route = .providerWizard
        } label: {
            Text("改用 API Key").font(Theme.Font.secondaryStrong).lineLimit(1)
        }
        .themedButton()
    }

    private func para(_ s: String, secondary: Bool = false) -> some View {
        Text(s).font(Theme.Font.body).lineHeight(.body).foregroundStyle(secondary ? .secondary : .primary)
            .fixedSize(horizontal: false, vertical: true)
    }

    private func bullet(_ s: String) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: Theme.Space.s) {
            Image(systemName: Theme.Icon.done).font(Theme.Font.label).foregroundStyle(Theme.accent)
            para(s)
        }
    }
}

/// A command to paste, with Copy.
private struct CommandLine: View {
    @Environment(AppModel.self) private var model
    var text: String

    var body: some View {
        HStack(spacing: Theme.Space.s) {
            Text(text).font(Theme.Font.code).textSelection(.enabled).frame(maxWidth: .infinity, alignment: .leading)
            Button {
                UIPasteboard.general.string = text
                Haptics.tap()
                model.toast = Toast(String(localized: "已复制"))
            } label: {
                Label("拷贝", systemImage: Theme.Icon.copy).font(Theme.Font.label).lineLimit(1)
            }
            .buttonStyle(.borderless)
            .foregroundStyle(Theme.accent)
        }
        .padding(Theme.Space.m)
        .background(Theme.codeBackground, in: .rect(cornerRadius: Theme.Radius.control, style: .continuous))
    }
}

extension AppModel {
    /// The send button of a new main conversation while it cannot answer (nil: send as before): it goes where the
    /// missing step's button goes.
    func readinessGate(route: Binding<ReadinessRoute?>) -> SendGate? {
        guard let step = readiness.blocking else { return nil }
        return SendGate(title: ReadinessRoute.sendTitle(step)) { [self] in
            let action = step.action?.id ?? ""
            if action == "test_assistant" {
                Task { await readiness.refresh(api: api, probe: true) }
            } else {
                route.wrappedValue = ReadinessRoute.route(for: action, launcher: step.launcher, machine: readiness.current?.machine?.name, model: self)
                    ?? .help("what_can_you_do", machine: nil)
            }
        }
    }

    /// The machines as far as the readiness cares: one added, confirmed, gone, or going on- or offline.
    var readinessMachinesKey: String { machines.map { "\($0.id):\($0.online ? 1 : 0)" }.joined(separator: ",") }

    /// Asks for the readiness on a hub that has it (a visit, the machines changing).
    func refreshReadiness(force: Bool = true) async {
        guard hubFeatures.contains(AssistantReadiness.feature) else { return }
        await readiness.refresh(api: api, force: force)
    }
}

/// Asks for the readiness while the page is in front (`.tabActiveTask`) and when the machines change.
struct ReadinessRefresh: ViewModifier {
    @Environment(AppModel.self) private var model
    func body(content: Content) -> some View {
        content
            // Once as it appears (not a poll: a window drawn in the background, the Mac's, is never 'in front'),
            // then again whenever the page comes to the front.
            .task { await model.refreshReadiness(force: false) }
            .tabActiveTask { await model.refreshReadiness() }
            .onChange(of: model.readinessMachinesKey) { Task { await model.refreshReadiness() } }
            // Right after pairing the hub's features come in after this page asked (and found none).
            .onChange(of: model.hubFeatures.contains(AssistantReadiness.feature)) { Task { await model.refreshReadiness() } }
    }
}

/// `ReadinessRefresh` for a page that is the main conversation only some of the time (a chat page).
struct MainReadinessRefresh: ViewModifier {
    var enabled: Bool
    func body(content: Content) -> some View {
        if enabled { content.modifier(ReadinessRefresh()) } else { content }
    }
}

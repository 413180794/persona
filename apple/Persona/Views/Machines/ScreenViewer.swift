import SwiftUI
import WebKit

/// A Mac's live screen (docs/computer-use.md「实时画面」; #1343): the hub's /screen/<machine> page, which draws macOS's
/// Screen Sharing through the hub's port relay with noVNC, in a drawer like the terminal's — up from the bottom,
/// dragged down to close. Over the picture, what the chat's model is doing on that Mac; under it, 只看 / 接管. The
/// app's device token goes in the fragment (never sent to a server): the page keeps it in memory, and the web view
/// stores nothing. RootView presents it for `AppModel.screenOpen` (`model.openScreen(ref)`).
struct ScreenDrawer: View {
    @Environment(AppModel.self) private var model
    let ref: MachineRef
    var unlock = false
    /// Closes it (ScreenWindow's): down and away, then `AppModel.screenOpen` goes.
    var onClose: () -> Void = {}
    @State private var offset: CGFloat = 0
    @State private var page = ScreenPageState()
    @State private var control = false

    private var name: String { model.machine(ref)?.name ?? ref.id }

    var body: some View {
        VStack(spacing: 0) {
            header
            TimelineView(.periodic(from: .now, by: 1)) { ctx in
                if let a = model.computerActivities[ref], a.shown(at: ctx.date) { stepLine(a) }
            }
            if let url = ScreenViewer.url(model, ref, unlock: unlock) {
                ScreenWebView(url: url, control: control, page: page, machine: ref)
                    .clipShape(.rect(cornerRadius: Theme.Radius.control, style: .continuous))
                    .padding(.horizontal, Theme.Space.s)
            } else {
                EmptyStateView(symbol: Theme.Icon.screen, title: "连不上 hub", message: "连上 hub 后再看这台 Mac 的画面。")
                    .frame(maxHeight: .infinity)
            }
            footer
        }
        .background(Theme.systemCard, in: .rect(cornerRadius: Theme.Radius.card, style: .continuous))
        #if os(iOS)
        .padding(.horizontal, Theme.Space.xs)
        .padding(.bottom, Theme.Space.xxs)
        .offset(y: offset)
        .background {
            // The page shows through, dimmed less as the drawer goes down (TerminalCover's).
            Color.black.opacity(Double(0.4 * (1 - min(offset / 500, 1)))).ignoresSafeArea()
        }
        #endif
        .accessibilityElement(children: .contain) // its parts keep their own identifiers
        .accessibilityIdentifier("screen-drawer")
    }

    /// The handle (a tap closes, a drag down closes), the Mac's name and how the connection is.
    private var header: some View {
        ZStack {
            Capsule().fill(Color.secondary.opacity(0.35)).scaledFrame(width: Theme.Size.wellXL, height: Theme.Size.grabber)
                .scaledFrame(width: Theme.Size.box110, height: Theme.Size.wellS)
                .contentShape(.rect)
                .onTapGesture { Haptics.tap(); onClose() }
                .accessibilityElement()
                .accessibilityLabel("关闭画面")
                .accessibilityAddTraits(.isButton)
                .accessibilityIdentifier("screen-close")
            HStack(spacing: Theme.Space.sm) {
                Image(systemName: Theme.Icon.screen).font(Theme.Font.captionStrong).foregroundStyle(.secondary)
                Text(name).font(Theme.Font.captionStrong).lineLimit(1)
                Spacer(minLength: Theme.Size.box110)
                if page.phase != .live { ProgressView().controlSize(.mini).opacity(page.phase.waiting ? 1 : 0) }
                Text(page.phase.label).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
                    .accessibilityIdentifier("screen-phase")
            }
        }
        .padding(.horizontal, Theme.Space.m)
        .scaledFrame(height: Theme.Size.wellM)
        .contentShape(.rect)
        #if os(iOS)
        .gesture(DragGesture(minimumDistance: 4, coordinateSpace: .global)
            .onChanged { offset = max(0, $0.translation.height) }
            .onEnded { g in
                if g.translation.height > 140 || g.predictedEndTranslation.height > 420 { onClose() } else {
                    withAnimation(Theme.Motion.layout) { offset = 0 }
                }
            })
        #endif
    }

    /// What the chat's model is doing (or just did) on this Mac.
    private func stepLine(_ a: ComputerActivity) -> some View {
        HStack(spacing: Theme.Space.sm) {
            if a.running { ProgressView().controlSize(.mini) } else { Image(systemName: Theme.Icon.success).foregroundStyle(Theme.positive) }
            Text((a.running ? "正在" : "刚才") + "：" + a.step).font(Theme.Font.label).lineLimit(1).truncationMode(.middle)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal, Theme.Space.m)
        .padding(.bottom, Theme.Space.s)
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("screen-step")
    }

    private var footer: some View {
        HStack {
            SegmentedChoice("只看或接管", [(false, "只看"), (true, "接管")], selection: $control)
                .disabled(page.phase != .live)
                .accessibilityIdentifier("screen-control")
        }
        .padding(Theme.Space.m)
    }

    /// The page's address for ref, through the way the app reaches its hub (its SSH tunnel when that is the way).
    @MainActor static func url(_ model: AppModel, _ ref: MachineRef) -> URL? { ScreenViewer.url(model, ref) }
}

/// Builds the page's address.
enum ScreenViewer {
    @MainActor static func url(_ model: AppModel, _ ref: MachineRef, unlock: Bool = false) -> URL? {
        guard let api = model.api(ref.group) else { return nil }
        #if os(macOS)
        let keep = "mac"
        #else
        let keep = "phone"
        #endif
        return url(api.url("/screen/\(ref.id)"), token: api.config.token, name: model.machine(ref)?.name, keep: keep, unlock: unlock)
    }

    /// RFC 3986's unreserved characters, the only ones that go into the fragment as they are.
    nonisolated static let unreserved = CharacterSet(charactersIn: "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~")

    /// The page with this device's token and the Mac's name in its fragment (`#token=…&name=…&keep=phone&embed=1`),
    /// each value percent-encoded once: the page reads them back with URLSearchParams. Set as the encoded fragment,
    /// since `URLComponents.fragment` encodes a second time (a `jd_` token arrived as `jd%5F`, #1498). `keep`: this
    /// device remembers the Mac's sign-in (phone | mac, #1322), the page offers 「在这台手机上记住」.
    nonisolated static func url(_ page: URL, token: String, name: String?, keep: String? = nil, unlock: Bool = false) -> URL? {
        func encoded(_ s: String) -> String { s.addingPercentEncoding(withAllowedCharacters: unreserved) ?? "" }
        var parts = ["token=" + encoded(token)]
        if let name, !name.isEmpty { parts.append("name=" + encoded(name)) }
        if let keep { parts.append("keep=" + encoded(keep)) }
        if unlock { parts.append("unlock=1") } // 一键解锁 (#1507 a-2): the page asks for the password once it is in
        parts.append("embed=1")
        var c = URLComponents(url: page, resolvingAgainstBaseURL: false)
        c?.percentEncodedFragment = parts.joined(separator: "&")
        return c?.url
    }
}

/// The live screen asked for (`AppModel.screenOpen`).
struct ScreenOpen: Identifiable, Hashable {
    let machine: MachineRef
    /// 一键解锁: once signed in, the kept password goes onto the Mac's lock screen.
    var unlock = false
    var id: MachineRef { machine }
}

/// How the page says it is (it posts its phase to the app: web/src/screen/ScreenView.tsx).
@Observable final class ScreenPageState {
    enum Phase: String {
        case checking, blocked, connecting, signin, live, closed

        var label: String {
            switch self {
            case .checking, .connecting: "连接中"
            case .signin: "要登录"
            case .live: "已连接"
            case .blocked, .closed: "没连上"
            }
        }

        var waiting: Bool { self == .checking || self == .connecting }
    }

    var phase: Phase = .checking
}

#if os(iOS)
/// The live screen in a window of its own (#1604): over the shell's window and whatever it presents (a sheet, a page
/// pushed in one), under the toasts' (`ToastWindow`). SwiftUI presents one thing at a time from the root, so a
/// fullScreenCover there was swallowed while a machine page was up in a sheet. `AppModel.screenOpen` shows and hides it.
@MainActor final class ScreenWindow {
    static let shared = ScreenWindow()
    private var window: UIWindow?
    private weak var before: UIWindow?

    func show(_ open: ScreenOpen?) {
        guard let open else {
            window?.isHidden = true
            window = nil
            before?.makeKey()
            return
        }
        let scenes = UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }
        guard let scene = scenes.first(where: { $0.activationState == .foregroundActive }) ?? scenes.first else { return }
        let shell = scene.windows.first { $0.isKeyWindow } ?? scene.windows.first
        if window == nil { before = shell }
        let w = window ?? UIWindow(windowScene: scene)
        w.windowLevel = UIWindow.Level(rawValue: UIWindow.Level.normal.rawValue + 0.5) // under ToastWindow's .normal + 1
        w.backgroundColor = .clear
        w.overrideUserInterfaceStyle = shell?.overrideUserInterfaceStyle ?? .unspecified
        let host = UIHostingController(rootView: ScreenWindowRoot(open: open).environment(AppModel.shared))
        host.view.backgroundColor = .clear
        w.rootViewController = host
        w.isHidden = false
        w.makeKey()
        window = w
    }
}

/// The drawer up from the bottom as it opens, and down again as it closes, then the window goes.
private struct ScreenWindowRoot: View {
    let open: ScreenOpen
    @State private var visible = false

    var body: some View {
        ZStack {
            if visible {
                ScreenDrawer(ref: open.machine, unlock: open.unlock, onClose: close).transition(.move(edge: .bottom))
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .onAppear { withAnimation(Theme.Motion.layout) { visible = true } }
    }

    private func close() {
        withAnimation(Theme.Motion.layout) { visible = false } completion: { AppModel.shared.screenOpen = nil }
    }
}
#else
/// The live screen in a window of its own on the Mac (#1604), as `AppModel.screenOpen` says; closing it clears that.
@MainActor final class ScreenWindow: NSObject, NSWindowDelegate {
    static let shared = ScreenWindow()
    private var window: NSWindow?

    func show(_ open: ScreenOpen?) {
        guard let open else {
            let w = window
            window = nil
            w?.close()
            return
        }
        let host = NSHostingController(rootView: ScreenDrawer(ref: open.machine, unlock: open.unlock) { AppModel.shared.screenOpen = nil }
            .environment(AppModel.shared))
        let w = window ?? NSWindow(contentViewController: host)
        w.contentViewController = host
        w.title = "VNC · " + (AppModel.shared.machine(open.machine)?.name ?? open.machine.id)
        w.styleMask.insert([.resizable, .closable, .miniaturizable])
        w.isReleasedWhenClosed = false
        w.delegate = self
        if window == nil {
            w.setContentSize(NSApp.mainWindow.map { NSSize(width: $0.frame.width * 0.75, height: $0.frame.height * 0.8) } ?? w.frame.size)
            w.center()
        }
        w.makeKeyAndOrderFront(nil)
        window = w
    }

    func windowWillClose(_ notification: Notification) {
        guard window != nil else { return }
        window = nil
        AppModel.shared.screenOpen = nil
    }
}
#endif

/// The page in a web view that stores nothing; it posts its phase (`screen` message), and 只看 / 接管 is passed in.
private final class ScreenBridge: NSObject, WKScriptMessageHandler {
    let page: ScreenPageState
    let machine: MachineRef
    init(page: ScreenPageState, machine: MachineRef) {
        self.page = page
        self.machine = machine
    }

    /// The page's phase, and the Mac's sign-in (#1322): asked for when the Mac wants one (answered from the keychain,
    /// or null for the form), handed over once it worked and 「在这台手机上记住」 was on, or forgotten when it was off.
    func userContentController(_ c: WKUserContentController, didReceive message: WKScriptMessage) {
        MainActor.assumeIsolated {
            guard let body = message.body as? [String: Any] else { return }
            if let raw = body["phase"] as? String, let phase = ScreenPageState.Phase(rawValue: raw) { page.phase = phase }
            switch body["credentials"] as? String {
            case "wanted":
                message.webView?.evaluateJavaScript("window.personaScreen && window.personaScreen.signIn(\(Self.script(ScreenCredentials.load(machine))))")
            case "worked":
                if let account = body["account"] as? String, let password = body["password"] as? String {
                    ScreenCredentials.save(.init(account: account, password: password), for: machine)
                }
            case "unlock":
                // The page is in and asks to unlock: the kept password, typed on the lock screen; the hub only logs it.
                guard let saved = ScreenCredentials.load(machine) else { break }
                message.webView?.evaluateJavaScript("window.personaScreen && window.personaScreen.unlock(\(Self.script(saved)))")
                Task { _ = try? await AppModel.shared.api(machine.group)?.computerJob(machine: machine.id, "unlocking") }
            case "forget":
                ScreenCredentials.forget(machine)
                ScreenUnlock.set(false, for: machine)
            default:
                break
            }
        }
    }

    /// Saved as a JavaScript value: a JSON object, or null.
    static func script(_ saved: ScreenCredentials.Saved?) -> String {
        guard let saved, let data = try? JSONSerialization.data(withJSONObject: ["account": saved.account, "password": saved.password]),
              let json = String(data: data, encoding: .utf8) else { return "null" }
        return json
    }

    static func configuration(_ bridge: ScreenBridge) -> WKWebViewConfiguration {
        let c = WKWebViewConfiguration()
        c.websiteDataStore = .nonPersistent()
        c.userContentController.add(bridge, name: "screen")
        return c
    }

    static func setControl(_ on: Bool, in v: WKWebView) {
        v.evaluateJavaScript("window.personaScreen && window.personaScreen.setControl(\(on))")
    }
}

#if os(iOS)
private struct ScreenWebView: UIViewRepresentable {
    let url: URL
    var control: Bool
    let page: ScreenPageState
    let machine: MachineRef

    func makeCoordinator() -> ScreenBridge { ScreenBridge(page: page, machine: machine) }

    func makeUIView(context: Context) -> WKWebView {
        let v = WKWebView(frame: .zero, configuration: ScreenBridge.configuration(context.coordinator))
        v.isOpaque = false
        v.backgroundColor = .clear
        v.scrollView.isScrollEnabled = false // the page fits; pinching and dragging are the remote screen's
        v.scrollView.contentInsetAdjustmentBehavior = .never
        v.scrollView.showsVerticalScrollIndicator = false
        v.scrollView.showsHorizontalScrollIndicator = false
        #if DEBUG
        v.isInspectable = true
        #endif
        v.load(URLRequest(url: url))
        return v
    }

    func updateUIView(_ v: WKWebView, context: Context) { ScreenBridge.setControl(control, in: v) }

    static func dismantleUIView(_ v: WKWebView, coordinator: ScreenBridge) {
        v.stopLoading()
        v.configuration.userContentController.removeScriptMessageHandler(forName: "screen")
    }
}
#else
private struct ScreenWebView: NSViewRepresentable {
    let url: URL
    var control: Bool
    let page: ScreenPageState
    let machine: MachineRef

    func makeCoordinator() -> ScreenBridge { ScreenBridge(page: page, machine: machine) }

    func makeNSView(context: Context) -> WKWebView {
        let v = WKWebView(frame: .zero, configuration: ScreenBridge.configuration(context.coordinator))
        v.setValue(false, forKey: "drawsBackground")
        #if DEBUG
        v.isInspectable = true
        #endif
        v.load(URLRequest(url: url))
        return v
    }

    func updateNSView(_ v: WKWebView, context: Context) { ScreenBridge.setControl(control, in: v) }

    static func dismantleNSView(_ v: WKWebView, coordinator: ScreenBridge) {
        v.configuration.userContentController.removeScriptMessageHandler(forName: "screen")
    }
}
#endif

/// VNC under a tool that operates a Mac: the assistant's computer_state / computer_run (the machine it names), or an
/// agent's own Cua Driver MCP tools (the agent's machine), while that Mac's live screen can be shown.
struct ScreenEntry: View {
    @Environment(AppModel.self) private var model
    @Environment(\.fileScope) private var scope
    let call: ToolCall

    var body: some View {
        if let ref = Self.machine(call.name, input: call.input, chat: scope?.machine, machines: model.machines), model.canShowScreen(ref) {
            Button { model.openScreen(ref) } label: {
                Label("VNC", systemImage: Theme.Icon.screen).font(Theme.Font.labelStrong).lineLimit(1)
            }
            .themedButton()
            .accessibilityIdentifier("tool-screen")
        }
    }

    /// The Mac a tool call operates: the one computer_* names (by id or name, in the chat's group of hubs), or the
    /// chat's own for an agent's Cua Driver tools (mcp__cua-driver__click…).
    static func machine(_ name: String, input: JSONValue, chat: MachineRef?, machines: [Machine]) -> MachineRef? {
        let base = ToolPresentation.baseName(name)
        if base == "computer_state" || base == "computer_run" {
            guard let named = input["machine"]?.string, !named.isEmpty else { return nil }
            let group = chat?.group ?? ""
            return machines.first { $0.group == group && ($0.id == named || $0.name.caseInsensitiveCompare(named) == .orderedSame) }?.ref
        }
        return ComputerSteps.isCuaTool(name) ? chat : nil
    }
}

/// 「正在操作 MacBook · 点 AC → 6」 with the chat's running work (#1343): while the model operates a Mac, and for a few
/// seconds after; a tap opens that Mac's live screen. The task dock's pill face (`DockPillLabel`).
struct ComputerActivityPill: View {
    @Environment(AppModel.self) private var model
    var activity: ComputerActivity?

    var body: some View {
        TimelineView(.periodic(from: .now, by: 1)) { ctx in
            if let a = activity, a.shown(at: ctx.date) {
                Button { Haptics.tap(); model.openScreen(a.machine) } label: {
                    DockPillLabel(title: (a.running ? "正在操作 " : "刚操作 ") + a.machineName + " · " + a.step, state: .working, waiting: false,
                                  maxWidth: Theme.Size.box280) {
                        if a.running {
                            ProgressView().controlSize(.mini)
                        } else {
                            Image(systemName: Theme.Icon.screen).font(Theme.Font.labelHeavy).foregroundStyle(.secondary)
                        }
                    }
                }
                .buttonStyle(.plain)
                .disabled(!model.canShowScreen(a.machine))
                .accessibilityElement(children: .combine)
                .accessibilityLabel((a.running ? "正在操作 " : "刚操作 ") + a.machineName + "，" + a.step)
                .accessibilityHint(model.canShowScreen(a.machine) ? "打开 VNC" : "")
                .accessibilityIdentifier("computer-activity")
                .transition(.opacity)
            }
        }
        .animation(Theme.Motion.state, value: activity?.toolUseID)
    }

    /// Shown at all (for the rows that only appear when there is something).
    static func shows(_ a: ComputerActivity?) -> Bool { a?.shown(at: .now) ?? false }
}

/// VNC in a chat's ··· menu (#1343, #1604): an agent's chat has its own Mac's, straight away, or none; the assistant's,
/// the Mac it operated last, else every Mac whose live screen can be shown (a submenu when there are several).
struct ScreenMenu: View {
    @Environment(AppModel.self) private var model
    /// An agent's chat: its own Mac, the only one (nothing when it is not a Mac whose screen can be shown).
    var machine: MachineRef? = nil
    /// The assistant's: the Mac it operated last; with none, the Macs whose screen can be shown.
    var recent: MachineRef? = nil

    var body: some View {
        if let machine {
            if model.canShowScreen(machine), let m = model.machine(machine) { item(m) }
        } else if let recent, model.canShowScreen(recent), let m = model.machine(recent) {
            item(m)
        } else {
            let macs = model.screenMachines
            if macs.count == 1, let m = macs.first {
                item(m)
            } else if !macs.isEmpty {
                Menu {
                    ForEach(macs) { m in Button { model.openScreen(m.ref) } label: { Text(m.name).lineLimit(1) } }
                } label: {
                    Label("VNC", systemImage: Theme.Icon.screen).lineLimit(1)
                }
            }
        }
    }

    private func item(_ m: Machine) -> some View {
        Button { model.openScreen(m.ref) } label: { Label("VNC · \(m.name)", systemImage: Theme.Icon.screen).lineLimit(1) }
    }
}

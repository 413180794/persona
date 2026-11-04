import SwiftUI
import WebKit

/// A Mac's live screen (docs/computer-use.md「实时画面」; #1343): the hub's /screen/<machine> page, which draws macOS's
/// Screen Sharing through the hub's port relay with noVNC, in a drawer like the terminal's — up from the bottom,
/// dragged down to close. Over the picture, what the chat's model is doing on that Mac; under it, 只看 / 接管. The
/// app's device token goes in the fragment (never sent to a server): the page keeps it in memory, and the web view
/// stores nothing. RootView presents it for `AppModel.screenOpen` (`model.openScreen(ref)`).
struct ScreenDrawer: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    let ref: MachineRef
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
            if let url = ScreenViewer.url(model, ref) {
                ScreenWebView(url: url, control: control, page: page)
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
                .onTapGesture { Haptics.tap(); dismiss() }
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
                if g.translation.height > 140 || g.predictedEndTranslation.height > 420 { dismiss() } else {
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
    @MainActor static func url(_ model: AppModel, _ ref: MachineRef) -> URL? {
        guard let api = model.api(ref.group) else { return nil }
        var c = URLComponents(url: api.url("/screen/\(ref.id)"), resolvingAgainstBaseURL: false)
        let token = api.config.token.addingPercentEncoding(withAllowedCharacters: .alphanumerics) ?? ""
        c?.fragment = "token=\(token)&embed=1"
        return c?.url
    }
}

/// The live screen asked for (`AppModel.screenOpen`).
struct ScreenOpen: Identifiable, Hashable {
    let machine: MachineRef
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

extension View {
    /// Presents the live screen `AppModel.screenOpen` asks for: the terminal's kind of drawer on the phone, a large
    /// sheet on the Mac.
    func screenDrawer(_ model: AppModel) -> some View {
        @Bindable var model = model
        #if os(iOS)
        return fullScreenCover(item: $model.screenOpen) { ScreenDrawer(ref: $0.machine).presentationBackground(.clear) }
        #else
        return sheet(item: $model.screenOpen) { ScreenDrawer(ref: $0.machine).macLargeSheet() }
        #endif
    }
}

/// The page in a web view that stores nothing; it posts its phase (`screen` message), and 只看 / 接管 is passed in.
private final class ScreenBridge: NSObject, WKScriptMessageHandler {
    let page: ScreenPageState
    init(page: ScreenPageState) { self.page = page }

    func userContentController(_ c: WKUserContentController, didReceive message: WKScriptMessage) {
        guard let body = message.body as? [String: Any], let raw = body["phase"] as? String,
              let phase = ScreenPageState.Phase(rawValue: raw) else { return }
        MainActor.assumeIsolated { page.phase = phase }
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

    func makeCoordinator() -> ScreenBridge { ScreenBridge(page: page) }

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

    func makeCoordinator() -> ScreenBridge { ScreenBridge(page: page) }

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

/// 看画面 under a tool that operates a Mac: the assistant's computer_state / computer_run (the machine it names), or an
/// agent's own Cua Driver MCP tools (the agent's machine), while that Mac's live screen can be shown.
struct ScreenEntry: View {
    @Environment(AppModel.self) private var model
    @Environment(\.fileScope) private var scope
    let call: ToolCall

    var body: some View {
        if let ref = Self.machine(call.name, input: call.input, chat: scope?.machine, machines: model.machines), model.canShowScreen(ref) {
            Button { model.openScreen(ref) } label: {
                Label("看画面", systemImage: Theme.Icon.screen).font(Theme.Font.labelStrong).lineLimit(1)
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
                .accessibilityHint(model.canShowScreen(a.machine) ? "看画面" : "")
                .accessibilityIdentifier("computer-activity")
                .transition(.opacity)
            }
        }
        .animation(Theme.Motion.state, value: activity?.toolUseID)
    }

    /// Shown at all (for the rows that only appear when there is something).
    static func shows(_ a: ComputerActivity?) -> Bool { a?.shown(at: .now) ?? false }
}

/// 看画面 in a chat's ··· menu (#1343): every Mac whose live screen can be shown, the one the chat's model operated
/// last first; one alone is a button. Nothing when there is none.
struct ScreenMenu: View {
    @Environment(AppModel.self) private var model
    /// The Mac this chat's model operated last; nil: the one any chat did most recently.
    var recent: MachineRef?

    var body: some View {
        let first = recent ?? model.computerActivities.values.max { $0.at < $1.at }?.machine
        let macs = model.screenMachines.sorted { a, b in a.ref == first && b.ref != first }
        if macs.count == 1, let m = macs.first {
            Button { model.openScreen(m.ref) } label: { Label("看画面 · \(m.name)", systemImage: Theme.Icon.screen).lineLimit(1) }
        } else if !macs.isEmpty {
            Menu {
                ForEach(macs) { m in Button { model.openScreen(m.ref) } label: { Text(m.name).lineLimit(1) } }
            } label: {
                Label("看画面", systemImage: Theme.Icon.screen).lineLimit(1)
            }
        }
    }
}

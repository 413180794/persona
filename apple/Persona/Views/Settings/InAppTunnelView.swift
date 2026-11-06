import SwiftUI
import UIKit
import WebKit

/// Settings › hub › the in-app tunnel (docs/network.md「经 App 内隧道连 hub」): one paired hub's tunnel, the account it signs
/// in with, and what it is doing now. Only a hub whose extension contributes a tunnel has the page, titled with the
/// tunnel's name; where the tunnel goes is that extension's setting on the hub, shown here as it is. 「总是走隧道」 is
/// this phone's.
struct HubInAppTunnelPage: View {
    @Environment(AppModel.self) private var model
    let hubID: String

    @State private var checking = false
    @State private var problem: String?
    @State private var core: InAppTunnel.Status?
    @State private var coreProblem: String?
    @State private var panel: URL?

    var body: some View {
        if let hub = model.registry.hubs.first(where: { $0.id == hubID }), let route = hub.inAppTunnel {
            form(hub, route)
                .detailTitle(route.title)
                .task { await watchCore() }
                .sheet(isPresented: Binding(get: { panel != nil }, set: { if !$0 { panel = nil } })) {
                    if let panel { InAppTunnelPanelSheet(url: panel, title: route.title) }
                }
        } else {
            EmptyStateView(symbol: Theme.Icon.inAppTunnel, title: String(localized: "这台 hub 没有内网隧道"),
                           message: String(localized: "在 hub 上装好并打开提供内网隧道的扩展，再回到这里。"))
        }
    }

    private func form(_ hub: PairedHub, _ route: InAppTunnelRoute) -> some View {
        Form {
            if !InAppTunnel.available {
                Section {
                    Label {
                        Text("这个版本的 App 没有内网隧道核心。").font(Theme.Font.secondary)
                            .fixedSize(horizontal: false, vertical: true)
                    } icon: {
                        Image(systemName: Theme.Icon.error).foregroundStyle(Theme.caution)
                    }
                    .accessibilityIdentifier("tunnel.missing")
                }
            } else if route.isUsable {
                status(hub)
            }
            account(route)
            Section {
                LabeledContent {
                    Text(verbatim: route.targetHost).foregroundStyle(.secondary).lineLimit(1).truncationMode(.middle)
                } label: {
                    Text("地址").lineLimit(1)
                }
                .accessibilityIdentifier("tunnel.targetHost")
                LabeledContent {
                    Text(verbatim: String(route.targetPort)).foregroundStyle(.secondary).lineLimit(1)
                } label: {
                    Text("端口").lineLimit(1)
                }
                .accessibilityIdentifier("tunnel.targetPort")
            } header: {
                SettingsHeading(String(localized: "隧道目标"))
            } footer: {
                Text("hub 在隧道那一侧网络里的地址和 HTTP 端口，在 hub 上这个扩展的设置里改。")
            }
            Section {
                Toggle(isOn: Binding(get: { route.always }, set: { on in
                    var r = route
                    r.always = on
                    model.setInAppTunnel(r, hub: hubID)
                })) { Text("总是走隧道").lineLimit(1) }
                    .accessibilityIdentifier("tunnel.always")
            } footer: {
                Text("关闭时先直连 hub，连不上才走隧道。只有 Persona 连 hub 的流量经过它，其他 App 不受影响；Persona 进后台时隧道断开，回到前台再连上。")
            }
            if let problem {
                Section {
                    Label {
                        Text(problem).font(Theme.Font.secondary).fixedSize(horizontal: false, vertical: true)
                    } icon: {
                        Image(systemName: Theme.Icon.error).foregroundStyle(Theme.danger)
                    }
                    .accessibilityIdentifier("tunnel.problem")
                }
            }
            Section {
                Button { Task { await test(route) } } label: {
                    HStack(spacing: Theme.Space.s) {
                        if checking { ProgressView() }
                        Text(checking ? String(localized: "正在测试…") : String(localized: "测试连接")).lineLimit(1).minimumScaleFactor(0.8)
                    }
                    .frame(maxWidth: .infinity)
                }
                .themedButton(prominent: true)
                .disabled(checking || !route.isUsable || !InAppTunnel.available)
                .accessibilityIdentifier("tunnel.test")
                .listRowBackground(Color.clear)
                .listRowInsets(EdgeInsets())
            } footer: {
                Text("会连上隧道，再经隧道访问一次 hub。")
            }
        }
        .themedPage()
    }

    private func status(_ hub: PairedHub) -> some View {
        let tunnels = InAppTunnels.shared
        let routed = tunnels.routed.contains(hub.id)
        let live = model.isLive(hub.group)
        let now: String = routed ? (live ? String(localized: "正在经隧道连 hub") : String(localized: "正在经隧道连接…"))
            : live ? String(localized: "没有使用：直连 hub 正常") : String(localized: "未连接")
        return Section {
            SettingsRow(title: now, symbol: routed ? Theme.Icon.inAppTunnel : Theme.Icon.server, tint: routed ? Theme.positive : Theme.Hue.gray)
                .accessibilityIdentifier("tunnel.status")
            if case .down(let why) = tunnels.states[hub.id] {
                Text("上次失败：\(why)").font(Theme.Font.caption).foregroundStyle(Theme.danger)
            }
        } header: {
            SettingsHeading(String(localized: "状态"))
        }
    }

    // MARK: The tunnel's account

    @ViewBuilder private func account(_ route: InAppTunnelRoute) -> some View {
        Section {
            SettingsRow(title: String(localized: "账号"), symbol: Theme.Icon.siteLogin, tint: Theme.Hue.blue, value: accountValue, look: .oneLine)
                .accessibilityIdentifier("tunnel.account")
            Button { Task { await openPanel(route) } } label: {
                SettingsRow(title: String(localized: "登录和选节点"), symbol: Theme.Icon.openExternal, tint: Theme.accent)
            }
            .modifier(SettingsButtonLook())
            .disabled(!InAppTunnel.available)
            .accessibilityIdentifier("tunnel.panel")
            .bareListRow()
            if let coreProblem {
                Text(coreProblem).font(Theme.Font.caption).foregroundStyle(Theme.danger)
            }
        } header: {
            SettingsHeading(String(localized: "隧道账号"))
        } footer: {
            Text("第一次使用时在面板里登录，再选一个节点。登录信息只存在这台设备上，不上传 hub。")
        }
    }

    private var accountValue: String {
        guard let core else { return String(localized: "未启动") }
        if !core.loggedIn || !core.hasServer { return String(localized: "未登录") }
        if core.needsMfa == true { return String(localized: "需要二次验证") }
        if core.connected { return core.serverName.map { String(localized: "已连接 · \($0)") } ?? String(localized: "已连接") }
        return String(localized: "已登录")
    }

    private func openPanel(_ route: InAppTunnelRoute) async {
        do {
            panel = try await InAppTunnel.start(probe: route)
            coreProblem = nil
        } catch {
            coreProblem = error.localizedDescription
        }
    }

    /// The core's login and tunnel as they change, while the page is open.
    private func watchCore() async {
        while !Task.isCancelled {
            let s = await InAppTunnel.status()
            if s != core { core = s }
            try? await Task.sleep(for: .seconds(2))
        }
    }

    private func test(_ route: InAppTunnelRoute) async {
        checking = true
        problem = nil
        defer { checking = false }
        do {
            try await InAppTunnels.shared.verify(route)
            Haptics.tap()
            model.toast = Toast(String(localized: "经隧道连上了 hub"), style: .success)
        } catch {
            problem = error.localizedDescription
        }
    }
}

/// The tunnel core's own control panel (on loopback, in this process): where its account signs in and a node is
/// picked. A sign-in that opens outside the app may end with a link the user copies there; it is offered to the core
/// from the clipboard when the user comes back (InAppTunnel.offerLink).
struct InAppTunnelPanelSheet: View {
    @Environment(\.dismiss) private var dismiss
    @Environment(\.scenePhase) private var scenePhase
    let url: URL
    let title: String
    @State private var store = InAppTunnelPanelStore()

    var body: some View {
        NavigationStack {
            InAppTunnelPanelView(url: url, store: store)
                .ignoresSafeArea(edges: .bottom)
                .navigationTitle(title)
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .cancellationAction) {
                        Button { store.reload() } label: { Image(systemName: Theme.Icon.reload) }
                            .accessibilityLabel(String(localized: "重新加载"))
                    }
                    ToolbarItem(placement: .confirmationAction) { Button("完成") { dismiss() }.lineLimit(1) }
                }
        }
        .onChange(of: scenePhase) { _, phase in
            if phase == .active { store.pickUpLoginLink() }
        }
    }
}

@MainActor
final class InAppTunnelPanelStore: NSObject, WKNavigationDelegate, WKUIDelegate {
    private(set) var webView: WKWebView?
    private var url: URL?
    /// When the panel last sent a link out of the app (a sign-in), and the clipboard's change count then.
    private var sentOutAt: Date?
    private var sentOutPasteboard = 0

    /// One web view per sheet: a new one would reload the login page, which starts a new authorisation and voids the
    /// one the user is finishing outside the app.
    func makeWebView(url: URL) -> WKWebView {
        if let webView { return webView }
        let webView = WKWebView(frame: .zero, configuration: WKWebViewConfiguration())
        webView.navigationDelegate = self
        webView.uiDelegate = self
        self.webView = webView
        self.url = url
        webView.load(URLRequest(url: url))
        return webView
    }

    func reload() {
        guard let webView else { return }
        if webView.url == nil, let url { webView.load(URLRequest(url: url)) } else { webView.reload() }
    }

    /// Back within 15 minutes of the panel sending a link out, with something copied since: offered to the core, and
    /// the panel reloaded when it took it. Reading the clipboard makes iOS ask「允许粘贴」 once.
    func pickUpLoginLink() {
        guard let sent = sentOutAt, Date().timeIntervalSince(sent) < 15 * 60 else { return }
        let board = UIPasteboard.general
        guard board.changeCount != sentOutPasteboard, board.hasStrings, let text = board.string else { return }
        let link = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !link.isEmpty, link.count <= 4096 else { return }
        sentOutPasteboard = board.changeCount
        Task {
            guard await InAppTunnel.offerLink(link) else { return }
            sentOutAt = nil
            reload()
        }
    }

    private func sendOut(_ url: URL) {
        sentOutAt = Date()
        sentOutPasteboard = UIPasteboard.general.changeCount
        UIApplication.shared.open(url)
    }

    func webView(_ webView: WKWebView, decidePolicyFor navigationAction: WKNavigationAction) async -> WKNavigationActionPolicy {
        guard let url = navigationAction.request.url else { return .allow }
        let host = url.host() ?? ""
        if host == "127.0.0.1" || host == "localhost" || ["about", "blob", "data"].contains(url.scheme ?? "") { return .allow }
        sendOut(url)
        return .cancel
    }

    func webView(_ webView: WKWebView, createWebViewWith configuration: WKWebViewConfiguration,
                 for navigationAction: WKNavigationAction, windowFeatures: WKWindowFeatures) -> WKWebView? {
        if let url = navigationAction.request.url { sendOut(url) }
        return nil
    }

    func webView(_ webView: WKWebView, runJavaScriptAlertPanelWithMessage message: String,
                 initiatedByFrame frame: WKFrameInfo) async {
        _ = await present(message, cancel: nil, ok: String(localized: "好"))
    }

    func webView(_ webView: WKWebView, runJavaScriptConfirmPanelWithMessage message: String,
                 initiatedByFrame frame: WKFrameInfo) async -> Bool {
        await present(message, cancel: String(localized: "取消"), ok: String(localized: "确定"))
    }

    private func present(_ message: String, cancel: String?, ok: String) async -> Bool {
        guard let root = webView?.window?.rootViewController else { return false }
        return await withCheckedContinuation { done in
            let alert = UIAlertController(title: nil, message: message, preferredStyle: .alert)
            if let cancel { alert.addAction(UIAlertAction(title: cancel, style: .cancel) { _ in done.resume(returning: false) }) }
            alert.addAction(UIAlertAction(title: ok, style: .default) { _ in done.resume(returning: true) })
            Self.topmost(root).present(alert, animated: true)
        }
    }

    private static func topmost(_ c: UIViewController) -> UIViewController {
        var c = c
        while let p = c.presentedViewController { c = p }
        return c
    }
}

/// The panel's web view, moved into whichever container is on screen (it outlives the view: see InAppTunnelPanelStore).
struct InAppTunnelPanelView: UIViewRepresentable {
    let url: URL
    let store: InAppTunnelPanelStore

    func makeUIView(context: Context) -> UIView {
        let container = UIView()
        adopt(into: container)
        return container
    }

    func updateUIView(_ uiView: UIView, context: Context) { adopt(into: uiView) }

    private func adopt(into container: UIView) {
        let webView = store.makeWebView(url: url)
        guard webView.superview !== container else { return }
        webView.frame = container.bounds
        webView.autoresizingMask = [.flexibleWidth, .flexibleHeight]
        container.addSubview(webView)
    }
}

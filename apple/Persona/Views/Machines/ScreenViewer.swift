import SwiftUI
import WebKit

/// A Mac's live screen (docs/computer-use.md「实时画面」): the hub's /screen/<machine> page, which draws macOS's Screen
/// Sharing through the hub's port relay with noVNC, in a web view. The app's device token goes in the fragment (never
/// sent to a server): the page keeps it in memory, and the web view stores nothing.
struct ScreenViewer: View {
    let url: URL
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            ScreenWebView(url: url)
                .ignoresSafeArea(edges: .bottom)
                .navigationTitle("看画面")
                #if os(iOS)
                .navigationBarTitleDisplayMode(.inline)
                #endif
                .toolbar {
                    ToolbarItem(placement: .cancellationAction) { Button("完成") { dismiss() }.lineLimit(1) }
                }
        }
    }

    /// The page's address for ref, through the way the app reaches its hub (its SSH tunnel when that is the way).
    @MainActor static func url(_ model: AppModel, _ ref: MachineRef) -> URL? {
        guard let api = model.api(ref.group) else { return nil }
        var c = URLComponents(url: api.url("/screen/\(ref.id)"), resolvingAgainstBaseURL: false)
        let token = api.config.token.addingPercentEncoding(withAllowedCharacters: .alphanumerics) ?? ""
        c?.fragment = "token=\(token)&embed=1"
        return c?.url
    }
}

/// The live screen's URL as a sheet's item.
struct ScreenPage: Identifiable {
    let url: URL
    var id: URL { url }
}

extension View {
    /// Shows the live screen while page is set: full screen on the phone, a large sheet on the Mac.
    func screenViewer(_ page: Binding<ScreenPage?>) -> some View {
        #if os(iOS)
        fullScreenCover(item: page) { ScreenViewer(url: $0.url) }
        #else
        sheet(item: page) { ScreenViewer(url: $0.url).macLargeSheet() }
        #endif
    }
}

#if os(iOS)
private struct ScreenWebView: UIViewRepresentable {
    let url: URL

    func makeUIView(context: Context) -> WKWebView {
        let v = WKWebView(frame: .zero, configuration: Self.configuration())
        v.scrollView.isScrollEnabled = false // the page fits the screen; pinching and dragging are the remote screen's
        v.scrollView.contentInsetAdjustmentBehavior = .never
        v.scrollView.showsVerticalScrollIndicator = false
        v.scrollView.showsHorizontalScrollIndicator = false
        #if DEBUG
        v.isInspectable = true
        #endif
        v.load(URLRequest(url: url))
        return v
    }

    func updateUIView(_ v: WKWebView, context: Context) {}

    static func dismantleUIView(_ v: WKWebView, coordinator: ()) { v.stopLoading() }

    static func configuration() -> WKWebViewConfiguration {
        let c = WKWebViewConfiguration()
        c.websiteDataStore = .nonPersistent()
        return c
    }
}
#else
private struct ScreenWebView: NSViewRepresentable {
    let url: URL

    func makeNSView(context: Context) -> WKWebView {
        let c = WKWebViewConfiguration()
        c.websiteDataStore = .nonPersistent()
        let v = WKWebView(frame: .zero, configuration: c)
        #if DEBUG
        v.isInspectable = true
        #endif
        v.load(URLRequest(url: url))
        return v
    }

    func updateNSView(_ v: WKWebView, context: Context) {}
}
#endif

/// 看画面 under a tool that operates a Mac: the assistant's computer_state / computer_run (the machine it names), or an
/// agent's own Cua Driver MCP tools (the agent's machine), while that Mac's live screen can be shown.
struct ScreenEntry: View {
    @Environment(AppModel.self) private var model
    @Environment(\.fileScope) private var scope
    let call: ToolCall
    @State private var page: ScreenPage?

    var body: some View {
        if let ref = Self.machine(call.name, input: call.input, chat: scope?.machine, machines: model.machines),
           let c = model.machine(ref)?.computer, c.enabled, c.screenSharing {
            Button {
                if let url = ScreenViewer.url(model, ref) { page = ScreenPage(url: url) }
            } label: {
                Label("看画面", systemImage: Theme.Icon.screen).font(Theme.Font.labelStrong).lineLimit(1)
            }
            .themedButton()
            .accessibilityIdentifier("tool-screen")
            .screenViewer($page)
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
        let lower = name.lowercased()
        return lower.hasPrefix("mcp__cua") || lower.contains("cua-driver") || lower.contains("cua_driver") ? chat : nil
    }
}

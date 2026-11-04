import SwiftUI
import WebKit

// The assistant's free drawing (#2025, docs/assistant-views.md「自由绘制」): an `html` component is a self-contained page
// (HTML / SVG / canvas / JS) the assistant wrote. It runs in a web view that can reach nothing: no network (a content
// rule list blocks every load but the page itself and inline data), no cookies or storage of the app (a non-persistent
// data store), no navigation away, no new windows, no downloads. Its only way out is `window.persona.reply(text)` /
// `window.persona.open(url)`, one message handler named `persona`; both are shown to the user in a native dialog first,
// and a card sends at most one reply. It does not run until the user taps 运行, and it is torn down when it scrolls away.

/// What a card's page asked the app for, read from what it posted to the `persona` handler.
nonisolated enum HTMLCardRequest: Equatable, Sendable {
    /// Send this as the user's reply in the conversation.
    case reply(String)
    /// Open this in the app (`AssistantViewLink.allowed`).
    case open(URL)

    /// A reply's longest text (the same as an action's `reply`).
    static let maxReply = 2000
    /// A link's longest text.
    static let maxURL = 2048

    /// The request in a posted message, nil for anything else: another frame's, another shape, too long, empty, or a
    /// link the app does not open.
    static func parse(_ body: Any, mainFrame: Bool) -> HTMLCardRequest? {
        guard mainFrame, let m = body as? [String: Any], let kind = m["kind"] as? String else { return nil }
        switch kind {
        case "reply":
            guard let t = m["text"] as? String else { return nil }
            let text = t.trimmingCharacters(in: .whitespacesAndNewlines)
            guard !text.isEmpty, text.count <= maxReply else { return nil }
            return .reply(text)
        case "open":
            guard let s = m["url"] as? String, s.count <= maxURL, let url = URL(string: s), AssistantViewLink.allowed(url) else { return nil }
            return .open(url)
        default:
            return nil
        }
    }
}

/// What a card may open (an `open` action, a page's `persona.open`): an https page, or one of the app's own places
/// `persona://session|task|kb|file/<something>`. Nothing else: no http, no other app's scheme, no javascript:.
nonisolated enum AssistantViewLink {
    static let places: Set<String> = ["session", "task", "kb", "file"]

    static func allowed(_ url: URL) -> Bool {
        switch url.scheme?.lowercased() {
        case "https":
            return url.host()?.isEmpty == false
        case "persona":
            guard let host = url.host()?.lowercased(), places.contains(host) else { return false }
            return url.path().split(separator: "/").contains { !$0.isEmpty }
        default:
            return false
        }
    }
}

/// An `html` component in the assistant's reply: its title and 运行; once run, the page in its sandbox. A page asking
/// to reply or open something is shown what it asks first.
struct AssistantHTMLCard: View {
    var title: String?
    var html: String
    /// The height the assistant asked for (160–640; kept in that range).
    var height: Int?
    /// What it says in words (shown when it cannot run here).
    var fallback: String?
    /// A reply from this card was already sent (here or on another device): it sends no more.
    var answered = false
    /// Runs as soon as it is shown instead of after 运行 (#2017 decision 1).
    var autoRun = false
    /// Sends the reply (the view action endpoint); false when it did not go.
    var onReply: (String) async -> Bool
    var onOpen: (URL) -> Void

    @State private var running = false
    @State private var asking: HTMLCardRequest?
    @State private var sending = false
    @State private var sent = false
    /// The user said no to a request this run: the page asks nothing more until it is run again.
    @State private var muted = false

    static let minHeight = 160
    static let maxHeight = 640
    static let defaultHeight = 320

    static func clampedHeight(_ h: Int?) -> CGFloat {
        CGFloat(min(max(h ?? defaultHeight, minHeight), maxHeight))
    }

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.s) {
            HStack(spacing: Theme.Space.s) {
                if let title, !title.isEmpty {
                    Text(title).font(Theme.Font.heading).lineLimit(2).accessibilityAddTraits(.isHeader)
                }
                Spacer(minLength: Theme.Space.s)
                if running {
                    Button("停止", systemImage: Theme.Icon.interrupt) { stop() }
                        .labelStyle(.iconOnly)
                        .themedButton()
                        .accessibilityIdentifier("html-card-stop")
                }
            }
            if running {
                SandboxedHTMLView(html: html) { request in
                    guard !muted, !sending, asking == nil else { return }
                    if case .reply = request, answered || sent { return }
                    asking = request
                }
                .frame(height: Self.clampedHeight(height))
                .clipShape(.rect(cornerRadius: Theme.Radius.control, style: .continuous))
                .accessibilityIdentifier("html-card-page")
            } else {
                idle
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .card(padding: Theme.Space.m)
        .onAppear { if autoRun { running = true } }
        // Off screen it costs nothing: the page and its scripts go away and come back from the start.
        .onDisappear { stop() }
        .alert(alertTitle, isPresented: Binding(get: { asking != nil }, set: { if !$0 { decline() } }), presenting: asking) { request in
            Button(confirmTitle(request)) { Task { await accept(request) } }
            Button("取消", role: .cancel) { decline() }
        } message: { request in
            switch request {
            case .reply(let text): Text(text)
            case .open(let url): Text(url.absoluteString)
            }
        }
    }

    private var idle: some View {
        VStack(alignment: .leading, spacing: Theme.Space.s) {
            if let fallback, !fallback.isEmpty {
                Text(fallback).font(Theme.Font.secondary).foregroundStyle(.secondary).lineLimit(4)
            }
            Button { running = true; muted = false } label: {
                Label("运行", systemImage: Theme.Icon.run).lineLimit(1)
            }
            .themedButton(prominent: true)
            .accessibilityIdentifier("html-card-run")
            Text("在隔离的环境里运行：不联网，拿不到你的数据；要回复或打开链接时会先问你。")
                .font(Theme.Font.caption).foregroundStyle(.secondary)
        }
    }

    private var alertTitle: String {
        switch asking {
        case .open: "卡片要打开链接"
        default: "卡片要替你回复"
        }
    }

    private func confirmTitle(_ r: HTMLCardRequest) -> String {
        if case .open = r { return "打开" }
        return "发送"
    }

    private func stop() {
        running = false
        asking = nil
    }

    private func decline() {
        guard asking != nil else { return }
        asking = nil
        muted = true
    }

    private func accept(_ r: HTMLCardRequest) async {
        asking = nil
        switch r {
        case .open(let url):
            onOpen(url)
        case .reply(let text):
            sending = true
            defer { sending = false }
            if await onReply(text) { sent = true }
        }
    }
}

// MARK: - The sandbox

/// Blocks every load but the page itself (`loadHTMLString` with no base URL is about:blank) and inline data: the
/// page's fetch, XHR, images, fonts, frames from anywhere else never leave the device. One rule a scheme (content rule
/// filters have no `|`).
private let sandboxRules = "[" + (
    [#"{"trigger":{"url-filter":".*"},"action":{"type":"block"}}"#] +
    ["about", "data", "blob"].map { #"{"trigger":{"url-filter":"^\#($0):"},"action":{"type":"ignore-previous-rules"}}"# }
).joined(separator: ",") + "]"

/// The only script the app adds: `window.persona`, which posts to the `persona` handler, and a quiet light / dark
/// default the page's own CSS overrides.
private let bridgeScript = """
(() => {
  const h = window.webkit && window.webkit.messageHandlers && window.webkit.messageHandlers.persona;
  const send = (m) => { try { h && h.postMessage(m); } catch (_) {} };
  Object.defineProperty(window, "persona", {
    value: Object.freeze({
      reply(text) { send({ kind: "reply", text: String(text) }); },
      open(url) { send({ kind: "open", url: String(url) }); },
    }),
    writable: false, configurable: false,
  });
  const s = document.createElement("style");
  s.textContent = ":root{color-scheme:light dark}body{margin:0;font:-apple-system-body;font-family:-apple-system,system-ui,sans-serif}";
  (document.head || document.documentElement).prepend(s);
})();
"""

/// A web view for a card's page that can reach nothing (see the top of this file); its requests come to `onRequest`.
@MainActor
final class SandboxedHTMLCoordinator: NSObject, WKNavigationDelegate, WKUIDelegate, WKScriptMessageHandler {
    var onRequest: (HTMLCardRequest) -> Void
    /// The page was put in: from now on every main-frame navigation is cancelled.
    private var started = false
    private var loaded: String?

    init(onRequest: @escaping (HTMLCardRequest) -> Void) { self.onRequest = onRequest }

    static let handler = "persona"

    /// The rules compiled once for the app's life (a few ms; the store keeps them).
    private static var rules: WKContentRuleList?

    static func makeWebView(_ coordinator: SandboxedHTMLCoordinator) -> WKWebView {
        let config = WKWebViewConfiguration()
        config.websiteDataStore = .nonPersistent()
        config.defaultWebpagePreferences.allowsContentJavaScript = true
        config.preferences.javaScriptCanOpenWindowsAutomatically = false
        config.preferences.isFraudulentWebsiteWarningEnabled = false // no lookup leaves the device
        config.mediaTypesRequiringUserActionForPlayback = .all
        #if os(iOS) || os(visionOS)
        config.dataDetectorTypes = []
        config.allowsPictureInPictureMediaPlayback = false
        config.allowsAirPlayForMediaPlayback = false
        config.allowsInlineMediaPlayback = true
        #endif
        let content = config.userContentController
        content.addUserScript(WKUserScript(source: bridgeScript, injectionTime: .atDocumentStart, forMainFrameOnly: true))
        // A weak hop: the controller keeps its handler alive, which would keep the coordinator (and its closures).
        content.add(WeakScriptHandler(coordinator), name: handler)
        let web = WKWebView(frame: .zero, configuration: config)
        web.navigationDelegate = coordinator
        web.uiDelegate = coordinator
        web.allowsLinkPreview = false
        web.allowsBackForwardNavigationGestures = false
        #if os(iOS) || os(visionOS)
        web.isOpaque = false
        web.backgroundColor = .clear
        web.scrollView.backgroundColor = .clear
        web.scrollView.bounces = false
        #endif
        return web
    }

    /// Puts the page in once the rules are in place; without the rules nothing is loaded at all.
    func load(_ html: String, in web: WKWebView) {
        guard loaded != html else { return }
        loaded = html
        Task {
            if Self.rules == nil {
                Self.rules = try? await WKContentRuleListStore.default().compileContentRuleList(
                    forIdentifier: "persona-html-card-sandbox", encodedContentRuleList: sandboxRules)
            }
            started = false
            guard let rules = Self.rules else {
                // No rules, no page: never run it with the network open by accident.
                web.loadHTMLString("<p>无法隔离网络，不运行这张卡片</p>", baseURL: nil)
                return
            }
            web.configuration.userContentController.removeAllContentRuleLists()
            web.configuration.userContentController.add(rules)
            web.loadHTMLString(html, baseURL: nil)
        }
    }

    // MARK: Navigation: the page itself, nothing else

    func webView(_ webView: WKWebView, decidePolicyFor action: WKNavigationAction,
                 preferences: WKWebpagePreferences) async -> (WKNavigationActionPolicy, WKWebpagePreferences) {
        (allows(action), preferences)
    }

    private func allows(_ action: WKNavigationAction) -> WKNavigationActionPolicy {
        if action.shouldPerformDownload { return .cancel }
        let scheme = action.request.url?.scheme?.lowercased() ?? ""
        if action.targetFrame?.isMainFrame == false {
            // The page's own frames (srcdoc, data:); never one from the network (the rules block those anyway).
            return ["about", "data"].contains(scheme) ? .allow : .cancel
        }
        // The main frame: the one load of the page, then no navigation away (links, location =, form posts).
        guard !started, scheme == "about" else { return .cancel }
        started = true
        return .allow
    }

    func webView(_ webView: WKWebView, decidePolicyFor response: WKNavigationResponse) async -> WKNavigationResponsePolicy {
        response.canShowMIMEType ? .allow : .cancel
    }

    /// target=_blank, window.open: no new windows.
    func webView(_ webView: WKWebView, createWebViewWith configuration: WKWebViewConfiguration, for action: WKNavigationAction,
                 windowFeatures: WKWindowFeatures) -> WKWebView? { nil }

    // MARK: The bridge

    func userContentController(_ controller: WKUserContentController, didReceive message: WKScriptMessage) {
        guard message.name == Self.handler,
              let request = HTMLCardRequest.parse(message.body, mainFrame: message.frameInfo.isMainFrame) else { return }
        onRequest(request)
    }
}

/// Holds the coordinator weakly for the user content controller.
@MainActor
private final class WeakScriptHandler: NSObject, WKScriptMessageHandler {
    weak var target: SandboxedHTMLCoordinator?
    init(_ target: SandboxedHTMLCoordinator) { self.target = target }

    func userContentController(_ controller: WKUserContentController, didReceive message: WKScriptMessage) {
        target?.userContentController(controller, didReceive: message)
    }
}

#if os(iOS) || os(visionOS)
struct SandboxedHTMLView: UIViewRepresentable {
    let html: String
    var onRequest: (HTMLCardRequest) -> Void

    func makeCoordinator() -> SandboxedHTMLCoordinator { SandboxedHTMLCoordinator(onRequest: onRequest) }

    func makeUIView(context: Context) -> WKWebView {
        let web = SandboxedHTMLCoordinator.makeWebView(context.coordinator)
        context.coordinator.load(html, in: web)
        return web
    }

    func updateUIView(_ web: WKWebView, context: Context) {
        context.coordinator.onRequest = onRequest
        context.coordinator.load(html, in: web)
    }

    static func dismantleUIView(_ web: WKWebView, coordinator: SandboxedHTMLCoordinator) {
        web.stopLoading()
        web.configuration.userContentController.removeAllScriptMessageHandlers()
    }
}
#else
struct SandboxedHTMLView: NSViewRepresentable {
    let html: String
    var onRequest: (HTMLCardRequest) -> Void

    func makeCoordinator() -> SandboxedHTMLCoordinator { SandboxedHTMLCoordinator(onRequest: onRequest) }

    func makeNSView(context: Context) -> WKWebView {
        let web = SandboxedHTMLCoordinator.makeWebView(context.coordinator)
        context.coordinator.load(html, in: web)
        return web
    }

    func updateNSView(_ web: WKWebView, context: Context) {
        context.coordinator.onRequest = onRequest
        context.coordinator.load(html, in: web)
    }

    static func dismantleNSView(_ web: WKWebView, coordinator: SandboxedHTMLCoordinator) {
        web.stopLoading()
        web.configuration.userContentController.removeAllScriptMessageHandlers()
    }
}
#endif

#if DEBUG
extension View {
    /// `-showSampleHTMLCard YES`: html cards in a sheet, one waiting for 运行 and one running, for screenshots.
    func htmlCardSample() -> some View { modifier(HTMLCardSample()) }
}

private struct HTMLCardSample: ViewModifier {
    @State private var shown = UserDefaults.standard.bool(forKey: "showSampleHTMLCard")
    @State private var replied: String?

    static let page = """
        <!doctype html><html><head><meta name="viewport" content="width=device-width,initial-scale=1">
        <style>
          body{padding:12px;color:#1c1c1e} @media (prefers-color-scheme:dark){body{color:#f2f2f7}}
          .bars{display:flex;gap:10px;align-items:flex-end;height:150px}
          .bar{flex:1;border-radius:8px 8px 4px 4px;background:linear-gradient(#5e8bff,#3a63e0);position:relative}
          .bar span{position:absolute;bottom:-20px;width:100%;text-align:center;font-size:12px;opacity:.7}
          button{margin-top:34px;font:inherit;padding:8px 14px;border-radius:10px;border:0;background:#3a63e0;color:#fff}
        </style></head><body>
        <div class="bars" role="img" aria-label="三个方案的得分：A 62，B 88，C 45">
          <div class="bar" style="height:62%"><span>A</span></div>
          <div class="bar" style="height:88%"><span>B</span></div>
          <div class="bar" style="height:45%"><span>C</span></div>
        </div>
        <button onclick="persona.reply('选 B')">选 B</button>
        </body></html>
        """

    func body(content: Content) -> some View {
        content.sheet(isPresented: $shown) {
            NavigationStack {
                ScrollView {
                    VStack(spacing: Theme.Space.l) {
                        AssistantHTMLCard(title: "三个方案的得分", html: Self.page, height: 240, fallback: "A 62 分，B 88 分，C 45 分；B 最高。",
                                          onReply: { replied = $0; return true }, onOpen: { _ in })
                        AssistantHTMLCard(title: "三个方案的得分（运行中）", html: Self.page, height: 240, fallback: "A 62 分，B 88 分，C 45 分。",
                                          autoRun: true, onReply: { replied = $0; return true }, onOpen: { _ in })
                        if let replied { Text("已回复：\(replied)").font(Theme.Font.secondary).accessibilityIdentifier("html-card-replied") }
                    }
                    .padding(Theme.Space.page)
                }
                .navigationTitle("HTML 卡片")
            }
        }
    }
}
#endif

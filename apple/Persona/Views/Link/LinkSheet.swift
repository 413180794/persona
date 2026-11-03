import BeautifulUI
import SwiftUI
#if canImport(UIKit)
import UIKit
#endif
import WebKit

// Links open inside the app, Telegram-style (CLAUDE.md): a sheet that rises to 60%, drags up to full screen and down
// to close, with the chat behind it still usable. Feishu docs open in the native reader and removed links as the post
// (FeishuLinks.route); any other page in a
// WKWebView taken from a pool that preloads the links of the latest answers. Text selected on a page can go to the
// agent as an edit request (AI 修改) or ride along with the next message (作为上下文), both as <jarvis-context>.

/// A piece of a page the user picked, carried in a message as
/// `<jarvis-context kind="context|edit" title="…" url="…">text</jarvis-context>`.
nonisolated struct ContextRef: Identifiable, Hashable, Sendable {
    var id = UUID()
    var kind: String // "context" | "edit"
    var title: String
    var url: String
    var text: String
    /// A quoted message's id in its chat (`kind` "quote"), so its block in the sent bubble leads back to it.
    var msg = ""

    var host: String { URL(string: url)?.host ?? url }

    var xml: String {
        func esc(_ s: String) -> String {
            s.replacingOccurrences(of: "&", with: "&amp;").replacingOccurrences(of: "\"", with: "&quot;")
                .replacingOccurrences(of: "<", with: "&lt;").replacingOccurrences(of: ">", with: "&gt;")
        }
        let msgAttr = msg.isEmpty ? "" : " msg=\"\(esc(msg))\""
        return "<jarvis-context kind=\"\(kind)\" title=\"\(esc(title))\" url=\"\(esc(url))\"\(msgAttr)>\n\(text.replacingOccurrences(of: "</jarvis-context>", with: ""))\n</jarvis-context>"
    }

    /// One block, its attributes in any order and any of them missing (a Feishu /agent request's context has no url).
    private static let tagRe = try! NSRegularExpression(pattern: #"<jarvis-context\b([^>]*)>\s*([\s\S]*?)\s*</jarvis-context>\s*"#)
    private static let attrRe = try! NSRegularExpression(pattern: #"(\w+)="([^"]*)""#)

    /// The contexts in a message and the message without them.
    private static let memo = TextMemo<(refs: [ContextRef], rest: String)>()

    static func parse(_ text: String) -> (refs: [ContextRef], rest: String) { memo(text, scan) }

    private static func unescape(_ s: String) -> String {
        s.replacingOccurrences(of: "&quot;", with: "\"").replacingOccurrences(of: "&lt;", with: "<")
            .replacingOccurrences(of: "&gt;", with: ">").replacingOccurrences(of: "&amp;", with: "&")
    }

    private static func scan(_ text: String) -> (refs: [ContextRef], rest: String) {
        let ns = text as NSString
        var refs: [ContextRef] = []
        for m in tagRe.matches(in: text, range: NSRange(location: 0, length: ns.length)) {
            let attrs = ns.substring(with: m.range(at: 1)) as NSString
            var a: [String: String] = [:]
            for am in attrRe.matches(in: attrs as String, range: NSRange(location: 0, length: attrs.length)) {
                a[attrs.substring(with: am.range(at: 1))] = unescape(attrs.substring(with: am.range(at: 2)))
            }
            refs.append(ContextRef(kind: a["kind"] ?? "context", title: a["title"] ?? "", url: a["url"] ?? "",
                                   text: ns.substring(with: m.range(at: 2)), msg: a["msg"] ?? ""))
        }
        let rest = tagRe.stringByReplacingMatches(in: text, range: NSRange(location: 0, length: ns.length), withTemplate: "")
        return (refs, rest.trimmingCharacters(in: .whitespacesAndNewlines))
    }

    /// What the row of a message's contexts says, folded: the one context's title, or how many.
    static func summary(_ refs: [ContextRef]) -> String {
        if refs.count == 1, let r = refs.first { return "\(r.kind == "edit" ? "要改的内容" : "上下文") · \(r.label)" }
        return "上下文 · \(refs.count) 项"
    }

    var label: String { title.isEmpty ? (url.isEmpty ? "引用" : host) : title }

    var icon: String {
        switch kind {
        case "instructions": Theme.Icon.info
        case "doc": Theme.Icon.document
        case "edit": Theme.Icon.ai
        default: Theme.Icon.quote
        }
    }
}

struct LinkTarget: Identifiable, Hashable {
    var url: URL
    /// A machine's port forwarded to the phone (docs/ports.md): what to call it ("机器名 :5173"); the url is the
    /// phone's loopback, which only this app serves.
    var forwarded: String? = nil
    var id: String { url.absoluteString }
}

extension EnvironmentValues {
    /// Opens a link in the in-app sheet (set by ChatView).
    @Entry var openLink: ((URL) -> Void)? = nil
    /// Inside the link sheet: hands picked text to the agent (text, "edit" | "context"). Native document views
    /// (the Feishu reader) show AI 修改 / 作为上下文 on long press when it is set.
    @Entry var docPick: ((String, String) -> Void)? = nil
}

/// The sheet's content: header (title, host, actions) over the page.
struct LinkSheet: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    let target: LinkTarget
    var key: SessionKey? = nil
    @State private var title = ""
    @State private var editing: ContextRef?
    @State private var instruction = ""
    /// Opens full screen (2026-10-28: half a screen was too little to read); pull down to 0.6 to see the chat again.
    @State private var detent: PresentationDetent = .large

    private var isFeishuDoc: Bool { FeishuLinks.isDoc(target.url) }
    /// Feishu links with a native view (documents, removed posts).
    private var feishuRoute: FeishuRoute? { FeishuLinks.route(target.url) }

    var body: some View {
        VStack(spacing: 0) {
            header
            Divider()
            if let feishuRoute {
                NavigationStack {
                    FeishuRouteView(route: feishuRoute)
                        .toolbar(.hidden, for: .navigationBar)
                        .feishuRoutes()
                        .environment(\.docPick) { text, kind in pick(text, kind: kind) }
                }
            } else {
                PooledWebView(url: target.url, title: $title) { selected, kind in pick(selected, kind: kind) }
                    .ignoresSafeArea(.container, edges: .bottom) // full screen: the page runs under the home indicator
            }
        }
        .presentationDetents([.fraction(0.6), .large], selection: $detent)
        .presentationDragIndicator(.visible)
        .presentationBackground(Theme.pageBackground) // pages with a clear background let the chat show through
        .presentationBackgroundInteraction(.enabled(upThrough: .fraction(0.6))) // the chat stays usable behind
        .presentationContentInteraction(.resizes) // a swipe up grows the sheet before it scrolls the page
        .alert("让 Agent 改这段", isPresented: Binding(get: { editing != nil }, set: { if !$0 { editing = nil } })) {
            TextField("怎么改？", text: $instruction)
            Button("取消", role: .cancel) { instruction = "" }
            Button("发送") { sendEdit() }
        } message: {
            Text(editing.map { String($0.text.prefix(120)) } ?? "")
        }
    }

    private var header: some View {
        HStack(spacing: Theme.Space.m) {
            VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                Text(title.isEmpty ? (isFeishuDoc ? "飞书文档" : feishuRoute != nil ? "removed" : (target.forwarded != nil ? nil : target.url.host) ?? "网页") : title)
                    .font(Theme.Font.secondaryStrong).lineLimit(1)
                Text(target.forwarded ?? target.url.host ?? target.url.absoluteString).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
            }
            Spacer(minLength: 6)
            if isFeishuDoc, key != nil {
                // The native reader has no custom selection menu: the whole doc can still go along.
                Button { pick("", kind: "context") } label: { Image(systemName: Theme.Icon.addContext) }
                    .accessibilityLabel("把这篇文档作为上下文")
            }
            Menu {
                Button { UIPasteboard.general.url = target.url; Haptics.success() } label: { Label("拷贝链接", systemImage: Theme.Icon.link) }
                if target.forwarded == nil { browserItems } // the phone's loopback means nothing to another app
            } label: { Image(systemName: Theme.Icon.moreMenu) }
            Button { dismiss() } label: { Image(systemName: Theme.Icon.clear).symbolRenderingMode(.hierarchical) }
                .accessibilityLabel("关闭")
                .accessibilityIdentifier("link-close")
        }
        .font(Theme.Font.title)
        .padding(.horizontal, Theme.Space.l)
        .padding(.top, Theme.Space.l)
        .padding(.bottom, Theme.Space.s)
    }

    @ViewBuilder private var browserItems: some View {
        #if os(iOS)
        Button { UIApplication.shared.open(target.url) } label: { Label("在 Safari 打开", systemImage: Theme.Icon.safari) }
        #else
        Button { UIApplication.shared.open(target.url) } label: { Label("在浏览器打开", systemImage: Theme.Icon.safari) }
        #endif
        ShareLink(item: target.url) { Label("分享", systemImage: Theme.Icon.share) }
    }

    private func pick(_ text: String, kind: String) {
        guard let key else {
            model.toast = Toast("请从会话打开链接，再交给 Agent 处理", style: .info)
            return
        }
        let ref = ContextRef(kind: kind, title: title.isEmpty ? (target.url.host ?? "") : title, url: target.url.absoluteString,
                             text: text.isEmpty ? "（整篇）" : text)
        if kind == "edit" {
            instruction = ""
            editing = ref
        } else {
            model.composerContexts[key, default: []].append(ref)
            Haptics.success()
            model.toast = Toast("已作为上下文，会随下一条消息发送", style: .success)
        }
    }

    private func sendEdit() {
        guard let ref = editing, let key else { return }
        let ask = instruction.trimmingCharacters(in: .whitespacesAndNewlines)
        model.sendNow(key, text: ref.xml + "\n\n" + (ask.isEmpty ? "按上面选中的内容修改。" : "按要求修改上面选中的内容：" + ask))
        instruction = ""
        editing = nil
    }
}

// MARK: - Web views

/// WKWebViews kept warm: the links of the latest answers load in the background, so opening one shows a finished
/// page. All share the default (on-disk) website data store, so assets and logins carry over.
@MainActor
final class WebViewPool {
    static let shared = WebViewPool()
    private var views: [String: SelectableWebView] = [:]
    private var order: [String] = []
    private let limit = 6
    private let config: WKWebViewConfiguration = {
        let c = WKWebViewConfiguration()
        c.websiteDataStore = .default()
        #if os(iOS)
        c.allowsInlineMediaPlayback = true
        #endif
        return c
    }()

    /// A view for this url, already loading (or loaded) if it was preloaded.
    func view(for url: URL) -> SelectableWebView {
        let key = url.absoluteString
        if let v = views[key] { touch(key); return v }
        let v = SelectableWebView(frame: .zero, configuration: config)
        #if os(iOS)
        v.scrollView.showsVerticalScrollIndicator = false
        v.scrollView.showsHorizontalScrollIndicator = false
        #endif
        v.allowsBackForwardNavigationGestures = true
        var req = URLRequest(url: url)
        // A page seen before paints from disk at once; a machine's dev server (the phone's loopback) is always asked:
        // its page changes as the agent works.
        req.cachePolicy = ["localhost", "127.0.0.1"].contains(url.host() ?? "") ? .reloadIgnoringLocalCacheData : .returnCacheDataElseLoad
        v.load(req)
        views[key] = v
        touch(key)
        return v
    }

    /// The view for this url if the pool has one (nothing is loaded).
    func existing(for url: URL) -> SelectableWebView? { views[url.absoluteString] }

    /// Starts loading links that may be opened soon.
    func preload(_ urls: [URL]) {
        for u in urls.prefix(3) where views[u.absoluteString] == nil && FeishuLinks.route(u) == nil { _ = view(for: u) }
    }

    private func touch(_ key: String) {
        order.removeAll { $0 == key }
        order.append(key)
        while order.count > limit {
            let old = order.removeFirst()
            views[old]?.removeFromSuperview()
            views[old] = nil
        }
    }
}

#if os(iOS)
/// A WKWebView whose selection menu has AI 修改 and 作为上下文.
final class SelectableWebView: WKWebView {
    var onPick: ((String, String) -> Void)?

    override func buildMenu(with builder: any UIMenuBuilder) {
        super.buildMenu(with: builder)
        let actions = UIMenu(options: .displayInline, children: [
            UIAction(title: "AI 修改", image: UIImage(systemName: Theme.Icon.ai)) { [weak self] _ in self?.pickSelection("edit") },
            UIAction(title: "作为上下文", image: UIImage(systemName: Theme.Icon.addContext)) { [weak self] _ in self?.pickSelection("context") },
        ])
        builder.insertChild(actions, atStartOfMenu: .root)
    }

    private func pickSelection(_ kind: String) {
        evaluateJavaScript("window.getSelection().toString()") { [weak self] result, _ in
            let text = (result as? String ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
            guard !text.isEmpty else { return }
            self?.onPick?(text, kind)
        }
    }
}

struct PooledWebView: UIViewRepresentable {
    var url: URL
    @Binding var title: String
    var onPick: (String, String) -> Void

    func makeUIView(context: Context) -> SelectableWebView {
        let v = WebViewPool.shared.view(for: url)
        v.removeFromSuperview()
        v.onPick = onPick
        context.coordinator.observe(v) { title = $0 }
        return v
    }

    func updateUIView(_ v: SelectableWebView, context: Context) { v.onPick = onPick }

    func makeCoordinator() -> Coordinator { Coordinator() }

    @MainActor
    final class Coordinator {
        private var token: NSKeyValueObservation?
        private var onTitle: ((String) -> Void)?
        func observe(_ v: WKWebView, title: @escaping (String) -> Void) {
            onTitle = title
            if let t = v.title, !t.isEmpty { title(t) }
            token = v.observe(\.title, options: [.new]) { [weak self] v, _ in
                let t = v.title ?? ""
                Task { @MainActor in if !t.isEmpty { self?.onTitle?(t) } }
            }
        }
    }
}
#endif

// MARK: - In messages

/// The contexts a message carries (picked page excerpts, everything a Feishu /agent request hands its agent): one
/// folded row by the bubble, the raw tags never show. Open, a list of titles; a title opens to its text. One context
/// opens straight to its text.
struct ContextGroup: View {
    var refs: [ContextRef]
    /// The message's id: the row stays put in the chat when it opens or closes (`ChatPin`).
    var key: String
    @Environment(\.chatPin) private var pin
    @State private var open = ContextGroup.shownOpen
    @State private var unfold = Unfolding()
    private var pinKey: String { "contexts-\(key)" }

    /// DEBUG `-showContexts YES`: groups and their first context start open (screenshots).
    static var shownOpen: Bool {
        #if DEBUG
        UserDefaults.standard.bool(forKey: "showContexts")
        #else
        false
        #endif
    }

    var body: some View {
        VStack(alignment: .trailing, spacing: Theme.Space.xs) {
            Button {
                Haptics.tap()
                toggleInPlace(pin, pinKey, animation: Theme.Motion.state, unfold: $unfold, closing: open) { open.toggle() }
            } label: {
                HStack(spacing: Theme.Space.sm) {
                    Image(systemName: refs.count == 1 ? refs[0].icon : Theme.Icon.contexts).font(Theme.Font.label)
                    Text(ContextRef.summary(refs)).font(Theme.Font.labelMedium).lineLimit(1).truncationMode(.middle)
                    Image(systemName: open ? Theme.Icon.collapse : Theme.Icon.expand).font(Theme.Font.label)
                }
                .foregroundStyle(.secondary)
                .padding(.horizontal, Theme.Space.m)
                .padding(.vertical, Theme.Space.sm)
                .background(Theme.Fill.well, in: .capsule)
                .contentShape(.capsule)
            }
            .buttonStyle(.plain)
            .accessibilityLabel(ContextRef.summary(refs))
            .accessibilityValue(open ? "已展开" : "已折叠")

            if open {
                VStack(alignment: .leading, spacing: 0) {
                    if refs.count == 1 {
                        ContextBody(ref: refs[0])
                    } else {
                        ForEach(Array(refs.enumerated()), id: \.offset) { n, r in
                            if n > 0 { Divider() }
                            ContextRow(ref: r, key: "\(pinKey)-\(n)", open: n == 0 && ContextGroup.shownOpen)
                        }
                    }
                }
                .scaledFrame(maxWidth: Theme.Size.box380, alignment: .leading)
                .background(Theme.Fill.well, in: .rect(cornerRadius: Theme.Radius.card, style: .continuous))
                .unfolding(unfold)
            }
        }
        .frame(maxWidth: .infinity, alignment: .trailing)
        .chatPinned(pinKey)
    }
}

/// One context in an open group: its title; a tap shows its text.
private struct ContextRow: View {
    var ref: ContextRef
    var key: String
    @Environment(\.chatPin) private var pin
    @State var open = false
    @State private var unfold = Unfolding()

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            Button {
                Haptics.tap()
                toggleInPlace(pin, key, animation: Theme.Motion.state, unfold: $unfold, closing: open) { open.toggle() }
            } label: {
                HStack(spacing: Theme.Space.s) {
                    Image(systemName: ref.icon).font(Theme.Font.label).foregroundStyle(.secondary)
                        .scaledFrame(width: Theme.Size.markM)
                    Text(ref.label).font(Theme.Font.secondary).foregroundStyle(Color.primary).lineLimit(1).truncationMode(.tail)
                    Spacer(minLength: Theme.Space.s)
                    if ref.text.isEmpty { Text("空").font(Theme.Font.label).foregroundStyle(.tertiary).lineLimit(1) }
                    Image(systemName: Theme.Icon.disclosure).font(Theme.Font.label).foregroundStyle(.tertiary)
                        .rotationEffect(.degrees(open ? 90 : 0))
                }
                .padding(.horizontal, Theme.Space.m)
                .padding(.vertical, Theme.Space.s)
                .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .accessibilityValue(open ? "已展开" : "已折叠")
            if open { ContextBody(ref: ref).unfolding(unfold) }
        }
        .chatPinned(key)
    }
}

/// A context's text (scrolls inside a fixed height when long) and its page, when it has one.
private struct ContextBody: View {
    var ref: ContextRef
    @Environment(\.openLink) private var openLink

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.s) {
            if ref.text.isEmpty {
                Text("没有内容").font(Theme.Font.caption).foregroundStyle(.tertiary)
            } else if ref.text.count > 1200 {
                ScrollView { text }.scaledFrame(height: Theme.Size.box300)
            } else {
                text
            }
            if let u = URL(string: ref.url), !ref.url.isEmpty {
                Button { openLink?(u) } label: {
                    Label("打开 \(ref.host)", systemImage: Theme.Icon.open).font(Theme.Font.labelMedium).lineLimit(1)
                }
                .buttonStyle(.plain)
                .foregroundStyle(Theme.accent)
            }
        }
        .padding(.horizontal, Theme.Space.m)
        .padding(.bottom, Theme.Space.m)
        .padding(.top, Theme.Space.xs)
    }

    private var text: some View {
        Text(ref.text).font(Theme.Font.caption).foregroundStyle(.secondary).textSelection(.enabled)
            .frame(maxWidth: .infinity, alignment: .leading)
    }
}

// MARK: - Picking text in native views

/// What a long press on a paragraph offers inside the link sheet (SwiftUI's own selection menu cannot take items).
struct DocPickMenu: View {
    var text: String
    var pick: (String, String) -> Void
    @Binding var selecting: Bool

    var body: some View {
        Button { pick(text, "edit") } label: { Label("AI 修改这段", systemImage: Theme.Icon.ai) }
        Button { pick(text, "context") } label: { Label("这段作为上下文", systemImage: Theme.Icon.addContext) }
        Button { selecting = true } label: { Label("选择文字…", systemImage: Theme.Icon.select) }
        Button { UIPasteboard.general.string = text; Haptics.success() } label: { Label("拷贝", systemImage: Theme.Icon.copy) }
    }
}

/// The paragraph in a text view where any span can be selected; its selection menu has AI 修改 and 作为上下文.
struct PickTextSheet: View {
    @Environment(\.dismiss) private var dismiss
    var text: String
    var pick: (String, String) -> Void

    var body: some View {
        NavigationStack {
            PickTextView(text: text) { t, kind in
                dismiss()
                pick(t, kind)
            }
            .navigationTitle("选择文字")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .cancellationAction) { Button("完成") { dismiss() } } }
        }
        .presentationDetents([.medium, .large])
    }
}

#if os(iOS)
struct PickTextView: UIViewRepresentable {
    var text: String
    var pick: (String, String) -> Void

    func makeUIView(context: Context) -> UITextView {
        let v = UITextView()
        v.isEditable = false
        v.isSelectable = true
        v.font = .preferredFont(forTextStyle: .body)
        v.adjustsFontForContentSizeCategory = true
        v.textContainerInset = UIEdgeInsets(top: 16, left: 12, bottom: 24, right: 12)
        v.backgroundColor = .clear
        v.delegate = context.coordinator
        v.text = text
        return v
    }

    func updateUIView(_ v: UITextView, context: Context) { context.coordinator.pick = pick }

    func makeCoordinator() -> Coordinator { Coordinator(pick: pick) }

    final class Coordinator: NSObject, UITextViewDelegate {
        var pick: (String, String) -> Void
        init(pick: @escaping (String, String) -> Void) { self.pick = pick }

        func textView(_ textView: UITextView, editMenuForTextIn range: NSRange, suggestedActions: [UIMenuElement]) -> UIMenu? {
            let selected = (textView.text as NSString).substring(with: range).trimmingCharacters(in: .whitespacesAndNewlines)
            guard !selected.isEmpty else { return UIMenu(children: suggestedActions) }
            let ours = UIMenu(options: .displayInline, children: [
                UIAction(title: "AI 修改", image: UIImage(systemName: Theme.Icon.ai)) { [weak self] _ in self?.pick(selected, "edit") },
                UIAction(title: "作为上下文", image: UIImage(systemName: Theme.Icon.addContext)) { [weak self] _ in self?.pick(selected, "context") },
            ])
            return UIMenu(children: [ours] + suggestedActions)
        }
    }
}
#endif

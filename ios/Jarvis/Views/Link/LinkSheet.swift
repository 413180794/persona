import SwiftUI
import UIKit
import WebKit

// Links open inside the app, Telegram-style (CLAUDE.md): a sheet that rises to 60%, drags up to full screen and down
// to close, with the chat behind it still usable. Feishu docs open in the native reader; any other page in a
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

    var host: String { URL(string: url)?.host ?? url }

    var xml: String {
        func esc(_ s: String) -> String {
            s.replacingOccurrences(of: "&", with: "&amp;").replacingOccurrences(of: "\"", with: "&quot;")
                .replacingOccurrences(of: "<", with: "&lt;").replacingOccurrences(of: ">", with: "&gt;")
        }
        return "<jarvis-context kind=\"\(kind)\" title=\"\(esc(title))\" url=\"\(esc(url))\">\n\(text.replacingOccurrences(of: "</jarvis-context>", with: ""))\n</jarvis-context>"
    }

    private static let tagRe = try! NSRegularExpression(
        pattern: #"<jarvis-context\s+kind="([^"]*)"\s+title="([^"]*)"\s+url="([^"]*)">\s*([\s\S]*?)\s*</jarvis-context>\s*"#)

    /// The contexts in a message and the message without them.
    static func parse(_ text: String) -> (refs: [ContextRef], rest: String) {
        let ns = text as NSString
        var refs: [ContextRef] = []
        for m in tagRe.matches(in: text, range: NSRange(location: 0, length: ns.length)) {
            func g(_ i: Int) -> String {
                ns.substring(with: m.range(at: i)).replacingOccurrences(of: "&quot;", with: "\"")
                    .replacingOccurrences(of: "&lt;", with: "<").replacingOccurrences(of: "&gt;", with: ">")
                    .replacingOccurrences(of: "&amp;", with: "&")
            }
            refs.append(ContextRef(kind: g(1), title: g(2), url: g(3), text: g(4)))
        }
        let rest = tagRe.stringByReplacingMatches(in: text, range: NSRange(location: 0, length: ns.length), withTemplate: "")
        return (refs, rest.trimmingCharacters(in: .whitespacesAndNewlines))
    }
}

struct LinkTarget: Identifiable, Hashable {
    var url: URL
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
    let key: SessionKey
    @State private var title = ""
    @State private var editing: ContextRef?
    @State private var instruction = ""

    private var isFeishuDoc: Bool { FeishuLinks.isDoc(target.url) }

    var body: some View {
        VStack(spacing: 0) {
            header
            Divider()
            if isFeishuDoc {
                NavigationStack {
                    FeishuDocReader(url: target.url.absoluteString, title: nil)
                        .toolbar(.hidden, for: .navigationBar)
                        .feishuRoutes()
                        .environment(\.docPick) { text, kind in pick(text, kind: kind) }
                }
            } else {
                PooledWebView(url: target.url, title: $title) { selected, kind in pick(selected, kind: kind) }
            }
        }
        .presentationDetents([.fraction(0.6), .large])
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
        HStack(spacing: 10) {
            VStack(alignment: .leading, spacing: 1) {
                Text(title.isEmpty ? (isFeishuDoc ? "飞书文档" : target.url.host ?? "网页") : title)
                    .font(.subheadline.weight(.semibold)).lineLimit(1)
                Text(target.url.host ?? target.url.absoluteString).font(.caption).foregroundStyle(.secondary).lineLimit(1)
            }
            Spacer(minLength: 6)
            if isFeishuDoc {
                // The native reader has no custom selection menu: the whole doc can still go along.
                Button { pick("", kind: "context") } label: { Image(systemName: "text.badge.plus") }
                    .accessibilityLabel("把这篇文档作为上下文")
            }
            Menu {
                Button { UIPasteboard.general.url = target.url; Haptics.success() } label: { Label("拷贝链接", systemImage: "link") }
                Button { UIApplication.shared.open(target.url) } label: { Label("在 Safari 打开", systemImage: "safari") }
                ShareLink(item: target.url) { Label("分享", systemImage: "square.and.arrow.up") }
            } label: { Image(systemName: "ellipsis.circle") }
            Button { dismiss() } label: { Image(systemName: "xmark.circle.fill").symbolRenderingMode(.hierarchical) }
                .accessibilityLabel("关闭")
        }
        .font(.title3)
        .padding(.horizontal, 16)
        .padding(.top, 14)
        .padding(.bottom, 8)
    }

    private func pick(_ text: String, kind: String) {
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
        guard let ref = editing else { return }
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
        c.allowsInlineMediaPlayback = true
        return c
    }()

    /// A view for this url, already loading (or loaded) if it was preloaded.
    func view(for url: URL) -> SelectableWebView {
        let key = url.absoluteString
        if let v = views[key] { touch(key); return v }
        let v = SelectableWebView(frame: .zero, configuration: config)
        v.allowsBackForwardNavigationGestures = true
        var req = URLRequest(url: url)
        req.cachePolicy = .returnCacheDataElseLoad // a page seen before paints from disk at once
        v.load(req)
        views[key] = v
        touch(key)
        return v
    }

    /// Starts loading links that may be opened soon.
    func preload(_ urls: [URL]) {
        for u in urls.prefix(3) where views[u.absoluteString] == nil && !FeishuLinks.isDoc(u) { _ = view(for: u) }
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

/// A WKWebView whose selection menu has AI 修改 and 作为上下文.
final class SelectableWebView: WKWebView {
    var onPick: ((String, String) -> Void)?

    override func buildMenu(with builder: any UIMenuBuilder) {
        super.buildMenu(with: builder)
        let actions = UIMenu(options: .displayInline, children: [
            UIAction(title: "AI 修改", image: UIImage(systemName: "sparkles")) { [weak self] _ in self?.pickSelection("edit") },
            UIAction(title: "作为上下文", image: UIImage(systemName: "text.badge.plus")) { [weak self] _ in self?.pickSelection("context") },
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

// MARK: - In messages

/// The contexts a message carries, as quote cards above it (the raw tag never shows).
struct ContextCards: View {
    var refs: [ContextRef]
    @Environment(\.openLink) private var openLink

    var body: some View {
        VStack(alignment: .trailing, spacing: 6) {
            ForEach(refs) { r in
                Button { if let u = URL(string: r.url) { openLink?(u) } } label: {
                    VStack(alignment: .leading, spacing: 4) {
                        Label(r.kind == "edit" ? "要改的内容 · \(r.title)" : "上下文 · \(r.title)",
                              systemImage: r.kind == "edit" ? "sparkles" : "text.quote")
                            .font(.caption.weight(.semibold)).foregroundStyle(Theme.accent).lineLimit(1)
                        Text(r.text).font(.footnote).foregroundStyle(.secondary).lineLimit(3).multilineTextAlignment(.leading)
                        Text(r.host).font(.caption2).foregroundStyle(.tertiary).lineLimit(1)
                    }
                    .padding(10)
                    .frame(maxWidth: 300, alignment: .leading)
                    .background(Color.primary.opacity(0.04), in: .rect(cornerRadius: 12, style: .continuous))
                    .overlay(alignment: .leading) { Capsule().fill(Theme.accent.opacity(0.5)).frame(width: 3).padding(.vertical, 8) }
                }
                .buttonStyle(.plain)
            }
        }
        .frame(maxWidth: .infinity, alignment: .trailing)
    }
}

/// Pending contexts in the composer: chips that go with the next message.
struct ContextChips: View {
    @Binding var refs: [ContextRef]

    var body: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 6) {
                ForEach(refs) { r in
                    HStack(spacing: 5) {
                        Image(systemName: "text.quote").font(.caption2)
                        Text(r.text == "（整篇）" ? r.title : String(r.text.prefix(24))).font(.caption).lineLimit(1)
                        Button { refs.removeAll { $0.id == r.id } } label: { Image(systemName: "xmark").font(.caption2.weight(.bold)) }
                            .buttonStyle(.plain)
                            .accessibilityLabel("移除这个上下文")
                    }
                    .padding(.horizontal, 10)
                    .padding(.vertical, 6)
                    .background(Theme.accent.opacity(0.12), in: .capsule)
                    .foregroundStyle(Theme.accent)
                }
            }
        }
    }
}

// MARK: - Picking text in native views

/// What a long press on a paragraph offers inside the link sheet (SwiftUI's own selection menu cannot take items).
struct DocPickMenu: View {
    var text: String
    var pick: (String, String) -> Void
    @Binding var selecting: Bool

    var body: some View {
        Button { pick(text, "edit") } label: { Label("AI 修改这段", systemImage: "sparkles") }
        Button { pick(text, "context") } label: { Label("这段作为上下文", systemImage: "text.badge.plus") }
        Button { selecting = true } label: { Label("选择文字…", systemImage: "selection.pin.in.out") }
        Button { UIPasteboard.general.string = text; Haptics.success() } label: { Label("拷贝", systemImage: "doc.on.doc") }
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
                UIAction(title: "AI 修改", image: UIImage(systemName: "sparkles")) { [weak self] _ in self?.pick(selected, "edit") },
                UIAction(title: "作为上下文", image: UIImage(systemName: "text.badge.plus")) { [weak self] _ in self?.pick(selected, "context") },
            ])
            return UIMenu(children: [ours] + suggestedActions)
        }
    }
}


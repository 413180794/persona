import AppKit
import SwiftUI
import WebKit

// Links open inside the app (CLAUDE.md). On the iPhone that is a card rising from the bottom; on the Mac it is a panel
// sliding in at the detail column's trailing edge, beside what it came from (docs/mac.md「和 iOS 的对应」): the page
// stays usable, the panel's width is dragged and remembered, ⌘W closes it, 「在浏览器打开」 hands it to the default
// browser, and 「在单独窗口打开」 moves it into a window of its own. Feishu documents and company-feed posts open in the
// native views (FeishuRouteView) while the hub has them (`LarkFeatures`), the rest in a WKWebView from the shared pool
// (`WebViewPool`, preloaded, reused).

extension View {
    /// Shows `link` in a panel at the trailing edge. `key` is the conversation 「AI 修改」 / 「作为上下文」 go to; without
    /// one (收件箱, 知识库) they go to Persona' main conversation.
    func macLinkPanel(_ link: Binding<LinkTarget?>, key: SessionKey? = nil) -> some View {
        modifier(MacLinkPanelHost(link: link, key: key))
    }

    /// Links followed inside this view (`openURL`) open in the panel; the Feishu ones in the native reader there.
    func macOpensLinksInPanel(_ link: Binding<LinkTarget?>) -> some View {
        environment(\.openURL, OpenURLAction { url in
            guard let scheme = url.scheme?.lowercased(), scheme == "http" || scheme == "https" else { return LinkPolicy.system(url) }
            link.wrappedValue = LinkTarget(url: url)
            return .handled
        })
        .environment(\.openLink) { link.wrappedValue = LinkTarget(url: $0) }
    }
}

/// The page with the panel beside it. The panel is a sibling in an HStack (not an overlay): the page narrows instead
/// of being covered, so what the link came from stays readable.
private struct MacLinkPanelHost: ViewModifier {
    @Binding var link: LinkTarget?
    var key: SessionKey?
    @AppStorage("mac.linkPanel.width") private var width: Double = Theme.Size.macLinkPanelIdeal
    /// The width while a drag is going (written to `width` when it ends, so the store is not hit every frame).
    @State private var dragging: Double?

    func body(content: Content) -> some View {
        GeometryReader { geo in
            // Too narrow for both side by side: the panel covers the column, as a page pushed on it would.
            let beside = geo.size.width >= Theme.Size.macLinkPanelMin + Theme.Size.macLinkPanelPageMin
            HStack(spacing: 0) {
                content
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                if let link, beside {
                    let w = clamp(dragging ?? width, in: geo.size.width)
                    MacPanelResizeHandle { delta in
                        dragging = clamp((dragging ?? width) - delta, in: geo.size.width)
                    } ended: {
                        if let dragging { width = dragging }
                        dragging = nil
                    }
                    panel(link)
                        .frame(width: w)
                }
            }
            .overlay {
                if let link, !beside { panel(link) }
            }
        }
        .animation(Theme.Motion.layout, value: link?.id)
    }

    private func panel(_ link: LinkTarget) -> some View {
        MacLinkPanel(target: link, key: key) { self.link = nil }
            .id(link.id)
            .transition(.move(edge: .trailing).combined(with: .opacity))
    }

    /// At least the panel's smallest, and never so wide the page goes under its smallest.
    private func clamp(_ w: Double, in total: CGFloat) -> CGFloat {
        let most = max(Theme.Size.macLinkPanelMin, total - Theme.Size.macLinkPanelPageMin)
        return min(max(w, Theme.Size.macLinkPanelMin), most)
    }
}

/// A divider that can be dragged sideways, with the resize cursor over it (as the split views' dividers).
struct MacPanelResizeHandle: View {
    var changed: (CGFloat) -> Void
    var ended: () -> Void
    @State private var last: CGFloat = 0

    var body: some View {
        Divider()
            .overlay {
                Color.clear
                    .frame(width: Theme.Size.macResizeHitWidth)
                    .contentShape(.rect)
                    .onHover { inside in if inside { NSCursor.resizeLeftRight.push() } else { NSCursor.pop() } }
                    .gesture(
                        DragGesture(minimumDistance: 1, coordinateSpace: .global)
                            .onChanged { v in
                                changed(v.translation.width - last)
                                last = v.translation.width
                            }
                            .onEnded { _ in last = 0; ended() }
                    )
            }
            .accessibilityHidden(true)
    }
}

/// The panel: a header (title, host, back / forward / reload, actions, close) over the page.
struct MacLinkPanel: View {
    @Environment(AppModel.self) private var model
    @Environment(\.openWindow) private var openWindow
    let target: LinkTarget
    var key: SessionKey?
    /// Closes the panel; nil in a window of its own (the window's close button does it).
    var close: (() -> Void)?
    @State private var title = ""
    @State private var editing: ContextRef?
    @State private var instruction = ""

    /// Feishu links with a native view (documents, company-feed posts), while the hub has them (`LarkFeatures`).
    private var feishuRoute: FeishuRoute? { FeishuLinks.route(target.url, lark: model.larkFeatures) }
    private var isFeishuDoc: Bool { if case .doc = feishuRoute { true } else { false } }
    /// Where 「AI 修改」 and 「作为上下文」 go: the conversation the link came from, else Persona' main one.
    private var destination: SessionKey? { key ?? model.assistantMain ?? model.assistantCurrent }

    var body: some View {
        VStack(spacing: 0) {
            header
            Divider()
            if let feishuRoute {
                NavigationStack {
                    FeishuRouteView(route: feishuRoute)
                        .feishuRoutes()
                        .environment(\.docPick) { text, kind in pick(text, kind: kind) }
                }
            } else {
                PooledWebView(url: target.url, title: $title) { selected, kind in pick(selected, kind: kind) }
            }
        }
        .background(Theme.pageBackground)
        .alert("让 Agent 改这段", isPresented: Binding(get: { editing != nil }, set: { if !$0 { editing = nil } })) {
            TextField("怎么改？", text: $instruction)
            Button("取消", role: .cancel) { instruction = "" }
            Button("发送") { sendEdit() }
        } message: {
            Text(editing.map { String($0.text.prefix(120)) } ?? "")
        }
        .accessibilityIdentifier("mac.linkPanel")
    }

    private var shownTitle: String {
        title.isEmpty ? (isFeishuDoc ? "飞书文档" : feishuRoute != nil ? "公司圈" : target.url.host ?? "网页") : title
    }

    private var header: some View {
        HStack(spacing: Theme.Space.s) {
            if feishuRoute == nil {
                ControlGroup {
                    Button { web?.goBack() } label: { Image(systemName: Theme.Icon.back) }
                        .help("后退")
                    Button { web?.goForward() } label: { Image(systemName: Theme.Icon.forward) }
                        .help("前进")
                }
                .controlGroupStyle(.navigation)
                .fixedSize()
            }
            VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                Text(shownTitle).font(Theme.Font.secondaryStrong).foregroundStyle(.primary).lineLimit(1)
                Text(target.url.host ?? target.url.absoluteString).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
            }
            .help(target.url.absoluteString)
            Spacer(minLength: Theme.Space.sm)
            if isFeishuDoc {
                Button { pick("", kind: "context") } label: { Image(systemName: Theme.Icon.addContext) }
                    .help("把这篇文档作为上下文")
                    .accessibilityLabel("把这篇文档作为上下文")
            } else if feishuRoute == nil {
                Button { web?.reload() } label: { Image(systemName: Theme.Icon.retry) }
                    .help("重新载入")
            }
            Menu {
                Button { NSWorkspace.shared.open(target.url) } label: { Label("在浏览器打开", systemImage: Theme.Icon.safari) }
                if close != nil {
                    Button {
                        openWindow(id: MacLinkWindow.id, value: target.url)
                        close?()
                    } label: { Label("在单独窗口打开", systemImage: Theme.Icon.newWindow) }
                }
                Divider()
                Button {
                    NSPasteboard.general.clearContents()
                    NSPasteboard.general.setString(target.url.absoluteString, forType: .string)
                    model.toast = Toast("已拷贝链接", style: .success)
                } label: { Label("拷贝链接", systemImage: Theme.Icon.link) }
                ShareLink(item: target.url) { Label("分享", systemImage: Theme.Icon.share) }
            } label: { Image(systemName: Theme.Icon.moreMenu) }
            .menuIndicator(.hidden)
            .fixedSize()
            .help("更多")
            if let close {
                Button(action: close) { Image(systemName: Theme.Icon.clear).symbolRenderingMode(.hierarchical) }
                    .keyboardShortcut("w", modifiers: .command)
                    .help("关闭（⌘W）")
                    .accessibilityLabel("关闭")
            }
        }
        .buttonStyle(.borderless)
        .foregroundStyle(.secondary)
        .labelStyle(.iconOnly)
        .font(Theme.Font.heading)
        .padding(.horizontal, Theme.Space.m)
        .padding(.vertical, Theme.Space.s)
    }

    /// The pooled view for this link (back / forward / reload act on it).
    private var web: WKWebView? { WebViewPool.shared.existing(for: target.url) }

    private func pick(_ text: String, kind: String) {
        let ref = ContextRef(kind: kind, title: title.isEmpty ? (target.url.host ?? "") : title, url: target.url.absoluteString,
                             text: text.isEmpty ? "（整篇）" : text)
        if kind == "edit" {
            instruction = ""
            editing = ref
        } else if let destination {
            model.composerContexts[destination, default: []].append(ref)
            model.toast = Toast(key == nil ? "已作为上下文，会随下一条发给 Persona 的消息发送" : "已作为上下文，会随下一条消息发送", style: .success)
        } else {
            Task { await model.askJarvis(ref.xml + "\n\n先记下这段，我接着说。") }
        }
    }

    private func sendEdit() {
        guard let ref = editing else { return }
        let ask = instruction.trimmingCharacters(in: .whitespacesAndNewlines)
        let text = ref.xml + "\n\n" + (ask.isEmpty ? "按上面选中的内容修改。" : "按要求修改上面选中的内容：" + ask)
        if let key { model.sendNow(key, text: text) } else { Task { await model.askJarvis(text) } }
        instruction = ""
        editing = nil
    }
}

/// A link in a window of its own (dragged out of the panel with 「在单独窗口打开」): the same panel, closed with the
/// window. Its 「AI 修改」 / 「作为上下文」 go to Persona.
struct MacLinkWindow: View {
    static let id = "link"
    @Environment(AppModel.self) private var model
    var url: URL?

    var body: some View {
        Group {
            if let url {
                MacLinkPanel(target: LinkTarget(url: url), key: nil, close: nil)
            } else {
                EmptyStateView(symbol: Theme.Icon.link, title: "没有链接", message: "从对话或收件箱里打开一个链接。")
            }
        }
        .macLook(model)
        .frame(minWidth: Theme.Size.macLinkPanelMin, minHeight: Theme.Size.macSheetMinHeight)
    }
}

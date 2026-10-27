import MarkdownUI
import SwiftUI

// The native document reader: Markdown export first (fast, faithful tables and lists); when the owner forbids export,
// the JSON node tree rendered natively. Around the text: title, where it lives, who edited it, read count, the cached
// AI summary, the outline, comments and 在飞书中打开. Base / whiteboard / file links get their own page.

/// What a document read produced.
enum FeishuDocsContent {
    case markdown(FeishuDocsMarkdownBody, title: String, copy: Bool)
    case tree(FeishuDocsView)
    case unsupported(FeishuDocsUnsupported)
}

struct FeishuDocsUnsupported {
    var kind: FeishuDocsKind
    var title: String
    var message: String
    /// A drive file that downloads into Quick Look.
    var file: (token: String, ext: String?)?
    /// A whiteboard's rendered picture.
    var image: String?
}

@MainActor @Observable
final class FeishuDocsReaderModel {
    var content: FeishuDocsContent?
    var error: String?
    var header: FeishuDocsHeader?
    var info: FeishuDocsReaderInfo?
    var summary: FeishuDocsAISummary?
    var loading = false

    func load(url: String, title: String?) async {
        guard let api = AppModel.shared.api else { error = "还没有连接 hub"; return }
        loading = true
        defer { loading = false }
        async let header: FeishuDocsHeader? = try? api.feishuDocsGet("documents/header", query: ["url": url])
        async let info: FeishuDocsReaderInfo? = try? api.feishuDocsGet("documents/reader/info", query: ["url": url])
        async let summary: FeishuDocsAISummary? = try? api.feishuDocsGet("documents/ai-summary", query: ["url": url])
        do {
            content = try await Self.read(api, url: url, title: title)
            error = nil
        } catch {
            if let msg = error.feishuDocsMessage { self.error = msg }
        }
        self.header = await header
        self.info = await info
        self.summary = await summary
    }

    /// Markdown when the owner allows export, else the JSON tree; what neither can show becomes `.unsupported`.
    static func read(_ api: APIClient, url: String, title: String?) async throws -> FeishuDocsContent {
        let kind = FeishuDocsKind.from(url: url)
        if kind == .bitable {
            return .unsupported(.init(kind: .bitable, title: title ?? "多维表格", message: "多维表格暂不支持在 Jarvis 中查看", file: nil, image: nil))
        }
        if kind != .file && kind != .whiteboard {
            do {
                let md: FeishuDocsMarkdownResponse = try await api.feishuDocsGet("documents/read", query: ["url": url, "format": "markdown"])
                return .markdown(FeishuDocsMarkdownExport.prepare(md.markdown, title: md.title), title: md.title, copy: md.authority?.copy ?? true)
            } catch let e as FeishuDocsFailure where e.code == "DOCUMENT_EXPORT_FORBIDDEN" || e.code == "DOCUMENT_FORMAT_UNSUPPORTED" || e.status == 422 {
                // Fall through to the tree.
            }
        }
        let view: FeishuDocsView
        do {
            view = try await api.feishuDocsGet("documents/read", query: ["url": url, "format": "json"])
        } catch let e as FeishuDocsFailure where e.code == "DOCUMENT_FORMAT_UNSUPPORTED" || e.status == 422 {
            return .unsupported(.init(kind: kind, title: title ?? kind.label, message: "这种内容暂不支持在 Jarvis 中查看", file: nil, image: nil))
        }
        let name = view.title.isEmpty ? (title ?? kind.label) : view.title
        if view.isBase {
            return .unsupported(.init(kind: .bitable, title: name, message: "多维表格暂不支持在 Jarvis 中查看", file: nil, image: nil))
        }
        if let o = view.object {
            switch o.kind {
            case "file":
                return .unsupported(.init(kind: .file, title: name, message: "云空间文件，可以下载后预览", file: (o.token, o.extension), image: nil))
            case "whiteboard":
                return .unsupported(.init(kind: .whiteboard, title: name, message: o.empty == true ? "这是一个空白画板" : "画板", file: nil, image: o.image))
            default:
                return .unsupported(.init(kind: kind, title: name, message: "这种内容暂不支持在 Jarvis 中查看", file: nil, image: nil))
            }
        }
        if view.isSpreadsheet && view.content.children.isEmpty {
            return .unsupported(.init(kind: .sheet, title: name, message: "作者禁止导出这个表格，请在飞书中查看", file: nil, image: nil))
        }
        return .tree(view)
    }
}

struct FeishuDocReader: View {
    var url: String
    var title: String?

    @Environment(\.feishuOpen) private var open
    @State private var model = FeishuDocsReaderModel()
    @State private var showTitle = false
    @State private var showOutline = false
    @State private var showComments = false
    @State private var summaryExpanded = false
    @State private var gallery: FeishuDocsGallery?
    @State private var jump: String?

    var body: some View {
        ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 12) {
                    switch model.content {
                    case .markdown(let body, _, _):
                        titleBlock
                        summaryCard
                        ForEach(body.chunks) { chunk in
                            if let table = chunk.table {
                                FeishuDocsMarkdownTableView(table: table)
                                    .padding(.vertical, 4)
                                    .id(chunk.id)
                            } else {
                                Markdown(chunk.text)
                                    .markdownTheme(.feishuDocs)
                                    .markdownImageProvider(FeishuDocsImageProvider(open: { openImage($0, in: body.images) }))
                                    .modifier(FeishuDocsSelectable(enabled: copyAllowed))
                                    .id(chunk.id)
                            }
                        }
                        footer
                    case .tree(let view):
                        titleBlock
                        summaryCard
                        treeRows(view)
                        footer
                    case .unsupported(let u):
                        FeishuDocsUnsupportedView(info: u, url: url)
                    case nil:
                        if let e = model.error {
                            EmptyStateView(symbol: "doc.questionmark", title: "打不开这篇文档", message: e,
                                           action: ("重试", { Task { await model.load(url: url, title: title) } }))
                                .padding(.top, 60)
                            Button("在飞书中打开") { FeishuDocsLinks.openInFeishu(url) }
                                .lineLimit(1).frame(maxWidth: .infinity)
                        } else {
                            FeishuDocsSkeleton(title: title)
                        }
                    }
                }
                .padding(.horizontal, 18)
                .padding(.vertical, 12)
            }
            .onScrollGeometryChange(for: Bool.self) { $0.contentOffset.y + $0.contentInsets.top > 90 } action: { _, v in
                withAnimation(.easeInOut(duration: 0.15)) { showTitle = v }
            }
            .onChange(of: jump) {
                guard let id = jump else { return }
                jump = nil
                withAnimation(.snappy) { proxy.scrollTo(id, anchor: .top) }
            }
        }
        .themedPage()
        .navigationTitle(showTitle ? docTitle : "")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar { toolbar }
        .refreshable { await model.load(url: url, title: title) }
        .task {
            if model.content == nil { await model.load(url: url, title: title) }
            #if DEBUG
            // Simulator automation: `-openFeishuDocHeading <n>` scrolls to the n-th outline entry, `-showFeishuDocSheet outline|comments` opens a sheet.
            let n = UserDefaults.standard.integer(forKey: "openFeishuDocHeading")
            if n > 0, n <= outline.count {
                try? await Task.sleep(for: .milliseconds(500))
                jump = rowID(for: outline[n - 1])
            }
            switch UserDefaults.standard.string(forKey: "showFeishuDocSheet") {
            case "outline": showOutline = true
            case "comments": showComments = true
            default: break
            }
            #endif
        }
        .environment(\.openURL, OpenURLAction { link in handle(link) })
        .environment(\.feishuDocsContext, FeishuDocsRenderContext(images: images, selectable: copyAllowed, openImage: { openImage($0, in: images) }))
        .sheet(isPresented: $showOutline) {
            FeishuDocsOutlineSheet(items: outline) { item in
                showOutline = false
                jump = rowID(for: item)
            }
            .presentationDetents([.medium, .large])
        }
        .sheet(isPresented: $showComments) {
            NavigationStack { FeishuDocsCommentsView(url: url, title: docTitle) }
                .presentationDetents([.large])
        }
        .fullScreenCover(item: $gallery) { g in LarkImageGallery(urls: g.urls, index: g.index) }
    }

    // MARK: Header

    private var docTitle: String {
        switch model.content {
        case .markdown(_, let t, _): t
        case .tree(let v): v.title.isEmpty ? (title ?? "文档") : v.title
        case .unsupported(let u): u.title
        case nil: title ?? "文档"
        }
    }

    private var titleBlock: some View {
        VStack(alignment: .leading, spacing: 10) {
            if let crumbs = breadcrumb { Text(crumbs).font(.caption).foregroundStyle(.secondary).lineLimit(1).truncationMode(.head) }
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                if let icon = model.header?.icon, icon.type == "emoji" { Text(icon.value).font(.title) }
                Text(docTitle).font(.title.weight(.bold)).fixedSize(horizontal: false, vertical: true)
                    .modifier(FeishuDocsSelectable(enabled: copyAllowed))
            }
            HStack(spacing: 8) {
                if let owner = model.info?.owner ?? treeOwner {
                    Button {
                        if let id = owner.id { open?(.profile(LarkPersonRef(id: id, name: owner.name, avatar: owner.avatarUrl))) }
                    } label: {
                        HStack(spacing: 6) {
                            LarkAvatarImage(name: owner.name, url: owner.avatarUrl, size: 22)
                            Text(owner.name).font(.footnote.weight(.medium)).foregroundStyle(.primary).lineLimit(1)
                        }
                    }
                    .buttonStyle(.plain)
                    .disabled(owner.id == nil)
                }
                if let edited = editedLine { Text("· " + edited).font(.footnote).foregroundStyle(.secondary).lineLimit(1) }
                Spacer(minLength: 0)
            }
            if let stats = statsLine {
                Text(stats).font(.caption).foregroundStyle(.tertiary).lineLimit(1)
            }
        }
        .padding(.bottom, 4)
    }

    private var breadcrumb: String? {
        guard let h = model.header else { return nil }
        let names = (h.path ?? []).map(\.name)
        if h.inMySpace == true { return (["我的空间"] + names).joined(separator: " / ") }
        return names.isEmpty ? nil : names.joined(separator: " / ")
    }

    private var treeOwner: FeishuDocsPerson? { if case .tree(let v) = model.content { v.owner } else { nil } }

    private var editedLine: String? {
        let when = model.header?.editedAt.flatMap(JSON.parseDate) ?? model.info?.updatedAt.flatMap(JSON.parseDate)
        guard let when else { return nil }
        let t = FeishuTime.short(when)
        let owner = (model.info?.owner ?? treeOwner)?.name
        if let who = model.header?.editorName, !who.isEmpty, who != owner { return "\(who) 更新于 \(t)" }
        return "更新于 \(t)"
    }

    private var statsLine: String? {
        guard let i = model.info else { return nil }
        var parts: [String] = []
        if let v = i.views, v > 0 { parts.append("\(v) 次阅读") }
        if let v = i.visitors, v > 0 { parts.append("\(v) 人看过") }
        if let l = i.likes, l > 0 { parts.append("\(l) 个赞") }
        if let c = i.comments, c > 0 { parts.append("\(c) 条评论") }
        return parts.isEmpty ? nil : parts.joined(separator: " · ")
    }

    @ViewBuilder private var summaryCard: some View {
        if let s = model.summary, s.state == "ready", !s.summary.isEmpty {
            VStack(alignment: .leading, spacing: 8) {
                HStack(spacing: 6) {
                    Image(systemName: "sparkles").foregroundStyle(LarkColor.hue("purple"))
                    Text("AI 摘要").font(.subheadline.weight(.semibold))
                    Spacer()
                    Button(summaryExpanded ? "收起" : "展开") { withAnimation(.snappy) { summaryExpanded.toggle() } }
                        .font(.footnote).lineLimit(1)
                }
                Markdown(s.summary)
                    .markdownTheme(.feishuDocsSmall)
                    .frame(maxHeight: summaryExpanded ? nil : 96, alignment: .top)
                    .clipped()
                    .mask(alignment: .top) {
                        if summaryExpanded { Rectangle() } else {
                            LinearGradient(stops: [.init(color: .black, location: 0.7), .init(color: .clear, location: 1)], startPoint: .top, endPoint: .bottom)
                        }
                    }
                    .contentShape(.rect)
                    .onTapGesture { withAnimation(.snappy) { summaryExpanded.toggle() } }
            }
            .padding(12)
            .background(LarkColor.hue("purple").opacity(0.07), in: .rect(cornerRadius: 14, style: .continuous))
            .overlay(RoundedRectangle(cornerRadius: 14, style: .continuous).strokeBorder(LarkColor.hue("purple").opacity(0.18), lineWidth: 0.5))
        }
    }

    private var footer: some View {
        VStack(spacing: 12) {
            Divider()
            HStack(spacing: 10) {
                if (model.info?.comments ?? 0) > 0 {
                    Button { showComments = true } label: {
                        Label("\(model.info?.comments ?? 0) 条评论", systemImage: "text.bubble").lineLimit(1)
                    }
                    .themedButton()
                }
                Button { FeishuDocsLinks.openInFeishu(url) } label: {
                    Label("在飞书中打开", systemImage: "arrow.up.forward.app").lineLimit(1)
                }
                .themedButton()
            }
            .font(.subheadline)
        }
        .frame(maxWidth: .infinity)
        .padding(.top, 20)
        .padding(.bottom, 12)
    }

    // MARK: Tree

    @ViewBuilder private func treeRows(_ view: FeishuDocsView) -> some View {
        let blocks = view.content.children
        let numbers = FeishuDocsTree.orderedNumbers(blocks)
        ForEach(Array(blocks.enumerated()), id: \.offset) { i, node in
            FeishuDocsBlock(node: node, number: numbers[i])
                .id(FeishuDocsTree.rowKey(node, index: i))
        }
        if view.partial == true {
            Label("文档较长，只显示了前面的部分", systemImage: "ellipsis.circle").font(.footnote).foregroundStyle(.secondary)
        }
    }

    // MARK: Outline, images, links

    private var outline: [FeishuDocsOutlineItem] {
        switch model.content {
        case .markdown(let b, _, _): b.outline
        case .tree(let v): v.outline.isEmpty ? FeishuDocsTree.outline(v.content) : v.outline
        default: []
        }
    }

    private func rowID(for item: FeishuDocsOutlineItem) -> String? {
        switch model.content {
        case .markdown: item.id
        case .tree(let v): FeishuDocsTree.rowID(for: item.id.replacingOccurrences(of: "doc-block-", with: ""), in: v.content)
            ?? FeishuDocsTree.rowID(for: item.id, in: v.content)
        default: nil
        }
    }

    private var images: [String] {
        switch model.content {
        case .markdown(let b, _, _): b.images
        case .tree(let v): FeishuDocsTree.images(v.content)
        default: []
        }
    }

    private var copyAllowed: Bool {
        switch model.content {
        case .markdown(_, _, let copy): copy
        case .tree(let v): v.authority?.copy ?? true
        default: true
        }
    }

    private func openImage(_ src: String, in list: [String]) {
        let all = list.isEmpty ? [src] : list
        gallery = FeishuDocsGallery(urls: all, index: all.firstIndex(of: src) ?? 0)
    }

    /// Mentions open the profile card, Feishu doc links the native reader, the rest the system.
    private func handle(_ link: URL) -> OpenURLAction.Result {
        if link.scheme == feishuDocsUserScheme, let id = link.host() {
            let name = URLComponents(url: link, resolvingAgainstBaseURL: false)?.queryItems?.first { $0.name == "name" }?.value ?? ""
            open?(.profile(LarkPersonRef(id: id, name: name)))
            return .handled
        }
        if link.scheme == nil, link.path.hasPrefix("/messenger/") { return .discarded }
        if let open, FeishuLinks.isDoc(link) {
            open(.doc(url: link.absoluteString, title: nil))
            return .handled
        }
        return .systemAction
    }

    @ToolbarContentBuilder private var toolbar: some ToolbarContent {
        ToolbarItemGroup(placement: .topBarTrailing) {
            if !outline.isEmpty {
                Button { showOutline = true } label: { Image(systemName: "list.bullet.indent") }
                    .accessibilityLabel("目录")
            }
            if model.content != nil, case .unsupported = model.content {} else if model.content != nil {
                Button { showComments = true } label: {
                    Image(systemName: "text.bubble")
                        .overlay(alignment: .topTrailing) {
                            if let n = model.info?.comments, n > 0 {
                                Text(n > 99 ? "99+" : "\(n)")
                                    .font(.system(size: 9, weight: .bold).monospacedDigit())
                                    .foregroundStyle(.white)
                                    .padding(.horizontal, 3)
                                    .frame(minWidth: 14, minHeight: 14)
                                    .background(LarkColor.hue("red"), in: .capsule)
                                    .offset(x: 9, y: -7)
                            }
                        }
                }
                .accessibilityLabel("评论")
            }
            Menu {
                FeishuDocsLinkMenu(url: url)
                Divider()
                Button { Task { await model.load(url: url, title: title) } } label: { Label("刷新", systemImage: "arrow.clockwise") }
            } label: { Image(systemName: "ellipsis") }
            .accessibilityLabel("更多")
        }
    }
}

struct FeishuDocsGallery: Identifiable {
    var urls: [String]
    var index: Int
    var id: String { urls.joined() + "#\(index)" }
}

private struct FeishuDocsSelectable: ViewModifier {
    var enabled: Bool
    func body(content: Content) -> some View {
        if enabled { content.textSelection(.enabled) } else { content }
    }
}

/// Markdown images through the hub, tappable into the gallery.
struct FeishuDocsImageProvider: ImageProvider {
    var open: (String) -> Void

    func makeImage(url: URL?) -> some View {
        let src = url?.absoluteString ?? ""
        return Button { open(src) } label: {
            LarkRemoteImage(url: src, maxPixel: 1600, contentMode: .fit)
                .frame(maxWidth: .infinity, minHeight: 60)
                .clipShape(.rect(cornerRadius: 8, style: .continuous))
        }
        .buttonStyle(.plain)
        .padding(.vertical, 4)
    }
}

extension MarkdownUI.Theme {
    /// Long-form reading: the app's Markdown theme with document headings and Feishu-blue links.
    @MainActor static var feishuDocs: MarkdownUI.Theme {
        MarkdownUI.Theme.jarvis
            .link { ForegroundColor(LarkColor.blue) }
            .heading1 { config in
                config.label.markdownMargin(top: 20, bottom: 8).markdownTextStyle { FontWeight(.bold); FontSize(.em(1.45)) }
            }
            .heading2 { config in
                config.label.markdownMargin(top: 16, bottom: 6).markdownTextStyle { FontWeight(.bold); FontSize(.em(1.25)) }
            }
            .heading3 { config in
                config.label.markdownMargin(top: 12, bottom: 4).markdownTextStyle { FontWeight(.semibold); FontSize(.em(1.1)) }
            }
            .heading4 { config in
                config.label.markdownMargin(top: 10, bottom: 4).markdownTextStyle { FontWeight(.semibold) }
            }
    }

    @MainActor static var feishuDocsSmall: MarkdownUI.Theme {
        MarkdownUI.Theme.jarvis
            .text { FontSize(15) }
            .paragraph { config in config.label.fixedSize(horizontal: false, vertical: true).relativeLineSpacing(.em(0.15)).markdownMargin(top: 0, bottom: 6) }
    }
}

/// Grey bars in the shape of a document while it loads.
struct FeishuDocsSkeleton: View {
    var title: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            if let title {
                Text(title).font(.title.weight(.bold)).fixedSize(horizontal: false, vertical: true)
            } else {
                bar(0.7, height: 26)
            }
            HStack(spacing: 8) { Circle().fill(Color.primary.opacity(0.08)).frame(width: 22, height: 22); bar(0.35, height: 12) }
            ForEach(0..<9, id: \.self) { i in bar([0.95, 0.88, 0.6, 0.92, 0.8, 0.45, 0.9, 0.85, 0.5][i], height: 14) }
            ProgressView().frame(maxWidth: .infinity).padding(.top, 8)
        }
        .redacted(reason: [])
    }

    private func bar(_ fraction: CGFloat, height: CGFloat) -> some View {
        GeometryReader { g in
            RoundedRectangle(cornerRadius: 4).fill(Color.primary.opacity(0.07)).frame(width: g.size.width * fraction)
        }
        .frame(height: height)
    }
}

/// Bases, whiteboards and files: what it is, a preview when there is one, and 在飞书中打开.
struct FeishuDocsUnsupportedView: View {
    var info: FeishuDocsUnsupported
    var url: String
    @State private var request: (url: String, name: String)?
    @State private var gallery: FeishuDocsGallery?

    var body: some View {
        VStack(spacing: 16) {
            if let image = info.image {
                Button { gallery = FeishuDocsGallery(urls: [image], index: 0) } label: {
                    LarkRemoteImage(url: image, maxPixel: 2000, contentMode: .fit)
                        .frame(maxWidth: .infinity, minHeight: 200)
                        .clipShape(.rect(cornerRadius: 12, style: .continuous))
                        .overlay(RoundedRectangle(cornerRadius: 12, style: .continuous).strokeBorder(LarkColor.cardStroke, lineWidth: 0.5))
                }
                .buttonStyle(.plain)
                Text(info.title).font(.headline).multilineTextAlignment(.center)
            } else {
                let file = info.kind == .file ? FeishuDocsFileKind.symbol(ext: info.file?.ext, title: info.title) : nil
                FeishuAppIcon(symbol: file?.symbol ?? info.kind.symbol, hue: file?.hue ?? info.kind.hue, size: 72)
                    .padding(.top, 48)
                Text(info.title).font(.title3.weight(.semibold)).multilineTextAlignment(.center)
                Text(info.message).font(.subheadline).foregroundStyle(.secondary).multilineTextAlignment(.center)
            }
            VStack(spacing: 10) {
                if let f = info.file {
                    Button {
                        request = ("/api/v1/lark/raw/documents/drive/files/\(f.token)/download", info.title)
                    } label: {
                        Label("预览文件", systemImage: "eye").lineLimit(1).frame(minWidth: 180)
                    }
                    .themedButton(prominent: true)
                }
                Button { FeishuDocsLinks.openInFeishu(url) } label: {
                    Label("在飞书中打开", systemImage: "arrow.up.forward.app").lineLimit(1).frame(minWidth: 180)
                }
                .themedButton(prominent: info.file == nil)
            }
            .padding(.top, 8)
        }
        .frame(maxWidth: .infinity)
        .modifier(LarkFileOpener(request: $request))
        .fullScreenCover(item: $gallery) { g in LarkImageGallery(urls: g.urls, index: g.index) }
    }
}

/// The outline: headings indented by level; picking one scrolls the reader there.
struct FeishuDocsOutlineSheet: View {
    var items: [FeishuDocsOutlineItem]
    var pick: (FeishuDocsOutlineItem) -> Void

    var body: some View {
        let top = items.map(\.level).min() ?? 1
        NavigationStack {
            List(items) { item in
                Button { pick(item) } label: {
                    Text(item.title)
                        .font(item.level == top ? .body.weight(.semibold) : .subheadline)
                        .foregroundStyle(item.level == top ? .primary : .secondary)
                        .lineLimit(2)
                        .padding(.leading, CGFloat(min(item.level - top, 4)) * 16)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .contentShape(.rect)
                }
                .buttonStyle(.plain)
            }
            .listStyle(.plain)
            .navigationTitle("目录")
            .navigationBarTitleDisplayMode(.inline)
        }
    }
}

// MARK: - Comments

/// Every comment thread of a document: the quoted text and its replies; open threads first, resolved ones on their own tab.
struct FeishuDocsCommentsView: View {
    var url: String
    var title: String
    @Environment(\.dismiss) private var dismiss
    @Environment(\.feishuOpen) private var open
    @State private var threads: [FeishuDocsComments.Thread]?
    @State private var error: String?
    @State private var resolved = false

    var body: some View {
        let shown = (threads ?? []).filter { $0.resolved == resolved }
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 12) {
                Picker("状态", selection: $resolved) {
                    Text("未解决 \(count(false))").tag(false)
                    Text("已解决 \(count(true))").tag(true)
                }
                .pickerStyle(.segmented)
                .padding(.bottom, 4)
                if threads == nil {
                    FeishuDocsListState(loading: error == nil, error: error, empty: ("text.bubble", "", ""), retry: load)
                } else if shown.isEmpty {
                    EmptyStateView(symbol: "text.bubble", title: resolved ? "没有已解决的评论" : "没有未解决的评论",
                                   message: resolved ? "解决后的评论会收在这里" : "在飞书里对文档发表的评论会出现在这里")
                        .padding(.top, 40)
                } else {
                    ForEach(shown) { FeishuDocsThreadCard(thread: $0) }
                }
            }
            .padding(16)
        }
        .themedPage()
        .navigationTitle("评论")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .topBarLeading) {
                Button { dismiss() } label: { Image(systemName: "xmark") }.accessibilityLabel("关闭")
            }
        }
        .refreshable { await load() }
        .task {
            LarkMedia.shared.loadEmojiCatalog()
            if threads == nil { await load() }
        }
    }

    private func count(_ r: Bool) -> Int { (threads ?? []).filter { $0.resolved == r }.count }

    private func load() async {
        guard let api = AppModel.shared.api else { return }
        do {
            let c: FeishuDocsComments = try await api.feishuDocsGet("documents/comments", query: ["url": url])
            threads = feishuDocsSortedThreads(c.threads)
            error = nil
        } catch {
            if let m = error.feishuDocsMessage { self.error = m }
        }
    }
}

struct FeishuDocsThreadCard: View {
    var thread: FeishuDocsComments.Thread

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 6) {
                if thread.whole {
                    Text("全文评论").font(.caption).foregroundStyle(.secondary)
                } else if !thread.quote.isEmpty {
                    Text(thread.quote)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                        .lineLimit(2)
                        .padding(.leading, 8)
                        .overlay(alignment: .leading) { Capsule().fill(LarkColor.hue("yellow")).frame(width: 3) }
                }
                Spacer(minLength: 0)
                if thread.resolved { FeishuDocsTag(text: "已解决", hue: "green") }
                if thread.visibility != nil { FeishuDocsTag(text: "部分可见", hue: "grey") }
            }
            ForEach(thread.replies) { r in
                HStack(alignment: .top, spacing: 8) {
                    LarkAvatarImage(name: r.author.name, url: r.author.avatarUrl, size: 28)
                    VStack(alignment: .leading, spacing: 3) {
                        HStack(spacing: 6) {
                            Text(r.author.name).font(.footnote.weight(.semibold)).lineLimit(1)
                            Text(FeishuTime.short(r.created)).font(.caption2).foregroundStyle(.tertiary).lineLimit(1)
                            if r.edited == true { Text("已编辑").font(.caption2).foregroundStyle(.tertiary) }
                        }
                        Text(replyText(r)).font(.subheadline).fixedSize(horizontal: false, vertical: true).textSelection(.enabled)
                        if let n = r.imageCount, n > 0 {
                            Label("\(n) 张图片", systemImage: "photo").font(.caption).foregroundStyle(.secondary)
                        }
                        if let rs = r.reactions, !rs.isEmpty {
                            HStack(spacing: 6) {
                                ForEach(rs, id: \.key) { re in
                                    HStack(spacing: 3) {
                                        LarkEmojiImage(key: re.key, size: 16)
                                        Text("\(re.count)").font(.caption2.monospacedDigit()).foregroundStyle(.secondary)
                                    }
                                    .padding(.horizontal, 6).padding(.vertical, 2)
                                    .background(Color.primary.opacity(0.05), in: .capsule)
                                }
                            }
                        }
                    }
                }
            }
        }
        .padding(12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(LarkColor.cardBackground, in: .rect(cornerRadius: 14, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: 14, style: .continuous).strokeBorder(LarkColor.cardStroke, lineWidth: 0.5))
    }

    private func replyText(_ r: FeishuDocsComments.Reply) -> AttributedString {
        var out = AttributedString()
        for seg in r.content {
            var s = AttributedString(seg.text)
            switch seg.kind {
            case "mention": s.foregroundColor = LarkColor.blue
            case "link":
                s.foregroundColor = LarkColor.blue
                if let h = seg.href { s.link = URL(string: h) }
            default: break
            }
            out += s
        }
        return out
    }
}

/// The reader's 在飞书中打开 / 拷贝链接 / 分享 menu items.
struct FeishuDocsLinkMenu: View {
    var url: String

    var body: some View {
        Button { FeishuDocsLinks.openInFeishu(url) } label: { Label("在飞书中打开", systemImage: "arrow.up.forward.app") }
        Button { UIPasteboard.general.string = url } label: { Label("拷贝链接", systemImage: "link") }
        if let u = URL(string: url) { ShareLink(item: u) { Label("分享", systemImage: "square.and.arrow.up") } }
    }
}

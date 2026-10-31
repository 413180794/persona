import BeautifulUI
import MarkdownUI
import SwiftUI

extension MarkdownUI.Theme {
    /// Reading-first markdown: system body text, roomy lists, quiet code blocks.
    /// The base size is body's (17); MarkdownUI scales it with Dynamic Type (a ScaledMetric relative to .body), so a
    /// reply and the user's bubble are one size at every setting. Headings follow `Theme.Font`: the first is the title
    /// level (22), the others the heading level (body size, semibold), told apart by the space above them; code
    /// blocks are the code level (13). Paragraphs have the body's line height (1.45: 0.26 em over SF's own 1.19).
    @MainActor static let jarvis = MarkdownUI.Theme()
        .text { FontSize(17) }
        .code {
            FontFamilyVariant(.monospaced)
            FontSize(.em(0.88))
            BackgroundColor(Theme.codeBackground)
        }
        .strong { FontWeight(.semibold) }
        .link { ForegroundColor(Theme.accent) }
        .heading1 { config in
            config.label.markdownMargin(top: 16, bottom: 8).markdownTextStyle { FontWeight(.bold); FontSize(.em(1.3)) }
        }
        .heading2 { config in
            config.label.markdownMargin(top: 14, bottom: 6).markdownTextStyle { FontWeight(.semibold); FontSize(.em(1)) }
        }
        .heading3 { config in
            config.label.markdownMargin(top: 12, bottom: 4).markdownTextStyle { FontWeight(.semibold); FontSize(.em(1)) }
        }
        .paragraph { config in
            config.label.fixedSize(horizontal: false, vertical: true).relativeLineSpacing(.em(0.26)).markdownMargin(top: 0, bottom: 10)
        }
        .listItem { config in config.label.markdownMargin(top: .em(0.2)) }
        .codeBlock { config in
            ScrollView(.horizontal, showsIndicators: false) {
                config.label
                    .relativeLineSpacing(.em(0.2))
                    .markdownTextStyle { FontFamilyVariant(.monospaced); FontSize(.em(13.0 / 17)) }
                    .padding(Theme.Space.m)
            }
            .background(Theme.codeBackground, in: .rect(cornerRadius: Theme.Radius.control, style: .continuous))
            .markdownMargin(top: 4, bottom: 12)
        }
        .blockquote { config in
            config.label
                .padding(.leading, Theme.Space.m)
                .overlay(alignment: .leading) { Rectangle().fill(Theme.accent.opacity(0.5)).frame(width: 3) }
                .markdownTextStyle { ForegroundColor(.secondary) }
        }
        .table { config in FittedTable(config: config) } // fits the width it gets; scrolls only when it cannot
        .tableCell { config in FittedTableCell(config: config) }
}

/// Offers its content at most `max` points of width (the whole `max` when the parent leaves width open), so text wraps
/// there and narrower content keeps its own size.
struct MaxWidthLayout: Layout {
    var max: CGFloat

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        guard let s = subviews.first else { return .zero }
        return s.sizeThatFits(.init(width: min(proposal.width ?? max, max), height: nil))
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        subviews.first?.place(at: bounds.origin, proposal: .init(width: bounds.width, height: bounds.height))
    }
}

extension EnvironmentValues {
    /// Opens a path mentioned in the conversation on the session's machine (set by ChatView).
    @Entry var openFile: ((String) -> Void)? = nil
}

extension EnvironmentValues {
    /// Every image in the conversation, in order: the viewer pages through all of them (set by ChatView).
    @Entry var chatImages: [ImagePayload] = []
    /// Puts an (edited) image into this chat's composer as an attachment for the next message (set by ChatView).
    @Entry var attachImage: ((UIImage) -> Void)? = nil
    /// Loads an image file on the session's machine (photos sent from the phone are saved there; set by ChatView).
    @Entry var loadImage: ((String) async -> ImagePayload?)? = nil
    /// Fetches the whole text of an item the page cut (`Item.fullSize`), by item id (set by ChatView).
    @Entry var loadFullText: ((String) async throws -> String)? = nil
}

struct UserBubble: View {
    var item: Item
    @Environment(\.openFile) private var openFile
    @Environment(\.loadImage) private var loadImage

    var body: some View {
        let d = RowMemo.user(item.id) { _ in UserRowText(item.text ?? "") } // once per message, keyed by its short id
        let parsed = (refs: d.refs, rest: d.text)
        let shown = d.shown
        let images = d.images
        VStack(alignment: .trailing, spacing: Theme.Space.sm) {
            if !parsed.refs.isEmpty { ContextCards(refs: parsed.refs) }
            if !images.isEmpty, let loadImage {
                AttachedImages(paths: images, load: loadImage)
            }
            if !shown.isEmpty {
                HStack {
                    Spacer(minLength: 48)
                    Text(shown)
                        .font(Theme.Font.body).lineHeight(.body)
                        .foregroundStyle(Theme.userBubbleText)
                        .padding(.horizontal, Theme.Space.l)
                        .padding(.vertical, Theme.Space.m)
                        .background(Theme.userBubbleFill, in: .rect(cornerRadius: Theme.Radius.bubble, style: .continuous))
                        .messageLongPress(id: item.id)
                }
            }
            if let openFile { FileChips(paths: d.files, open: openFile) }
        }
        .frame(maxWidth: .infinity, alignment: .trailing)
        .messageActionBar(id: item.id, text: shown, isUser: true, date: item.ts)
    }
}

extension UserBubble {
    /// `<attached_images>` (current), and the plain-text notes older builds appended (own last line / old wording).
    nonisolated private static let notePatterns = [#"\s*<attached_images>[\s\S]*?</attached_images>\s*"#,
                                       #"(?:\A|\n)\s*相关图片：[^\n]*\z"#, #"\s*（我从手机发来了图片，请用 Read 查看：[^）]*）\s*\z"#]

    /// What was typed: the note the phone appends for attached photos becomes thumbnails above the bubble instead.
    nonisolated private static let displayMemo = TextMemo<String>()
    nonisolated private static let pathsMemo = TextMemo<[String]>()

    nonisolated static func display(_ text: String) -> String { displayMemo(text, computeDisplay) }

    nonisolated private static func computeDisplay(_ text: String) -> String {
        var t = text
        for pattern in notePatterns {
            while let r = t.range(of: pattern, options: .regularExpression) { t.replaceSubrange(r, with: "\n") }
        }
        // Claude's placeholder for an image it attached; the image itself shows as a thumbnail.
        t = t.replacingOccurrences(of: #"\[Image #\d+\]\s*"#, with: "", options: .regularExpression)
        return t.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    /// The photo paths listed in that note.
    nonisolated static func imagePaths(_ text: String) -> [String] { pathsMemo(text, computeImagePaths) }

    nonisolated private static func computeImagePaths(_ text: String) -> [String] {
        for pattern in notePatterns {
            guard let r = text.range(of: pattern, options: .regularExpression) else { continue }
            var note = String(text[r])
            for token in ["<attached_images>", "</attached_images>", "相关图片：", "（我从手机发来了图片，请用 Read 查看：", "）"] {
                note = note.replacingOccurrences(of: token, with: "\n")
            }
            return note.components(separatedBy: CharacterSet(charactersIn: "\n、"))
                .map { $0.trimmingCharacters(in: .whitespaces) }
                .filter { $0.hasPrefix("/") || $0.hasPrefix("~") }
        }
        return []
    }
}

/// A message another Claude session sent this one: on the left like the agent's side, in a quiet card that names
/// the sender — it was not typed by you.
struct PeerMessage: View {
    var item: Item
    var from: String

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.sm) {
            Label("来自 \(from)", systemImage: "arrow.left.arrow.right.circle")
                .font(Theme.Font.labelStrong)
                .foregroundStyle(.secondary)
                .lineLimit(1)
            Text(item.text ?? "")
                .font(Theme.Font.body).lineHeight(.body)
                .foregroundStyle(.primary)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
        .padding(Theme.Space.m)
        .background(Theme.Fill.well, in: .rect(cornerRadius: Theme.Radius.card, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous).stroke(Color.primary.opacity(0.08), lineWidth: 0.5))
        .padding(.trailing, Theme.Space.xxl)
        .messageLongPress(id: item.id)
        .messageActionBar(id: item.id, text: item.text ?? "", isUser: false, date: item.ts)
    }
}

/// A message just sent from this phone, before the transcript has it: drawn like a sent bubble right away, with a
/// quiet clock while the request runs, the clock and 连上后自动发送 while it waits for the network (Outbox.swift),
/// and a red "重发" when it failed.
struct PendingBubble: View {
    var msg: PendingMessage
    var retry: () -> Void
    var discard: () -> Void

    var body: some View {
        VStack(alignment: .trailing, spacing: Theme.Space.sm) {
            if !msg.images.isEmpty {
                HStack(spacing: Theme.Space.sm) {
                    Spacer(minLength: 48)
                    ForEach(Array(msg.images.enumerated()), id: \.offset) { _, p in
                        Theme.Fill.chip
                            .frame(width: msg.images.count == 1 ? 120 : 76, height: msg.images.count == 1 ? 120 : 76)
                            .overlay { ChatThumbnail(payload: p) }
                            .clipShape(.rect(cornerRadius: Theme.Radius.control, style: .continuous))
                    }
                }
            }
            HStack(alignment: .bottom, spacing: Theme.Space.s) {
                Spacer(minLength: 40)
                switch msg.state {
                case .sending: Image(systemName: Theme.Icon.time).font(Theme.Font.label).foregroundStyle(.tertiary)
                case .sent: EmptyView()
                case .waiting:
                    Menu {
                        Button(action: retry) { Label("现在重试", systemImage: Theme.Icon.retry) }
                        Button(role: .destructive, action: discard) { Label("不发了", systemImage: Theme.Icon.delete) }
                    } label: {
                        Image(systemName: Theme.Icon.time).font(Theme.Font.caption).foregroundStyle(.secondary)
                            .frame(width: 24, height: 24)
                            .contentShape(.rect)
                    }
                    .accessibilityLabel("等网络，连上后自动发送")
                case .failed:
                    Menu {
                        Button(action: retry) { Label("重发", systemImage: Theme.Icon.retry) }
                        Button(role: .destructive, action: discard) { Label("删除", systemImage: Theme.Icon.delete) }
                    } label: {
                        Image(systemName: Theme.Icon.errorFilled).font(Theme.Font.body).foregroundStyle(Theme.danger)
                    }
                    .accessibilityLabel("发送失败，重发或删除")
                }
                Text(ContextRef.parse(msg.text).rest)
                    .font(Theme.Font.body).lineHeight(.body)
                    .foregroundStyle(Theme.userBubbleText)
                    .padding(.horizontal, Theme.Space.l)
                    .padding(.vertical, Theme.Space.m)
                    .background(Theme.userBubbleFill, in: .rect(cornerRadius: Theme.Radius.bubble, style: .continuous))
                    .opacity(msg.state == .failed || msg.state == .waiting ? 0.6 : 1)
            }
            if msg.state == .waiting {
                Text("连上后自动发送").font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
                    .transition(.opacity)
            }
        }
        .frame(maxWidth: .infinity, alignment: .trailing)
        .transition(.move(edge: .bottom).combined(with: .opacity))
    }
}

/// Several messages of a finished turn as one input: one bubble, the messages separated by short light rules.
struct MergedUserBubble: View {
    var items: [Item]

    var body: some View {
        let texts = items.map { UserBubble.display(ContextRef.parse($0.text ?? "").rest) }.filter { !$0.isEmpty }
        HStack {
            Spacer(minLength: 48)
            VStack(alignment: .leading, spacing: Theme.Space.s) {
                ForEach(Array(texts.enumerated()), id: \.offset) { n, t in
                    if n > 0 {
                        Capsule().fill(Theme.userBubbleText.opacity(0.22)).frame(width: 36, height: 1.5)
                            .frame(maxWidth: .infinity) // centered in the bubble
                    }
                    Text(t).font(Theme.Font.body).lineHeight(.body).foregroundStyle(Theme.userBubbleText)
                }
            }
            .padding(.horizontal, Theme.Space.l)
            .padding(.vertical, Theme.Space.m)
            .background(Theme.userBubbleFill, in: .rect(cornerRadius: Theme.Radius.bubble, style: .continuous))
            .messageLongPress(id: items.first?.id ?? "")
        }
        .frame(maxWidth: .infinity, alignment: .trailing)
        .messageActionBar(id: items.first?.id ?? "", text: texts.joined(separator: "\n\n"), isUser: true, date: items.first?.ts)
        .accessibilityElement(children: .combine)
        .accessibilityLabel("\(texts.count) 条消息：" + texts.joined(separator: "；"))
    }
}

/// Photos that came with one of your messages (from the transcript): square thumbnails on your side, tap to view.
struct SentPhotos: View {
    @Environment(\.chatImages) private var chatImages
    @Environment(\.attachImage) private var attachImage
    var images: [ImagePayload]
    @State private var viewer: Int?

    var body: some View {
        HStack(spacing: Theme.Space.sm) {
            Spacer(minLength: 48)
            ForEach(Array(images.enumerated()), id: \.offset) { i, p in
                let side: CGFloat = images.count == 1 ? 140 : 84
                Button { viewer = i } label: {
                    Theme.Fill.chip
                        .frame(width: side, height: side)
                        .overlay { ChatThumbnail(payload: p) }
                        .clipShape(.rect(cornerRadius: Theme.Radius.control, style: .continuous))
                        .overlay(RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous).stroke(Color.primary.opacity(0.1), lineWidth: 0.5))
                }
                .buttonStyle(.plain)
                .accessibilityLabel("图片 \(i + 1)，点按查看大图")
            }
        }
        .frame(maxWidth: .infinity, alignment: .trailing)
        .fullScreenCover(isPresented: Binding(get: { viewer != nil }, set: { if !$0 { viewer = nil } })) {
            if let i = viewer { ImageViewer(images: images, index: min(i, images.count - 1), conversation: chatImages, attach: attachImage) }
        }
    }
}

/// Photos sent with a message: a row of square thumbnails above the text; tap one to view them full screen.
struct AttachedImages: View {
    @Environment(\.chatImages) private var chatImages
    @Environment(\.attachImage) private var attachImage
    var paths: [String]
    var load: (String) async -> ImagePayload?
    @State private var loaded: [String: ImagePayload] = [:]
    @State private var viewer: Int?

    var body: some View {
        HStack(spacing: Theme.Space.sm) {
            Spacer(minLength: 48)
            ForEach(Array(paths.enumerated()), id: \.element) { i, path in
                Button { if loaded[path] != nil { viewer = i } } label: { thumb(path) }
                    .buttonStyle(.plain)
                    .accessibilityLabel("图片 \(i + 1)")
            }
        }
        .task(id: paths) {
            for path in paths where loaded[path] == nil {
                var payload = AttachedImageCache.get(path)
                if payload == nil { payload = await load(path) }
                if let payload {
                    AttachedImageCache.put(path, payload)
                    loaded[path] = payload
                }
            }
        }
        .fullScreenCover(isPresented: Binding(get: { viewer != nil }, set: { if !$0 { viewer = nil } })) {
            let images = paths.compactMap { loaded[$0] }
            if let i = viewer, !images.isEmpty { ImageViewer(images: images, index: min(i, images.count - 1), conversation: chatImages, attach: attachImage) }
        }
    }

    private func thumb(_ path: String) -> some View {
        let side: CGFloat = paths.count == 1 ? 120 : 76
        return Theme.Fill.chip
            .frame(width: side, height: side)
            .overlay { // the square sets the size; the photo fills and is cropped to it
                if let p = loaded[path] {
                    ChatThumbnail(payload: p)
                } else {
                    ProgressView().controlSize(.small)
                }
            }
            .clipShape(.rect(cornerRadius: Theme.Radius.control, style: .continuous))
            .overlay(RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous).stroke(Color.primary.opacity(0.1), lineWidth: 0.5))
            .contentShape(.rect)
    }
}

/// Decoded thumbnails survive scrolling a lazy list (rows are rebuilt when they come back on screen).
enum AttachedImageCache {
    private static var payloads: [String: ImagePayload] = [:]

    static func get(_ path: String) -> ImagePayload? { payloads[path] }
    static func put(_ path: String, _ p: ImagePayload) { payloads[path] = p }
}

struct AssistantText: View {
    var item: Item
    @Environment(\.openFile) private var openFile

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.sm) {
            let d = RowMemo.assistant(item.id) { _ in AssistantRowText(item.text ?? "") } // once per message
            Markdown(d.markdown)
                .task(id: item.id) { await Self.preloadLinks(item) }
                .markdownTheme(.jarvis)
                .frame(maxWidth: .infinity, alignment: .leading)
                .contentShape(.rect)
                .messageLongPress(id: item.id)
            if let openFile { FileChips(paths: d.files, open: openFile) }
        }
        .messageActionBar(id: item.id, text: item.text ?? "", isUser: false, date: item.ts)
    }

    /// Links in a fresh answer open instantly: their pages load in the background. Only for recent answers — every
    /// old one scrolling past used to create web views on the main thread, and those were the long freezes.
    private static func preloadLinks(_ item: Item) async {
        guard let ts = item.ts, Date.now.timeIntervalSince(ts) < 15 * 60, let text = item.text else { return }
        let links = await Task.detached(priority: .utility) { WebLinks.find(in: text) }.value // data detection is slow
        if !links.isEmpty, !Task.isCancelled { WebViewPool.shared.preload(links) }
    }
}

struct CopyButton: View {
    var text: String
    var body: some View {
        Button { UIPasteboard.general.string = text; Haptics.success() } label: { Label("拷贝", systemImage: Theme.Icon.copy) }
        ShareLink(item: text) { Label("分享", systemImage: Theme.Icon.share) }
    }
}

/// A thought, folded to its first line; open, the whole text under a guide line. In the BeautifulUI style it is that
/// library's thinking trace.
struct ThinkingRow: View {
    var item: Item
    var compact = false
    @State private var open = false

    var body: some View {
        if AppStyle.current.usesBeautifulUI { beautiful } else { plain }
    }

    private var plain: some View {
        VStack(alignment: .leading, spacing: Theme.Space.sm) {
            Button {
                withAnimation(Theme.Motion.standard) { open.toggle() }
            } label: {
                HStack(spacing: Theme.Space.sm) {
                    Image(systemName: Theme.Icon.memory).font(Theme.Font.label)
                    Text(open ? "思考过程" : ToolPresentation.firstLine(item.text ?? ""))
                        .font(Theme.Font.label)
                        .lineLimit(1)
                    Spacer(minLength: 0)
                    Image(systemName: Theme.Icon.disclosure).font(Theme.Font.label).rotationEffect(.degrees(open ? 90 : 0))
                }
                .foregroundStyle(.secondary)
                .padding(.horizontal, compact ? 10 : 0)
                .padding(.vertical, compact ? 4 : 2)
                .contentShape(.rect)
            }
            .buttonStyle(.plain)
            if open {
                Text(item.text ?? "")
                    .font(Theme.Font.caption)
                    .foregroundStyle(.secondary)
                    .textSelection(.enabled)
                    .padding(.leading, Theme.Space.m)
                    .overlay(alignment: .leading) { Rectangle().fill(Color.secondary.opacity(0.3)).frame(width: 2) }
                    .padding(.horizontal, compact ? 10 : 0)
            }
        }
    }

    private var beautiful: some View {
        let text = item.text ?? ""
        return ThinkingState(
            .reasoning,
            isWorking: false,
            doneTitle: open ? "思考过程" : ToolPresentation.firstLine(text),
            rows: [BeautifulUI.ThinkingRow(id: item.id, text)],
            isExpanded: $open,
            maxWidth: .infinity
        )
        .textSelection(.enabled)
        .padding(.horizontal, compact ? 10 : 0)
    }
}

struct NoticeRow: View {
    var item: Item
    @State private var expanded = false

    var body: some View {
        if item.wake == true { wake } else if item.task != nil { TaskNoteRow(item: item) } else if Self.isShort(item.text ?? "") { plain } else { long }
    }

    /// One short line reads as a centered capsule ("已中断", "上下文已压缩"); anything longer would be a block of centered
    /// gray text, screens tall (a local command's output): it is a left-aligned box of a few lines instead.
    static func isShort(_ s: String) -> Bool { !s.contains("\n") && s.count <= 60 }

    private var long: some View {
        Button {
            withAnimation(Theme.Motion.state) { expanded.toggle() }
        } label: {
            HStack(alignment: .top, spacing: Theme.Space.s) {
                Image(systemName: Theme.Icon.info).font(Theme.Font.label).foregroundStyle(.tertiary)
                Text(item.text ?? "")
                    .font(Theme.Font.label)
                    .foregroundStyle(.secondary)
                    .multilineTextAlignment(.leading)
                    .lineLimit(expanded ? nil : 4)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
            .padding(.horizontal, Theme.Space.m)
            .padding(.vertical, Theme.Space.s)
            .background(Theme.Fill.well, in: .rect(cornerRadius: Theme.Radius.card, style: .continuous))
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .contextMenu { CopyButton(text: item.text ?? "") }
    }

    private var plain: some View {
        Text(item.text ?? "")
            .font(Theme.Font.label)
            .foregroundStyle(.secondary)
            .multilineTextAlignment(.center)
            .padding(.horizontal, Theme.Space.m)
            .padding(.vertical, Theme.Space.sm)
            .background(Theme.Fill.well, in: .capsule)
            .frame(maxWidth: .infinity)
    }

    /// A background task woke the agent: opens the work that follows, like a message would.
    private var wake: some View {
        let w = Self.wakeSummary(item)
        return HStack(spacing: Theme.Space.sm) {
            Image(systemName: w.failed ? Theme.Icon.errorFilled : "bell.fill")
                .foregroundStyle(w.failed ? Theme.danger : Theme.accent)
            Text(w.text).foregroundStyle(.secondary).lineLimit(1).truncationMode(.middle)
        }
        .font(Theme.Font.label)
        .padding(.horizontal, Theme.Space.m)
        .padding(.vertical, Theme.Space.sm)
        .background(Theme.Fill.well, in: .capsule)
        .frame(maxWidth: .infinity)
    }

    /// `Background command "Release" completed (exit code 0)` → "后台命令完成 · Release".
    private static let wakeMemo = TextMemo<(text: String, failed: Bool)>(limit: 500)

    static func wakeSummary(_ s: String) -> (text: String, failed: Bool) { wakeMemo(s, computeWakeSummary) }

    /// The hub's one line when it read the notification (`Item.task`), else worked out from an older hub's summary.
    static func wakeSummary(_ item: Item) -> (text: String, failed: Bool) {
        if let n = item.task { return (item.text ?? n.title, n.failed) }
        return wakeSummary(item.text ?? "")
    }

    private static func computeWakeSummary(_ s: String) -> (text: String, failed: Bool) {
        guard let m = s.firstMatch(of: /^(.*?)\s*"(.+)"\s*(.*)$/) else { return (s, false) }
        let kind = switch String(m.1) {
        case "Background command": "后台命令"
        case "Agent": "子 Agent "
        case "Monitor": "监控"
        default: "后台任务"
        }
        let rest = m.3.lowercased()
        let code = rest.firstMatch(of: /exit code (\d+)/).flatMap { Int($0.1) }
        let failed = rest.contains("fail") || rest.contains("error") || (code ?? 0) != 0
        let state = failed ? "失败" : rest.contains("kill") || rest.contains("stop") ? "已停止"
            : rest.contains("complet") ? "完成" : "有更新"
        return ("\(kind)\(state) · \(m.2)", failed)
    }
}

/// Under an answer: background tasks reported back after it. Short and quiet; tap for what the agent did next.
struct WakeChip: View {
    var notices: [Item]
    var live: Bool
    var open: () -> Void

    var body: some View {
        let summary = notices.count == 1 ? NoticeRow.wakeSummary(notices[0]) : nil
        let failed = notices.contains { NoticeRow.wakeSummary($0).failed }
        Button(action: open) {
            HStack(spacing: Theme.Space.sm) {
                if live {
                    ProgressView().controlSize(.mini)
                } else {
                    Image(systemName: failed ? Theme.Icon.errorFilled : "bell.fill")
                        .foregroundStyle(failed ? Theme.danger : Theme.accent)
                }
                Text(live ? "后台任务完成 · 正在处理" : summary?.text ?? "\(notices.count) 个后台任务完成")
                    .lineLimit(1)
                    .truncationMode(.middle)
                Image(systemName: Theme.Icon.disclosure).font(Theme.Font.labelStrong)
            }
            .font(Theme.Font.caption)
            .foregroundStyle(.secondary)
            .padding(.horizontal, Theme.Space.m)
            .padding(.vertical, Theme.Space.sm)
            .background(Theme.Fill.well, in: .capsule)
            .contentShape(.capsule)
        }
        .buttonStyle(.plain)
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityLabel("后台任务完成，查看之后的消息")
    }
}

/// The drawer behind a WakeChip: which tasks reported back, then what the agent did and said after.
struct WakeDrawer<Content: View>: View {
    @Environment(\.dismiss) private var dismiss
    var notices: [Item]
    @ViewBuilder var content: () -> Content

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: Theme.Space.l) {
                    content()
                }
                .padding(Theme.Space.l)
            }
            .background(Theme.pageBackground)
            .navigationTitle(notices.count > 1 ? "\(notices.count) 个后台任务完成" : "后台任务完成")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .confirmationAction) { Button("完成") { dismiss() } } }
        }
        .presentationDetents([.medium, .large])
        .presentationDragIndicator(.visible)
    }
}

/// Claude Code's "while you were away" recap: like a background task's report, one short line ("离开期间回顾 · 10:02 ›");
/// the recap itself opens in a drawer.
struct RecapCard: View {
    var item: Item
    @State private var open = false

    var body: some View {
        Button {
            Haptics.tap()
            open = true
        } label: {
            HStack(spacing: Theme.Space.sm) {
                Image(systemName: "arrow.uturn.backward.circle.fill").foregroundStyle(Theme.accent)
                Text("离开期间回顾").lineLimit(1)
                if let ts = item.ts, ts.isMeaningful { Text("· " + Fmt.relative(ts)).lineLimit(1) }
                Image(systemName: Theme.Icon.disclosure).font(Theme.Font.labelStrong)
            }
            .font(Theme.Font.caption)
            .foregroundStyle(.secondary)
            .padding(.horizontal, Theme.Space.m)
            .padding(.vertical, Theme.Space.sm)
            .background(Theme.Fill.well, in: .capsule)
            .contentShape(.capsule)
        }
        .buttonStyle(.plain)
        .frame(maxWidth: .infinity, alignment: .leading)
        .contextMenu { CopyButton(text: item.text ?? "") }
        .accessibilityLabel("离开期间回顾，点击查看")
        .sheet(isPresented: $open) { RecapDrawer(item: item) }
        #if DEBUG
        .task { if UserDefaults.standard.bool(forKey: "showRecapDrawer") { try? await Task.sleep(for: .seconds(3)); open = true } }
        #endif
    }
}

/// The newest recap in full, above the composer when you come back to a chat: a tap closes it.
struct RecapStrip: View {
    var item: Item
    var close: () -> Void

    var body: some View {
        Button {
            Haptics.tap()
            close()
        } label: {
            HStack(alignment: .top, spacing: Theme.Space.m) {
                Capsule().fill(Theme.accent).frame(width: 3)
                VStack(alignment: .leading, spacing: Theme.Space.xs) {
                    HStack(spacing: Theme.Space.sm) {
                        Image(systemName: "arrow.uturn.backward.circle.fill").foregroundStyle(Theme.accent)
                        Text("离开期间回顾").font(Theme.Font.captionStrong).lineLimit(1)
                        if let ts = item.ts, ts.isMeaningful {
                            Text("· " + Fmt.relative(ts)).font(Theme.Font.caption).foregroundStyle(.secondary).lineLimit(1)
                        }
                        Spacer(minLength: 4)
                        Image(systemName: Theme.Icon.close).font(Theme.Font.labelHeavy).foregroundStyle(.tertiary)
                    }
                    Text(Fmt.plain(item.text ?? ""))
                        .font(Theme.Font.secondary)
                        .foregroundStyle(.primary)
                        .lineLimit(8)
                        .multilineTextAlignment(.leading)
                        .frame(maxWidth: .infinity, alignment: .leading)
                }
            }
            .fixedSize(horizontal: false, vertical: true) // as tall as the text: the bar would take all the room
            .padding(.horizontal, Theme.Space.m)
            .padding(.vertical, Theme.Space.m)
            .background(Theme.accent.opacity(0.08), in: .rect(cornerRadius: Theme.Radius.card, style: .continuous))
            .contentShape(.rect(cornerRadius: Theme.Radius.card))
        }
        .buttonStyle(.plain)
        .contextMenu { CopyButton(text: item.text ?? "") }
        .accessibilityLabel("离开期间回顾：" + (item.text ?? "") + "。点按关闭")
    }
}

/// The recap in full, in the same kind of drawer as a background task's report.
struct RecapDrawer: View {
    @Environment(\.dismiss) private var dismiss
    var item: Item

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: Theme.Space.m) {
                    if let ts = item.ts, ts.isMeaningful {
                        Text("写于 " + Fmt.relative(ts)).font(Theme.Font.caption).foregroundStyle(.secondary)
                    }
                    Markdown(Links.markdown(item.text ?? ""))
                        .markdownTheme(.jarvis)
                        .textSelection(.enabled)
                        .frame(maxWidth: .infinity, alignment: .leading)
                }
                .padding(Theme.Space.l)
            }
            .background(Theme.pageBackground)
            .navigationTitle("离开期间回顾")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .confirmationAction) { Button("完成") { dismiss() } } }
        }
        .presentationDetents([.medium, .large])
        .presentationDragIndicator(.visible)
    }
}

/// The assistant's answer while it is still being generated, with a blinking caret.
struct StreamingText: View {
    var text: String

    var body: some View {
        Markdown(MarkdownMemo.content(text + " ▍"))
            .markdownTheme(.jarvis)
            .frame(maxWidth: .infinity, alignment: .leading)
    }
}

nonisolated enum WebLinks {
    private static let memo = TextMemo<[URL]>()

    /// The web links in a message, in order (memoized: rows ask on every appearance). Same rule as the links drawn
    /// in the message (`Links`).
    static func find(in text: String) -> [URL] { memo(text) { Links.find(in: $0) } }
}


/// Parsed Markdown per text: MarkdownUI parses (cmark) in `Markdown.init`, on every redraw of the row.
nonisolated enum MarkdownMemo {
    private static let memo = TextMemo<MarkdownContent>(limit: 128)
    static func content(_ text: String) -> MarkdownContent { memo(text) { MarkdownContent(Links.markdown($0)) } }
}

/// What a chat row derives from its message (parsed Markdown, paths, links, the shown text), computed once per
/// message and keyed by the item's short id: rows redraw constantly, and hashing a long text on every redraw
/// was itself measurable. Items never change their text under the same id.
nonisolated enum RowMemo {
    static let assistant = TextMemo<AssistantRowText>(limit: 20_000) // a very long session's worth of messages
    static let user = TextMemo<UserRowText>(limit: 20_000)

    /// Fills both memos off the main thread for messages just loaded, so a row scrolling into view only lays out.
    static func prewarm(_ items: [Item]) {
        for it in items {
            if Task.isCancelled { return }
            if it.kind == .toolUse { ToolDiffMemo.prewarm(it) }
            if it.kind == .toolUse || it.kind == .toolResult { ToolTextMemo.prewarm(it) }
            guard it.kind == .text else { continue }
            guard let text = it.text, !text.isEmpty else { continue }
            if it.role == "assistant" { _ = assistant(it.id) { _ in AssistantRowText(text) } }
            if it.role == "user" { _ = user(it.id) { _ in UserRowText(text) } }
        }
    }
}

nonisolated struct AssistantRowText {
    let markdown: MarkdownContent
    let files: [String]
    init(_ text: String) {
        markdown = MarkdownContent(Links.markdown(text)) // links end where `Links` says, not at the next space
        files = FilePaths.find(in: text)
    }
}

nonisolated struct UserRowText {
    let refs: [ContextRef]
    let text: String   // without the context tags
    let shown: String  // without the image notes
    let images: [String]
    let files: [String]
    init(_ raw: String) {
        let parsed = ContextRef.parse(raw)
        refs = parsed.refs
        text = parsed.rest
        shown = UserBubble.display(parsed.rest)
        let images = UserBubble.imagePaths(parsed.rest)
        self.images = images
        files = FilePaths.find(in: parsed.rest).filter { !images.contains($0) }
    }
}

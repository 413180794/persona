import MarkdownUI
import SwiftUI

extension MarkdownUI.Theme {
    /// Reading-first markdown: system body text, roomy lists, quiet code blocks.
    @MainActor static let jarvis = MarkdownUI.Theme()
        .text { FontSize(16) }
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
            config.label.markdownMargin(top: 14, bottom: 6).markdownTextStyle { FontWeight(.bold); FontSize(.em(1.15)) }
        }
        .heading3 { config in
            config.label.markdownMargin(top: 12, bottom: 4).markdownTextStyle { FontWeight(.semibold); FontSize(.em(1.05)) }
        }
        .paragraph { config in
            config.label.fixedSize(horizontal: false, vertical: true).relativeLineSpacing(.em(0.2)).markdownMargin(top: 0, bottom: 10)
        }
        .listItem { config in config.label.markdownMargin(top: .em(0.2)) }
        .codeBlock { config in
            ScrollView(.horizontal, showsIndicators: false) {
                config.label
                    .relativeLineSpacing(.em(0.2))
                    .markdownTextStyle { FontFamilyVariant(.monospaced); FontSize(.em(0.8)) }
                    .padding(12)
            }
            .background(Theme.codeBackground, in: .rect(cornerRadius: 12, style: .continuous))
            .markdownMargin(top: 4, bottom: 12)
        }
        .blockquote { config in
            config.label
                .padding(.leading, 12)
                .overlay(alignment: .leading) { Rectangle().fill(Theme.accent.opacity(0.5)).frame(width: 3) }
                .markdownTextStyle { ForegroundColor(.secondary) }
        }
        .table { config in
            ScrollView(.horizontal, showsIndicators: false) {
                config.label
                    .markdownTableBorderStyle(.init(color: Color.primary.opacity(0.12)))
                    .markdownTableBackgroundStyle(.alternatingRows(Color.clear, Color.primary.opacity(0.03)))
            }
            .markdownMargin(top: 4, bottom: 12)
        }
        .tableCell { config in
            config.label.markdownTextStyle { FontSize(.em(0.88)) }.padding(.horizontal, 8).padding(.vertical, 5)
        }
}

extension EnvironmentValues {
    /// Opens a path mentioned in the conversation on the session's machine (set by ChatView).
    @Entry var openFile: ((String) -> Void)? = nil
}

extension EnvironmentValues {
    /// Loads an image file on the session's machine (photos sent from the phone are saved there; set by ChatView).
    @Entry var loadImage: ((String) async -> ImagePayload?)? = nil
}

struct UserBubble: View {
    var item: Item
    @Environment(\.openFile) private var openFile
    @Environment(\.loadImage) private var loadImage

    var body: some View {
        let text = item.text ?? ""
        let shown = Self.display(text)
        let images = Self.imagePaths(text)
        VStack(alignment: .trailing, spacing: 6) {
            if !images.isEmpty, let loadImage {
                AttachedImages(paths: images, load: loadImage)
            }
            if !shown.isEmpty {
                HStack {
                    Spacer(minLength: 48)
                    Text(shown)
                        .font(.body)
                        .foregroundStyle(Theme.userBubbleText)
                        .padding(.horizontal, 14)
                        .padding(.vertical, 10)
                        .background(Theme.userBubbleFill, in: .rect(cornerRadius: 20, style: .continuous))
                        .messageLongPress(id: item.id)
                }
            }
            if let openFile { FileChips(paths: FilePaths.find(in: text).filter { !images.contains($0) }, open: openFile) }
        }
        .frame(maxWidth: .infinity, alignment: .trailing)
        .messageActionBar(id: item.id, text: shown, isUser: true)
    }
}

extension UserBubble {
    /// `<attached_images>` (current), and the plain-text notes older builds appended (own last line / old wording).
    private static let notePatterns = [#"\s*<attached_images>[\s\S]*?</attached_images>\s*"#,
                                       #"(?:\A|\n)\s*相关图片：[^\n]*\z"#, #"\s*（我从手机发来了图片，请用 Read 查看：[^）]*）\s*\z"#]

    /// What was typed: the note the phone appends for attached photos becomes thumbnails above the bubble instead.
    static func display(_ text: String) -> String {
        var t = text
        for pattern in notePatterns {
            while let r = t.range(of: pattern, options: .regularExpression) { t.replaceSubrange(r, with: "\n") }
        }
        return t.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    /// The photo paths listed in that note.
    static func imagePaths(_ text: String) -> [String] {
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

/// A message just sent from this phone, before the transcript has it: drawn like a sent bubble right away, with a
/// quiet clock while the request runs and a red "重发" when it failed.
struct PendingBubble: View {
    var msg: PendingMessage
    var retry: () -> Void
    var discard: () -> Void

    var body: some View {
        VStack(alignment: .trailing, spacing: 6) {
            if !msg.images.isEmpty {
                HStack(spacing: 6) {
                    Spacer(minLength: 48)
                    ForEach(Array(msg.images.enumerated()), id: \.offset) { _, p in
                        Color.primary.opacity(0.06)
                            .frame(width: msg.images.count == 1 ? 120 : 76, height: msg.images.count == 1 ? 120 : 76)
                            .overlay { if let img = AttachedImageCache.image(p) { Image(uiImage: img).resizable().scaledToFill() } }
                            .clipShape(.rect(cornerRadius: 12, style: .continuous))
                    }
                }
            }
            HStack(alignment: .bottom, spacing: 8) {
                Spacer(minLength: 40)
                switch msg.state {
                case .sending: Image(systemName: "clock").font(.caption2).foregroundStyle(.tertiary)
                case .sent: EmptyView()
                case .failed:
                    Menu {
                        Button(action: retry) { Label("重发", systemImage: "arrow.clockwise") }
                        Button(role: .destructive, action: discard) { Label("删除", systemImage: "trash") }
                    } label: {
                        Image(systemName: "exclamationmark.circle.fill").font(.body).foregroundStyle(Theme.danger)
                    }
                    .accessibilityLabel("发送失败，重发或删除")
                }
                Text(msg.text)
                    .font(.body)
                    .foregroundStyle(Theme.userBubbleText)
                    .padding(.horizontal, 14)
                    .padding(.vertical, 10)
                    .background(Theme.userBubbleFill, in: .rect(cornerRadius: 20, style: .continuous))
                    .opacity(msg.state == .failed ? 0.6 : 1)
            }
        }
        .frame(maxWidth: .infinity, alignment: .trailing)
        .transition(.move(edge: .bottom).combined(with: .opacity))
    }
}

/// Photos sent with a message: a row of square thumbnails above the text; tap one to view them full screen.
struct AttachedImages: View {
    var paths: [String]
    var load: (String) async -> ImagePayload?
    @State private var loaded: [String: ImagePayload] = [:]
    @State private var viewer: Int?

    var body: some View {
        HStack(spacing: 6) {
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
            if let i = viewer, !images.isEmpty { ImageViewer(images: images, index: min(i, images.count - 1)) }
        }
    }

    private func thumb(_ path: String) -> some View {
        let side: CGFloat = paths.count == 1 ? 120 : 76
        return Color.primary.opacity(0.06)
            .frame(width: side, height: side)
            .overlay { // the square sets the size; the photo fills and is cropped to it
                if let p = loaded[path], let img = AttachedImageCache.image(p) {
                    Image(uiImage: img).resizable().scaledToFill()
                } else {
                    ProgressView().controlSize(.small)
                }
            }
            .clipShape(.rect(cornerRadius: 12, style: .continuous))
            .overlay(RoundedRectangle(cornerRadius: 12, style: .continuous).stroke(Color.primary.opacity(0.1), lineWidth: 0.5))
            .contentShape(.rect)
    }
}

/// Decoded thumbnails survive scrolling a lazy list (rows are rebuilt when they come back on screen).
enum AttachedImageCache {
    private static var payloads: [String: ImagePayload] = [:]
    private static var images: [String: UIImage] = [:]

    static func get(_ path: String) -> ImagePayload? { payloads[path] }
    static func put(_ path: String, _ p: ImagePayload) { payloads[path] = p }
    static func image(_ p: ImagePayload) -> UIImage? {
        let key = String(p.data.prefix(64)) + String(p.data.count)
        if let img = images[key] { return img }
        guard let data = Data(base64Encoded: p.data), let img = UIImage(data: data)?.preparingThumbnail(of: CGSize(width: 360, height: 360)) else { return nil }
        images[key] = img
        return img
    }
}

struct AssistantText: View {
    var item: Item
    @Environment(\.openFile) private var openFile

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Markdown(item.text ?? "")
                .markdownTheme(.jarvis)
                .frame(maxWidth: .infinity, alignment: .leading)
                .contentShape(.rect)
                .messageLongPress(id: item.id)
            if let openFile { FileChips(paths: FilePaths.find(in: item.text ?? ""), open: openFile) }
        }
        .messageActionBar(id: item.id, text: item.text ?? "", isUser: false)
    }
}

struct CopyButton: View {
    var text: String
    var body: some View {
        Button { UIPasteboard.general.string = text; Haptics.success() } label: { Label("拷贝", systemImage: "doc.on.doc") }
        ShareLink(item: text) { Label("分享", systemImage: "square.and.arrow.up") }
    }
}

struct ThinkingRow: View {
    var item: Item
    var compact = false
    @State private var open = false

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Button {
                withAnimation(.snappy) { open.toggle() }
            } label: {
                HStack(spacing: 6) {
                    Image(systemName: "brain").font(.caption)
                    Text(open ? "思考过程" : ToolPresentation.firstLine(item.text ?? ""))
                        .font(.caption)
                        .lineLimit(1)
                    Spacer(minLength: 0)
                    Image(systemName: "chevron.right").font(.caption2).rotationEffect(.degrees(open ? 90 : 0))
                }
                .foregroundStyle(.secondary)
                .padding(.horizontal, compact ? 10 : 0)
                .padding(.vertical, compact ? 4 : 2)
                .contentShape(.rect)
            }
            .buttonStyle(.plain)
            if open {
                Text(item.text ?? "")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                    .textSelection(.enabled)
                    .padding(.leading, 10)
                    .overlay(alignment: .leading) { Rectangle().fill(Color.secondary.opacity(0.3)).frame(width: 2) }
                    .padding(.horizontal, compact ? 10 : 0)
            }
        }
    }
}

struct NoticeRow: View {
    var item: Item
    var body: some View {
        Text(item.text ?? "")
            .font(.caption)
            .foregroundStyle(.secondary)
            .multilineTextAlignment(.center)
            .padding(.horizontal, 12)
            .padding(.vertical, 5)
            .background(Color.primary.opacity(0.05), in: .capsule)
            .frame(maxWidth: .infinity)
    }
}

/// Claude Code's "while you were away" recap: a quiet card, not a centered notice pill.
struct RecapCard: View {
    var item: Item

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 6) {
                Image(systemName: "arrow.uturn.backward.circle.fill")
                Text("离开期间回顾")
                Spacer()
                if let ts = item.ts, ts.isMeaningful { Text(Fmt.relative(ts)).foregroundStyle(.secondary) }
            }
            .font(.caption.weight(.semibold))
            .foregroundStyle(Theme.accent)
            Text(LocalizedStringKey(item.text ?? ""))
                .font(.callout)
                .lineSpacing(3)
                .foregroundStyle(.primary)
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
        .padding(14)
        .background(Theme.accent.opacity(0.07), in: .rect(cornerRadius: 16, style: .continuous))
        .overlay(alignment: .leading) {
            UnevenRoundedRectangle(topLeadingRadius: 16, bottomLeadingRadius: 16, style: .continuous)
                .fill(Theme.accent.opacity(0.6))
                .frame(width: 3)
        }
        .contextMenu { CopyButton(text: item.text ?? "") }
    }
}

/// The assistant's answer while it is still being generated, with a blinking caret.
struct StreamingText: View {
    var text: String

    var body: some View {
        Markdown(text + " ▍")
            .markdownTheme(.jarvis)
            .frame(maxWidth: .infinity, alignment: .leading)
    }
}

import MarkdownUI
import Photos
import SwiftUI

/// A message to share as a picture (分享 in the long-press bar).
struct ShareMessage: Identifiable {
    var text: String
    var isUser: Bool
    var source: String
    var date: Date?
    var agent = "Claude"
    var id: String { text }
}

/// The message laid out as a card: who said it, where, the words, and a quiet Jarvis mark — drawn into an image.
struct MessageShareCard: View {
    var message: ShareMessage
    var dark: Bool

    static let width: CGFloat = 390
    /// Very long answers are cut: a picture of ten screens helps nobody.
    static let maxChars = 3000

    private var text: String {
        let t = message.text.trimmingCharacters(in: .whitespacesAndNewlines)
        return t.count > Self.maxChars ? String(t.prefix(Self.maxChars)) + "\n\n…" : t
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            HStack(spacing: 10) {
                Image(systemName: message.isUser ? "person.fill" : Self.symbol(message.agent))
                    .font(.system(size: 15, weight: .semibold))
                    .foregroundStyle(.white)
                    .frame(width: 32, height: 32)
                    .background(message.isUser ? AnyShapeStyle(Color.gray) : AnyShapeStyle(Self.fill(message.agent)), in: .circle)
                VStack(alignment: .leading, spacing: 2) {
                    Text(message.isUser ? "我" : message.agent).font(.system(size: 15, weight: .semibold))
                    if !message.source.isEmpty {
                        Text(message.source).font(.system(size: 12)).foregroundStyle(.secondary).lineLimit(1)
                    }
                }
                Spacer(minLength: 8)
                if let d = message.date, d.isMeaningful {
                    Text(d.formatted(.dateTime.month().day().hour().minute())).font(.system(size: 12)).foregroundStyle(.secondary)
                }
            }
            Markdown(text)
                .markdownTheme(.card)
                .fixedSize(horizontal: false, vertical: true)
                .frame(maxWidth: .infinity, alignment: .leading)
            HStack(spacing: 6) {
                Image(systemName: "sparkles").font(.system(size: 11, weight: .semibold))
                Text("Jarvis").font(.system(size: 12, weight: .semibold))
                Spacer()
            }
            .foregroundStyle(.tertiary)
            .padding(.top, 2)
        }
        .padding(22)
        .background(dark ? Color(white: 0.11) : .white, in: .rect(cornerRadius: 22, style: .continuous))
        .shadow(color: .black.opacity(dark ? 0.4 : 0.08), radius: 16, y: 6)
        .padding(24)
        .frame(width: Self.width)
        .background(LinearGradient(colors: dark ? [Color(hex: 0x1C2440), Color(hex: 0x2B1D3F)] : [Color(hex: 0xDCE8FF), Color(hex: 0xF1E4FF)],
                                   startPoint: .topLeading, endPoint: .bottomTrailing))
        .environment(\.colorScheme, dark ? .dark : .light)
    }

    private static let claude = LinearGradient(colors: [Color(hex: 0xE0875A), Color(hex: 0xC9643C)], startPoint: .top, endPoint: .bottom)
    private static let codex = LinearGradient(colors: [Color(hex: 0x3A3A3C), Color(hex: 0x111111)], startPoint: .top, endPoint: .bottom)
    private static let traex = LinearGradient(colors: [Color(hex: 0x2FD38F), Color(hex: 0x12935E)], startPoint: .top, endPoint: .bottom)

    /// Each agent its own mark on the card: Claude's sparkle, Codex's code brackets, TRAEX's terminal.
    private static func symbol(_ agent: String) -> String {
        switch agent {
        case "Codex": "chevron.left.forwardslash.chevron.right"
        case "TRAEX": "terminal.fill"
        default: "sparkle"
        }
    }

    private static func fill(_ agent: String) -> LinearGradient {
        switch agent {
        case "Codex": codex
        case "TRAEX": traex
        default: claude
        }
    }

    @MainActor static func render(_ message: ShareMessage, dark: Bool) -> UIImage? {
        let r = ImageRenderer(content: MessageShareCard(message: message, dark: dark))
        r.proposedSize = ProposedViewSize(width: width, height: nil) // measured at the width it is drawn at
        r.scale = 3
        return r.uiImage
    }
}

extension MarkdownUI.Theme {
    /// The chat's theme with code blocks that wrap instead of scrolling (an image can't scroll; the renderer
    /// draws no scroll views at all).
    @MainActor static let card = MarkdownUI.Theme.jarvis
        .text { FontSize(15) }
        .codeBlock { config in
            config.label
                .relativeLineSpacing(.em(0.2))
                .markdownTextStyle { FontFamilyVariant(.monospaced); FontSize(.em(0.8)) }
                .padding(12)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(Theme.codeBackground, in: .rect(cornerRadius: 12, style: .continuous))
                .markdownMargin(top: 4, bottom: 12)
        }
        .table { config in
            config.label
                .markdownTableBorderStyle(.init(color: Color.primary.opacity(0.12)))
                .markdownTableBackgroundStyle(.alternatingRows(Color.clear, Color.primary.opacity(0.03)))
                .markdownMargin(top: 4, bottom: 12)
        }
}

/// Preview of the card, light or dark, then 保存到相册 or 分享 (the image, or the plain text).
struct MessageShareSheet: View {
    @Environment(\.dismiss) private var dismiss
    @Environment(\.colorScheme) private var scheme
    var message: ShareMessage
    @State private var dark: Bool?
    @State private var image: UIImage?
    @State private var saving = false

    private var isDark: Bool { dark ?? (scheme == .dark) }

    var body: some View {
        NavigationStack {
            ScrollView {
                Group {
                    if let image {
                        Image(uiImage: image)
                            .resizable()
                            .scaledToFit()
                            .clipShape(.rect(cornerRadius: 18, style: .continuous))
                            .shadow(color: .black.opacity(0.12), radius: 12, y: 4)
                    } else {
                        ProgressView().frame(maxWidth: .infinity, minHeight: 240)
                    }
                }
                .padding(20)
            }
            .background(Color(.systemGroupedBackground))
            .safeAreaInset(edge: .bottom, spacing: 0) { actions }
            .navigationTitle("分享消息")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("完成") { dismiss() } }
                ToolbarItem(placement: .principal) {
                    Picker("样式", selection: Binding(get: { isDark }, set: { dark = $0 })) {
                        Text("浅色").tag(false)
                        Text("深色").tag(true)
                    }
                    .pickerStyle(.segmented)
                    .frame(width: 140)
                }
            }
            .task(id: isDark) { image = MessageShareCard.render(message, dark: isDark) }
        }
        .presentationDetents([.large])
    }

    private var actions: some View {
        HStack(spacing: 12) {
            Button { Task { await save() } } label: {
                Label(saving ? "正在保存" : "保存图片", systemImage: "square.and.arrow.down")
                    .font(.body.weight(.semibold)).lineLimit(1).frame(maxWidth: .infinity, minHeight: 50)
            }
            .themedButton(prominent: false)
            .disabled(image == nil || saving)
            if let image {
                ShareLink(item: Image(uiImage: image), preview: SharePreview("消息卡片", image: Image(uiImage: image))) {
                    Label("分享图片", systemImage: "square.and.arrow.up")
                        .font(.body.weight(.semibold)).lineLimit(1).frame(maxWidth: .infinity, minHeight: 50)
                }
                .themedButton(prominent: true)
            }
            ShareLink(item: message.text) {
                Image(systemName: "text.alignleft").font(.body.weight(.semibold)).frame(width: 50, height: 50)
            }
            .themedButton(prominent: false)
            .accessibilityLabel("分享文字")
        }
        .padding(.horizontal, 20)
        .padding(.vertical, 12)
        .background(.bar)
    }

    private func save() async {
        guard let image else { return }
        saving = true
        defer { saving = false }
        let status = await PHPhotoLibrary.requestAuthorization(for: .addOnly)
        guard status == .authorized || status == .limited else {
            Haptics.error()
            AppModel.shared.toast = Toast("没有相册权限：请在 设置 › Jarvis › 照片 里允许「仅添加照片」", style: .error)
            return
        }
        do {
            try await PHPhotoLibrary.shared().performChanges { PHAssetChangeRequest.creationRequestForAsset(from: image) }
            Haptics.success()
            AppModel.shared.toast = Toast("已保存到相册", style: .success)
        } catch {
            Haptics.error()
            if !error.isCancellation { AppModel.shared.toast = Toast("保存失败：\(error.localizedDescription)", style: .error) }
        }
    }
}

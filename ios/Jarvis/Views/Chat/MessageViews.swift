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

struct UserBubble: View {
    var item: Item
    @Environment(\.openFile) private var openFile

    var body: some View {
        VStack(alignment: .trailing, spacing: 6) {
        HStack {
            Spacer(minLength: 48)
            Text(Self.display(item.text ?? ""))
                .font(.body)
                .foregroundStyle(.white)
                .padding(.horizontal, 14)
                .padding(.vertical, 10)
                .background(Theme.userBubble.gradient, in: .rect(cornerRadius: 20, style: .continuous))
                .messageLongPress(id: item.id)
        }
            if let openFile { FileChips(paths: FilePaths.find(in: item.text ?? ""), open: openFile) }
        }
        .frame(maxWidth: .infinity, alignment: .trailing)
        .messageActionBar(id: item.id, text: item.text ?? "", isUser: true)
    }
}

extension UserBubble {
    /// The note the phone appends for attached images ("相关图片：…", earlier "（我从手机发来了图片…）") shows as image
    /// chips under the bubble; the bubble keeps just what was typed.
    static func display(_ text: String) -> String {
        var t = text
        for pattern in [#"\n*相关图片：[^\n]*$"#, #"\n*（我从手机发来了图片，请用 Read 查看：[^）]*）\s*$"#] {
            if let r = t.range(of: pattern, options: .regularExpression) { t.removeSubrange(r) }
        }
        return t.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ? text : t
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

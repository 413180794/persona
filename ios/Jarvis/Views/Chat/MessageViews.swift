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
            Text(item.text ?? "")
                .font(.body)
                .foregroundStyle(.white)
                .textSelection(.enabled)
                .padding(.horizontal, 14)
                .padding(.vertical, 10)
                .background(Theme.userBubble.gradient, in: .rect(cornerRadius: 20, style: .continuous))
                .contextMenu { CopyButton(text: item.text ?? "") }
        }
            if let openFile { FileChips(paths: FilePaths.find(in: item.text ?? ""), open: openFile) }
        }
        .frame(maxWidth: .infinity, alignment: .trailing)
    }
}

struct AssistantText: View {
    var item: Item
    @Environment(\.openFile) private var openFile

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Markdown(item.text ?? "")
                .markdownTheme(.jarvis)
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
                .contextMenu { CopyButton(text: item.text ?? "") }
            if let openFile { FileChips(paths: FilePaths.find(in: item.text ?? ""), open: openFile) }
        }
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

/// Live "what is Claude doing right now" strip, fed by the terminal screen sampled every 400ms.
struct LiveActivityBar: View {
    var store: ChatStore
    var status: SessionStatus
    var openTerminal: () -> Void
    var interrupt: () -> Void
    @State private var expanded = false

    var body: some View {
        let lines = store.livePreview
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 10) {
                ShimmerDot()
                Text(store.spinnerLine ?? "Claude 正在工作…")
                    .font(.footnote.weight(.medium))
                    .contentTransition(.numericText())
                    .lineLimit(1)
                Spacer(minLength: 4)
                Button(action: openTerminal) { Image(systemName: "terminal") }
                    .buttonStyle(.plain)
                    .foregroundStyle(.secondary)
                    .accessibilityLabel("打开终端")

            }
            if expanded && !lines.isEmpty {
                Text(lines.joined(separator: "\n"))
                    .font(.system(size: 11, design: .monospaced))
                    .foregroundStyle(.secondary)
                    .lineLimit(8)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .transition(.opacity)
            }
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 10)
        .glassEffect(.regular.tint(Theme.working.opacity(0.08)), in: .rect(cornerRadius: 20, style: .continuous))
        .contentShape(.rect)
        .onTapGesture { withAnimation(.snappy) { expanded.toggle() } }
        .animation(.snappy, value: store.spinnerLine)
    }
}

struct ShimmerDot: View {
    @State private var on = false
    var body: some View {
        ZStack {
            Circle().fill(Theme.working.opacity(0.25)).frame(width: 18, height: 18).scaleEffect(on ? 1.2 : 0.7)
            Image(systemName: "sparkle")
                .font(.system(size: 11, weight: .bold))
                .foregroundStyle(Theme.working)
                .rotationEffect(.degrees(on ? 180 : 0))
        }
        .onAppear { withAnimation(.easeInOut(duration: 1.1).repeatForever(autoreverses: true)) { on = true } }
    }
}

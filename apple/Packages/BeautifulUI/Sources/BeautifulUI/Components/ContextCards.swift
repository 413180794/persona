import SwiftUI

/// A retrieved knowledge chunk and the document it came from.
public struct ContextChunk: Identifiable, Sendable {
    public var id: String
    public var title: String
    /// Size note, e.g. "290 characters".
    public var size: String
    public var body: String
    public var source: String
    /// Short file-type badge, e.g. "PDF".
    public var badge: String
    public var badgeTint: Color
    public var url: URL?

    public init(id: String? = nil, title: String, size: String, body: String, source: String, badge: String, badgeTint: Color, url: URL? = nil) {
        self.id = id ?? title
        self.title = title
        self.size = size
        self.body = body
        self.source = source
        self.badge = badge
        self.badgeTint = badgeTint
        self.url = url
    }
}

/// Retrieved chunks as cards with their source chips. Cards fade up in turn; source chips
/// pop in 0.7 s later.
public struct ContextCards: View {
    var title: String?
    var count: String?
    var chunks: [ContextChunk]
    var maxWidth: CGFloat?
    var bodyLines: Int?
    var onOpenSource: ((ContextChunk) -> Void)?

    @State private var chipsShown = false
    @Environment(\.buiTheme) private var theme
    @Environment(\.openURL) private var openURL

    /// - Parameters:
    ///   - title: `nil` leaves out the heading row (a host showing a quote or two above a message).
    ///   - maxWidth: the original's 380 pt; `nil` fills the host's width.
    ///   - bodyLines: cut each chunk's text after this many lines.
    public init(
        title: String? = "All chunks",
        count: String? = nil,
        chunks: [ContextChunk],
        maxWidth: CGFloat? = 380,
        bodyLines: Int? = nil,
        onOpenSource: ((ContextChunk) -> Void)? = nil
    ) {
        self.title = title
        self.count = count
        self.chunks = chunks
        self.maxWidth = maxWidth
        self.bodyLines = bodyLines
        self.onOpenSource = onOpenSource
    }

    /// The source chips pop in after the cards; with entrances off (a host's lazy list) they are simply there, and no
    /// task starts each time the cards appear.
    private var showsChips: Bool { chipsShown || !theme.entrances }

    public var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            if let title {
            HStack(spacing: 8) {
                Text(title)
                    .buiFont(.title)
                    .foregroundStyle(theme.colors.ink)
                if let count {
                    Text(count)
                        .buiFont(BUITextStyle(size: 11.5, weight: .medium))
                        .monospacedDigit()
                        .foregroundStyle(theme.colors.ink2)
                        .padding(.horizontal, 6)
                        .frame(height: 20)
                        .background(theme.colors.inset, in: .rect(cornerRadius: 6, style: .continuous))
                        .overlay(RoundedRectangle(cornerRadius: 6, style: .continuous).strokeBorder(theme.colors.line, lineWidth: 1))
                }
            }
            .padding(.horizontal, 2)
            .buiEntrance(.fadeIn, duration: 0.4, curve: .easeOut)
            }

            ForEach(Array(chunks.enumerated()), id: \.element.id) { index, chunk in
                card(chunk, index: index)
                    .buiEntrance(.fadeUp, duration: 0.4, delay: Double(index) * 0.1)
            }
        }
        .frame(maxWidth: maxWidth ?? .infinity)
        .modifier(RevealChips(active: theme.entrances, shown: $chipsShown))
    }

    private func card(_ chunk: ContextChunk, index: Int) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 10) {
                HStack(spacing: 6) {
                    BUIIcon(BUIGlyph("M4 6h16M4 12h16M4 18h10"), size: 11, lineWidth: 2.5)
                    Text(chunk.title)
                        .lineLimit(1)
                }
                .buiFont(.label)
                .foregroundStyle(theme.colors.ink)
                Spacer(minLength: 0)
                Text(chunk.size)
                    .buiFont(.caption)
                    .monospacedDigit()
                    .foregroundStyle(theme.colors.ink3)
                    .fixedSize()
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 10)
            .overlay(alignment: .bottom) {
                Rectangle().fill(theme.colors.line).frame(height: 1)
            }
            Text(chunk.body)
                .buiFont(BUITextStyle(size: 12.5, lineHeight: 1.625))
                .foregroundStyle(theme.colors.ink2)
                .lineLimit(bodyLines)
                .fixedSize(horizontal: false, vertical: true)
                .padding(.horizontal, 12)
                .padding(.top, 8)
                .padding(.bottom, 4)
            Button {
                if let onOpenSource { onOpenSource(chunk) } else if let url = chunk.url { openURL(url) }
            } label: {
                HStack(spacing: 6) {
                    // 7 pt bold, allowed to overflow the 14 pt tile like the original.
                    RoundedRectangle(cornerRadius: 4, style: .continuous)
                        .fill(chunk.badgeTint)
                        .frame(width: 14, height: 14)
                        .overlay {
                            Text(chunk.badge)
                                .font(.system(size: 7, weight: .bold))
                                .foregroundStyle(.white)
                                .fixedSize()
                        }
                    Text(chunk.source)
                        .buiFont(BUITextStyle(size: 12, weight: .medium))
                        .lineLimit(1)
                    BUIIcon(BUIGlyph("M7 17L17 7M7 7h10v10"), size: 9, lineWidth: 2.5)
                }
                .foregroundStyle(theme.colors.ink2)
                .padding(.horizontal, 8)
                .frame(height: 24)
                .buiSurface(Capsule(), fill: theme.colors.inset, elevation: .button)
            }
            .buttonStyle(.plain)
            .opacity(showsChips ? 1 : 0)
            .scaleEffect(showsChips ? 1 : 0.95)
            .animation(BUICurve.outStrong.animation(duration: 0.3).delay(Double(index) * 0.08), value: showsChips)
            .padding(.horizontal, 12)
            .padding(.bottom, 12)
            .accessibilityLabel("Open \(chunk.source)")
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .buiSurface(radius: theme.radii.card, elevation: .card)
    }
}

/// Pops the source chips in 0.7 s after the cards appear, only while entrances are on.
private struct RevealChips: ViewModifier {
    var active: Bool
    @Binding var shown: Bool

    func body(content: Content) -> some View {
        if active {
            content.task {
                try? await Task.sleep(for: .milliseconds(700))
                shown = true
            }
        } else {
            content
        }
    }
}

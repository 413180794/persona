import SwiftUI

/// A cited source: shown as an inline chip, in the avatar stack and in the sources list.
public struct StreamingSource: Identifiable, Sendable {
    public var id: String { domain }
    public var name: String
    public var domain: String
    public var url: URL?
    public var avatar: BUIAvatar

    public init(name: String, domain: String, url: URL? = nil, avatar: BUIAvatar) {
        self.name = name
        self.domain = domain
        self.url = url
        self.avatar = avatar
    }
}

/// One piece of a streamed answer.
public enum StreamingToken: Hashable, Sendable {
    /// Text, including its own spacing (a word plus its trailing space, or a whole chunk).
    case text(String)
    /// An inline citation chip for `sources[index]`.
    case citation(Int)

    /// Splits prose into space-terminated words, the granularity the demo streams at.
    public static func words(_ prose: String) -> [StreamingToken] {
        prose.split(separator: " ", omittingEmptySubsequences: true).map { .text($0 + " ") }
    }
}

/// The feedback actions under a finished answer.
public enum StreamingAction: String, CaseIterable, Sendable {
    case copy, retry, like, dislike

    var glyph: BUIGlyph {
        switch self {
        case .copy: .copy
        case .retry: .retry
        case .like: .thumbUp
        case .dislike: .thumbDown
        }
    }

    var label: String {
        switch self {
        case .copy: "Copy"
        case .retry: "Retry"
        case .like: "Good response"
        case .dislike: "Bad response"
        }
    }
}

/// A streamed answer: text arrives token by token behind a caret, citations appear as
/// inline source chips, and when streaming ends the feedback actions, the sources toggle and
/// follow-up prompts fade in.
///
/// The paragraph is one `Text`, so it wraps any script (CJK included) and stays cheap to
/// re-render on every token; chips and the caret are drawn images inside it.
public struct StreamingText: View {
    var tokens: [StreamingToken]
    var isStreaming: Bool
    var sources: [StreamingSource]
    var sourcesLabel: String?
    var followUps: [String]
    var followUpsTitle: String
    var minimumHeight: CGFloat
    var onAction: ((StreamingAction) -> Void)?
    var onFollowUp: ((String, Int) -> Void)?
    var externalSourcesOpen: Binding<Bool>?

    @State private var internalSourcesOpen = false
    @Environment(\.buiTheme) private var theme
    @Environment(\.openURL) private var openURL
    @Environment(\.self) private var environment
    @ScaledMetric(relativeTo: .footnote) private var scale: CGFloat = 1

    public init(
        tokens: [StreamingToken],
        isStreaming: Bool,
        sources: [StreamingSource] = [],
        sourcesLabel: String? = nil,
        followUps: [String] = [],
        followUpsTitle: String = "Follow-ups",
        minimumHeight: CGFloat = 0,
        sourcesExpanded: Binding<Bool>? = nil,
        onAction: ((StreamingAction) -> Void)? = nil,
        onFollowUp: ((String, Int) -> Void)? = nil
    ) {
        self.externalSourcesOpen = sourcesExpanded
        self.tokens = tokens
        self.isStreaming = isStreaming
        self.sources = sources
        self.sourcesLabel = sourcesLabel
        self.followUps = followUps
        self.followUpsTitle = followUpsTitle
        self.minimumHeight = minimumHeight
        self.onAction = onAction
        self.onFollowUp = onFollowUp
    }

    /// Consecutive text tokens merged, so the paragraph is a short chain of runs.
    nonisolated static func runs(_ tokens: [StreamingToken]) -> [StreamingToken] {
        var out: [StreamingToken] = []
        for token in tokens {
            if case .text(let s) = token, case .text(let previous)? = out.last {
                out[out.count - 1] = .text(previous + s)
            } else {
                out.append(token)
            }
        }
        return out
    }

    private var done: Bool { !isStreaming }

    private var sourcesOpen: Bool {
        get { externalSourcesOpen?.wrappedValue ?? internalSourcesOpen }
        nonmutating set {
            if let externalSourcesOpen { externalSourcesOpen.wrappedValue = newValue } else { internalSourcesOpen = newValue }
        }
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            paragraph
            actions
                .padding(.top, 8)
                .opacity(done ? 1 : 0)
                .allowsHitTesting(done)
            sourcesList
            followUpList
                .padding(.top, 10)
                .opacity(done ? 1 : 0)
                .allowsHitTesting(done)
        }
        .frame(maxWidth: .infinity, minHeight: minimumHeight, alignment: .topLeading)
        .animation(.easeInOut(duration: 0.4), value: done)
    }

    // MARK: Paragraph

    private var paragraph: some View {
        let style = BUITextStyle(size: 13, lineHeight: 1.625)
        var text = Text("")
        for run in Self.runs(tokens) {
            switch run {
            case .text(let s):
                text = Text("\(text)\(s)")
            case .citation(let index):
                if sources.indices.contains(index) {
                    text = Text("\(text)\(chip(index))")
                }
            }
        }
        if isStreaming {
            text = Text("\(text)\(caret)")
        }
        return text
            .buiFont(style)
            .foregroundStyle(theme.colors.ink)
            .textRenderer(SourceChipRenderer(
                sources: sources,
                fill: Color(theme.colors.inset.resolve(in: environment)),
                ring: Color(theme.colors.line.resolve(in: environment)),
                scale: scale
            ))
            .frame(maxWidth: .infinity, alignment: .leading)
            .accessibilityLabel(Text(Self.plainText(tokens, sources: sources)))
    }

    nonisolated static func plainText(_ tokens: [StreamingToken], sources: [StreamingSource]) -> String {
        tokens.map { token in
            switch token {
            case .text(let s): s
            case .citation(let i): sources.indices.contains(i) ? "(\(sources[i].domain)) " : ""
            }
        }
        .joined()
    }

    /// `ml-0.5 h-3 w-0.5 translate-y-0.5 rounded-full bg-ink`
    private var caret: Text {
        let ink = theme.colors.ink
        let s = scale
        let image = Image(size: CGSize(width: 4 * s, height: 12 * s)) { gc in
            gc.fill(Path(roundedRect: CGRect(x: 2 * s, y: 0, width: 2 * s, height: 12 * s), cornerRadius: s), with: .color(ink))
        }
        return Text(image).baselineOffset(-2 * s)
    }

    /// The chip's domain as a real mono text run; `SourceChipRenderer` draws the chip's
    /// fill, ring and avatar behind it, so the line keeps the paragraph's own height.
    /// No-break spaces (one mono advance each) reserve the avatar slot and the trailing
    /// margin and keep the chip on one line.
    private func chip(_ index: Int) -> Text {
        Text("\u{00A0}\u{00A0}\u{00A0}\(sources[index].domain)\u{00A0}")
            .font(.system(size: 10.5 * scale, design: .monospaced))
            .tracking(0)
            .foregroundStyle(theme.colors.ink2)
            .customAttribute(SourceChipAttribute(index: index))
    }

    // MARK: Actions and sources

    private var actions: some View {
        HStack(spacing: 2) {
            ForEach(StreamingAction.allCases, id: \.self) { action in
                Button {
                    onAction?(action)
                } label: {
                    BUIIcon(action.glyph, size: 15, lineWidth: 1.8)
                        .foregroundStyle(theme.colors.ink3)
                        .frame(width: 24, height: 24)
                        .contentShape(.rect(cornerRadius: 6))
                }
                .buttonStyle(PressHighlightStyle(radius: 6))
                .accessibilityLabel(action.label)
            }
            if !sources.isEmpty {
                Button {
                    withAnimation(BUICurve.outStrong.animation(duration: 0.3)) { sourcesOpen.toggle() }
                } label: {
                    HStack(spacing: 6) {
                        HStack(spacing: -4) {
                            ForEach(sources) { source in
                                source.avatar.view(size: 14)
                                    .clipShape(Circle())
                                    .background(Circle().fill(theme.colors.surface))
                                    .overlay(Circle().stroke(theme.colors.canvas, lineWidth: 1.5).padding(-0.75))
                            }
                        }
                        Text(sourcesLabel ?? "\(sources.count) sources")
                            .buiFont(.caption)
                            .foregroundStyle(theme.colors.ink2)
                    }
                    .padding(.horizontal, 4)
                    .padding(.vertical, 2)
                    .contentShape(.rect(cornerRadius: 6))
                }
                .buttonStyle(PressHighlightStyle(radius: 6, color: theme.colors.hover))
                .padding(.leading, 6)
                .accessibilityValue(sourcesOpen ? "Expanded" : "Collapsed")
            }
        }
    }

    private var sourcesList: some View {
        let open = done && sourcesOpen
        return VStack(spacing: 0) {
            ForEach(sources) { source in
                Button {
                    if let url = source.url { openURL(url) }
                } label: {
                    HStack(spacing: 8) {
                        source.avatar.view(size: 16)
                            .clipShape(.rect(cornerRadius: 4))
                        Text(source.name)
                            .buiFont(.caption)
                            .foregroundStyle(theme.colors.ink2)
                        Spacer(minLength: 8)
                        Text(source.domain)
                            .buiFont(BUITextStyle(size: 10.5, design: .monospaced))
                            .foregroundStyle(theme.colors.ink3)
                    }
                    .padding(.horizontal, 6)
                    .padding(.vertical, 4)
                    .contentShape(.rect(cornerRadius: 6))
                }
                .buttonStyle(PressHighlightStyle(radius: 6, color: theme.colors.hover))
                .accessibilityAddTraits(.isLink)
            }
        }
        .padding(4)
        .background(theme.colors.inset, in: .rect(cornerRadius: theme.radii.card))
        .overlay(RoundedRectangle(cornerRadius: theme.radii.card).strokeBorder(theme.colors.line, lineWidth: 1))
        .padding(.top, 6)
        .frame(height: open ? nil : 0, alignment: .top)
        .opacity(open ? 1 : 0)
        .clipped()
    }

    private var followUpList: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(followUpsTitle)
                .buiFont(BUITextStyle(size: 12, weight: .medium))
                .foregroundStyle(theme.colors.ink2)
            VStack(spacing: 0) {
                ForEach(Array(followUps.enumerated()), id: \.element) { index, prompt in
                    Button {
                        onFollowUp?(prompt, index)
                    } label: {
                        HStack(spacing: 8) {
                            BUIIcon(.reply, size: 11)
                                .foregroundStyle(theme.colors.ink3)
                            Text(prompt)
                                .buiFont(BUITextStyle(size: 12.5))
                                .foregroundStyle(theme.colors.ink)
                                .multilineTextAlignment(.leading)
                            Spacer(minLength: 0)
                        }
                        .padding(.horizontal, 6)
                        .padding(.vertical, 6)
                        .overlay(alignment: .bottom) {
                            Rectangle().fill(theme.colors.line).frame(height: 1)
                        }
                        .contentShape(.rect(cornerRadius: 7))
                    }
                    .buttonStyle(PressHighlightStyle(radius: 7))
                    .padding(.horizontal, -6)
                    .modifier(FollowUpReveal(shown: done, delay: Double(index) * 0.09))
                }
            }
        }
    }
}

struct SourceChipAttribute: TextAttribute {
    var index: Int
}

/// Draws `h-4.5 rounded-[5px] bg-inset shadow-hairline` chips with a 12 pt avatar behind
/// runs tagged with `SourceChipAttribute`. Colors arrive resolved: a renderer's context
/// doesn't carry the view's color scheme into dynamic colors.
struct SourceChipRenderer: TextRenderer {
    var sources: [StreamingSource]
    var fill: Color
    var ring: Color
    var scale: CGFloat

    func draw(layout: Text.Layout, in ctx: inout GraphicsContext) {
        for line in layout {
            // A chip's text can split into several runs (font fallback); merge consecutive ones.
            var group: (index: Int, rect: CGRect, baseline: CGFloat)?
            for run in line {
                let chip = run[SourceChipAttribute.self]
                if let g = group, chip?.index != g.index {
                    drawChip(sources[g.index], rect: g.rect, baseline: g.baseline, in: ctx)
                    group = nil
                }
                if let chip, sources.indices.contains(chip.index) {
                    let bounds = run.typographicBounds
                    group = group.map { ($0.index, $0.rect.union(bounds.rect), $0.baseline) }
                        ?? (chip.index, bounds.rect, bounds.origin.y)
                }
            }
            if let g = group { drawChip(sources[g.index], rect: g.rect, baseline: g.baseline, in: ctx) }
            for run in line { ctx.draw(run) }
        }
    }

    private func drawChip(_ source: StreamingSource, rect run: CGRect, baseline: CGFloat, in ctx: GraphicsContext) {
        let s = scale
        let advance = run.width / CGFloat(source.domain.count + 4)
        // The trailing space is 3 pt of padding plus the chip's right margin.
        let width = run.width - (advance - 3 * s)
        let height = 18 * s
        // CSS align-middle on the body x-height, nudged up 1 px.
        let midY = baseline - 4.5 * s
        let rect = CGRect(x: run.minX, y: midY - height / 2, width: width, height: height)
        let body = Path(roundedRect: rect.insetBy(dx: 0.5, dy: 0.5), cornerRadius: 5 * s)
        ctx.fill(body, with: .color(fill))
        ctx.stroke(body, with: .color(ring), lineWidth: 1)
        source.avatar.draw(in: ctx, rect: CGRect(x: run.minX + 3 * s, y: midY - 6 * s, width: 12 * s, height: 12 * s), radius: 3 * s)
    }
}

/// Follow-ups fade up each time an answer finishes (staggered 90 ms) and hide while streaming.
private struct FollowUpReveal: ViewModifier {
    var shown: Bool
    var delay: Double
    @State private var visible = false

    func body(content: Content) -> some View {
        content
            .opacity(visible ? 1 : 0)
            .offset(y: visible ? 0 : 8)
            .onChange(of: shown, initial: true) { _, now in
                if now {
                    withAnimation(BUICurve.outStrong.animation(duration: 0.35).delay(delay)) { visible = true }
                } else {
                    visible = false
                }
            }
    }
}

import SwiftUI

/// One option the agent can recommend.
public struct Recommendation: Identifiable, Sendable {
    public enum Confidence: Sendable {
        /// Three green bars.
        case high
        /// Two orange bars.
        case review
        /// No bars.
        case none

        var bars: Int {
            switch self {
            case .high: 3
            case .review: 2
            case .none: 0
            }
        }

        func tint(_ c: BUIColors) -> Color {
            switch self {
            case .high: c.green
            case .review: c.orange
            case .none: c.ink3
            }
        }
    }

    public var id: String
    public var body: [BUIInlineSpan]
    /// One-line summary shown in the alternatives list.
    public var summary: String
    public var confidence: Confidence
    public var confidenceLabel: String
    public var actionTitle: String
    /// `accent` for a confident one-tap accept, `primary` otherwise.
    public var actionKind: BUIButtonStyle.Kind

    public init(
        id: String,
        body: [BUIInlineSpan],
        summary: String,
        confidence: Confidence,
        confidenceLabel: String,
        actionTitle: String,
        actionKind: BUIButtonStyle.Kind = .primary
    ) {
        self.id = id
        self.body = body
        self.summary = summary
        self.confidence = confidence
        self.confidenceLabel = confidenceLabel
        self.actionTitle = actionTitle
        self.actionKind = actionKind
    }
}

/// An agent suggestion with a confidence meter. "Alternatives" opens a drawer of the other
/// options; picking one promotes it, and the primary action confirms.
public struct RecommendationCard: View {
    var title: String
    var options: [Recommendation]
    var alternativesLabel: String
    var otherOptionsLabel: String
    var acceptedLabel: String
    var onAccept: ((Recommendation) -> Void)?

    @State private var selected: Int
    @State private var drawerOpen: Bool
    @State private var accepted = false
    @Environment(\.buiTheme) private var theme

    public init(
        title: String,
        options: [Recommendation],
        selected: Int = 0,
        showsAlternatives: Bool = false,
        alternativesLabel: String = "Alternatives",
        otherOptionsLabel: String = "Other options",
        acceptedLabel: String = "Accepted",
        onAccept: ((Recommendation) -> Void)? = nil
    ) {
        self.title = title
        self.options = options
        self.alternativesLabel = alternativesLabel
        self.otherOptionsLabel = otherOptionsLabel
        self.acceptedLabel = acceptedLabel
        self.onAccept = onAccept
        _selected = State(initialValue: selected)
        _drawerOpen = State(initialValue: showsAlternatives)
    }

    public var body: some View {
        let active = options[selected]
        VStack(alignment: .leading, spacing: 0) {
            VStack(alignment: .leading, spacing: 6) {
                Text(title)
                    .buiFont(BUITextStyle(size: 14, weight: .medium))
                    .foregroundStyle(theme.colors.ink)
                BUIInlineText(active.body)
                    .frame(maxWidth: .infinity, minHeight: 48, alignment: .topLeading)
                    .id(active.id)
                    .transition(.opacity.animation(.easeOut(duration: 0.18)))
            }
            .padding(12)

            drawer(excluding: selected)
                .frame(height: drawerOpen ? nil : 0, alignment: .top)
                .opacity(drawerOpen ? 1 : 0)
                .clipped()

            HStack(spacing: 12) {
                HStack(spacing: 8) {
                    ConfidenceMeter(confidence: active.confidence)
                    Text(active.confidenceLabel)
                        .buiFont(BUITextStyle(size: 12.5, weight: .medium))
                        .foregroundStyle(theme.colors.ink2)
                }
                Spacer(minLength: 0)
                HStack(spacing: 8) {
                    Button(alternativesLabel) {
                        withAnimation(BUICurve.link.animation(duration: 0.3)) { drawerOpen.toggle() }
                    }
                    .buttonStyle(.bui(.secondary, fontSize: 12.5, horizontalPadding: 10, isActive: drawerOpen))
                    .accessibilityValue(drawerOpen ? "Expanded" : "Collapsed")
                    Button(accepted ? acceptedLabel : active.actionTitle) {
                        withAnimation(BUIMotion.standard) { accepted = true }
                        onAccept?(active)
                    }
                    .buttonStyle(.bui(accepted ? .success : active.actionKind, fontSize: 12.5))
                }
                .padding(.trailing, -2)
            }
            .padding(10)
        }
        .frame(maxWidth: 380)
        .buiSurface(radius: theme.radii.card, elevation: .card)
    }

    private func drawer(excluding index: Int) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            Text(otherOptionsLabel)
                .buiFont(BUITextStyle(size: 11, weight: .medium))
                .foregroundStyle(theme.colors.ink3)
                .padding(.horizontal, 6)
                .padding(.bottom, 4)
            ForEach(Array(options.enumerated()).filter { $0.offset != index }, id: \.element.id) { i, option in
                Button {
                    withAnimation(BUIMotion.standard) {
                        selected = i
                        accepted = false
                    }
                } label: {
                    HStack(spacing: 10) {
                        ConfidenceMeter(confidence: option.confidence)
                        Text(option.summary)
                            .buiFont(BUITextStyle(size: 12.5))
                            .foregroundStyle(theme.colors.ink)
                            .lineLimit(1)
                            .frame(maxWidth: .infinity, alignment: .leading)
                        Text(option.confidenceLabel)
                            .buiFont(BUITextStyle(size: 11))
                            .foregroundStyle(theme.colors.ink3)
                    }
                    .padding(6)
                    .contentShape(.rect(cornerRadius: theme.radii.control))
                }
                .buttonStyle(PressHighlightStyle(radius: theme.radii.control, color: theme.colors.hover))
            }
        }
        .padding(8)
        .frame(maxWidth: .infinity, alignment: .leading)
        .overlay(alignment: .top) {
            Rectangle().fill(theme.colors.line).frame(height: 1)
        }
    }
}

/// Three 4 × 10 pt bars, filled up to the confidence level.
struct ConfidenceMeter: View {
    var confidence: Recommendation.Confidence
    @Environment(\.buiTheme) private var theme

    var body: some View {
        HStack(alignment: .bottom, spacing: 2) {
            ForEach(0..<3, id: \.self) { bar in
                Capsule()
                    .fill(bar < confidence.bars ? confidence.tint(theme.colors) : theme.colors.lineStrong)
                    .frame(width: 4, height: 10)
            }
        }
        .animation(.easeInOut(duration: 0.3), value: confidence.bars)
        .accessibilityHidden(true)
    }
}

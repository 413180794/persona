import SwiftUI

/// A 3×3 pixel-grid loader. Drive and Dots run a chevron wavefront to the right
/// (the 650 ms cycle is shorter than the sweep, so two fronts are always in flight);
/// Orbit laps a comet around the perimeter. Reduce Motion freezes it dim.
public struct PixelLoader: View {
    public enum Pattern: String, CaseIterable, Sendable, Identifiable {
        case drive, dots, orbit
        public var id: Self { self }

        /// Per-cell `animation-delay` in seconds, row-major; `nil` cells never light.
        var delays: [Double?] {
            switch self {
            case .drive, .dots: Self.chevronDelays
            case .orbit: Self.orbitDelays
            }
        }

        private static let chevronDelays: [Double?] = (0..<9).map { (i: Int) -> Double? in
            let step = i % 3 + abs(i / 3 - 1)
            return Double(step) * 0.09
        }

        private static let orbitDelays: [Double?] = (0..<9).map { (i: Int) -> Double? in
            guard let k = orbitOrder.firstIndex(of: i) else { return nil }
            return Double(k) * 0.11
        }

        var duration: Double { self == .orbit ? 0.95 : 0.65 }
        var round: Bool { self == .dots }
        static let orbitOrder = [0, 1, 2, 5, 8, 7, 6, 3]
    }

    /// `pixel-on`: dim, flash on, hold, fade back.
    nonisolated static let pixelOn = BUIKeyframes([(0, 0.15), (0.18, 1), (0.42, 1), (0.62, 0.15), (1, 0.15)], curve: .easeInOut)

    /// Opacity of cell `index` `elapsed` seconds after the loader appeared.
    nonisolated static func opacity(_ pattern: Pattern, cell index: Int, elapsed: Double) -> Double {
        guard let delay = pattern.delays[index] else { return 0.07 }
        return pixelOn.value(elapsed: elapsed, duration: pattern.duration, delay: delay, rest: 0.15)
    }

    var pattern: Pattern
    var tint: Color?
    @Environment(\.buiTheme) private var theme
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var start = Date()

    public init(_ pattern: Pattern = .drive, tint: Color? = nil) {
        self.pattern = pattern
        self.tint = tint
    }

    private static let cell: CGFloat = 4
    private static let gap: CGFloat = 1.5
    private static let side = cell * 3 + gap * 2

    public var body: some View {
        let color = tint ?? theme.colors.ink
        TimelineView(.animation(minimumInterval: 1 / 60, paused: reduceMotion)) { context in
            let elapsed = reduceMotion ? -1 : context.date.timeIntervalSince(start)
            Canvas { gc, _ in
                for index in 0..<9 {
                    let rect = CGRect(
                        x: CGFloat(index % 3) * (Self.cell + Self.gap),
                        y: CGFloat(index / 3) * (Self.cell + Self.gap),
                        width: Self.cell,
                        height: Self.cell
                    )
                    let path = pattern.round
                        ? Path(ellipseIn: rect)
                        : Path(roundedRect: rect, cornerRadius: 1)
                    gc.fill(path, with: .color(color.opacity(Self.opacity(pattern, cell: index, elapsed: elapsed))))
                }
            }
        }
        .frame(width: Self.side, height: Self.side)
        .accessibilityHidden(true)
    }
}

/// Status line for long-running work: a pixel loader, a shimmering label and a live
/// elapsed timer in tabular mono figures. Pass `accessory` to pin a preview card under
/// the line (the original's "Surfer" variant shows a video there).
public struct LoadingState<Accessory: View>: View {
    var label: String
    var pattern: PixelLoader.Pattern
    var startDate: Date?
    var accessory: Accessory?
    @State private var appeared = Date()
    @State private var accessoryShown = false
    @Environment(\.buiTheme) private var theme

    public init(_ label: String = "Churning", pattern: PixelLoader.Pattern = .drive, since startDate: Date? = nil)
    where Accessory == EmptyView {
        self.label = label
        self.pattern = pattern
        self.startDate = startDate
        self.accessory = nil
    }

    public init(
        _ label: String,
        pattern: PixelLoader.Pattern = .drive,
        since startDate: Date? = nil,
        @ViewBuilder accessory: () -> Accessory
    ) {
        self.label = label
        self.pattern = pattern
        self.startDate = startDate
        self.accessory = accessory()
    }

    public var body: some View {
        let origin = startDate ?? appeared
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 10) {
                PixelLoader(pattern)
                Text(label)
                    .buiFont(.label)
                    .buiShimmer()
                TimelineView(.periodic(from: origin, by: 0.1)) { context in
                    Text(LoadingState<EmptyView>.formatElapsed(context.date.timeIntervalSince(origin)))
                        .buiFont(.mono)
                        .monospacedDigit()
                        .foregroundStyle(theme.colors.ink3)
                }
            }
            .accessibilityElement(children: .ignore)
            .accessibilityLabel(label)
            .accessibilityAddTraits(.updatesFrequently)

            if let accessory {
                // w-56, aspect-video
                accessory
                    .frame(width: 224, height: 126)
                    .buiSurface(radius: theme.radii.card, fill: theme.colors.tooltipBackground, elevation: .overlay)
                    .scaleEffect(accessoryShown ? 1 : 0.95, anchor: .topLeading)
                    .opacity(accessoryShown ? 1 : 0)
                    .onAppear {
                        withAnimation(BUICurve.link.animation(duration: 0.2)) { accessoryShown = true }
                    }
            }
        }
        .fixedSize()
    }
}

public extension LoadingState where Accessory == EmptyView {
    /// `12.3s` under a minute, `2m 5.0s` after.
    nonisolated static func formatElapsed(_ seconds: TimeInterval) -> String {
        let tenths = Int((max(0, seconds) * 10).rounded(.down))
        let total = Double(tenths) / 10
        if total < 60 { return String(format: "%.1fs", total) }
        let minutes = tenths / 600
        let rest = Double(tenths % 600) / 10
        return String(format: "%dm %.1fs", minutes, rest)
    }
}

/// The placeholder the original shows in its preview card when the video can't load.
public struct LoadingPreviewPlaceholder: View {
    var message: String
    @Environment(\.buiTheme) private var theme

    public init(_ message: String = "Video unavailable") {
        self.message = message
    }

    public var body: some View {
        VStack(spacing: 6) {
            PixelLoader(.drive, tint: theme.colors.tooltipForeground)
            Text(message)
                .buiFont(BUITextStyle(size: 10, design: .monospaced))
                .foregroundStyle(theme.colors.tooltipMuted)
                .padding(.horizontal, 12)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}

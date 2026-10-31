import SwiftUI

/// One page of the insights carousel: a sentence, a mini visualisation and a follow-up prompt.
public struct InsightPage: Identifiable, Sendable {
    public enum Card: Sendable {
        /// Two series compared over time, with their latest values as headline deltas.
        case comparison([InsightSeries])
        /// A metric that broke its threshold, switchable between two measures.
        case anomaly(InsightAnomaly)
        /// A segmented share-of-total bar.
        case allocation(title: String, monogram: String, segments: [InsightSegment], note: String)
    }

    public var id: String
    public var prose: [BUIInlineSpan]
    public var card: Card
    public var prompt: String

    public init(id: String, prose: [BUIInlineSpan], card: Card, prompt: String) {
        self.id = id
        self.prose = prose
        self.card = card
        self.prompt = prompt
    }
}

public struct InsightSeries: Identifiable, Sendable {
    public var id: String { name }
    public var name: String
    public var values: [Double]
    /// Mono line under the headline, e.g. "-$2,377.66".
    public var detail: String
    public var color: Color

    public init(name: String, values: [Double], detail: String, color: Color) {
        self.name = name
        self.values = values
        self.detail = detail
        self.color = color
    }
}

public struct InsightAnomaly: Sendable {
    public var title: String
    public var primaryLabel: String
    public var primary: [Double]
    public var primaryThreshold: String
    public var secondaryLabel: String
    public var secondary: [Double]
    public var secondaryThreshold: String
    public var headline: String
    public var delta: String
    public var comparison: String
    public var format: @Sendable (Double, Bool) -> String

    public init(
        title: String,
        primaryLabel: String, primary: [Double], primaryThreshold: String,
        secondaryLabel: String, secondary: [Double], secondaryThreshold: String,
        headline: String, delta: String, comparison: String,
        format: @escaping @Sendable (Double, Bool) -> String
    ) {
        self.title = title
        self.primaryLabel = primaryLabel
        self.primary = primary
        self.primaryThreshold = primaryThreshold
        self.secondaryLabel = secondaryLabel
        self.secondary = secondary
        self.secondaryThreshold = secondaryThreshold
        self.headline = headline
        self.delta = delta
        self.comparison = comparison
        self.format = format
    }
}

public struct InsightSegment: Identifiable, Sendable {
    public var id: String { code }
    public var code: String
    public var label: String
    public var percent: Double
    public var amount: String
    public var color: Color

    public init(code: String, label: String, percent: Double, amount: String, color: Color) {
        self.code = code
        self.label = label
        self.percent = percent
        self.amount = amount
        self.color = color
    }
}

/// Paged agent insights with scrub-ready charts: "Insights N ‹ ›", a sentence, a card and a
/// follow-up prompt. Drag across a chart to read values.
public struct InsightCards: View {
    var title: String
    var pages: [InsightPage]
    var onPrompt: ((String) -> Void)?

    @State private var page: Int
    @Environment(\.buiTheme) private var theme

    public init(title: String = "Insights", pages: [InsightPage], page: Int = 0, onPrompt: ((String) -> Void)? = nil) {
        self.title = title
        self.pages = pages
        self.onPrompt = onPrompt
        _page = State(initialValue: page)
    }

    /// Catmull-Rom resampling: `perSegment` points between each pair, ending on the last value.
    nonisolated static func smooth(_ values: [Double], perSegment: Int = 9) -> [Double] {
        guard values.count >= 3 else { return values }
        var out: [Double] = []
        let n = values.count
        for i in 0..<(n - 1) {
            let p0 = values[max(0, i - 1)], p1 = values[i], p2 = values[i + 1], p3 = values[min(n - 1, i + 2)]
            for s in 0..<perSegment {
                let t = Double(s) / Double(perSegment), t2 = t * t, t3 = t2 * t
                out.append(0.5 * (2 * p1 + (-p0 + p2) * t + (2 * p0 - 5 * p1 + 4 * p2 - p3) * t2 + (-p0 + 3 * p1 - 3 * p2 + p3) * t3))
            }
        }
        out.append(values[n - 1])
        return out
    }

    nonisolated static func percent(_ v: Double) -> String {
        String(format: "%@%.2f%%", v > 0 ? "+" : "", v)
    }

    public var body: some View {
        let current = pages[page]
        VStack(alignment: .leading, spacing: 0) {
            HStack {
                HStack(alignment: .firstTextBaseline, spacing: 6) {
                    Text(title).buiFont(.title).foregroundStyle(theme.colors.ink)
                    Text("\(pages.count)").buiFont(.body).monospacedDigit().foregroundStyle(theme.colors.ink3)
                }
                Spacer()
                HStack(spacing: 2) {
                    pagerButton("M15 18l-6-6 6-6", label: "Previous insight", step: -1)
                    pagerButton("M9 6l6 6-6 6", label: "Next insight", step: 1)
                }
            }
            VStack(alignment: .leading, spacing: 8) {
                BUIInlineText(current.prose, style: BUITextStyle(size: 12.5, lineHeight: 1.625))
                    .padding(.top, 6)
                Group {
                    switch current.card {
                    case .comparison(let series): ComparisonCard(series: series)
                    case .anomaly(let anomaly): AnomalyCard(anomaly: anomaly)
                    case .allocation(let title, let monogram, let segments, let note):
                        AllocationCard(title: title, monogram: monogram, segments: segments, note: note)
                    }
                }
                Button(current.prompt) { onPrompt?(current.prompt) }
                    .buttonStyle(PromptPillStyle())
            }
            .id(current.id)
            .transition(.opacity.combined(with: .blurReplace))
        }
        .frame(maxWidth: 344)
        .frame(minHeight: 408, alignment: .top)
    }

    private func pagerButton(_ d: String, label: String, step: Int) -> some View {
        Button {
            withAnimation(BUICurve.link.animation(duration: 0.25)) {
                page = (page + step + pages.count) % pages.count
            }
        } label: {
            BUIIcon(BUIGlyph(d), size: 13, lineWidth: 2.2)
        }
        .buttonStyle(IconButtonStyle(size: 24))
        .accessibilityLabel(label)
    }
}

private struct PromptPillStyle: ButtonStyle {
    @Environment(\.buiTheme) private var theme

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .buiFont(BUITextStyle(size: 12, lineHeight: 1.19))
            .foregroundStyle(theme.colors.ink)
            .padding(.horizontal, 12)
            .padding(.vertical, 6)
            .buiSurface(Capsule(), fill: configuration.isPressed ? theme.colors.hover : nil, elevation: .button)
    }
}

/// `rounded-card bg-surface p-3 shadow-hairline`, at least 278 pt tall.
private struct InsightFrame<Content: View>: View {
    @ViewBuilder var content: Content
    @Environment(\.buiTheme) private var theme

    var body: some View {
        VStack(alignment: .leading, spacing: 0) { content }
            .padding(12)
            .frame(maxWidth: .infinity, minHeight: 278, alignment: .topLeading)
            .buiSurface(radius: theme.radii.card, elevation: .hairline)
    }
}

/// The inset chart panel with a header row.
private struct ChartPanel<Header: View, Chart: View>: View {
    @ViewBuilder var header: Header
    @ViewBuilder var chart: Chart
    @Environment(\.buiTheme) private var theme

    var body: some View {
        VStack(spacing: 0) {
            HStack { header }
                .padding(.horizontal, 10)
                .padding(.vertical, 6)
                .overlay(alignment: .bottom) { Rectangle().fill(theme.colors.line).frame(height: 1) }
            chart.frame(height: 166)
        }
        .background(theme.colors.inset)
        .clipShape(.rect(cornerRadius: theme.radii.control, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: theme.radii.control, style: .continuous).strokeBorder(theme.colors.line, lineWidth: 1))
    }
}

private struct SnapshotBadge: View {
    @Environment(\.buiTheme) private var theme
    var body: some View {
        Text("Snapshot")
            .buiFont(BUITextStyle(size: 10.5, weight: .medium, lineHeight: 1.19))
            .foregroundStyle(theme.colors.ink2)
            .padding(.horizontal, 8)
            .padding(.vertical, 2)
            .background(theme.colors.field, in: Capsule())
    }
}

/// Scrub cursor plus the dark tooltip, clamped to the middle 28–72 %.
private struct ScrubOverlay: View {
    var fraction: CGFloat
    var rows: [(String, Color)]
    @Environment(\.buiTheme) private var theme

    var body: some View {
        GeometryReader { proxy in
            let x = proxy.size.width * fraction
            Rectangle().fill(theme.colors.ink.opacity(0.26)).frame(width: 1).offset(x: x)
            HStack(spacing: 12) {
                ForEach(rows.indices, id: \.self) { i in
                    HStack(spacing: 6) {
                        Circle().fill(rows[i].1).frame(width: 8, height: 8)
                        Text(rows[i].0)
                    }
                }
            }
            .buiFont(BUITextStyle(size: 11.5, lineHeight: 1.19))
            .monospacedDigit()
            .foregroundStyle(theme.colors.tooltipForeground)
            .padding(.horizontal, 9)
            .padding(.vertical, 5)
            .background(theme.colors.tooltipBackground, in: .rect(cornerRadius: 8))
            .overlay(RoundedRectangle(cornerRadius: 8).strokeBorder(theme.colors.lineStrong, lineWidth: 1))
            .fixedSize()
            .position(x: proxy.size.width * min(max(fraction, 0.28), 0.72), y: 22)
        }
        .allowsHitTesting(false)
    }
}

/// A drag that reports the index under the finger and clears on release.
@MainActor private func scrub(count: Int, width: CGFloat, index: Binding<Int?>) -> some Gesture {
    DragGesture(minimumDistance: 0)
        .onChanged { value in
            let p = min(max(value.location.x / max(width, 1), 0), 1)
            index.wrappedValue = Int((p * CGFloat(count - 1)).rounded())
        }
        .onEnded { _ in index.wrappedValue = nil }
}

private struct ComparisonCard: View {
    var series: [InsightSeries]
    @State private var hover: Int?
    @State private var width: CGFloat = 0
    @Environment(\.buiTheme) private var theme

    var body: some View {
        let smoothed = series.map { InsightCards.smooth($0.values) }
        let count = smoothed.first?.count ?? 0
        InsightFrame {
            HStack(spacing: 16) {
                ForEach(Array(series.enumerated()), id: \.element.id) { i, s in
                    let last = smoothed[i].last ?? 0
                    VStack(alignment: .leading, spacing: 0) {
                        HStack(spacing: 6) {
                            Circle().fill(s.color).frame(width: 8, height: 8)
                            Text(s.name)
                        }
                        .buiFont(BUITextStyle(size: 11.5, lineHeight: 1.19))
                        .foregroundStyle(theme.colors.ink2)
                        Text(InsightCards.percent(last))
                            .buiFont(BUITextStyle(size: 17, weight: .semibold, lineHeight: 1.4))
                            .monospacedDigit()
                            .foregroundStyle(last < 0 ? theme.colors.red : theme.colors.green)
                        Text(s.detail)
                            .buiFont(BUITextStyle(size: 11.5, design: .monospaced, lineHeight: 1.4))
                            .foregroundStyle(last < 0 ? theme.colors.red : theme.colors.green)
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                }
            }
            ChartPanel {
                Text("Trend snapshot").buiFont(BUITextStyle(size: 11, lineHeight: 1.19)).foregroundStyle(theme.colors.ink3)
                Spacer()
                SnapshotBadge()
            } chart: {
                ZStack(alignment: .topLeading) {
                    HStack(spacing: 4) {
                        ForEach(series) { s in Circle().fill(s.color).frame(width: 6, height: 6) }
                    }
                    .padding(.horizontal, 6)
                    .padding(.vertical, 4)
                    .background(theme.colors.field, in: .rect(cornerRadius: 5))
                    .padding(.leading, 4)
                    .padding(.top, 6)
                    LineChart(series: smoothed, colors: series.map(\.color), paddingTop: 40, paddingBottom: 22, fadesIn: true, dashedLevel: true)
                    if let hover, count > 1 {
                        ScrubOverlay(
                            fraction: CGFloat(hover) / CGFloat(count - 1),
                            rows: series.indices.map { (InsightCards.percent(smoothed[$0][hover]), series[$0].color) }
                        )
                    }
                }
                .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { width = $0 }
                .contentShape(.rect)
                .gesture(scrub(count: count, width: width, index: $hover))
            }
            .padding(.top, 8)
        }
    }
}

private struct AnomalyCard: View {
    var anomaly: InsightAnomaly
    @State private var primary = true
    @State private var hover: Int?
    @State private var width: CGFloat = 0
    @Environment(\.buiTheme) private var theme

    var body: some View {
        let values = primary ? anomaly.primary : anomaly.secondary
        let red = Color(light: 0xEE5C61, dark: 0xEE5C61)
        InsightFrame {
            HStack {
                HStack(spacing: 6) {
                    BUIIcon(.arrowUp, size: 12, lineWidth: 2.5).foregroundStyle(theme.colors.red)
                    Text(anomaly.title)
                }
                .buiFont(BUITextStyle(size: 12, weight: .medium, lineHeight: 1.19))
                .foregroundStyle(theme.colors.ink)
                Spacer()
                SnapshotBadge()
            }
            ChartPanel {
                Text(hover.map { anomaly.format(values[$0], primary) } ?? "\(primary ? anomaly.primaryThreshold : anomaly.secondaryThreshold) threshold")
                    .buiFont(BUITextStyle(size: 11, lineHeight: 1.19))
                    .monospacedDigit()
                    .foregroundStyle(theme.colors.ink3)
                Spacer()
                HStack(spacing: 0) {
                    ForEach([true, false], id: \.self) { isPrimary in
                        let on = primary == isPrimary
                        Button(isPrimary ? anomaly.primaryLabel : anomaly.secondaryLabel) {
                            withAnimation(BUIMotion.standard) { primary = isPrimary }
                        }
                        .buttonStyle(.plain)
                        .buiFont(BUITextStyle(size: 10.5, weight: .medium, lineHeight: 1.19))
                        .foregroundStyle(on ? theme.colors.ink : theme.colors.ink3)
                        .padding(.horizontal, 8)
                        .padding(.vertical, 2)
                        .background {
                            if on {
                                Capsule().fill(theme.colors.surface)
                                    .overlay(Capsule().strokeBorder(BUIElevation.button.ring(theme.colors) ?? .clear, lineWidth: 1))
                            }
                        }
                    }
                }
                .padding(2)
                .background(theme.colors.field, in: Capsule())
            } chart: {
                ZStack {
                    LineChart(series: [InsightCards.smooth(values)], colors: [red], paddingTop: 34, paddingBottom: 22, grid: true, dashedLevel: true, timeAxis: true, rangePadding: 0.04)
                    if let hover, values.count > 1 {
                        ScrubOverlay(fraction: CGFloat(hover) / CGFloat(values.count - 1), rows: [(anomaly.format(values[hover], primary), red)])
                    }
                }
                .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { width = $0 }
                .contentShape(.rect)
                .gesture(scrub(count: values.count, width: width, index: $hover))
            }
            .padding(.top, 8)
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                Text(anomaly.headline)
                    .buiFont(BUITextStyle(size: 17, weight: .semibold, lineHeight: 1.4))
                    .monospacedDigit()
                    .foregroundStyle(theme.colors.ink)
                Text(anomaly.delta)
                    .buiFont(BUITextStyle(size: 11.5, design: .monospaced, lineHeight: 1.4))
                    .foregroundStyle(theme.colors.red)
                Text(anomaly.comparison)
                    .buiFont(BUITextStyle(size: 11, lineHeight: 1.4))
                    .foregroundStyle(theme.colors.ink3)
            }
            .padding(.top, 6)
        }
    }
}

private struct AllocationCard: View {
    var title: String
    var monogram: String
    var segments: [InsightSegment]
    var note: String
    @State private var selected: String?
    @Environment(\.buiTheme) private var theme

    var body: some View {
        let active = segments.first { $0.code == (selected ?? segments.first?.code) } ?? segments[0]
        InsightFrame {
            HStack(spacing: 6) {
                Text(monogram)
                    .font(.system(size: 8, weight: .bold))
                    .foregroundStyle(.white)
                    .frame(width: 14, height: 14)
                    .background(segments.first?.color ?? theme.colors.orange, in: Circle())
                Text(title)
            }
            .buiFont(BUITextStyle(size: 12, weight: .medium, lineHeight: 1.19))
            .foregroundStyle(theme.colors.ink)
            Text(active.amount)
                .buiFont(BUITextStyle(size: 20, weight: .semibold, lineHeight: 1.4))
                .monospacedDigit()
                .foregroundStyle(theme.colors.ink)
                .contentTransition(.numericText())
                .padding(.top, 4)
            GeometryReader { proxy in
                let inner = proxy.size.width - 4 - CGFloat(segments.count - 1) * 2
                HStack(spacing: 2) {
                    ForEach(segments) { s in
                        let on = s.code == active.code
                        Button {
                            withAnimation(BUICurve.link.animation(duration: 0.3)) { selected = s.code }
                        } label: {
                            Capsule()
                                .fill(s.color)
                                .overlay(alignment: .leading) {
                                    Capsule()
                                        .fill(Color.white.opacity(0.2))
                                        .padding(4)
                                        .frame(maxWidth: on ? .infinity : 0, alignment: .leading)
                                        .opacity(on ? 1 : 0)
                                        .animation(BUICurve.link.animation(duration: 0.5), value: on)
                                }
                                .overlay(Capsule().strokeBorder(Color.white.opacity(on ? 0.22 : 0), lineWidth: 1))
                                .opacity(on ? 1 : 0.58)
                                .frame(width: max(0, inner * s.percent / 100))
                        }
                        .buttonStyle(ShrinkPressStyle())
                        .accessibilityLabel("\(s.label): \(s.percent)%")
                    }
                }
                .padding(2)
            }
            .frame(height: 36)
            .background(theme.colors.field, in: Capsule())
            .padding(.top, 12)
            HStack(spacing: 6) {
                ForEach(segments) { s in
                    let on = s.code == active.code
                    Button {
                        withAnimation(BUICurve.link.animation(duration: 0.3)) { selected = s.code }
                    } label: {
                        HStack(spacing: 4) {
                            Circle().fill(s.color).frame(width: 6, height: 6)
                            Text("\(s.code) \(s.percent.formatted())%")
                        }
                        .buiFont(BUITextStyle(size: 11, lineHeight: 1.19))
                        .monospacedDigit()
                        .foregroundStyle(on ? theme.colors.ink : theme.colors.ink2)
                        .padding(.horizontal, 6)
                        .padding(.vertical, 2)
                        .background(on ? theme.colors.field : .clear, in: Capsule())
                    }
                    .buttonStyle(ShrinkPressStyle())
                }
            }
            .padding(.top, 8)
            VStack(alignment: .leading, spacing: 4) {
                Text(active.label)
                    .buiFont(BUITextStyle(size: 11.5, weight: .medium, lineHeight: 1.19))
                    .foregroundStyle(active.code == segments.first?.code ? (segments.first?.color ?? theme.colors.ink) : theme.colors.ink2)
                Text(note)
                    .buiFont(BUITextStyle(size: 11, lineHeight: 1.625))
                    .foregroundStyle(theme.colors.ink3)
                    .fixedSize(horizontal: false, vertical: true)
            }
            .padding(.horizontal, 10)
            .padding(.vertical, 8)
            .frame(maxWidth: .infinity, minHeight: 64, alignment: .topLeading)
            .background(theme.colors.inset, in: .rect(cornerRadius: theme.radii.control))
            .overlay(RoundedRectangle(cornerRadius: theme.radii.control).strokeBorder(theme.colors.line, lineWidth: 1))
            .padding(.top, 12)
        }
    }
}

private struct ShrinkPressStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .scaleEffect(configuration.isPressed ? 0.97 : 1)
            .animation(.easeOut(duration: 0.15), value: configuration.isPressed)
    }
}

/// Lines with end dots, an optional dashed line at each series' latest value, a dotted grid,
/// a left-edge fade and a time axis (the original uses the Liveline library).
private struct LineChart: View {
    var series: [[Double]]
    var colors: [Color]
    var paddingTop: CGFloat
    var paddingBottom: CGFloat
    var fadesIn = false
    var grid = false
    var dashedLevel = false
    var timeAxis = false
    var rangePadding: Double = 0.12
    @Environment(\.buiTheme) private var theme

    var body: some View {
        Canvas { gc, size in
            let all = series.flatMap { $0 }
            guard let minV = all.min(), let maxV = all.max(), maxV > minV else { return }
            // Breathing room above and below the data, as the original chart library leaves.
            let pad = (maxV - minV) * rangePadding
            let lo = minV - pad, hi = maxV + pad
            let plot = CGRect(x: 12, y: paddingTop, width: size.width - 24, height: size.height - paddingTop - paddingBottom)
            func point(_ i: Int, _ v: Double, _ count: Int) -> CGPoint {
                CGPoint(x: plot.minX + plot.width * CGFloat(i) / CGFloat(max(count - 1, 1)), y: plot.maxY - plot.height * CGFloat((v - lo) / (hi - lo)))
            }
            if grid {
                for row in 0...3 {
                    let y = plot.minY + plot.height * CGFloat(row) / 3
                    var line = Path()
                    line.move(to: CGPoint(x: 0, y: y))
                    line.addLine(to: CGPoint(x: size.width, y: y))
                    gc.stroke(line, with: .color(theme.colors.line), style: StrokeStyle(lineWidth: 1, dash: [1, 3]))
                }
            }
            for (values, color) in zip(series, colors) {
                guard let last = values.last else { continue }
                if dashedLevel {
                    let y = point(values.count - 1, last, values.count).y
                    var level = Path()
                    level.move(to: CGPoint(x: 0, y: y))
                    level.addLine(to: CGPoint(x: size.width, y: y))
                    gc.stroke(level, with: .color(color.opacity(0.5)), style: StrokeStyle(lineWidth: 1, dash: [4, 4]))
                }
                var path = Path()
                for (i, v) in values.enumerated() {
                    let p = point(i, v, values.count)
                    if i == 0 { path.move(to: p) } else { path.addLine(to: p) }
                }
                let shading: GraphicsContext.Shading = fadesIn
                    ? .linearGradient(Gradient(colors: [color.opacity(0), color, color]), startPoint: CGPoint(x: 0, y: 0), endPoint: CGPoint(x: size.width, y: 0))
                    : .color(color)
                gc.stroke(path, with: shading, style: StrokeStyle(lineWidth: 2.25, lineCap: .round, lineJoin: .round))
                let end = point(values.count - 1, last, values.count)
                gc.fill(Path(ellipseIn: CGRect(x: end.x - 4, y: end.y - 4, width: 8, height: 8)), with: .color(color))
            }
            if timeAxis {
                let now = Date()
                let formatter = DateFormatter()
                formatter.dateFormat = "HH:mm:ss"
                for k in 0..<5 {
                    let x = plot.minX + plot.width * CGFloat(k) / 4
                    let label = formatter.string(from: now.addingTimeInterval(Double(k - 4) * 10))
                    gc.draw(
                        Text(label).font(.system(size: 11, design: .monospaced)).foregroundStyle(theme.colors.ink3.opacity(k == 0 || k == 4 ? 0.5 : 1)),
                        at: CGPoint(x: x, y: size.height - 10)
                    )
                }
            }
        }
    }
}

import Charts
import SwiftUI

/// Colors of the machine charts (docs/machines.md「图」): one hue per measure, the same on the dashboard and the
/// detail page; light and dark are separate steps checked against their surfaces, not one color flipped.
enum MetricColor {
    static let cpu = Color(light: 0x2A78D6, dark: 0x3987E5)
    static let mem = Color(light: 0x1BAF7A, dark: 0x199E70)
    static let rx = Color(light: 0xEB6834, dark: 0xD95926)
    static let tx = Color(light: 0x6250D6, dark: 0x9085E9)
    static let load = Color(light: 0xE87BA4, dark: 0xD55181)
    static let warning = Color(light: 0xFAB219, dark: 0xFAB219)
    static let critical = Color(light: 0xD03B3B, dark: 0xD03B3B)
    static let grid = Color(light: 0xE1E0D9, dark: 0x2C2C2A)

    static func level(_ l: MetricLevel) -> Color {
        switch l {
        case .normal: .secondary
        case .warning: warning
        case .critical: critical
        }
    }
}

/// One plotted value. `series` is the line it belongs to: a measure split at every gap, so no line crosses a time
/// the machine sent nothing.
struct ChartValue: Identifiable {
    var id: String { "\(series)@\(date.timeIntervalSince1970)" }
    var series: String
    var name: String
    var date: Date
    var value: Double
}

/// What a chart draws: a name (the legend) and how to read it from a point.
struct ChartSeries {
    var name: String
    var color: Color
    var value: (MetricPoint) -> Double?
}

extension Array where Element == MetricPoint {
    /// The series' values, each run between gaps its own line.
    func chartValues(_ s: ChartSeries) -> [ChartValue] {
        var out: [ChartValue] = []
        var segment = 0
        for p in self {
            if p.isGap { segment += 1; continue }
            if let v = s.value(p) { out.append(ChartValue(series: "\(s.name)#\(segment)", name: s.name, date: p.date, value: v)) }
        }
        return out
    }

    /// The point nearest to `date`, within two steps (a scrub over a gap finds nothing).
    func nearest(_ date: Date, step: TimeInterval) -> MetricPoint? {
        let real = filter { !$0.isGap }
        guard let p = real.min(by: { abs($0.date.timeIntervalSince(date)) < abs($1.date.timeIntervalSince(date)) }),
              abs(p.date.timeIntervalSince(date)) <= step * 2 else { return nil }
        return p
    }
}

/// A tiny line with its wash, no axes: the dashboard's cards.
///
/// Drawn on a canvas, not as a Swift Charts chart: the Machines page shows a dozen of these, and each chart's own
/// layout cost about 130 ms whenever the page came up on the Mac (#390; vectorized plots were no cheaper, the cost is
/// per chart, not per point). The look is the chart's it replaced: each series a monotone line (one run per stretch
/// between gaps), over a wash down to 0 when there is one series; x over `domain`, y from 0 to the larger of `yMax`
/// and the largest value; nothing drawn past the edges.
struct Sparkline: View {
    var points: [MetricPoint]
    var series: [ChartSeries]
    var domain: ClosedRange<Date>
    var yMax: Double?
    /// Read so that a change of appearance while the page is up draws the canvas again in the new colors (the
    /// renderer resolves them, but nothing in it depends on the appearance).
    @Environment(\.colorScheme) private var colorScheme

    var body: some View {
        let _ = colorScheme
        let lines = series.map { (color: $0.color, values: points.chartValues($0)) }
        let top = max(yMax ?? 0, lines.flatMap { $0.values.map(\.value) }.max() ?? 0, 1e-9)
        let wash = series.count == 1
        Canvas { context, size in
            for line in lines {
                let runs = SparklineGeometry.runs(line.values, domain: domain, top: top, size: size)
                let stroke = StrokeStyle(lineWidth: 1.5, lineCap: .round, lineJoin: .round)
                if wash {
                    let paths = runs.map { (points: $0, path: SparklineGeometry.line($0)) }
                    for run in paths {
                        context.fill(SparklineGeometry.wash(run.points, line: run.path, bottom: size.height),
                                     with: .color(line.color.opacity(0.12)))
                    }
                    for run in paths { context.stroke(run.path, with: .color(line.color), style: stroke) }
                } else {
                    for run in runs { context.stroke(SparklineGeometry.line(run), with: .color(line.color), style: stroke) }
                }
            }
        }
        .clipped() // a point a step before the range would draw past the edge
        .accessibilityHidden(true) // the numbers beside it say it
    }
}

/// Where a sparkline's points go, and the monotone curve through them.
enum SparklineGeometry {
    /// Each run of values (consecutive values of one `series`, split at gaps) as points in `size`: x over `domain`,
    /// y from 0 at the bottom to `top` at the top. A run of one point has nothing to draw and is left out.
    static func runs(_ values: [ChartValue], domain: ClosedRange<Date>, top: Double, size: CGSize) -> [[CGPoint]] {
        let start = domain.lowerBound.timeIntervalSince1970
        let span = max(domain.upperBound.timeIntervalSince1970 - start, 1e-9)
        var out: [[CGPoint]] = []
        var current: [CGPoint] = []
        var id: String?
        for v in values {
            if v.series != id {
                if current.count > 1 { out.append(current) }
                current = []
                id = v.series
            }
            current.append(CGPoint(x: size.width * (v.date.timeIntervalSince1970 - start) / span,
                                   y: size.height * (1 - v.value / top)))
        }
        if current.count > 1 { out.append(current) }
        return out
    }

    /// A monotone cubic through the points (Steffen's tangents, as d3's monotoneX; it looks the same as the charts'
    /// `.monotone`): it never overshoots a point, so a flat stretch stays flat and a step stays a step.
    static func line(_ p: [CGPoint]) -> Path {
        Path { path in
            guard let first = p.first else { return }
            path.move(to: first)
            let t = tangents(p)
            for i in p.indices.dropLast() {
                let dx = (p[i + 1].x - p[i].x) / 3
                path.addCurve(to: p[i + 1], control1: CGPoint(x: p[i].x + dx, y: p[i].y + dx * t[i]),
                              control2: CGPoint(x: p[i + 1].x - dx, y: p[i + 1].y - dx * t[i + 1]))
            }
        }
    }

    /// The line closed down to `bottom` (the value 0): the wash under it.
    static func wash(_ p: [CGPoint], bottom: CGFloat) -> Path {
        wash(p, line: line(p), bottom: bottom)
    }

    /// Close a copy of the already-built stroke; the Canvas can reuse its original monotone path.
    static func wash(_ p: [CGPoint], line: Path, bottom: CGFloat) -> Path {
        var path = line
        guard let first = p.first, let last = p.last else { return path }
        path.addLine(to: CGPoint(x: last.x, y: bottom))
        path.addLine(to: CGPoint(x: first.x, y: bottom))
        path.closeSubpath()
        return path
    }

    /// The slope at each point: 0 where the neighbours turn, never steeper than keeps the curve between them.
    static func tangents(_ p: [CGPoint]) -> [CGFloat] {
        let n = p.count
        guard n > 1 else { return Array(repeating: 0, count: n) }
        func slope(_ a: CGPoint, _ b: CGPoint) -> CGFloat { b.x == a.x ? 0 : (b.y - a.y) / (b.x - a.x) }
        guard n > 2 else { let s = slope(p[0], p[1]); return [s, s] }
        func sign(_ x: CGFloat) -> CGFloat { x < 0 ? -1 : 1 }
        var t = [CGFloat](repeating: 0, count: n)
        for i in 1..<(n - 1) {
            let h0 = p[i].x - p[i - 1].x, h1 = p[i + 1].x - p[i].x
            let s0 = slope(p[i - 1], p[i]), s1 = slope(p[i], p[i + 1])
            let weighted = h0 + h1 == 0 ? 0 : (s0 * h1 + s1 * h0) / (h0 + h1)
            t[i] = (sign(s0) + sign(s1)) * min(abs(s0), abs(s1), abs(weighted) / 2)
        }
        // The ends: the one-sided slope that makes the end piece a parabola (d3's slope2).
        func endSlope(_ a: CGPoint, _ b: CGPoint, _ inner: CGFloat) -> CGFloat {
            b.x == a.x ? inner : (3 * slope(a, b) - inner) / 2
        }
        t[0] = endSlope(p[0], p[1], t[1])
        t[n - 1] = endSlope(p[n - 2], p[n - 1], t[n - 2])
        return t
    }
}

/// A detail chart: title and the value now (or at the scrubbed moment), the lines over the range, a hairline grid,
/// and a shared scrub: holding any chart shows the same moment in all of them.
struct MetricChart: View {
    var title: String
    var points: [MetricPoint]
    var series: [ChartSeries]
    var domain: ClosedRange<Date>
    var step: TimeInterval
    /// Fixed y range (percent charts); nil: from 0 to a little over the largest value.
    var yDomain: ClosedRange<Double>?
    /// A level to mark (a load chart's core count), with its label.
    var reference: (value: Double, label: String)?
    var format: (Double) -> String
    /// The value(s) shown when nothing is scrubbed.
    var now: [Double?]
    @Binding var selection: Date?

    var body: some View {
        let picked = selection.flatMap { points.nearest($0, step: step) }
        VStack(alignment: .leading, spacing: Theme.Space.m) {
            header(picked)
            Chart {
                ForEach(series.indices, id: \.self) { i in
                    let s = series[i]
                    ForEach(points.chartValues(s)) { v in
                        if series.count == 1 {
                            AreaMark(x: .value(String(localized: "Time"), v.date), y: .value(s.name, v.value), series: .value(String(localized: "Segment"), v.series))
                                .foregroundStyle(s.color.opacity(0.1))
                                .interpolationMethod(.monotone)
                        }
                        LineMark(x: .value(String(localized: "Time"), v.date), y: .value(s.name, v.value), series: .value(String(localized: "Segment"), v.series))
                            .foregroundStyle(s.color)
                            .lineStyle(StrokeStyle(lineWidth: 2, lineCap: .round, lineJoin: .round))
                            .interpolationMethod(.monotone)
                    }
                }
                if let reference {
                    RuleMark(y: .value(String(localized: "Reference"), reference.value))
                        .foregroundStyle(Color.secondary.opacity(0.6))
                        .lineStyle(StrokeStyle(lineWidth: 1, dash: [3, 3]))
                        .annotation(position: .top, alignment: .leading, spacing: 2) {
                            Text(reference.label).font(Theme.Font.label).foregroundStyle(.secondary)
                        }
                }
                if let picked {
                    RuleMark(x: .value(String(localized: "Selected [Metric Charts 45c21a]"), picked.date))
                        .foregroundStyle(Color.secondary.opacity(0.5))
                        .lineStyle(StrokeStyle(lineWidth: 1))
                    ForEach(series.indices, id: \.self) { i in
                        if let v = series[i].value(picked) {
                            PointMark(x: .value(String(localized: "Time"), picked.date), y: .value(series[i].name, v))
                                .symbolSize(64)
                                .foregroundStyle(series[i].color)
                                .symbol { Circle().fill(series[i].color).scaledFrame(width: Theme.Size.dotL, height: Theme.Size.dotL)
                                    .overlay(Circle().stroke(Color(.secondarySystemGroupedBackground), lineWidth: 2)) }
                        }
                    }
                }
            }
            .chartXScale(domain: domain)
            .chartYScale(domain: yDomain ?? 0...niceMax)
            .chartXAxis {
                AxisMarks(values: .automatic(desiredCount: 4)) { _ in
                    AxisGridLine(stroke: StrokeStyle(lineWidth: 0.5)).foregroundStyle(MetricColor.grid)
                    // An explicit label color: date labels otherwise take the app's tint.
                    AxisValueLabel(format: .dateTime.hour().minute(), collisionResolution: .greedy)
                        .font(Theme.Font.label).foregroundStyle(Color(.secondaryLabel))
                }
            }
            .chartYAxis {
                AxisMarks(position: .trailing, values: .automatic(desiredCount: 3)) { v in
                    AxisGridLine(stroke: StrokeStyle(lineWidth: 0.5)).foregroundStyle(MetricColor.grid)
                    AxisValueLabel { if let d = v.as(Double.self) { Text(format(d)) } }
                        .font(Theme.Font.label).foregroundStyle(Color(.secondaryLabel))
                }
            }
            .chartLegend(.hidden)
            .chartPlotStyle { $0.clipped() } // a point a step before the range would draw past the y axis
            .chartXSelection(value: $selection)
            .scaledFrame(height: Theme.Size.box150)
            .accessibilityElement(children: .ignore)
            .accessibilityLabel(title)
            .accessibilityValue(summary)
            .accessibilityIdentifier("chart-" + (series.first?.name ?? title))
        }
        .card(padding: 14)
    }

    @ViewBuilder private func header(_ picked: MetricPoint?) -> some View {
        HStack(alignment: .firstTextBaseline) {
            Text(title).font(Theme.Font.secondaryStrong)
            Spacer(minLength: 8)
            if let picked {
                Text(picked.date, format: .dateTime.hour().minute()).font(Theme.Font.label.monospacedDigit()).foregroundStyle(.secondary)
            }
        }
        // Values in text ink; a dot in the series color says which is which (the legend, for two series).
        HStack(spacing: Theme.Space.l) {
            ForEach(series.indices, id: \.self) { i in
                let v = picked.map { series[i].value($0) } ?? (i < now.count ? now[i] : nil)
                HStack(spacing: Theme.Space.sm) {
                    if series.count > 1 {
                        Circle().fill(series[i].color).scaledFrame(width: Theme.Size.dot, height: Theme.Size.dot)
                        Text(series[i].name).font(Theme.Font.label).foregroundStyle(.secondary)
                    }
                    Text(v.map(format) ?? "—")
                        .font(series.count > 1 ? Theme.Font.heading.monospacedDigit() : Theme.Font.title.monospacedDigit())
                        .contentTransition(.numericText())
                }
            }
        }
    }

    /// The y top: a little over the largest value (and the reference), so the line does not touch the top.
    private var niceMax: Double {
        let m = max(series.flatMap { s in points.compactMap { s.value($0) } }.max() ?? 0, reference?.value ?? 0)
        return m > 0 ? m * 1.15 : 1
    }

    private var summary: String {
        let parts = series.indices.map { i in
            let vals = points.compactMap { series[i].value($0) }
            let now = i < self.now.count ? self.now[i] : nil
            let current = now.map(format) ?? String(localized: "Unknown [Session Modes 4d8c1c]")
            let peak = vals.max().map(format) ?? String(localized: "Unknown [Session Modes 4d8c1c]")
            return series.count > 1
                ? String(localized: "\(series[i].name) now \(current), peak \(peak)")
                : String(localized: "Now \(current), peak \(peak)")
        }
        return parts.joined(separator: String(localized: "; ", comment: "Separator between chart series summaries"))
    }
}

/// A bar for a share (disk, memory): the used part in its color, the rest a faint track.
struct MeterBar: View {
    var fraction: Double
    var color: Color

    var body: some View {
        GeometryReader { g in
            ZStack(alignment: .leading) {
                Capsule().fill(Color.primary.opacity(0.08))
                Capsule().fill(color).frame(width: max(4, g.size.width * min(max(fraction, 0), 1)))
            }
        }
        .scaledFrame(height: Theme.Size.dotXS)
        .accessibilityHidden(true)
    }
}

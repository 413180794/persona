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
struct Sparkline: View {
    var points: [MetricPoint]
    var series: [ChartSeries]
    var domain: ClosedRange<Date>
    var yMax: Double?

    var body: some View {
        Chart {
            ForEach(series.indices, id: \.self) { i in
                let s = series[i]
                ForEach(points.chartValues(s)) { v in
                    if series.count == 1 {
                        AreaMark(x: .value("时间", v.date), y: .value(s.name, v.value), series: .value("段", v.series))
                            .foregroundStyle(s.color.opacity(0.12))
                            .interpolationMethod(.monotone)
                    }
                    LineMark(x: .value("时间", v.date), y: .value(s.name, v.value), series: .value("段", v.series))
                        .foregroundStyle(s.color)
                        .lineStyle(StrokeStyle(lineWidth: 1.5, lineCap: .round, lineJoin: .round))
                        .interpolationMethod(.monotone)
                }
            }
        }
        .chartXScale(domain: domain)
        .chartYScale(domain: 0...max(yMax ?? 0, maxValue, 1e-9))
        .chartPlotStyle { $0.clipped() } // a point a step before the range would draw past the edge
        .chartXAxis(.hidden)
        .chartYAxis(.hidden)
        .chartLegend(.hidden)
        .accessibilityHidden(true) // the numbers beside it say it
    }

    private var maxValue: Double {
        series.flatMap { s in points.compactMap { s.value($0) } }.max() ?? 0
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
                            AreaMark(x: .value("时间", v.date), y: .value(s.name, v.value), series: .value("段", v.series))
                                .foregroundStyle(s.color.opacity(0.1))
                                .interpolationMethod(.monotone)
                        }
                        LineMark(x: .value("时间", v.date), y: .value(s.name, v.value), series: .value("段", v.series))
                            .foregroundStyle(s.color)
                            .lineStyle(StrokeStyle(lineWidth: 2, lineCap: .round, lineJoin: .round))
                            .interpolationMethod(.monotone)
                    }
                }
                if let reference {
                    RuleMark(y: .value("参考", reference.value))
                        .foregroundStyle(Color.secondary.opacity(0.6))
                        .lineStyle(StrokeStyle(lineWidth: 1, dash: [3, 3]))
                        .annotation(position: .top, alignment: .leading, spacing: 2) {
                            Text(reference.label).font(Theme.Font.label).foregroundStyle(.secondary)
                        }
                }
                if let picked {
                    RuleMark(x: .value("选中", picked.date))
                        .foregroundStyle(Color.secondary.opacity(0.5))
                        .lineStyle(StrokeStyle(lineWidth: 1))
                    ForEach(series.indices, id: \.self) { i in
                        if let v = series[i].value(picked) {
                            PointMark(x: .value("时间", picked.date), y: .value(series[i].name, v))
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
            return "\(series.count > 1 ? series[i].name + " " : "")现在 \(now.map(format) ?? "未知")，最高 \(vals.max().map(format) ?? "未知")"
        }
        return parts.joined(separator: "；")
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

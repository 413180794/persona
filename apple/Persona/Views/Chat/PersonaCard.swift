import Charts
import SwiftUI

/// A card the assistant puts in a reply (#886): a ```persona-card fenced block holding one JSON object. The app draws
/// the ones it understands; anything else (unknown type, broken or still-streaming JSON) stays the code block it is, so
/// nothing is lost on an older app, the web or Android. The prompt that teaches the format is the assistant's
/// claude_md.md「做事方式」.
struct PersonaCard: Decodable, Equatable {
    static let language = "persona-card"

    enum Kind: String, Decodable {
        /// One number that matters: `value` + `unit`, `note` beside it (比昨天 +12%).
        case stat
        /// A total and its parts: `segments`, `rest` (the unassigned part), `total` (else their sum).
        case stackedBar = "stacked_bar"
        /// By day: `points` as a line (two or more) or as bars.
        case line, bar
    }

    struct Amount: Decodable, Equatable {
        var value: Double
        var unit: String?
    }

    struct Part: Decodable, Equatable {
        var label: String
        var value: Double
        var note: String?
    }

    var type: Kind
    var title: String?
    var subtitle: String?
    var value: Double?
    var unit: String?
    var note: String?
    var total: Amount?
    var segments: [Part]?
    var rest: Part?
    var points: [Part]?

    /// The card in a fenced block's text, or nil when it is not one the app can draw.
    static func parse(_ text: String) -> PersonaCard? {
        guard let data = text.data(using: .utf8), let card = try? JSONDecoder().decode(PersonaCard.self, from: data),
              card.drawable else { return nil }
        return card
    }

    var drawable: Bool {
        switch type {
        case .stat: value != nil
        case .stackedBar: !parts.isEmpty && parts.allSatisfy { $0.value >= 0 } && sum > 0
        case .line: (points ?? []).count >= 2
        case .bar: !(points ?? []).isEmpty
        }
    }

    /// The stacked bar's pieces in order, the rest last.
    var parts: [Part] { (segments ?? []) + (rest.map { [$0] } ?? []) }
    var sum: Double { parts.reduce(0) { $0 + $1.value } }

    /// The headline amount: a stat's value, a stacked bar's total (or its parts' sum), a trend's optional value.
    var headline: Amount? {
        switch type {
        case .stackedBar: total ?? Amount(value: sum, unit: unit)
        default: value.map { Amount(value: $0, unit: unit) }
        }
    }

    /// 2,250 / 7.5: grouped, at most one decimal.
    static func number(_ v: Double) -> String { v.formatted(.number.precision(.fractionLength(0...1))) }
}

struct PersonaCardView: View {
    var card: PersonaCard

    /// Parts are told apart, nothing more; the rest is quiet.
    static let hues = [Theme.Hue.blue, Theme.Hue.green, Theme.Hue.orange, Theme.Hue.purple]

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.s) {
            if let t = card.title { Text(t).font(Theme.Font.heading).accessibilityAddTraits(.isHeader) }
            if let s = card.subtitle { Text(s).font(Theme.Font.secondary).foregroundStyle(.secondary) }
            if let a = card.headline { amount(a) }
            if card.type == .stat, let n = card.note { Text(n).font(Theme.Font.secondary).foregroundStyle(.secondary) }
            switch card.type {
            case .stat: EmptyView()
            case .stackedBar: shares
            case .line, .bar: trend
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .card(padding: Theme.Space.m)
    }

    private func amount(_ a: PersonaCard.Amount) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: Theme.Space.xs) {
            Text(PersonaCard.number(a.value)).font(Theme.Font.display).monospacedDigit()
            if let u = a.unit ?? card.unit { Text(u).font(Theme.Font.heading) }
        }
        .accessibilityElement(children: .combine)
    }

    private func color(_ i: Int) -> Color {
        i < (card.segments ?? []).count ? Self.hues[i % Self.hues.count] : Theme.Hue.gray
    }

    private var shares: some View {
        let parts = card.parts, sum = card.sum
        return VStack(alignment: .leading, spacing: Theme.Space.s) {
            GeometryReader { g in
                let gaps = Theme.Size.rule * CGFloat(max(parts.count - 1, 0))
                HStack(spacing: Theme.Size.rule) {
                    ForEach(parts.indices, id: \.self) { i in
                        Rectangle().fill(color(i))
                            .frame(width: max(g.size.width - gaps, 0) * parts[i].value / sum)
                    }
                }
            }
            .scaledFrame(height: Theme.Size.meter)
            .clipShape(.capsule)
            .accessibilityHidden(true) // the rows below say the same
            ForEach(parts.indices, id: \.self) { i in
                let p = parts[i], isRest = i >= (card.segments ?? []).count
                if i > 0 { Divider() }
                HStack(spacing: Theme.Space.s) {
                    if !isRest { Circle().fill(color(i)).scaledFrame(width: Theme.Size.dot, height: Theme.Size.dot) }
                    Text(p.label).font(Theme.Font.body).foregroundStyle(isRest ? .secondary : .primary).lineLimit(2)
                    if let n = p.note { Text(n).font(Theme.Font.secondary).foregroundStyle(.secondary).lineLimit(1) }
                    Spacer(minLength: Theme.Space.s)
                    Text(PersonaCard.number(p.value) + (card.headline?.unit.map { " " + $0 } ?? ""))
                        .font(isRest ? Theme.Font.body : Theme.Font.bodyStrong).monospacedDigit()
                        .foregroundStyle(isRest ? .secondary : .primary)
                        .fixedSize() // the number stays whole; the label wraps
                }
                .accessibilityElement(children: .combine)
            }
        }
    }

    private var trend: some View {
        // Placed by position, named by label: two points both called 周一 are still two points.
        let points = Array((card.points ?? []).enumerated()), n = points.count
        let shown = Array(stride(from: 0, to: n, by: max(1, (n + 6) / 7))) // at most about seven labels
        return VStack(alignment: .leading, spacing: Theme.Space.xs) {
            Chart(points, id: \.offset) { i, p in
                if card.type == .line {
                    LineMark(x: .value("日期", String(i)), y: .value(card.unit ?? "值", p.value))
                        .foregroundStyle(Theme.Hue.blue)
                        .interpolationMethod(.monotone)
                    PointMark(x: .value("日期", String(i)), y: .value(card.unit ?? "值", p.value))
                        .foregroundStyle(Theme.Hue.blue)
                } else {
                    BarMark(x: .value("日期", String(i)), y: .value(card.unit ?? "值", p.value))
                        .foregroundStyle(Theme.Hue.blue)
                        .cornerRadius(Theme.Radius.badge)
                }
            }
            .chartXAxis { // days as labels; only the value lines cross the plot
                AxisMarks(values: shown.map(String.init)) { v in
                    AxisValueLabel { if let i = v.as(String.self).flatMap(Int.init), i < n { Text(points[i].element.label) } }
                }
            }
            .accessibilityLabel(card.title ?? "")
            .accessibilityValue(points.map { "\($0.element.label) \(PersonaCard.number($0.element.value))" }.joined(separator: "，"))
            .scaledFrame(height: Theme.Size.box150)
            if let n = card.note { Text(n).font(Theme.Font.secondary).foregroundStyle(.secondary) }
        }
    }
}

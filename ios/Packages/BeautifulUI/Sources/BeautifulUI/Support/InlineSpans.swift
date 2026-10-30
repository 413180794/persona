import SwiftUI

/// Rich inline text for card bodies: plain runs, emphasis, entity chips (a monogram avatar
/// in a pill) and value pills — the original's `EntityChip` and `ValuePill` atoms.
public enum BUIInlineSpan: Sendable {
    public enum Tone: Sendable { case neutral, green, orange, red, accent }

    case text(String)
    /// `font-medium text-ink`.
    case emphasis(String)
    /// A named thing with a coloured monogram, e.g. a supplier.
    case entity(String, monogram: String, tint: Color)
    /// A value in a tinted pill.
    case value(String, tone: Tone = .neutral)
    /// `@name` after a coloured dot, in medium ink.
    case mention(String, dot: Color)
    /// A figure in the mono face, tinted (e.g. a red loss).
    case code(String, tone: Tone)
}

struct InlinePillAttribute: TextAttribute {
    enum Kind: Hashable { case entity(Int), value(Int) }
    var kind: Kind
}

/// Builds the text for `spans` and the renderer that draws their pills.
struct InlineSpanText {
    var spans: [BUIInlineSpan]
    var colors: BUIColors
    var environment: EnvironmentValues
    var fontSize: CGFloat

    /// Reserves exactly `width` points: a transparent 1 pt-tall image, so the line box keeps
    /// the paragraph's height (kerning a lone space is dropped at run ends).
    private func gap(_ width: CGFloat) -> Text {
        Text(Image(size: CGSize(width: width, height: 1)) { _ in })
    }

    var text: Text {
        var out = Text("")
        for (index, span) in spans.enumerated() {
            let piece: Text
            switch span {
            case .text(let s):
                piece = Text(s)
            case .emphasis(let s):
                piece = Text(s).fontWeight(.medium).foregroundStyle(colors.ink)
            case .entity(let name, _, _):
                // mx-0.5 + pl-[3px] + 16 pt avatar + gap-1 | name | pr-1.5 + mx-0.5
                let name = Text(name).font(.system(size: 12, weight: .medium)).foregroundStyle(colors.ink)
                piece = Text("\(gap(2 + 3 + 16 + 4))\(name)\(gap(6 + 2))")
                    .customAttribute(InlinePillAttribute(kind: .entity(index)))
            case .mention(let name, let dot):
                let mark = Text("\u{25CF}").font(.system(size: 10)).foregroundStyle(dot)
                piece = Text("\(mark)\u{2009}\(Text("@\(name)").fontWeight(.medium).foregroundStyle(colors.ink))")
            case .code(let s, let tone):
                piece = Text(s).font(.system(size: 11.5, design: .monospaced)).foregroundStyle(Self.ink(tone, colors))
            case .value(let value, let tone):
                let label = Text(value).font(.system(size: 12, weight: .medium)).foregroundStyle(Self.ink(tone, colors))
                piece = Text("\(gap(2 + 6))\(label)\(gap(6 + 2))")
                    .customAttribute(InlinePillAttribute(kind: .value(index)))
            }
            out = Text("\(out)\(piece)")
        }
        return out
    }

    var renderer: InlinePillRenderer {
        InlinePillRenderer(spans: spans, resolved: ResolvedPillColors(colors, environment))
    }

    static func ink(_ tone: BUIInlineSpan.Tone, _ c: BUIColors) -> Color {
        switch tone {
        case .neutral: c.ink2
        case .green: c.green
        case .orange: c.orange
        case .red: c.red
        case .accent: c.accentInk
        }
    }

    static func fill(_ tone: BUIInlineSpan.Tone, _ c: BUIColors) -> Color {
        switch tone {
        case .neutral: c.field
        case .green: c.greenTint
        case .orange: c.orangeTint
        case .red: c.redTint
        case .accent: c.accentTint
        }
    }

    static func ring(_ tone: BUIInlineSpan.Tone, _ c: BUIColors) -> Color {
        switch tone {
        case .neutral: c.line
        case .green: c.green.opacity(0.28)
        case .orange: c.orange.opacity(0.28)
        case .red: c.red.opacity(0.28)
        case .accent: c.accent.opacity(0.28)
        }
    }
}

/// Colours resolved against the view's environment (a renderer can't resolve dynamic ones).
struct ResolvedPillColors: Sendable {
    var field: Color
    var line: Color
    var tones: [BUIInlineSpan.Tone: (fill: Color, ring: Color)]

    init(_ c: BUIColors, _ env: EnvironmentValues) {
        func r(_ color: Color) -> Color { Color(color.resolve(in: env)) }
        field = r(c.field)
        line = r(c.line)
        var tones: [BUIInlineSpan.Tone: (Color, Color)] = [:]
        for tone in [BUIInlineSpan.Tone.neutral, .green, .orange, .red, .accent] {
            tones[tone] = (r(InlineSpanText.fill(tone, c)), r(InlineSpanText.ring(tone, c)))
        }
        self.tones = tones
    }
}

struct InlinePillRenderer: TextRenderer {
    var spans: [BUIInlineSpan]
    var resolved: ResolvedPillColors

    func draw(layout: Text.Layout, in ctx: inout GraphicsContext) {
        for line in layout {
            var group: (kind: InlinePillAttribute.Kind, rect: CGRect, baseline: CGFloat)?
            func flush() {
                if let g = group { drawPill(g.kind, rect: g.rect, baseline: g.baseline, in: ctx) }
                group = nil
            }
            for run in line {
                let attr = run[InlinePillAttribute.self]
                if group != nil, attr?.kind != group?.kind { flush() }
                if let attr {
                    let b = run.typographicBounds
                    group = group.map { ($0.kind, $0.rect.union(b.rect), $0.baseline) } ?? (attr.kind, b.rect, b.origin.y)
                }
            }
            flush()
            for run in line { ctx.draw(run) }
        }
    }

    private func drawPill(_ kind: InlinePillAttribute.Kind, rect run: CGRect, baseline: CGFloat, in ctx: GraphicsContext) {
        // align-middle on a 13 pt body's x-height.
        let midY = baseline - 3.5
        switch kind {
        case .entity(let index):
            guard case .entity(_, let monogram, let tint) = spans[index] else { return }
            let pill = CGRect(x: run.minX + 2, y: midY - 9, width: run.width - 4, height: 18)
            let shape = Path(roundedRect: pill.insetBy(dx: 0.5, dy: 0.5), cornerRadius: 9)
            ctx.fill(shape, with: .color(resolved.field))
            ctx.stroke(shape, with: .color(resolved.line), lineWidth: 1)
            let avatar = CGRect(x: pill.minX + 3, y: midY - 8, width: 16, height: 16)
            ctx.fill(Path(ellipseIn: avatar), with: .color(tint))
            ctx.draw(Text(monogram).font(.system(size: 9, weight: .semibold)).foregroundStyle(.white), at: CGPoint(x: avatar.midX, y: avatar.midY))
        case .value(let index):
            guard case .value(_, let tone) = spans[index], let colors = resolved.tones[tone] else { return }
            let pill = CGRect(x: run.minX + 2, y: midY - 9.5, width: run.width - 4, height: 19)
            let shape = Path(roundedRect: pill.insetBy(dx: 0.5, dy: 0.5), cornerRadius: 9.5)
            ctx.fill(shape, with: .color(colors.fill))
            ctx.stroke(shape, with: .color(colors.ring), lineWidth: 1)
        }
    }
}

/// A paragraph of `BUIInlineSpan`s.
public struct BUIInlineText: View {
    var spans: [BUIInlineSpan]
    var style: BUITextStyle
    @Environment(\.buiTheme) private var theme
    @Environment(\.self) private var environment

    public init(_ spans: [BUIInlineSpan], style: BUITextStyle = BUITextStyle(size: 13, lineHeight: 1.625)) {
        self.spans = spans
        self.style = style
    }

    public var body: some View {
        let built = InlineSpanText(spans: spans, colors: theme.colors, environment: environment, fontSize: 12)
        built.text
            .buiFont(style)
            .foregroundStyle(theme.colors.ink2)
            .textRenderer(built.renderer)
            .fixedSize(horizontal: false, vertical: true)
    }
}

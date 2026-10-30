import SwiftUI

/// A text style in the original's pixel sizes. Sizes map 1:1 to points at the default
/// Dynamic Type size and scale with the user's setting relative to `textStyle`.
///
/// The original uses Inter and JetBrains Mono; the port uses SF Pro and SF Mono so the
/// library ships no fonts.
public struct BUITextStyle: Sendable, Hashable {
    public var size: CGFloat
    public var weight: Font.Weight
    public var design: Font.Design
    /// CSS `line-height` as a multiple of the font size; only affects multi-line text.
    public var lineHeight: CGFloat
    /// Letter spacing in em. The site sets `-0.01em` on body text; mono text defaults to
    /// `-0.03em` because SF Mono's advance (0.618 em) is wider than JetBrains Mono's (0.6 em).
    public var tracking: CGFloat
    public var textStyle: Font.TextStyle

    public init(
        size: CGFloat,
        weight: Font.Weight = .regular,
        design: Font.Design = .default,
        lineHeight: CGFloat = 1.5,
        tracking: CGFloat? = nil,
        textStyle: Font.TextStyle? = nil
    ) {
        self.size = size
        self.weight = weight
        self.design = design
        self.lineHeight = lineHeight
        self.tracking = tracking ?? (design == .monospaced ? -0.03 : -0.01)
        self.textStyle = textStyle ?? Self.textStyle(for: size)
    }

    /// The Dynamic Type style whose default size is closest to `size`.
    static func textStyle(for size: CGFloat) -> Font.TextStyle {
        switch size {
        case 17...: .body
        case 15..<17: .subheadline
        case 13..<15: .footnote
        case 12..<13: .caption
        default: .caption2
        }
    }
}

public extension BUITextStyle {
    /// 13 semibold — component titles.
    static let title = BUITextStyle(size: 13, weight: .semibold)
    /// 13 medium — primary labels and row titles.
    static let label = BUITextStyle(size: 13, weight: .medium)
    /// 13 regular — running text inside components.
    static let body = BUITextStyle(size: 13)
    /// 14 regular, 1.5 line height — prose such as chat messages and streamed answers.
    static let prose = BUITextStyle(size: 14, lineHeight: 1.6)
    /// 12.5 regular — secondary descriptions.
    static let detail = BUITextStyle(size: 12.5)
    /// 12 regular — meta lines.
    static let caption = BUITextStyle(size: 12)
    /// 11 medium — chips and tags.
    static let micro = BUITextStyle(size: 11, weight: .medium)
    /// 12 monospaced — timers, ids, code meta.
    static let mono = BUITextStyle(size: 12, design: .monospaced)

    func weight(_ weight: Font.Weight) -> BUITextStyle {
        var copy = self
        copy.weight = weight
        return copy
    }
}

struct BUIFontModifier: ViewModifier {
    let style: BUITextStyle
    @ScaledMetric private var scaledSize: CGFloat
    @Environment(\.buiTheme.typeScale) private var typeScale

    init(style: BUITextStyle) {
        self.style = style
        _scaledSize = ScaledMetric(wrappedValue: style.size, relativeTo: style.textStyle)
    }

    /// SF's natural line height is about 1.19 × the point size.
    nonisolated static let naturalLineHeight: CGFloat = 1.19

    func body(content: Content) -> some View {
        let size = scaledSize * typeScale
        // CSS spreads `line-height` as half-leading above and below every line;
        // SwiftUI only adds `lineSpacing` between lines, so pad the remainder.
        let leading = max(0, (style.lineHeight - Self.naturalLineHeight) * size)
        content
            .font(.system(size: size, weight: style.weight, design: style.design))
            // A host's `.fontDesign` (an app may set one for its whole tree) overrides the font's own design: keep
            // monospaced / rounded styles, and let `.default` ones follow the host.
            .modifier(KeepDesign(design: style.design))
            .tracking(style.tracking * size)
            .lineSpacing(leading)
            .padding(.vertical, leading / 2)
    }
}

/// `.fontDesign(nil)` would reset the host's design too, so `.default` adds nothing.
private struct KeepDesign: ViewModifier {
    let design: Font.Design

    func body(content: Content) -> some View {
        if design == .default { content } else { content.fontDesign(design) }
    }
}

public extension View {
    /// Applies a Beautiful UI text style, scaled with Dynamic Type.
    func buiFont(_ style: BUITextStyle) -> some View {
        modifier(BUIFontModifier(style: style))
    }
}

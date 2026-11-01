import SwiftUI

/// A settings-style row in the task rows' language: an optional leading mark, the title with a line under it, a
/// value and the views a host adds on the right (a toggle, a pill), then a chevron or a check. It draws no surface:
/// put it in a `BUIListSection`, or in a host's own list row.
public struct BUIListRow<Leading: View, Trailing: View>: View {
    public enum Accessory: Sendable, Equatable {
        case none
        /// A right chevron: the row opens a page.
        case disclosure
        /// A check: the row is the chosen one.
        case check
    }

    var title: String
    var detail: String?
    var value: String?
    var accessory: Accessory
    var detailLines: Int
    var leading: Leading
    var trailing: Trailing
    @Environment(\.buiTheme) private var theme

    public init(
        _ title: String,
        detail: String? = nil,
        value: String? = nil,
        accessory: Accessory = .none,
        detailLines: Int = 2,
        @ViewBuilder leading: () -> Leading,
        @ViewBuilder trailing: () -> Trailing
    ) {
        self.title = title
        self.detail = detail
        self.value = value
        self.accessory = accessory
        self.detailLines = detailLines
        self.leading = leading()
        self.trailing = trailing()
    }

    public var body: some View {
        HStack(spacing: 10) {
            leading
            VStack(alignment: .leading, spacing: 2) {
                Text(title)
                    .buiFont(.label)
                    .foregroundStyle(theme.colors.ink)
                    .fixedSize(horizontal: false, vertical: true)
                if let detail, !detail.isEmpty {
                    Text(detail)
                        .buiFont(.detail)
                        .foregroundStyle(theme.colors.ink2)
                        .lineLimit(detailLines)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            if let value, !value.isEmpty {
                Text(value)
                    .buiFont(.detail)
                    .monospacedDigit()
                    .foregroundStyle(theme.colors.ink2)
                    .lineLimit(1)
            }
            trailing
            switch accessory {
            case .disclosure:
                BUIIcon(.chevronRight, size: 14, lineWidth: 2.2)
                    .foregroundStyle(theme.colors.ink3)
            case .check:
                BUIIcon(.check, size: 15, lineWidth: 2.6)
                    .foregroundStyle(theme.colors.accent)
            case .none:
                EmptyView()
            }
        }
        .padding(.vertical, 8)
        .frame(minHeight: 44 * max(1, theme.typeScale * 0.9))
        .contentShape(.rect)
    }
}

public extension BUIListRow where Leading == EmptyView, Trailing == EmptyView {
    init(_ title: String, detail: String? = nil, value: String? = nil, accessory: Accessory = .none, detailLines: Int = 2) {
        self.init(title, detail: detail, value: value, accessory: accessory, detailLines: detailLines) { EmptyView() } trailing: { EmptyView() }
    }
}

public extension BUIListRow where Trailing == EmptyView {
    init(
        _ title: String,
        detail: String? = nil,
        value: String? = nil,
        accessory: Accessory = .none,
        detailLines: Int = 2,
        @ViewBuilder leading: () -> Leading
    ) {
        self.init(title, detail: detail, value: value, accessory: accessory, detailLines: detailLines, leading: leading) { EmptyView() }
    }
}

/// A row's mark: an SF Symbol on a rounded tile in a tint (the sources' avatar, `BUIAvatar.symbol`).
public struct BUIIconTile: View {
    var symbol: String
    var tint: Color
    var size: CGFloat
    @Environment(\.buiTheme) private var theme

    public init(_ symbol: String, tint: Color, size: CGFloat = 26) {
        self.symbol = symbol
        self.tint = tint
        self.size = size
    }

    public var body: some View {
        let side = size * max(1, theme.typeScale * 0.9)
        Image(systemName: symbol)
            .font(.system(size: side * 0.5, weight: .semibold))
            .foregroundStyle(.white)
            .frame(width: side, height: side)
            .background(tint, in: .rect(cornerRadius: theme.radii.chip + 1, style: .continuous))
            .accessibilityHidden(true) // decoration: the row's words say what it is
    }
}

/// Rows in one card with a line between them (`TaskRows`' list card), an optional heading over it and a note under it.
public struct BUIListSection<Content: View, Accessory: View>: View {
    var title: String?
    var count: Int?
    var footer: String?
    var content: Content
    var accessory: Accessory
    @Environment(\.buiTheme) private var theme

    public init(
        _ title: String? = nil,
        count: Int? = nil,
        footer: String? = nil,
        @ViewBuilder content: () -> Content,
        @ViewBuilder accessory: () -> Accessory
    ) {
        self.title = title
        self.count = count
        self.footer = footer
        self.content = content()
        self.accessory = accessory()
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            if let title {
                BUISectionHeading(title, count: count) { accessory }
                    .padding(.horizontal, 4)
            }
            VStack(spacing: 0) {
                Group(subviews: content) { rows in
                    ForEach(rows) { row in
                        row
                            .padding(.horizontal, 12)
                            .overlay(alignment: .bottom) {
                                if row.id != rows.last?.id {
                                    Rectangle().fill(theme.colors.line).frame(height: 1).padding(.leading, 12)
                                }
                            }
                    }
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .buiSurface(radius: theme.radii.card, elevation: .card)
            if let footer {
                Text(footer)
                    .buiFont(.caption)
                    .foregroundStyle(theme.colors.ink3)
                    .fixedSize(horizontal: false, vertical: true)
                    .padding(.horizontal, 4)
            }
        }
    }
}

public extension BUIListSection where Accessory == EmptyView {
    init(_ title: String? = nil, count: Int? = nil, footer: String? = nil, @ViewBuilder content: () -> Content) {
        self.init(title, count: count, footer: footer, content: content) { EmptyView() }
    }
}

/// A group's heading: the title, its count in a small boxed number, and a host's control at the end.
public struct BUISectionHeading<Accessory: View>: View {
    var title: String
    var count: Int?
    var accessory: Accessory
    @Environment(\.buiTheme) private var theme

    public init(_ title: String, count: Int? = nil, @ViewBuilder accessory: () -> Accessory) {
        self.title = title
        self.count = count
        self.accessory = accessory()
    }

    public var body: some View {
        HStack(spacing: 8) {
            Text(title)
                .buiFont(.title)
                .foregroundStyle(theme.colors.ink)
                .lineLimit(1)
                .accessibilityAddTraits(.isHeader)
            if let count {
                Text("\(count)")
                    .buiFont(BUITextStyle(size: 11.5, weight: .medium))
                    .monospacedDigit()
                    .foregroundStyle(theme.colors.ink2)
                    .padding(.horizontal, 6)
                    .frame(minHeight: 20 * max(1, theme.typeScale * 0.9))
                    .background(theme.colors.inset, in: .rect(cornerRadius: theme.radii.chip, style: .continuous))
                    .overlay(RoundedRectangle(cornerRadius: theme.radii.chip, style: .continuous).strokeBorder(theme.colors.line, lineWidth: 1))
            }
            Spacer(minLength: 0)
            accessory
        }
    }
}

public extension BUISectionHeading where Accessory == EmptyView {
    init(_ title: String, count: Int? = nil) {
        self.init(title, count: count) { EmptyView() }
    }
}

/// Nothing to show yet, or something went wrong: a mark on a raised tile, a title, a line of help and one action.
public struct BUIEmptyState: View {
    var symbol: String
    var title: String
    var message: String?
    var actionTitle: String?
    var action: (() -> Void)?
    @Environment(\.buiTheme) private var theme

    public init(symbol: String, title: String, message: String? = nil, actionTitle: String? = nil, action: (() -> Void)? = nil) {
        self.symbol = symbol
        self.title = title
        self.message = message
        self.actionTitle = actionTitle
        self.action = action
    }

    public var body: some View {
        VStack(spacing: 10) {
            Image(systemName: symbol)
                .font(.system(size: 20 * theme.typeScale, weight: .medium))
                .foregroundStyle(theme.colors.ink2)
                .frame(width: 44 * theme.typeScale, height: 44 * theme.typeScale)
                .buiSurface(radius: theme.radii.control, elevation: .button)
                .padding(.bottom, 4)
            Text(title)
                .buiFont(.title)
                .foregroundStyle(theme.colors.ink)
                .multilineTextAlignment(.center)
            if let message {
                Text(message)
                    .buiFont(.detail)
                    .foregroundStyle(theme.colors.ink3)
                    .multilineTextAlignment(.center)
                    .fixedSize(horizontal: false, vertical: true)
            }
            if let actionTitle, let action {
                Button(actionTitle, action: action)
                    .buttonStyle(.bui(.secondary))
                    .lineLimit(1)
                    .padding(.top, 4)
            }
        }
        .padding(24)
        .frame(maxWidth: 320)
    }
}

/// A reading on a card: what it is, the number large, a line under it and an optional meter (`InsightCards`' figures).
public struct BUIStatTile: View {
    var label: String
    var value: String
    var detail: String?
    var tone: BUIPill.Tone
    var progress: Double?
    var drawsSurface: Bool
    @Environment(\.buiTheme) private var theme

    /// - Parameters:
    ///   - tone: neutral keeps the number in ink; the others color it and the meter (a warning, a count that needs you).
    ///   - progress: 0…1 for a meter under the number.
    public init(_ label: String, value: String, detail: String? = nil, tone: BUIPill.Tone = .neutral, progress: Double? = nil,
                drawsSurface: Bool = true) {
        self.label = label
        self.value = value
        self.detail = detail
        self.tone = tone
        self.progress = progress
        self.drawsSurface = drawsSurface
    }

    private var ink: Color {
        switch tone {
        case .neutral: theme.colors.ink
        case .accent: theme.colors.accent
        case .green: theme.colors.green
        case .red: theme.colors.red
        case .orange: theme.colors.orange
        case .custom(let ink, _): ink
        case .outline: theme.colors.ink2
        }
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(label)
                .buiFont(.caption)
                .foregroundStyle(theme.colors.ink2)
                .lineLimit(1)
            Text(value)
                .buiFont(BUITextStyle(size: 18, weight: .semibold))
                .monospacedDigit()
                .foregroundStyle(ink)
                .lineLimit(1)
                .minimumScaleFactor(0.7)
                .contentTransition(.numericText())
            if let detail {
                Text(detail)
                    .buiFont(.caption)
                    .foregroundStyle(theme.colors.ink3)
                    .lineLimit(1)
            }
            if let progress {
                GeometryReader { g in
                    Capsule().fill(theme.colors.field)
                        .overlay(alignment: .leading) {
                            Capsule().fill(tone == .neutral ? theme.colors.accent : ink)
                                .frame(width: g.size.width * min(1, max(0, progress)))
                        }
                }
                .frame(height: 4)
                .padding(.top, 2)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(drawsSurface ? 12 : 0)
        .modifier(StatSurface(active: drawsSurface))
        .accessibilityElement(children: .combine)
    }
}

private struct StatSurface: ViewModifier {
    var active: Bool
    @Environment(\.buiTheme) private var theme

    func body(content: Content) -> some View {
        if active { content.buiSurface(radius: theme.radii.card, elevation: .card) } else { content }
    }
}

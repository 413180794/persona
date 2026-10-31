import SwiftUI

/// One line in a thinking trace.
public struct ThinkingRow: Identifiable, Hashable, Sendable {
    public var id: String
    public var title: String
    public var detail: String?
    /// Draw `detail` in the mono face (file names, commands).
    public var monospacedDetail: Bool
    public var additions: Int?
    public var deletions: Int?
    /// Search results open this link when tapped.
    public var url: URL?

    public init(
        id: String? = nil,
        _ title: String,
        detail: String? = nil,
        monospacedDetail: Bool = false,
        additions: Int? = nil,
        deletions: Int? = nil,
        url: URL? = nil
    ) {
        self.id = id ?? title
        self.title = title
        self.detail = detail
        self.monospacedDetail = monospacedDetail
        self.additions = additions
        self.deletions = deletions
        self.url = url
    }
}

/// An expandable agent trace. While `isWorking` the header shimmers and the trace opens
/// on its own; once work ends the header settles to `doneTitle` and the trace folds
/// away after `collapseDelay`. Tapping the header toggles it at any time.
public struct ThinkingState: View {
    public enum Style: String, CaseIterable, Sendable, Identifiable {
        /// Step list: spinner on the current step, muted checks behind it.
        case steps
        /// Wrapped prose reasoning.
        case reasoning
        /// Web search: the query, then sources read.
        case search
        /// Tool calls: files read, edits with line counts, commands.
        case coding

        public var id: Self { self }
    }

    var style: Style
    var isWorking: Bool
    var workingTitle: String
    var doneTitle: String
    var rows: [ThinkingRow]
    var query: String?
    var footnote: String?
    var collapseDelay: Double
    var minimumHeight: CGFloat
    var maxWidth: CGFloat
    var externalExpanded: Binding<Bool>?

    @State private var manualExpanded: Bool?
    @State private var lingering = false
    @State private var selectedRow: String?
    @Environment(\.buiTheme) private var theme
    @Environment(\.openURL) private var openURL

    /// - Parameters:
    ///   - rows: the rows that have arrived so far; new ones fade up as they are appended.
    ///   - query: shown above the rows (search traces).
    ///   - footnote: a muted line after the rows once work ends, e.g. "+7 more".
    ///   - isExpanded: pass a binding to own expansion; leave `nil` for the automatic behaviour.
    ///   - minimumHeight: height reserved while working or open, so surrounding layout
    ///     doesn't jump as rows stream in (the original reserves 176 pt).
    public init(
        _ style: Style = .steps,
        isWorking: Bool,
        workingTitle: String = "Thinking",
        doneTitle: String,
        rows: [ThinkingRow],
        query: String? = nil,
        footnote: String? = nil,
        isExpanded: Binding<Bool>? = nil,
        collapseDelay: Double = 2.6,
        minimumHeight: CGFloat = 0,
        maxWidth: CGFloat = 380
    ) {
        self.style = style
        self.isWorking = isWorking
        self.workingTitle = workingTitle
        self.doneTitle = doneTitle
        self.rows = rows
        self.query = query
        self.footnote = footnote
        self.externalExpanded = isExpanded
        self.collapseDelay = collapseDelay
        self.minimumHeight = minimumHeight
        self.maxWidth = maxWidth
    }

    /// Whether the trace is open when nobody has toggled it.
    nonisolated static func autoExpanded(isWorking: Bool, hasContent: Bool, lingering: Bool) -> Bool {
        (isWorking && hasContent) || lingering
    }

    private var expanded: Bool {
        if let externalExpanded { return externalExpanded.wrappedValue }
        return manualExpanded ?? Self.autoExpanded(
            isWorking: isWorking,
            hasContent: !rows.isEmpty || query != nil,
            lingering: lingering
        )
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            header
            // Built only while open: folded, a long trace would still be laid out at zero height on every
            // appearance (each thought of an opened work line in a host's chat list).
            if expanded {
                trace
                    .transition(.opacity)
            }
        }
        .frame(maxWidth: maxWidth, minHeight: isWorking || expanded ? minimumHeight : nil, alignment: .topLeading)
        .animation(BUICurve.outStrong.animation(duration: 0.4), value: expanded)
        .animation(BUICurve.outStrong.animation(duration: 0.4), value: isWorking)
        // On the change only: `.task(id:)` started a task every time the trace appeared (each row scrolling into a
        // host's lazy list), and opened finished traces for `collapseDelay` on appearing.
        .onChange(of: isWorking) { _, working in
            guard !working, !rows.isEmpty else { return }
            lingering = true
            Task {
                try? await Task.sleep(for: .seconds(collapseDelay))
                lingering = false
            }
        }
    }

    private var header: some View {
        Button {
            let next = !expanded
            if let externalExpanded {
                externalExpanded.wrappedValue = next
            } else {
                manualExpanded = next
            }
        } label: {
            HStack(spacing: 8) {
                BUIIcon(.sparkle, size: 16)
                    .foregroundStyle(isWorking ? theme.colors.ink2 : theme.colors.ink3)
                if isWorking {
                    Text(workingTitle)
                        .buiFont(.label)
                        .lineLimit(1)
                        .buiShimmer()
                } else {
                    let title = Text(doneTitle)
                        .buiFont(.label)
                        .lineLimit(1)
                        .foregroundStyle(theme.colors.ink2)
                    // Without the modifier at all when entrances are off: its state and onAppear on every row.
                    if theme.entrances {
                        title.buiEntrance(.fadeIn, duration: 0.35, curve: .easeOut)
                    } else {
                        title
                    }
                }
                BUIIcon(.chevronDown, size: 14, lineWidth: 2.2)
                    .foregroundStyle(theme.colors.ink3)
                    .rotationEffect(.degrees(expanded ? 180 : 0))
                    .animation(.easeInOut(duration: 0.3), value: expanded)
            }
            .padding(.horizontal, 6)
            .padding(.vertical, 4)
            .contentShape(.rect(cornerRadius: theme.radii.control))
        }
        .buttonStyle(PressHighlightStyle(radius: theme.radii.control))
        .padding(.horizontal, -6)
        .accessibilityLabel(isWorking ? workingTitle : doneTitle)
        .accessibilityValue(theme.strings.expanded(expanded))
        .accessibilityAddTraits(isWorking ? .updatesFrequently : [])
    }

    private var trace: some View {
        VStack(alignment: .leading, spacing: 4) {
            if let query {
                HStack(spacing: 8) {
                    BUIIcon(.magnifier, size: 14)
                        .foregroundStyle(theme.colors.ink3)
                    Text(query)
                        .buiFont(.detail)
                        .foregroundStyle(theme.colors.ink2)
                }
                .frame(height: 24)
                .padding(.horizontal, 6)
                .buiEntrance(.fadeUp, duration: 0.3)
            }
            ForEach(Array(rows.enumerated()), id: \.element.id) { index, row in
                rowView(row, index: index)
                    .buiEntrance(.fadeUp, duration: 0.32, delay: Double(index) * 0.12)
            }
            if let footnote, !isWorking {
                Text(footnote)
                    .buiFont(.caption)
                    .foregroundStyle(theme.colors.ink3)
                    .buiEntrance(.fadeIn, duration: 0.3, curve: .easeOut)
            }
        }
        .padding(.vertical, 4)
        .frame(maxWidth: .infinity, alignment: .leading)
        // The guide line starts under the header icon and tracks the trace's height.
        .background(alignment: .topLeading) {
            Rectangle()
                .fill(theme.colors.line)
                .frame(width: 1)
                .padding(.top, -8)
                .padding(.bottom, 2)
                .offset(x: -13)
        }
        .padding(.leading, 16)
        .padding(.leading, 5)
        .padding(.top, 4)
    }

    @ViewBuilder
    private func rowView(_ row: ThinkingRow, index: Int) -> some View {
        let content = HStack(spacing: 8) {
            leadingMark(index: index)
            Text(row.title)
                .buiFont(style == .reasoning ? BUITextStyle(size: 12.5, lineHeight: 1.625) : BUITextStyle(size: 12.5, weight: .medium))
                .foregroundStyle(style == .reasoning ? theme.colors.ink2 : theme.colors.ink)
                .lineLimit(style == .reasoning ? nil : 1)
                .fixedSize(horizontal: false, vertical: style == .reasoning)
            if let detail = row.detail {
                Text(detail)
                    .buiFont(BUITextStyle(size: 11.5, design: row.monospacedDetail ? .monospaced : .default))
                    .foregroundStyle(theme.colors.ink3)
                    .lineLimit(1)
                    .layoutPriority(1)
            }
            if let additions = row.additions {
                HStack(spacing: 4) {
                    Text("+\(additions)").foregroundStyle(theme.colors.green)
                    if let deletions = row.deletions {
                        Text("−\(deletions)").foregroundStyle(theme.colors.red)
                    }
                }
                .buiFont(BUITextStyle(size: 11, design: .monospaced))
                .monospacedDigit()
                .layoutPriority(1)
            }
            Spacer(minLength: 0)
        }
        .padding(.horizontal, 6)
        .padding(.vertical, 2)
        .frame(minHeight: 28)

        switch style {
        case .search:
            Button {
                if let url = row.url { openURL(url) }
            } label: {
                content.contentShape(.rect(cornerRadius: 6))
            }
            .buttonStyle(PressHighlightStyle(radius: 6, color: theme.colors.hover))
            .accessibilityAddTraits(.isLink)
        case .coding:
            let selected = selectedRow == row.id
            Button {
                withAnimation(BUIMotion.standard) { selectedRow = selected ? nil : row.id }
            } label: {
                content
                    .background(selected ? theme.colors.inset : .clear, in: .rect(cornerRadius: 6))
                    .contentShape(.rect(cornerRadius: 6))
            }
            .buttonStyle(PressHighlightStyle(radius: 6, color: theme.colors.hover))
            .accessibilityAddTraits(selected ? .isSelected : [])
        case .steps, .reasoning:
            content
        }
    }

    @ViewBuilder
    private func leadingMark(index: Int) -> some View {
        switch style {
        case .search:
            Circle()
                .fill([theme.colors.accent, theme.colors.orange, theme.colors.green][index % 3])
                .overlay { BUIIcon(.globe, size: 9, lineWidth: 2.5).foregroundStyle(.white) }
                .frame(width: 14, height: 14)
        case .steps:
            if index < rows.count - 1 || !isWorking {
                BUIIcon(.check, size: 14, lineWidth: 2.5)
                    .foregroundStyle(theme.colors.ink3)
            } else {
                BUISpinner()
            }
        case .reasoning, .coding:
            EmptyView()
        }
    }
}

/// iOS stand-in for the site's hover fills: the fill shows while the finger is down.
struct PressHighlightStyle: ButtonStyle {
    var radius: CGFloat
    var color: Color?
    @Environment(\.buiTheme) private var theme

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .background(
                configuration.isPressed ? (color ?? theme.colors.hover2) : .clear,
                in: .rect(cornerRadius: radius, style: .continuous)
            )
            .animation(.easeOut(duration: 0.1), value: configuration.isPressed)
    }
}

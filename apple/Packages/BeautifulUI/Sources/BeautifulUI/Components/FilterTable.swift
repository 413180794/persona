import SwiftUI

/// A status a task can be in, with its hue.
public struct TaskStatus: Identifiable, Hashable, Sendable {
    public var id: String
    public var label: String
    public var tint: Color

    public init(id: String, label: String, tint: Color) {
        self.id = id
        self.label = label
        self.tint = tint
    }
}

/// A task row in a filter table.
public struct FilterTableRow: Identifiable, Sendable {
    public var id: String { task }
    public var task: String
    public var date: String
    public var statusID: String
    public var owner: String

    public init(task: String, date: String, statusID: String, owner: String) {
        self.task = task
        self.date = date
        self.statusID = statusID
        self.owner = owner
    }
}

/// Status chips with counts that filter a task table; filtered-out rows fold away.
public struct FilterTable: View {
    var statuses: [TaskStatus]
    var rows: [FilterTableRow]
    var columns: [String]
    var allLabel: String

    @State private var filter: String?
    @Environment(\.buiTheme) private var theme

    private static let fractions: [CGFloat] = [1.3, 0.6, 0.95, 0.9].map { $0 / 3.75 }

    public init(statuses: [TaskStatus], rows: [FilterTableRow], columns: [String] = ["Task name", "Date", "Status", "Advisor"], allLabel: String = "All", filter: String? = nil) {
        self.statuses = statuses
        self.rows = rows
        self.columns = columns
        self.allLabel = allLabel
        _filter = State(initialValue: filter)
    }

    /// Rows that pass `filter` (`nil` keeps all).
    nonisolated static func visible(_ rows: [FilterTableRow], filter: String?) -> [FilterTableRow] {
        rows.filter { filter == nil || $0.statusID == filter }
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            ScrollView(.horizontal) {
                HStack(spacing: 4) {
                    chip(id: nil, label: allLabel, dot: nil, count: rows.count)
                    ForEach(statuses) { status in
                        chip(id: status.id, label: status.label, dot: status.tint, count: rows.filter { $0.statusID == status.id }.count)
                    }
                }
                .padding(4)
            }
            .scrollIndicators(.hidden)
            .padding(.horizontal, -4)

            ScrollView(.horizontal) {
                VStack(spacing: 0) {
                    BUIColumns(Self.fractions) {
                        ForEach(Array(columns.enumerated()), id: \.offset) { index, title in
                            Text(title)
                                .buiFont(BUITextStyle(size: 12.5, weight: .medium, lineHeight: 1.19))
                                .foregroundStyle(theme.colors.ink2)
                                .tableCell(rule: index < columns.count - 1, theme: theme)
                        }
                    }
                    .overlay(alignment: .bottom) { gridLine(.horizontal) }
                    ForEach(rows) { row in
                        let shown = filter == nil || row.statusID == filter
                        dataRow(row)
                            .frame(height: shown ? nil : 0, alignment: .top)
                            .opacity(shown ? 1 : 0)
                            .clipped()
                    }
                }
                .frame(minWidth: 420)
                .containerRelativeFrame(.horizontal) { width, _ in max(width, 420) }
            }
            .scrollIndicators(.hidden)
            .buiSurface(radius: theme.radii.card, elevation: .card)
        }
        .frame(maxWidth: 420)
        .animation(BUICurve.outStrong.animation(duration: 0.3), value: filter)
    }

    private func chip(id: String?, label: String, dot: Color?, count: Int) -> some View {
        BUIFilterChip(label, dot: dot, count: count, isActive: filter == id) { filter = id }
    }

    private func dataRow(_ row: FilterTableRow) -> some View {
        let status = statuses.first { $0.id == row.statusID }
        return BUIColumns(Self.fractions) {
            Text(row.task)
                .buiFont(BUITextStyle(size: 13, weight: .medium, lineHeight: 1.19))
                .foregroundStyle(theme.colors.ink)
                .lineLimit(1)
                .tableCell(rule: true, theme: theme)
            Text(row.date)
                .buiFont(BUITextStyle(size: 13, lineHeight: 1.19))
                .monospacedDigit()
                .foregroundStyle(theme.colors.ink2)
                .fixedSize()
                .tableCell(rule: true, theme: theme)
            BUIHueTag(status?.label ?? row.statusID, base: status?.tint ?? theme.colors.ink3)
                .tableCell(rule: true, theme: theme)
            Text(row.owner)
                .buiFont(BUITextStyle(size: 13, lineHeight: 1.19))
                .foregroundStyle(theme.colors.ink2)
                .lineLimit(1)
                .tableCell(rule: false, theme: theme)
        }
        .overlay(alignment: .bottom) { gridLine(.horizontal) }
    }

    private func gridLine(_ axis: Axis) -> some View {
        Rectangle()
            .fill(theme.colors.line.opacity(0.78))
            .frame(width: axis == .vertical ? 1 : nil, height: axis == .horizontal ? 1 : nil)
    }
}

private extension View {
    func tableCell(rule: Bool, theme: BUITheme) -> some View {
        padding(.horizontal, 12)
            .padding(.vertical, 8)
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .leading)
            .overlay(alignment: .trailing) {
                if rule { Rectangle().fill(theme.colors.line.opacity(0.78)).frame(width: 1) }
            }
    }
}

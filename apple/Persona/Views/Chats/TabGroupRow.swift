import SwiftUI

/// The row of a herdr tab holding several agents (#2320), over their rows in the Agents list: its most urgent agent's
/// state, the tab's name and what that agent is doing, 「N 个 Agent」 and the fold's chevron. Tapping it folds the tab's
/// agents away (kept per tab, `TabPanes.foldID`).
struct TabGroupRow: View {
    var group: TabPanes.Group
    var folded: Bool
    @Environment(\.compactList) private var compact

    var body: some View {
        let lead = group.lead
        HStack(spacing: compact ? CompactList.gap : Theme.Space.s) {
            StatusDot(state: lead.state, size: Theme.Size.dot)
                .scaledFrame(width: compact ? CompactList.mark : Theme.Size.iconXS)
            HStack(alignment: .firstTextBaseline, spacing: compact ? CompactList.gap : Theme.Space.sm) {
                Text(name)
                    .font(compact ? CompactList.metaStrong : Theme.Font.secondaryMedium)
                    .foregroundStyle(Color.primary)
                    .lineLimit(1)
                    .layoutPriority(1)
                Text(lead.displayTitle)
                    .font(compact ? CompactList.meta : Theme.Font.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
            }
            Spacer(minLength: Theme.Space.xs)
            Text(count)
                .font(compact ? CompactList.meta : Theme.Font.caption)
                .foregroundStyle(.secondary)
                .lineLimit(1)
                .fixedSize()
            FoldChevron(folded: folded)
        }
        .padding(.vertical, compact ? 0 : Theme.Space.xxs)
        .contentShape(.rect)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(["标签页 \(name)", count, lead.state.label, folded ? "已折叠" : "已展开"].joined(separator: "，"))
        .accessibilityHint(folded ? "展开这个标签页的 Agent" : "收起")
        .accessibilityAddTraits(.isButton)
    }

    private var name: String { group.label ?? "标签页" }
    private var count: String { "\(group.sessions.count) 个 Agent" }
}

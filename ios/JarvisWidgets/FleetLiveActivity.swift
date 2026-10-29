import ActivityKit
import SwiftUI
import WidgetKit

/// Draws the Fleet activity: lock screen (and the Watch Smart Stack's small family), Dynamic Island compact / minimal /
/// expanded. The compact island is two counts or numbers the device chose (IslandCell, 设置 › 通知 › 灵动岛); the expanded
/// island and the lock screen lead with the same counts, then the agents. Status colors are Jarvis' agent colors
/// (docs/agent-states.md), fixed rather than following the app's style: the extension cannot read it.
struct FleetLiveActivity: Widget {
    /// Agent rows that fit: the expanded island and the lock screen are both capped at about 160 pt.
    static let rows = 2

    var body: some WidgetConfiguration {
        ActivityConfiguration(for: JarvisActivityAttributes.self) { context in
            FleetLockScreen(state: context.state, stale: context.isStale)
                .widgetURL(FleetLink.whole(context.state))
                .activitySystemActionForegroundColor(FleetPalette.coral)
        } dynamicIsland: { context in
            let state = context.state
            return DynamicIsland {
                DynamicIslandExpandedRegion(.leading) {
                    HStack(spacing: 10) {
                        if state.needsYou > 0 { IslandCellView(cell: .needsYou(state.needsYou)) }
                        if state.working > 0 { IslandCellView(cell: .working(state.working)) }
                        if state.unread > 0 { IslandCellView(cell: .unread(state.unread)) }
                    }
                    .padding(.leading, 6)
                }
                DynamicIslandExpandedRegion(.trailing) {
                    FleetStatsLine(state: state, onDark: true).padding(.trailing, 6)
                }
                DynamicIslandExpandedRegion(.bottom) {
                    VStack(alignment: .leading, spacing: 8) {
                        if !context.isStale, let act = state.firstActionable {
                            // A request the buttons can settle takes the room: it, its buttons, and how many more.
                            Link(destination: FleetLink.url(act)!) { FleetRow(item: act, showTitle: true, onDark: true) }
                            FleetActionRow(item: act)
                            if state.hidden(showing: 1) > 0 {
                                Text(FleetText.more(state.hidden(showing: 1))).font(.caption).foregroundStyle(.white.opacity(0.6))
                                    .padding(.leading, 15)
                            }
                        } else {
                            ForEach(state.items.prefix(Self.rows)) { item in
                                Link(destination: FleetLink.url(item)!) { FleetRow(item: item, showTitle: true, onDark: true) }
                            }
                        }
                        if context.isStale || state.firstActionable == nil, state.hidden(showing: Self.rows) > 0 {
                            Text(FleetText.more(state.hidden(showing: Self.rows))).font(.caption).foregroundStyle(.white.opacity(0.6))
                                .padding(.leading, 15) // under the titles, clear of the rounded corner
                        }
                    }
                    .padding(.horizontal, 4)
                    .padding(.top, 4)
                }
            } compactLeading: {
                IslandCellView(cell: IslandCell.sides(state).left).padding(.leading, 2)
            } compactTrailing: {
                IslandCellView(cell: IslandCell.sides(state).right).padding(.trailing, 2)
            } minimal: {
                IslandCellView(cell: IslandCell.minimal(state), small: true)
            }
            .widgetURL(FleetLink.whole(state))
            .keylineTint(FleetPalette.coral)
        }
        .supplementalActivityFamilies([.small])
    }
}

// MARK: - Pieces

/// The coral four-point star on a dark disc, like the app icon.
struct FleetMark: View {
    var size: CGFloat
    var body: some View {
        Image(systemName: "sparkle")
            .font(.system(size: size * 0.6, weight: .bold))
            .foregroundStyle(FleetPalette.coral)
            .frame(width: size, height: size)
            .background(Circle().fill(Color(red: 0.12, green: 0.09, blue: 0.08)))
            .overlay(Circle().strokeBorder(FleetPalette.coral.opacity(0.45), lineWidth: 0.75))
    }
}

struct FleetTimer: View {
    var since: Date
    var body: some View {
        Text(timerInterval: since...Date.distantFuture, countsDown: false, showsHours: true)
            .monospacedDigit()
            .multilineTextAlignment(.trailing)
            .lineLimit(1)
    }
}

/// One agent: a colored dot, its name (or what kind of request, when names are hidden), what it wants.
struct FleetRow: View {
    var item: FleetState.Item
    var showTitle: Bool
    var onDark = false

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Circle().fill(FleetPalette.color(item.kind)).frame(width: 7, height: 7).alignmentGuide(.firstTextBaseline) { $0[.bottom] - 1 }
            VStack(alignment: .leading, spacing: 1) {
                if showTitle {
                    HStack(spacing: 4) {
                        Text(item.title).font(.subheadline.weight(.semibold)).lineLimit(1)
                        if item.kind == .unread, !item.detail.isEmpty {
                            // Finished: what it said, on the same line (rows must fit three in the island).
                            Text(item.detail).font(.caption).foregroundStyle(secondary).lineLimit(1)
                        } else if !item.place.isEmpty {
                            Text(item.place).font(.caption2).foregroundStyle(secondary).lineLimit(1)
                        }
                    }
                    if item.needsYou {
                        Text(FleetText.line(item)).font(.caption).foregroundStyle(secondary).lineLimit(1)
                    }
                } else {
                    Text(FleetText.anonymous(item)).font(.subheadline.weight(.medium)).lineLimit(1)
                }
            }
            Spacer(minLength: 4)
            if item.kind == .working, let since = item.sinceDate {
                FleetTimer(since: since).font(.caption.weight(.medium)).foregroundStyle(FleetPalette.working).frame(maxWidth: 60, alignment: .trailing)
            }
        }
        .foregroundStyle(onDark ? Color.white : Color.primary)
    }

    private var secondary: Color { onDark ? .white.opacity(0.65) : .secondary }
}

/// Counts: 「💬 2 需要你  ⚙ 3 在工作  ● 1 未看」, the first line of the lock screen (the island's symbols).
struct FleetCounts: View {
    var state: FleetState
    /// Numbers only (the symbols say which): when the island's numbers share the line.
    var short = false
    var body: some View {
        HStack(spacing: short ? 8 : 10) {
            if state.needsYou > 0 { chip(Image(systemName: "exclamationmark.bubble.fill"), "\(state.needsYou)", "需要你", FleetPalette.needsYou) }
            if state.working > 0 { chip(Image(systemName: "gearshape.fill"), "\(state.working)", "在工作", FleetPalette.working) }
            if state.unread > 0 { chip(Image(systemName: "circle.fill").font(.system(size: 7)), "\(state.unread)", "未看", FleetPalette.unread) }
        }
        .layoutPriority(1)
    }

    private func chip(_ symbol: some View, _ count: String, _ words: String, _ color: Color) -> some View {
        HStack(spacing: 4) {
            symbol.foregroundStyle(color).font(.footnote)
            Text(short ? count : count + " " + words).font(.subheadline.weight(.semibold)).monospacedDigit().lineLimit(1)
        }
        .fixedSize()
    }
}

/// The numbers the device chose for its island (TPM, today's tokens, load, the assistant), right of the counts.
struct FleetStatsLine: View {
    var state: FleetState
    var onDark = false

    static func count(_ s: FleetState) -> Int { [s.left, s.right].filter { $0 != .status && $0 != .none }.count }

    var body: some View {
        let cells = [state.left, state.right].filter { $0 != .status && $0 != .none }.map { IslandCell.cell($0, state, status: nil) }
        HStack(spacing: 8) {
            ForEach(Array(cells.enumerated()), id: \.offset) { _, cell in
                if case let .timer(since) = cell {
                    FleetTimer(since: since).font(.caption.weight(.semibold)).foregroundStyle(FleetPalette.working).frame(maxWidth: 52, alignment: .trailing)
                } else if let text = FleetText.stat(cell) {
                    Text(text).font(.caption.weight(.semibold)).monospacedDigit().lineLimit(1).minimumScaleFactor(0.8)
                        .foregroundStyle(onDark ? Color.white.opacity(0.75) : Color.secondary)
                }
            }
        }
    }
}

/// 拒绝 / 允许 for a permission, 查看 / 批准 for a plan (rejecting a plan needs words: that is the app's job).
/// Hidden while the content is stale, so nobody approves something the agent has moved past.
struct FleetActionRow: View {
    var item: FleetState.Item

    var body: some View {
        if let id = item.interaction {
            HStack(spacing: 10) {
                switch item.kind {
                case .permission:
                    Button(intent: DenyPermissionIntent(interactionID: id)) { label("拒绝", "xmark") }
                        .tint(Color.gray.opacity(0.45))
                    Button(intent: AllowPermissionIntent(interactionID: id)) { label("允许", "checkmark") }
                        .tint(FleetPalette.coral)
                case .plan:
                    Link(destination: FleetLink.url(item)!) {
                        label("查看", "doc.text")
                            .padding(.vertical, 7)
                            .background(Color.gray.opacity(0.45), in: .capsule)
                    }
                    Button(intent: ApprovePlanIntent(interactionID: id)) { label("批准", "checkmark") }
                        .tint(FleetPalette.coral)
                default:
                    EmptyView()
                }
            }
            .buttonStyle(.borderedProminent)
            .buttonBorderShape(.capsule)
            .foregroundStyle(.white)
            .padding(.leading, 15)
        }
    }

    private func label(_ text: String, _ symbol: String) -> some View {
        Label(text, systemImage: symbol)
            .font(.subheadline.weight(.semibold))
            .lineLimit(1)
            .frame(maxWidth: .infinity)
    }
}

// MARK: - Lock screen

struct FleetLockScreen: View {
    var state: FleetState
    var stale: Bool
    @Environment(\.activityFamily) private var family

    var body: some View {
        if family == .small {
            small
        } else {
            full
        }
    }

    private var full: some View {
        VStack(alignment: .leading, spacing: 9) {
            HStack(spacing: 10) {
                FleetMark(size: 22)
                FleetCounts(state: state, short: FleetStatsLine.count(state) > 1)
                Spacer(minLength: 0)
                FleetStatsLine(state: state)
            }
            if !stale, let act = state.firstActionable {
                // 允许 / 批准 need Face ID first (authenticationRequired); 拒绝 works locked.
                FleetRow(item: act, showTitle: state.details)
                FleetActionRow(item: act)
            } else if state.details {
                ForEach(state.items.prefix(FleetLiveActivity.rows)) { FleetRow(item: $0, showTitle: true) }
            } else if let first = state.items.first, first.needsYou {
                FleetRow(item: first, showTitle: false)
            }
            if stale {
                Label("等待 hub 更新…", systemImage: "wifi.exclamationmark").font(.caption2).foregroundStyle(.secondary)
            } else if state.details, state.firstActionable == nil, state.hidden(showing: FleetLiveActivity.rows) > 0 {
                Text(FleetText.more(state.hidden(showing: FleetLiveActivity.rows))).font(.caption).foregroundStyle(.secondary)
                    .padding(.leading, 15)
            }
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 14)
    }

    /// Apple Watch Smart Stack: one glance.
    private var small: some View {
        HStack(spacing: 8) {
            IslandCellView(cell: IslandCell.minimal(state)).font(.title3)
            VStack(alignment: .leading, spacing: 0) {
                Text(FleetText.headline(state)).font(.headline).lineLimit(1).minimumScaleFactor(0.8)
                if state.phase == .needsYou, state.working > 0 {
                    Text("\(state.working) 个在工作").font(.caption).foregroundStyle(.secondary)
                } else if state.phase != .done, state.unread > 0 {
                    Text("\(state.unread) 个未看").font(.caption).foregroundStyle(.secondary)
                }
            }
            Spacer(minLength: 0)
        }
        .padding(10)
    }
}

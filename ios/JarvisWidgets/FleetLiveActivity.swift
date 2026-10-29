import ActivityKit
import SwiftUI
import WidgetKit

/// Draws the Fleet activity: lock screen (and the Watch Smart Stack's small family), Dynamic Island compact / minimal /
/// expanded. Status colors are Jarvis' agent colors (docs/agent-states.md), fixed rather than following the app's
/// style: the extension cannot read it. The mark is the app icon's coral four-point star.
struct FleetLiveActivity: Widget {
    /// Agent rows that fit: the expanded island and the lock screen are both capped at about 160 pt.
    static let rows = 2

    var body: some WidgetConfiguration {
        ActivityConfiguration(for: JarvisActivityAttributes.self) { context in
            FleetLockScreen(state: context.state, stale: context.isStale)
                .widgetURL(FleetLink.url(context.state.items.first))
                .activitySystemActionForegroundColor(FleetPalette.coral)
        } dynamicIsland: { context in
            let state = context.state
            return DynamicIsland {
                DynamicIslandExpandedRegion(.leading) {
                    HStack(spacing: 6) {
                        FleetMark(size: 18)
                        Text(FleetText.headline(state)).font(.subheadline.weight(.semibold)).lineLimit(1)
                    }
                    .padding(.leading, 4)
                }
                DynamicIslandExpandedRegion(.trailing) {
                    if let since = state.workingSinceDate {
                        FleetTimer(since: since).font(.subheadline.weight(.medium)).foregroundStyle(FleetPalette.working)
                            .frame(maxWidth: 64, alignment: .trailing).padding(.trailing, 4)
                    }
                }
                DynamicIslandExpandedRegion(.bottom) {
                    VStack(alignment: .leading, spacing: 8) {
                        ForEach(state.items.prefix(Self.rows)) { item in
                            Link(destination: FleetLink.url(item)!) { FleetRow(item: item, showTitle: true, onDark: true) }
                        }
                        if state.hidden(showing: Self.rows) > 0 {
                            Text(FleetText.more(state.hidden(showing: Self.rows))).font(.caption).foregroundStyle(.white.opacity(0.6))
                                .padding(.leading, 15) // under the titles, clear of the rounded corner
                        }
                    }
                    .padding(.horizontal, 4)
                    .padding(.top, 4)
                }
            } compactLeading: {
                FleetGlyph(phase: state.phase).padding(.leading, 2)
            } compactTrailing: {
                FleetCompactTrailing(state: state)
            } minimal: {
                FleetGlyph(phase: state.phase)
            }
            .widgetURL(FleetLink.url(state.items.first))
            .keylineTint(FleetPalette.coral)
        }
        .supplementalActivityFamilies([.small])
    }
}

/// Jarvis' agent colors (ios/Shared/Theme.swift), copied: the extension does not build the app's Theme.
enum FleetPalette {
    static let needsYou = Color(light: 0xE5484D, dark: 0xFF6369)
    static let working = Color(light: 0xE8890C, dark: 0xFFB224)
    static let unread = Color(light: 0x30A46C, dark: 0x4CC38A)
    static let coral = Color(light: 0xD4683F, dark: 0xE8835C)

    static func color(_ phase: FleetState.Phase) -> Color {
        switch phase {
        case .needsYou: needsYou
        case .working: working
        case .done: unread
        case .none: .secondary
        }
    }

    static func color(_ kind: FleetState.Item.Kind) -> Color {
        switch kind {
        case .working: working
        case .unread: unread
        default: needsYou
        }
    }
}

extension Color {
    init(light: UInt32, dark: UInt32) {
        self.init(uiColor: UIColor { $0.userInterfaceStyle == .dark ? UIColor(rgb: dark) : UIColor(rgb: light) })
    }
}

extension UIColor {
    convenience init(rgb: UInt32) {
        self.init(red: CGFloat((rgb >> 16) & 0xFF) / 255, green: CGFloat((rgb >> 8) & 0xFF) / 255, blue: CGFloat(rgb & 0xFF) / 255, alpha: 1)
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

/// One symbol per phase: needs you pulses red, working is the coral star, finished is a green check.
struct FleetGlyph: View {
    var phase: FleetState.Phase
    var body: some View {
        switch phase {
        case .needsYou:
            Image(systemName: "exclamationmark.bubble.fill").foregroundStyle(FleetPalette.needsYou)
                .symbolEffect(.pulse, options: .repeating)
        case .working:
            Image(systemName: "sparkle").foregroundStyle(FleetPalette.coral)
        case .done:
            Image(systemName: "checkmark.circle.fill").foregroundStyle(FleetPalette.unread)
        case .none:
            Image(systemName: "sparkle").foregroundStyle(.secondary)
        }
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

/// Right of the camera: how many need you, the working clock, or how many finished.
struct FleetCompactTrailing: View {
    var state: FleetState
    var body: some View {
        switch state.phase {
        case .needsYou:
            Text("\(state.needsYou)").font(.body.weight(.bold)).monospacedDigit().foregroundStyle(FleetPalette.needsYou)
        case .working:
            if let since = state.workingSinceDate {
                FleetTimer(since: since).font(.caption.weight(.semibold)).foregroundStyle(FleetPalette.working).frame(maxWidth: 46)
            } else {
                Text("\(state.working)").font(.body.weight(.bold)).foregroundStyle(FleetPalette.working)
            }
        case .done:
            Text("\(state.unread)").font(.body.weight(.bold)).monospacedDigit().foregroundStyle(FleetPalette.unread)
        case .none:
            EmptyView()
        }
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

/// Counts: 「● 2 需要你  ✦ 3 在工作  ✓ 1 做完了」, the first line of the lock screen.
struct FleetCounts: View {
    var state: FleetState
    var body: some View {
        HStack(spacing: 12) {
            if state.needsYou > 0 { chip("exclamationmark.bubble.fill", "\(state.needsYou) 需要你", FleetPalette.needsYou) }
            if state.working > 0 { chip("sparkle", "\(state.working) 在工作", FleetPalette.working) }
            if state.unread > 0 { chip("checkmark.circle.fill", "\(state.unread) 做完了", FleetPalette.unread) }
            Spacer(minLength: 0)
        }
    }

    private func chip(_ symbol: String, _ text: String, _ color: Color) -> some View {
        HStack(spacing: 4) {
            Image(systemName: symbol).foregroundStyle(color).font(.footnote)
            Text(text).font(.subheadline.weight(.semibold)).lineLimit(1).minimumScaleFactor(0.8)
        }
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
                FleetCounts(state: state)
                if state.phase == .working, let since = state.workingSinceDate {
                    FleetTimer(since: since).font(.subheadline.weight(.medium)).foregroundStyle(FleetPalette.working).frame(width: 64, alignment: .trailing)
                }
            }
            if state.details {
                ForEach(state.items.prefix(FleetLiveActivity.rows)) { FleetRow(item: $0, showTitle: true) }
            } else if let first = state.items.first, first.needsYou {
                FleetRow(item: first, showTitle: false)
            }
            if stale {
                Label("等待 hub 更新…", systemImage: "wifi.exclamationmark").font(.caption2).foregroundStyle(.secondary)
            } else if state.details, state.hidden(showing: FleetLiveActivity.rows) > 0 {
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
            FleetGlyph(phase: state.phase).font(.title3)
            VStack(alignment: .leading, spacing: 0) {
                Text(FleetText.headline(state)).font(.headline).lineLimit(1).minimumScaleFactor(0.8)
                if let since = state.workingSinceDate, state.phase == .working {
                    FleetTimer(since: since).font(.caption).foregroundStyle(.secondary).multilineTextAlignment(.leading)
                } else if state.working > 0 {
                    Text("\(state.working) 个在工作").font(.caption).foregroundStyle(.secondary)
                }
            }
            Spacer(minLength: 0)
        }
        .padding(10)
    }
}

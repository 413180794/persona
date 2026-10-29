import SwiftUI

/// Jarvis' agent colors: the dots of StatusPalette (Shared/StatusPalette.swift, which the extension builds too; it does
/// not build the app's Theme). Shared by the widget and the island preview in 设置 › 通知 › 灵动岛.
enum FleetPalette {
    static var needsYou: Color { StatusPalette.needsYou.dot }
    static var working: Color { StatusPalette.working.dot }
    static var unread: Color { StatusPalette.unread.dot }
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

/// One side of the compact Dynamic Island: a symbol and a short number. Counts take their state's color; the
/// numbers (TPM, tokens…) stay white beside a coral symbol. `small` is the minimal island's circle.
struct IslandCellView: View {
    var cell: IslandCell
    var small = false

    var body: some View {
        switch cell {
        case let .needsYou(n):
            pair(Image(systemName: "exclamationmark.bubble.fill"), "\(n)", FleetPalette.needsYou, number: FleetPalette.needsYou)
        case let .working(n):
            // Static: Live Activities do not run repeating symbol effects (a .rotate gear stayed still, 2026-10-29).
            pair(Image(systemName: "gearshape.fill"), "\(n)", FleetPalette.working, number: FleetPalette.working)
        case let .unread(n):
            pair(Image(systemName: "circle.fill").font(.system(size: small ? 6 : 8)), "\(n)", FleetPalette.unread, number: FleetPalette.unread)
        case let .tpm(n):
            pair(Image(systemName: "bolt.fill"), FleetText.short(n), FleetPalette.coral)
        case let .tokens(n):
            pair(Image(systemName: "chart.bar.fill"), FleetText.short(n), FleetPalette.coral)
        case let .requests(n):
            pair(Image(systemName: "arrow.up.arrow.down"), FleetText.short(n), FleetPalette.coral)
        case let .assistant(on):
            Image(systemName: "sparkle")
                .font(.system(size: small ? 12 : 15, weight: .bold))
                .foregroundStyle(on ? FleetPalette.coral : Color.white.opacity(0.4))
        case let .load(_, p):
            pair(Image(systemName: "cpu"), "\(p)%", FleetPalette.coral)
        case let .timer(since):
            HStack(spacing: 3) {
                Image(systemName: "timer").font(.system(size: 11, weight: .bold)).foregroundStyle(FleetPalette.working)
                Text(timerInterval: since...Date.distantFuture, countsDown: false, showsHours: false)
                    .font(.system(size: 13, weight: .semibold)).monospacedDigit().lineLimit(1)
                    .frame(maxWidth: 40, alignment: .leading)
            }
        case .empty:
            EmptyView()
        }
    }

    private func pair(_ symbol: some View, _ text: String, _ tint: Color, number: Color = .white) -> some View {
        HStack(spacing: small ? 1 : 3) {
            symbol.font(.system(size: small ? 10 : 13, weight: .bold)).foregroundStyle(tint)
            Text(text).font(.system(size: small ? 12 : 15, weight: .bold, design: .rounded)).monospacedDigit()
                .foregroundStyle(number).lineLimit(1).minimumScaleFactor(0.7)
        }
    }
}

/// The compact island as a picture, for 设置 › 通知 › 灵动岛: the two sides around a camera on a black pill.
struct IslandPreview: View {
    var state: FleetState

    var body: some View {
        let sides = IslandCell.sides(state)
        HStack(spacing: 0) {
            IslandCellView(cell: sides.left).frame(maxWidth: .infinity, alignment: .leading)
            Capsule().fill(Color(white: 0.12)).frame(width: 70, height: 22) // where the camera sits
            IslandCellView(cell: sides.right).frame(maxWidth: .infinity, alignment: .trailing)
        }
        .padding(.horizontal, 14)
        .frame(width: 230, height: 36)
        .background(Capsule().fill(.black))
        .environment(\.colorScheme, .dark)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(IslandPreview.describe(sides.left) + "，" + IslandPreview.describe(sides.right))
    }

    static func describe(_ cell: IslandCell) -> String {
        switch cell {
        case let .needsYou(n): "\(n) 个需要你"
        case let .working(n): "\(n) 个在工作"
        case let .unread(n): "\(n) 个做完未看"
        case .timer: "计时"
        case .empty: "空"
        default: FleetText.stat(cell) ?? ""
        }
    }
}

import SwiftUI

/// A small tag after a chat's name: 外部, 机器人, 公开, 会议…
struct LarkNameTag: View {
    var text: String

    var body: some View {
        let hue: Color = switch text {
        case "机器人", "AI": LarkColor.hue("orange")
        case "公开": LarkColor.hue("green")
        default: LarkColor.blue
        }
        Text(text)
            .font(Theme.Font.labelMedium)
            .foregroundStyle(hue)
            .lineLimit(1)
            .padding(.horizontal, Theme.Space.xs)
            .padding(.vertical, 1.5)
            .background(hue.opacity(0.13), in: .rect(cornerRadius: 4, style: .continuous))
            .fixedSize()
    }
}

/// Feishu's list times, used by the inbox rows and the doc reader.
nonisolated enum LarkTime {
    /// Feishu's feed times: 05:59, 昨天, 星期三, 10月21日, 2025/10/21.
    static func short(_ d: Date?, now: Date = .now) -> String {
        guard let d, d.isMeaningful else { return "" }
        let cal = Calendar.current
        if cal.isDateInToday(d) { return d.formatted(.dateTime.hour(.twoDigits(amPM: .omitted)).minute(.twoDigits)) }
        if cal.isDateInYesterday(d) { return "昨天" }
        if now.timeIntervalSince(d) < 6 * 86400 { return d.formatted(.dateTime.weekday(.wide)) }
        if cal.isDate(d, equalTo: now, toGranularity: .year) { return d.formatted(.dateTime.month().day()) }
        return d.formatted(.dateTime.year().month(.twoDigits).day(.twoDigits))
    }
}

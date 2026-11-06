import Foundation
import Testing
@testable import Persona

/// Where a Lark chat puts its time separators and avatars (`LarkChatLayout`), with a fixed clock and calendar.
@MainActor struct LarkChatLayoutTests {
    private static let cal: Calendar = {
        var c = Calendar(identifier: .gregorian)
        c.timeZone = TimeZone(identifier: "Asia/Shanghai")!
        return c
    }()

    /// A time in the calendar's time zone.
    private func at(_ y: Int, _ mo: Int, _ d: Int, _ h: Int, _ mi: Int) -> Date {
        Self.cal.date(from: DateComponents(year: y, month: mo, day: d, hour: h, minute: mi))!
    }

    /// 2026-11-04 (a Wednesday), the day `now` is in.
    private func at(_ h: Int, _ mi: Int, _ s: Int = 0) -> Date {
        Self.cal.date(from: DateComponents(year: 2026, month: 11, day: 4, hour: h, minute: mi, second: s))!
    }

    private func msg(_ id: String, from name: String, uid: String? = nil, mine: Bool = false, at d: Date?, type: String = "TEXT") -> LarkMessage {
        LarkMessage(id: id, position: id, senderID: uid, senderName: name, isMine: mine, type: type, text: "hi", createdAt: d,
                    recalled: false, replyCount: 0)
    }

    private var now: Date { at(15, 0) }

    @Test func theSeparatorWordsFollowFeishusTiers() {
        let label = { (d: Date) in LarkChatLayout.dayLabel(d, now: self.now, calendar: Self.cal) }
        #expect(label(at(9, 5)) == "09:05")
        #expect(label(at(2026, 11, 3, 23, 59)) == "昨天 23:59")
        #expect(label(at(2026, 11, 2, 14, 32)) == "星期一 14:32")
        #expect(label(at(2026, 10, 29, 8, 0)) == "星期四 08:00", "six days back is still a weekday")
        #expect(label(at(2026, 10, 28, 8, 0)) == "10月28日 08:00", "a week back: the date")
        #expect(label(at(2026, 1, 1, 0, 0)) == "1月1日 00:00")
        #expect(label(at(2025, 12, 31, 23, 59)) == "2025年12月31日 23:59", "last year")
        #expect(LarkChatLayout.dayLabel(nil, now: now, calendar: Self.cal) == "")
    }

    @Test func theListAndTheSeparatorsShareTheTiers() {
        let tier = { (d: Date) in LarkTime.tier(of: d, now: self.now, calendar: Self.cal) }
        #expect(tier(at(0, 0)) == .today)
        #expect(tier(at(2026, 11, 3, 0, 0)) == .yesterday)
        #expect(tier(at(2026, 11, 2, 23, 59)) == .thisWeek)
        #expect(tier(at(2026, 10, 29, 0, 0)) == .thisWeek)
        #expect(tier(at(2026, 10, 28, 23, 59)) == .thisYear)
        #expect(tier(at(2025, 12, 31, 23, 59)) == .older)
        // Just after midnight on New Year's day: yesterday is last year, still Yesterday.
        let jan1 = at(2027, 1, 1, 0, 10)
        #expect(LarkTime.tier(of: at(2026, 12, 31, 23, 50), now: jan1, calendar: Self.cal) == .yesterday)
        #expect(LarkTime.tier(of: at(2026, 12, 20, 9, 0), now: jan1, calendar: Self.cal) == .older)
        #expect(LarkChatLayout.dayLabel(at(2026, 12, 31, 23, 50), now: jan1, calendar: Self.cal) == "昨天 23:50")
    }

    @Test func aSeparatorAfterFiveQuietMinutesOrANewDay() {
        let a = msg("1", from: "Tom", uid: "u1", at: at(10, 0))
        let sep = { (m: LarkMessage, p: LarkMessage?) in LarkChatLayout.showsTimeSeparator(m, after: p, calendar: Self.cal) }
        #expect(sep(a, nil), "the first message has its time")
        #expect(!sep(msg("2", from: "Tom", uid: "u1", at: at(10, 4, 59)), a))
        #expect(sep(msg("2", from: "Tom", uid: "u1", at: at(10, 5)), a), "five minutes exactly")
        // Two minutes apart across midnight: a new day all the same.
        let late = msg("3", from: "Tom", uid: "u1", at: at(2026, 11, 3, 23, 59))
        let early = msg("4", from: "Tom", uid: "u1", at: at(0, 1))
        #expect(LarkChatLayout.startsDay(early, after: late, calendar: Self.cal))
        #expect(sep(early, late))
        #expect(!LarkChatLayout.continues(early, after: late, calendar: Self.cal), "a run does not cross a separator")
        // Across a year.
        let eve = msg("5", from: "Tom", uid: "u1", at: at(2025, 12, 31, 23, 58))
        let ny = msg("6", from: "Tom", uid: "u1", at: at(2026, 1, 1, 0, 0))
        #expect(sep(ny, eve) && LarkChatLayout.startsDay(ny, after: eve, calendar: Self.cal))
        #expect(!sep(msg("7", from: "Tom", at: nil), a), "no time, no separator")
    }

    @Test func runsGroupBySenderID() {
        let a = msg("1", from: "李雷", uid: "u1", at: at(10, 0))
        let c = { (m: LarkMessage, p: LarkMessage?) in LarkChatLayout.continues(m, after: p, calendar: Self.cal) }
        #expect(!c(a, nil))
        #expect(c(msg("2", from: "李雷", uid: "u1", at: at(10, 1)), a))
        #expect(!c(msg("2", from: "李雷", uid: "u2", at: at(10, 1)), a), "same name, another person")
        #expect(c(msg("2", from: "李雷（新名字）", uid: "u1", at: at(10, 1)), a), "renamed, same person")
        #expect(c(msg("2", from: "李雷", at: at(10, 1)), a), "no id: by name")
        #expect(!c(msg("2", from: "韩梅梅", at: at(10, 1)), a))
        #expect(!c(msg("2", from: "李雷", uid: "u1", at: at(10, 6)), a), "after a separator: avatar again")
        #expect(!c(msg("2", from: "李雷", uid: "u1", at: at(10, 1), type: "SYSTEM"), a), "system lines stand alone")
        #expect(!c(msg("2", from: "李雷", uid: "u1", at: nil), a), "no time, no run")
    }

    @Test func myMessagesAreOneSenderEvenOnTheirWay() {
        let sent = msg("1", from: "王小明", uid: "u-me", mine: true, at: at(10, 0))
        let onItsWay = LarkOutgoing(text: "hi", at: at(10, 1)).message(like: nil) // Me, no id: no message of mine loaded
        #expect(LarkChatLayout.continues(onItsWay, after: sent, calendar: Self.cal))
        #expect(!LarkChatLayout.continues(onItsWay, after: msg("2", from: "我", uid: "u-other", at: at(10, 0)), calendar: Self.cal),
                "someone else called 我 is not me")
    }

    @Test func mineGoRightWithoutMyAvatarOrName() {
        let mine = msg("1", from: "王小明", uid: "u-me", mine: true, at: at(10, 0))
        let theirs = msg("2", from: "Tom", uid: "u1", at: at(10, 0))
        let system = msg("3", from: "", mine: true, at: at(10, 0), type: "SYSTEM")
        #expect(LarkChatLayout.side(of: mine) == .right)
        #expect(LarkChatLayout.side(of: theirs) == .left)
        #expect(LarkChatLayout.side(of: system) == .left, "system lines are centred, never pushed right")
        #expect(LarkChatLayout.side(of: LarkOutgoing(text: "hi").message(like: nil)) == .right, "on its way: right too")
        #expect(!LarkChatLayout.showsAvatar(mine, grouped: false) && !LarkChatLayout.showsName(mine, grouped: false, isGroup: true))
        #expect(LarkChatLayout.showsAvatar(theirs, grouped: false) && LarkChatLayout.showsName(theirs, grouped: false, isGroup: true))
        #expect(!LarkChatLayout.showsAvatar(theirs, grouped: true) && !LarkChatLayout.showsName(theirs, grouped: true, isGroup: true))
        #expect(LarkChatLayout.showsAvatar(theirs, grouped: false) && !LarkChatLayout.showsName(theirs, grouped: false, isGroup: false),
                "one-to-one: avatar, no name")
    }
}

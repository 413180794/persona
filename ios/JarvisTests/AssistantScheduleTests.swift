import Foundation
import Testing
@testable import Jarvis

/// Jarvis › 定时任务: the rules in words, the times, and what runs next (docs/assistant.md).
@MainActor
struct AssistantScheduleTests {
    private func routine(_ time: String, _ days: [Int], enabled: Bool = true, next: Date? = nil) -> APIClient.Routine {
        var r = APIClient.Routine(id: "r", title: "早报", prompt: "x", time: time, weekdays: days, enabled: enabled)
        r.nextRun = next
        return r
    }

    @Test func rulesInWords() {
        #expect(AssistantScheduleText.sentence(routine("09:00", [1, 2, 3, 4, 5])) == "每个工作日 上午 9:00")
        #expect(AssistantScheduleText.sentence(routine("21:30", [])) == "每天 晚上 9:30")
        #expect(AssistantScheduleText.sentence(routine("17:00", [5])) == "每周五 下午 5:00")
        #expect(AssistantScheduleText.sentence(routine("08:15", [6, 7])) == "每个周末 早上 8:15")
        #expect(AssistantScheduleText.sentence(routine("00:05", [1, 3])) == "每周一、三 凌晨 12:05")
        #expect(AssistantScheduleText.clock("12:30") == "中午 12:30")
    }

    /// 17:00 is "17:00": the system's two-digit hour without AM/PM is 12-hour in a Chinese locale ("05:00").
    @Test func timesAre24Hour() {
        let d = Calendar.current.date(bySettingHour: 17, minute: 5, second: 0, of: .now)!
        #expect(AssistantScheduleText.hm(d) == "17:05")
        #expect(AssistantScheduleText.moment(d) == "今天 17:05")
    }

    @Test func upcomingMergesRoutinesAndFollowupsSoonestFirst() {
        let now = Date.now
        let routines = [routine("09:00", [], next: now.addingTimeInterval(7200)),
                        routine("10:00", [], enabled: false, next: now.addingTimeInterval(60))]
        let followups = [APIClient.Followup(id: "f", at: now.addingTimeInterval(600), title: nil, prompt: "看看 CI\n细节",
                                            machineID: "m", sessionID: "s")]
        let up = AssistantUpcoming.merge(routines: routines, followups: followups)
        #expect(up.map(\.id) == ["f-f", "r-r"]) // the switched-off routine is not coming up
        #expect(up[0].title == "看看 CI")
        #expect(up[0].key == SessionKey(machineID: "m", sessionID: "s"))
    }

    @Test func runDetail() {
        let due = Date(timeIntervalSince1970: 1_000_000)
        func run(_ late: TimeInterval, _ status: String = "ok", manual: Bool? = nil) -> APIClient.ScheduleRun {
            APIClient.ScheduleRun(id: "x", kind: "routine", refID: "r", title: "早报", due: due, at: due.addingTimeInterval(late),
                                  status: status, error: status == "failed" ? "offline" : nil, manual: manual)
        }
        #expect(AssistantRunText.detail(run(0.4)) == "准时")
        #expect(AssistantRunText.detail(run(21)) == "晚了 21 秒")
        #expect(AssistantRunText.detail(run(40 * 60)) == "晚了 40 分钟")
        #expect(AssistantRunText.detail(run(0, "failed")) == "offline")
        #expect(AssistantRunText.detail(run(0, manual: true)) == "手动运行")
    }
}

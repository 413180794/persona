import Foundation
import Testing
@testable import Jarvis

/// Jarvis › 定时任务: the rules in words, the times, and what runs next (docs/assistant.md).
@MainActor
struct AssistantScheduleTests {
    private func routine(_ time: String, _ days: [Int], enabled: Bool = true, next: Date? = nil) -> APIClient.ScheduledTask {
        var r = APIClient.ScheduledTask(id: "r", title: "早报", prompt: "x", time: time, weekdays: days, enabled: enabled)
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
        #expect(AssistantScheduleText.sentence(.init(title: "巡检", prompt: "x", repeats: .interval, everyMinutes: 30)) == "每 30 分钟")
        #expect(AssistantScheduleText.every(120) == "每 2 小时")
        #expect(AssistantScheduleText.every(90) == "每 1 小时 30 分钟")
    }

    @Test func whatATaskRunsWith() {
        var t = APIClient.ScheduledTask(title: "巡检", prompt: "x", repeats: .interval, everyMinutes: 30)
        #expect(AssistantScheduleText.runsWith(t) == nil, "the defaults say nothing")
        t.launcher = "claude-fast"
        t.permissionMode = "acceptEdits"
        t.effort = "low"
        #expect(AssistantScheduleText.runsWith(t)?.text == "claude-fast · 自动改文件 · 思考低")
        #expect(AssistantScheduleText.runsWith(t)?.danger == false)
        t.permissionMode = "bypassPermissions"
        #expect(AssistantScheduleText.runsWith(t)?.danger == true)
    }

    /// 17:00 is "17:00": the system's two-digit hour without AM/PM is 12-hour in a Chinese locale ("05:00").
    @Test func timesAre24Hour() {
        let d = Calendar.current.date(bySettingHour: 17, minute: 5, second: 0, of: .now)!
        #expect(AssistantScheduleText.hm(d) == "17:05")
        #expect(AssistantScheduleText.moment(d) == "今天 17:05")
    }

    @Test func upcomingMergesEveryKindSoonestFirst() {
        let now = Date.now
        var off = routine("10:00", [], enabled: false, next: now.addingTimeInterval(60))
        off.id = "off"
        var f = APIClient.ScheduledTask(id: "f", title: "", prompt: "看看 CI\n细节", repeats: .once, at: now.addingTimeInterval(600))
        f.machineID = "m"
        f.sessionID = "s"
        var p = APIClient.ScheduledTask(id: "p", title: "巡检", prompt: "x", repeats: .interval, everyMinutes: 30)
        p.nextRun = now.addingTimeInterval(1200)
        let up = AssistantUpcoming.merge([routine("09:00", [], next: now.addingTimeInterval(7200)), off, f, p])
        #expect(up.map(\.id) == ["t-f", "t-p", "t-r"]) // the switched-off one is not coming up
        #expect(up[1].detail == "每 30 分钟")
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

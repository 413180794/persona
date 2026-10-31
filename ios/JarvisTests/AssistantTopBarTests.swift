import Foundation
import Testing
import UIKit
@testable import Jarvis

struct AssistantTopBarTests {
    @Test func copyInfoHasWhatAnotherAgentNeeds() {
        let t = AssistantCopyText.info(title: "早报", isMain: false, sessionID: "abc-123", machine: "Server", machineID: "server",
                                       cwd: "/home/me/.jarvis/assistant", launcher: "claude", model: "Opus")
        #expect(t == """
        Jarvis 助手的对话 「早报」
        会话 ID：abc-123
        所在机器：Server（server）
        工作目录：/home/me/.jarvis/assistant
        启动器：claude · 模型：Opus
        在那台机器上接着聊：cd /home/me/.jarvis/assistant && claude --resume abc-123
        """)
        // The new-conversation page: no session lines.
        let home = AssistantCopyText.info(title: nil, isMain: false, sessionID: nil, machine: nil, machineID: "server",
                                          cwd: "~/.jarvis/assistant", launcher: "claude", model: nil)
        #expect(home == "Jarvis 助手（hub 上的 Claude）\n所在机器：server\n工作目录：~/.jarvis/assistant\n启动器：claude")
    }

    @Test func dragSettles() {
        #expect(AssistantDrawer.settlesOpen(progress: 0.6, velocity: 0, width: 300))
        #expect(!AssistantDrawer.settlesOpen(progress: 0.4, velocity: 0, width: 300))
        #expect(AssistantDrawer.settlesOpen(progress: 0.2, velocity: 900, width: 300)) // flicked open
        #expect(!AssistantDrawer.settlesOpen(progress: 0.9, velocity: -900, width: 300)) // flicked shut
        #expect(AssistantDrawer.settlesOpen(progress: 0.4, velocity: 400, width: 300)) // still moving towards open
        #expect(AssistantDrawer.settlesOpen(progress: 0.22, velocity: 495, width: 360)) // a short quick flick (the simulator's)
        #expect(!AssistantDrawer.settlesOpen(progress: 0.25, velocity: 212, width: 360)) // half way, let go slowly: back
    }

    /// The drawer takes a touch within 45° of horizontal — a thumb's slant — and while closed only to the right.
    @Test func dragDirection() {
        #expect(AssistantDrawer.takes(dx: 11, dy: 6, isOpen: false)) // 30°: the drag that did nothing before
        #expect(AssistantDrawer.takes(dx: 10, dy: -8, isOpen: false)) // 40° upwards
        #expect(!AssistantDrawer.takes(dx: 6, dy: 10, isOpen: false)) // steeper: the list scrolls
        #expect(!AssistantDrawer.takes(dx: -12, dy: 2, isOpen: false)) // to the left while closed
        #expect(AssistantDrawer.takes(dx: -12, dy: 2, isOpen: true)) // closing
    }

    @Test func searchMatchesTitleOrLastMessage() throws {
        func s(_ id: String, _ title: String, _ last: String?) throws -> Session {
            var o: [String: Any] = ["session_id": id, "status": "idle", "title": title, "assistant": true]
            if let last { o["last_text"] = last }
            return try JSON.decoder.decode(Session.self, from: JSONSerialization.data(withJSONObject: o))
        }
        let all = [try s("1", "早报 · 周三", "两个会议"), try s("2", "Weekly Report", nil), try s("3", "吃饭", "已经加到日历")]
        #expect(AssistantSidebarSearch.filter(all, "日历").map(\.sessionID) == ["3"])
        #expect(AssistantSidebarSearch.filter(all, "ｗｅｅｋｌｙ").map(\.sessionID) == ["2"]) // width and case
        #expect(AssistantSidebarSearch.filter(all, "  ").count == 3)
    }

    /// The sidebar's days: 今天 / 昨天 / 最近 7 天 / 更早 counted from midnight, newest first, no time goes to 更早.
    @Test func conversationsGroupByDay() throws {
        var cal = Calendar(identifier: .gregorian)
        cal.timeZone = TimeZone(identifier: "Asia/Shanghai")!
        let now = cal.date(from: DateComponents(year: 2026, month: 10, day: 30, hour: 9))!
        func s(_ id: String, hoursAgo: Double?) throws -> Session {
            let o: [String: Any] = ["session_id": id, "status": "idle", "title": id, "assistant": true]
            var x = try JSON.decoder.decode(Session.self, from: JSONSerialization.data(withJSONObject: o))
            x.updatedAt = hoursAgo.map { now.addingTimeInterval(-$0 * 3600) }
            return x
        }
        // 8 h ago is today (01:00); 10 h ago is yesterday (23:00); 3 days ago; 30 days ago; never.
        let groups = AssistantSidebarGroups.group([try s("old", hoursAgo: 720), try s("today", hoursAgo: 8), try s("none", hoursAgo: nil),
                                                   try s("yesterday", hoursAgo: 10), try s("week", hoursAgo: 72),
                                                   try s("today2", hoursAgo: 1)], now: now, calendar: cal)
        #expect(groups.map(\.title) == ["今天", "昨天", "最近 7 天", "更早"])
        #expect(groups.map { $0.sessions.map(\.sessionID) } == [["today2", "today"], ["yesterday"], ["week"], ["old", "none"]])
    }

    /// A drag to the right on something that scrolls sideways and is not at its left edge scrolls it, not the sidebar.
    @Test @MainActor func sidewaysScrollersGoFirst() {
        let root = UIView(frame: CGRect(x: 0, y: 0, width: 400, height: 800))
        let code = UIScrollView(frame: CGRect(x: 0, y: 100, width: 400, height: 100))
        code.contentSize = CGSize(width: 1200, height: 100)
        let line = UIView(frame: CGRect(x: 0, y: 0, width: 1200, height: 100))
        code.addSubview(line)
        root.addSubview(code)
        let list = UIScrollView(frame: CGRect(x: 0, y: 300, width: 400, height: 400)) // scrolls only up and down
        list.contentSize = CGSize(width: 400, height: 4000)
        root.addSubview(list)
        let here = CGPoint(x: 200, y: 150)
        #expect(!DrawerPan.Coordinator.sidewaysScroller(at: here, in: root)) // at its left edge: the sidebar may open
        code.contentOffset.x = 300
        #expect(DrawerPan.Coordinator.sidewaysScroller(at: here, in: root))
        #expect(!DrawerPan.Coordinator.sidewaysScroller(at: CGPoint(x: 200, y: 500), in: root))
    }
}

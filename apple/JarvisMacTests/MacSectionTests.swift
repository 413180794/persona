import Foundation
import Testing
@testable import Jarvis

/// The window's sidebar: the iPhone's four tabs plus 知识库 and 机器 (docs/mac.md「主窗口」), on ⌘1…⌘6.
@MainActor
struct MacSectionTests {
    @Test func orderAndShortcuts() {
        #expect(MacSection.allCases.map(\.title) == ["Jarvis", "Agents", "收件箱", "知识库", "机器", "设置"])
    }

    @Test func tabSectionsFollowTheTab() {
        // Nothing picked on the Mac side: the section is the tab's (a notification, ⌘K, a deep link moved it).
        #expect(MacSection.shown(override: nil, tab: .mail) == .inbox)
        #expect(MacSection.shown(override: .machines, tab: .mail) == .machines)
    }

    @Test func pickingASectionMovesTheTabOrTheOverride() {
        #expect(MacSection.pick(.agents) == (tab: .chats, override: nil))
        #expect(MacSection.pick(.knowledge) == (tab: nil, override: .knowledge))
        #expect(MacSection.pick(.machines) == (tab: nil, override: .machines))
    }

    @Test func quickOpenFindsTheNewSections() {
        let t = MacQuickOpen.sections("监控", sessions: [], machines: [], inbox: []).flatMap(\.hits).map(\.title)
        #expect(t == ["机器"])
        let k = MacQuickOpen.sections("knowledge", sessions: [], machines: [], inbox: []).flatMap(\.hits).map(\.title)
        #expect(k == ["知识库"])
    }
}

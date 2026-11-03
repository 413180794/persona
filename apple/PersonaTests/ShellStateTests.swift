#if os(iOS)
import Foundation
import Testing
@testable import Jarvis

/// The shell's one source of the page in front (docs/shell.md): `select` and `route` change it at once and the strip
/// and the app's tab follow; the strip's `slide` is idempotent and a slide not started yet is replaced. And the status
/// row (`ShellNotices`): only the link's state (the toasts float), waiting for a finger unless urgent.
@MainActor
struct ShellStateTests {
    @Test func selectMovesThePageTheStripAndTheTab() {
        let model = AppModel()
        model.selectedTab = .assistant
        let shell = ShellState()
        shell.attach(to: model)
        defer { shell.detach(from: model) }
        shell.select(1)
        #expect(shell.page == 1)
        #expect(model.selectedTab == .chats)
        #expect(shell.agentsBuilt)
        shell.select(0)
        #expect(shell.page == 0)
        #expect(model.selectedTab == .assistant)
    }

    @Test func aTabSetAnywhereIsRoutedAsItIsSet() {
        let model = AppModel()
        model.selectedTab = .assistant
        let shell = ShellState()
        shell.attach(to: model)
        defer { shell.detach(from: model) }
        model.selectedTab = .chats // a notification, 交给 Agent…
        #expect(shell.page == 1, "the shell did not take the tab as it was set")
        model.selectedTab = .mail // the inbox lands on 助手
        #expect(shell.page == 0)
        #expect(model.selectedTab == .assistant)
        #expect(shell.request?.page == .inbox)
        shell.select(1)
        model.selectedTab = .settings // 设置 is a sheet over the page in front
        #expect(model.shellSettings)
        #expect(shell.page == 1)
        #expect(model.selectedTab == .chats)
    }

    @Test func aDetachedShellNoLongerRoutes() {
        let model = AppModel()
        let shell = ShellState()
        shell.attach(to: model)
        shell.detach(from: model)
        let page = shell.page
        model.selectedTab = page == 0 ? .chats : .assistant
        #expect(shell.page == page)
    }

    @Test func onlyThePageInFrontIsBuiltAtLaunch() {
        let onAgents = ShellState(page: 1)
        #expect(onAgents.page == 1 && onAgents.pager.page == 1)
        #expect(onAgents.agentsBuilt && !onAgents.assistantBuilt, "助手 built behind Agents at launch")
        onAgents.select(0)
        #expect(onAgents.assistantBuilt)
        let onAssistant = ShellState()
        #expect(onAssistant.assistantBuilt && !onAssistant.agentsBuilt)
        let dragged = ShellState(page: 1)
        dragged.pager.dragBegan() // the page behind comes in under the finger
        #expect(dragged.assistantBuilt)
    }

    @Test func aSlideNotStartedYetIsReplaced() {
        let pager = ShellPager()
        pager.slide(to: 1) // starts on the next turn
        #expect(pager.moving)
        pager.slide(to: 0) // tapped back before it started: nothing to slide
        #expect(!pager.moving)
        #expect(pager.position == 0)
    }

    @Test func theSameSlideTwiceIsOne() {
        let pager = ShellPager()
        pager.slide(to: 1, animated: false)
        #expect(pager.position == 1)
        pager.slide(to: 1)
        #expect(!pager.moving, "a slide to where the strip rests started moving")
    }

    @Test func theStripComingToRestUnderAFingerKeepsTheMovingBar() {
        let pager = ShellPager()
        pager.fingers(down: true)
        pager.slide(to: 1)
        pager.slide(to: 0) // at rest again at once, the finger still down
        #expect(!pager.moving)
        #expect(pager.lingering, "the moving bar went from under the finger")
        pager.fingers(down: false)
        #expect(!pager.lingering, "the moving bar stayed after the finger let go")
        pager.slide(to: 1)
        pager.slide(to: 0)
        #expect(!pager.lingering, "the moving bar stayed with no finger down")
    }

    @Test func theLinkOpensTheRowAndFoldsIt() {
        let n = ShellNotices()
        n.linkChanged(.connecting)
        #expect(n.shown == .link(.connecting))
        n.linkChanged(.updating)
        #expect(n.shown == .link(.updating))
        n.linkChanged(.online)
        #expect(n.shown == nil)
        #expect(n.opened == 1 && n.folded == 1, "opened \(n.opened), folded \(n.folded): the row folded in between")
    }

    /// A toast is the result of something just done: it floats (`ToastWindow`), and the row, which moves the pages,
    /// stays as it was (#1053: 已归档 opened it and pushed the drawer down).
    @Test func aToastLeavesTheRowAlone() {
        let model = AppModel()
        let shell = ShellState()
        shell.attach(to: model)
        defer { shell.detach(from: model) }
        let toast = Toast("已归档", style: .success)
        model.toast = toast
        #expect(model.toast == toast, "the toast is the model's to show, not taken into the row")
        #expect(shell.notices.shown == nil && shell.notices.opened == 0 && shell.notices.room == 0)
    }

    @Test func theRowWaitsForTheFingerUnlessUrgent() {
        let n = ShellNotices()
        n.fingers(busy: true)
        n.linkChanged(.connecting)
        #expect(n.shown == nil, "opened under the finger")
        n.fingers(busy: false)
        #expect(n.shown == .link(.connecting))
        n.fingers(busy: true)
        n.linkChanged(.online)
        #expect(n.shown == .link(.connecting), "folded under the finger")
        n.fingers(busy: false)
        #expect(n.shown == nil)
        n.fingers(busy: true)
        n.linkChanged(.waitingForNetwork)
        #expect(n.shown == .link(.waitingForNetwork), "no network waited for the finger")
    }
}
#endif

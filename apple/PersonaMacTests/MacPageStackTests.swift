import AppKit
import SwiftUI
import Testing
@testable import Persona

/// The sections' pages are kept in the window once shown (#390): a switch hides one and shows another, telling each
/// whether it is in front; nothing is built again.
@MainActor
struct MacPageStackTests {
    private final class Built { var sections: [MacSection] = [] }

    private func show(_ stack: MacPageStackView, _ section: MacSection, _ built: Built) {
        stack.show(section, chrome: MacPageChrome()) { s in
            built.sections.append(s)
            return AnyView(Text(s.title))
        }
    }

    /// The turn after this one's frame is committed, when the stack tells the pages who is in front: the run loop has to
    /// go to sleep once (`CFRunLoopActivity.beforeWaiting`).
    private func afterTheFrame() async { try? await Task.sleep(for: .milliseconds(100)) }

    private func hosts(_ stack: MacPageStackView) -> [MacPageHostingView] { stack.subviews.compactMap { $0 as? MacPageHostingView } }

    @Test func aPageIsBuiltOnceAndOnlyTheOneInFrontShows() async {
        let stack = MacPageStackView(frame: NSRect(x: 0, y: 0, width: 800, height: 600))
        let built = Built()
        show(stack, .agents, built)
        show(stack, .inbox, built)
        show(stack, .agents, built)
        show(stack, .inbox, built)
        #expect(built.sections == [.agents, .inbox])
        let h = hosts(stack)
        #expect(h.count == 2)
        #expect(h.filter { !$0.isHidden }.count == 1)
        let front = h.first { !$0.isHidden }
        #expect(front?.rootView.section == .inbox)
        // Who is in front (toolbar items, search fields, holds) changes after the switch's frame is committed, so the
        // page shows in the first frame and the window's toolbar is redone in the next one.
        await afterTheFrame()
        #expect(front?.rootView.activity.inFront == true)
        #expect(h.first { $0.isHidden }?.rootView.activity.inFront == false)
    }

    @Test func theSwitchTellsThePagesAfterItsFrame() async {
        let stack = MacPageStackView(frame: NSRect(x: 0, y: 0, width: 800, height: 600))
        let built = Built()
        show(stack, .agents, built)
        show(stack, .inbox, built)
        let h = hosts(stack)
        let inbox = h.first { $0.rootView.section == .inbox }
        let agents = h.first { $0.rootView.section == .agents }
        #expect(inbox?.isHidden == false)
        #expect(agents?.rootView.activity.inFront == true) // not yet: the frame with Inbox in it comes first
        await afterTheFrame()
        #expect(agents?.rootView.activity.inFront == false)
        #expect(inbox?.rootView.activity.inFront == true)
    }

    @Test func aPluginPageHasEveryPageBehindItAndBuildsNone() async {
        let stack = MacPageStackView(frame: NSRect(x: 0, y: 0, width: 800, height: 600))
        let built = Built()
        show(stack, .agents, built)
        stack.show(nil, chrome: MacPageChrome()) { _ in AnyView(EmptyView()) } // a plugin's page over them
        await afterTheFrame()
        let h = hosts(stack)
        #expect(h.map(\.isHidden) == [true])
        #expect(h.first?.rootView.activity.inFront == false)
        show(stack, .agents, built) // back from it: the same page, not built again
        #expect(built.sections == [.agents])
        #expect(h.first?.isHidden == false)
    }

    @Test func showingTheSamePageAgainChangesNothing() {
        let stack = MacPageStackView(frame: NSRect(x: 0, y: 0, width: 800, height: 600))
        let built = Built()
        show(stack, .knowledge, built)
        show(stack, .knowledge, built)
        #expect(built.sections == [.knowledge])
        #expect(hosts(stack).map(\.isHidden) == [false])
    }
}

/// UserDefaults writes that can wait (`DeferredDefaults`): on the Mac the section and tab to come back to are written
/// once the switching rests, not on every switch (each write draws every `@AppStorage` view again, #390).
@MainActor
struct DeferredDefaultsTests {
    @Test func theStandardDefaultsWaitForAFlush() {
        let key = "test.deferred.\(UUID().uuidString)"
        defer { UserDefaults.standard.removeObject(forKey: key) }
        DeferredDefaults.set("a", forKey: key)
        DeferredDefaults.set("b", forKey: key)
        #expect(UserDefaults.standard.string(forKey: key) == nil)
        DeferredDefaults.flush()
        #expect(UserDefaults.standard.string(forKey: key) == "b")
        DeferredDefaults.set(nil, forKey: key)
        DeferredDefaults.flush()
        #expect(UserDefaults.standard.string(forKey: key) == nil)
    }

    @Test func otherDefaultsAreWrittenAtOnce() throws {
        let d = try #require(UserDefaults(suiteName: "DeferredDefaultsTests"))
        defer { d.removePersistentDomain(forName: "DeferredDefaultsTests") }
        DeferredDefaults.set("x", forKey: "k", in: d)
        #expect(d.string(forKey: "k") == "x")
    }
}

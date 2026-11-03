import SwiftUI
import Testing
@testable import Jarvis

@MainActor
struct TabActivityTests {
    private final class Calls {
        var log: [String] = []
    }

    private func update(_ state: TabVisibilityState, _ calls: Calls, mounted: Bool? = nil, active: Bool? = nil) {
        state.update(mounted: mounted, active: active, appear: { calls.log.append("appear") }, disappear: { calls.log.append("disappear") })
    }

    @Test func appearsOnceWhenInTheWindowAndInFront() {
        let state = TabVisibilityState(), calls = Calls()
        update(state, calls, mounted: true, active: true)
        update(state, calls, mounted: true, active: true) // a second onAppear (back from a pushed page) without a disappear
        #expect(calls.log == ["appear"])
        #expect(state.visible)
    }

    @Test func goingToTheBackDisappearsOnceThenLeavingTheWindowDoesNothing() {
        let state = TabVisibilityState(), calls = Calls()
        update(state, calls, mounted: true, active: true)
        update(state, calls, active: false) // a container that keeps the tab in the window
        update(state, calls, mounted: false, active: false) // the system TabView takes it out as well
        #expect(calls.log == ["appear", "disappear"])
        #expect(!state.visible)
    }

    @Test func aHiddenTabPutBackInTheWindowDoesNotAppear() {
        let state = TabVisibilityState(), calls = Calls()
        update(state, calls, mounted: true, active: true)
        update(state, calls, mounted: false, active: false)
        update(state, calls, mounted: true, active: false) // the system briefly puts the old tab back on the way out
        update(state, calls, mounted: false, active: false)
        #expect(calls.log == ["appear", "disappear"])
    }

    @Test func comingBackToTheFrontAppearsAgainWhicheverArrivesFirst() {
        let state = TabVisibilityState(), calls = Calls()
        update(state, calls, mounted: true, active: true)
        update(state, calls, mounted: false, active: false)
        update(state, calls, active: true) // the tab is picked before its view is back in the window
        #expect(calls.log == ["appear", "disappear"])
        update(state, calls, mounted: true, active: true)
        #expect(calls.log == ["appear", "disappear", "appear"])
    }
}

#if os(iOS)
/// The helpers on a real view: a page that stays in the window (the shell's hidden page) still has its work cancelled.
@MainActor
struct TabActiveTaskTests {
    private final class Calls {
        var log: [String] = []
    }

    @Observable @MainActor final class Front {
        var active = true
    }

    private struct Page: View {
        let front: Front
        let calls: Calls

        var body: some View {
            Color.clear
                .tabActiveTask {
                    calls.log.append("start")
                    await withTaskCancellationHandler {
                        try? await Task.sleep(for: .seconds(60))
                    } onCancel: {
                        Task { @MainActor in calls.log.append("cancel") }
                    }
                }
                .environment(\.tabIsActive, front.active)
        }
    }

    private func settle() async { try? await Task.sleep(for: .milliseconds(300)) }

    @Test func aPageStillInTheWindowStopsItsWorkWhenItGoesToTheBack() async {
        let front = Front(), calls = Calls()
        let window = UIWindow(frame: CGRect(x: 0, y: 0, width: 200, height: 200))
        window.rootViewController = UIHostingController(rootView: Page(front: front, calls: calls))
        window.makeKeyAndVisible()
        await settle()
        #expect(calls.log == ["start"])

        front.active = false
        await settle()
        #expect(calls.log == ["start", "cancel"]) // cancelled, and not started again while behind

        front.active = true
        await settle()
        #expect(calls.log == ["start", "cancel", "start"]) // back in front: from the top
        window.isHidden = true
    }
}
#endif

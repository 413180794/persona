import AppKit
import Testing
@testable import Persona

/// Paging the Mac image viewer (#3481): a single click schedules the close after `NSEvent.doubleClickInterval` so a
/// double click can cancel it. Swapping the image before that delay fired used to leave the old work item alive, so
/// tapping once and immediately paging closed the viewer by itself half a second later.
///
/// These tests are `async` and wait with `Task.sleep`, not by pumping `RunLoop.main` themselves: under Swift Testing's
/// Mac host a synchronous test that runs the main run loop re-entrantly does not service `DispatchQueue.main`
/// schedule blocks (the close work item), so the single-tap test could never see its callback fire. Awaiting yields
/// the main thread to the host, whose run loop drains the main queue normally.
@MainActor
struct MacZoomableImageTests {
    private func makeView() -> (ZoomScrollView, Counter) {
        let counter = Counter()
        let view = ZoomScrollView(image: NSImage(size: NSSize(width: 100, height: 80)))
        view.onTap = { counter.taps += 1 }
        return (view, counter)
    }

    private func click(_ count: Int) throws -> NSEvent {
        try #require(NSEvent.mouseEvent(with: .leftMouseUp, location: .zero, modifierFlags: [], timestamp: 0,
                                       windowNumber: 0, context: nil, eventNumber: 0,
                                       clickCount: count, pressure: 1))
    }

    /// Let the delayed main-queue work item run (or be skipped as cancelled). Waiting with `Task.sleep` (an `async`
    /// test) is what lets the host's main run loop deliver the single click's `DispatchQueue.main.asyncAfter` item.
    private func waitPastDoubleClickInterval() async throws {
        try await Task.sleep(for: .seconds(NSEvent.doubleClickInterval + 0.3))
    }

    @Test func pagingCancelsThePendingSingleTapClose() async throws {
        let (view, counter) = makeView()
        try view.clicked(click(1))
        // Paging lands before the double-click interval is over.
        view.setImage(NSImage(size: NSSize(width: 120, height: 90)))
        try await waitPastDoubleClickInterval()
        #expect(counter.taps == 0, "paging must discard the single-tap close scheduled for the previous image")
    }

    @Test func singleTapStillClosesAfterTheDoubleClickInterval() async throws {
        let (view, counter) = makeView()
        try view.clicked(click(1))
        try await waitPastDoubleClickInterval()
        #expect(counter.taps == 1)
    }

    @Test func doubleClickCancelsThePendingSingleTap() async throws {
        let (view, counter) = makeView()
        try view.clicked(click(1))
        try view.clicked(click(2))
        try await waitPastDoubleClickInterval()
        #expect(counter.taps == 0, "the zoom double click must not also count as a closing tap")
    }

    private final class Counter {
        var taps = 0
    }
}

import Testing
@testable import Persona

@Suite("Pinned conversations scrolling and pull arbitration")
struct LarkPinnedInteractionTests {
    @Test func explicitExpansionDoesNotCollapseFromTopLayoutCallbacks() {
        var state = LarkPinnedInteraction()
        #expect(!state.isExpanded)
        state.toggle()
        state.update(scrollOffset: 0, shelfPassed: false)
        state.update(scrollOffset: 0, shelfPassed: true) // stale pre-layout header position
        state.update(scrollOffset: -10, shelfPassed: false)
        #expect(state.isExpanded)
        state.toggle()
        #expect(!state.isExpanded)
    }

    @Test func reachingStickyFiltersImmediatelyCollapsesAndStaysCollapsed() {
        var state = LarkPinnedInteraction()
        state.toggle()
        state.update(scrollOffset: 80, shelfPassed: false)
        state.update(scrollOffset: 0, shelfPassed: false)
        #expect(state.isExpanded)
        state.update(scrollOffset: 700, shelfPassed: true)
        state.update(scrollOffset: 15, shelfPassed: false)
        #expect(!state.isExpanded)
        state.update(scrollOffset: 4, shelfPassed: false)
        #expect(!state.isExpanded)
        state.toggle()
        state.update(scrollOffset: 0, shelfPassed: false)
        #expect(state.isExpanded) // next explicit expansion starts a fresh cycle
    }

    @Test func expandOccursOnReleaseAndConsumesOnlyOneAction() {
        var state = LarkPinnedInteraction()
        state.beginDrag(scrollOffset: 0, shelfPassed: false, canExpand: true)
        state.update(scrollOffset: -72, shelfPassed: false)
        #expect(state.pullAction == .expand)
        #expect(state.isPullArmed && state.pullProgress == 1)
        #expect(!state.isExpanded)
        #expect(state.endDrag(canExpand: true) == .expand)
        #expect(state.isExpanded && !state.isPullArmed && state.pullAction == nil)
        #expect(state.endDrag(canExpand: true) == nil)
        state.update(scrollOffset: -100, shelfPassed: false) // bounce after release
        #expect(state.endDrag(canExpand: true) == nil)
    }

    @Test(arguments: [0.0, 35, 71.99])
    func belowThresholdDoesNothing(distance: Double) {
        var state = LarkPinnedInteraction()
        state.beginDrag(scrollOffset: 0, shelfPassed: false, canExpand: true)
        state.update(scrollOffset: -distance, shelfPassed: false)
        #expect(!state.isPullArmed)
        #expect(state.endDrag(canExpand: true) == nil)
        #expect(!state.isExpanded)
    }

    @Test func retreatingAfterThresholdCancelsInsteadOfUsingTheMaximumDistance() {
        var state = LarkPinnedInteraction()
        state.beginDrag(scrollOffset: 0, shelfPassed: false, canExpand: true)
        state.update(scrollOffset: -120, shelfPassed: false)
        #expect(state.isPullArmed)
        state.update(scrollOffset: -30, shelfPassed: false)
        #expect(!state.isPullArmed)
        #expect(state.endDrag(canExpand: true) == nil)
        #expect(!state.isExpanded)
    }

    @Test func cancelledTrackingDoesNotAct() {
        var state = LarkPinnedInteraction()
        state.beginDrag(scrollOffset: 0, shelfPassed: false, canExpand: true)
        state.update(scrollOffset: -100, shelfPassed: false)
        #expect(state.endDrag(canExpand: true, cancelled: true) == nil)
        #expect(!state.isExpanded && state.pullAction == nil)
    }

    @Test func scrollingBackFromStickyFiltersCannotExpandInTheSameGesture() {
        var state = LarkPinnedInteraction()
        state.beginDrag(scrollOffset: 300, shelfPassed: true, canExpand: true)
        state.update(scrollOffset: 0, shelfPassed: false)
        state.update(scrollOffset: -150, shelfPassed: false)
        #expect(state.endDrag(canExpand: true) == nil)
        state.beginDrag(scrollOffset: 0, shelfPassed: false, canExpand: true)
        state.update(scrollOffset: -72, shelfPassed: false)
        #expect(state.endDrag(canExpand: true) == .expand)
    }

    @Test func dragBeforeFiltersStickCanReturnToTopAndThenPullToExpand() {
        var state = LarkPinnedInteraction()
        state.beginDrag(scrollOffset: 60, shelfPassed: false, canExpand: true)
        state.update(scrollOffset: 30, shelfPassed: false)
        #expect(state.pullProgress == 0 && !state.isPullArmed)
        state.update(scrollOffset: -72, shelfPassed: false)
        #expect(state.endDrag(canExpand: true) == .expand)
        #expect(state.isExpanded)
    }

    @Test func stickyStartAndCrossingStickyFiltersCancelEligibility() {
        var state = LarkPinnedInteraction()
        state.beginDrag(scrollOffset: 0, shelfPassed: true, canExpand: true)
        state.update(scrollOffset: -100, shelfPassed: false)
        #expect(state.endDrag(canExpand: true) == nil)
        state.beginDrag(scrollOffset: 0, shelfPassed: false, canExpand: true)
        state.update(scrollOffset: 300, shelfPassed: true)
        state.update(scrollOffset: -100, shelfPassed: false)
        #expect(state.endDrag(canExpand: true) == nil)
        state.beginDrag(scrollOffset: 0, shelfPassed: false, canExpand: true)
        state.update(scrollOffset: 0, shelfPassed: true)
        state.update(scrollOffset: -100, shelfPassed: false)
        #expect(state.endDrag(canExpand: true) == nil)
    }

    @Test func expandedPinsDoNotRefreshOrToggle() {
        var state = LarkPinnedInteraction()
        state.toggle()
        state.beginDrag(scrollOffset: 0, shelfPassed: false, canExpand: true)
        state.update(scrollOffset: -90, shelfPassed: false)
        #expect(state.pullAction == nil)
        #expect(state.endDrag(canExpand: true) == nil)
        #expect(state.isExpanded)
    }

    @Test func noExpandablePinsDoNotRefresh() {
        var state = LarkPinnedInteraction()
        state.beginDrag(scrollOffset: 0, shelfPassed: true, canExpand: false)
        state.update(scrollOffset: -90, shelfPassed: true)
        #expect(state.endDrag(canExpand: false) == nil)
        #expect(!state.isExpanded)
    }

    @Test func disappearingPinsDoNotTurnAnExpansionIntoARefresh() {
        var state = LarkPinnedInteraction()
        state.beginDrag(scrollOffset: 0, shelfPassed: false, canExpand: true)
        state.update(scrollOffset: -90, shelfPassed: false)
        #expect(state.endDrag(canExpand: false) == nil)
        #expect(!state.isExpanded)
    }

    @Test func explicitToggleCancelsAnInFlightPull() {
        var state = LarkPinnedInteraction()
        state.beginDrag(scrollOffset: 0, shelfPassed: false, canExpand: true)
        state.update(scrollOffset: -90, shelfPassed: false)
        state.toggle()
        #expect(state.endDrag(canExpand: true) == nil)
        #expect(state.isExpanded)
    }

    @Test func invalidGeometryAndMomentumWithoutTrackingNeverAct() {
        var state = LarkPinnedInteraction()
        state.update(scrollOffset: -100, shelfPassed: false)
        #expect(state.endDrag(canExpand: true) == nil)
        state.beginDrag(scrollOffset: .nan, shelfPassed: false, canExpand: true)
        state.update(scrollOffset: -100, shelfPassed: false)
        #expect(state.endDrag(canExpand: true) == nil)
        state.beginDrag(scrollOffset: 0, shelfPassed: false, canExpand: true)
        state.update(scrollOffset: -100, shelfPassed: false)
        state.update(scrollOffset: .infinity, shelfPassed: false)
        #expect(state.endDrag(canExpand: true) == nil)
    }
}

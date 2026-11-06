import SwiftUI
import Testing
@testable import Persona

/// #3549: in the Mac Agents/Persona lists plain Tab moves focus to the composer, but ⇧Tab must stay the system's
/// reverse focus traversal — only the shift modifier reverses it.
struct MacListTabKeyTests {
    @Test func noModifiersMovesToComposer() {
        #expect(MacTabKey.movesToComposer(modifiers: []))
    }

    @Test func shiftLeavesTabForReverseTraversal() {
        #expect(!MacTabKey.movesToComposer(modifiers: .shift))
    }

    @Test func shiftWithCommandStillLeavesTab() {
        #expect(!MacTabKey.movesToComposer(modifiers: [.shift, .command]))
    }

    @Test func commandAloneStillMovesToComposer() {
        #expect(MacTabKey.movesToComposer(modifiers: .command))
    }

    @Test func optionAndControlAloneDoNotReverse() {
        #expect(MacTabKey.movesToComposer(modifiers: .option))
        #expect(MacTabKey.movesToComposer(modifiers: .control))
    }
}

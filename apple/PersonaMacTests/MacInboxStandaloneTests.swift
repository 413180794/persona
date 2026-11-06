import Testing
@testable import Persona

/// A standalone inbox-item window must not drive the main window's shared `reading` state: opening a link or closing
/// the window used to fold and clear the inbox page's filters and list (#3477). Nor take the list's one-shot sheet
/// signals: the sheet opened in the window behind (#3817).
struct MacInboxStandaloneTests {
    @Test func embeddedDetailWritesSharedReading() {
        // The detail inside the inbox page folds the page's filters and list while its link panel is open.
        #expect(MacInboxItemDetail.writesSharedReading(standalone: false))
    }

    @Test func standaloneWindowNeverWritesSharedReading() {
        // A window of its own never writes the shared state, so the main window's list stays as the user left it.
        #expect(!MacInboxItemDetail.writesSharedReading(standalone: true))
    }

    @Test func embeddedDetailTakesTheListsSignalForItsItem() {
        // The main window's list asked for AI Draft Reply / Hand to AI on this item: its sheet opens beside the list.
        #expect(MacInboxItemDetail.takesListSignal("item-1", itemID: "item-1", standalone: false))
        #expect(!MacInboxItemDetail.takesListSignal("item-2", itemID: "item-1", standalone: false))
        #expect(!MacInboxItemDetail.takesListSignal(nil, itemID: "item-1", standalone: false))
    }

    @Test func standaloneWindowNeverTakesTheListsSignal() {
        // The same item open in a window of its own leaves the list's signal to the main window: the sheet used to open
        // in whichever window took it first, often the one behind (#3817). Its own buttons still open its sheets.
        #expect(!MacInboxItemDetail.takesListSignal("item-1", itemID: "item-1", standalone: true))
        #expect(!MacInboxItemDetail.takesListSignal(nil, itemID: "item-1", standalone: true))
    }
}

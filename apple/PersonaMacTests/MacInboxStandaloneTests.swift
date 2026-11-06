import Testing
@testable import Persona

/// A standalone inbox-item window must not drive the main window's shared `reading` state: opening a link or closing
/// the window used to fold and clear the inbox page's filters and list (#3477).
struct MacInboxStandaloneTests {
    @Test func embeddedDetailWritesSharedReading() {
        // The detail inside the inbox page folds the page's filters and list while its link panel is open.
        #expect(MacInboxItemDetail.writesSharedReading(standalone: false))
    }

    @Test func standaloneWindowNeverWritesSharedReading() {
        // A window of its own never writes the shared state, so the main window's list stays as the user left it.
        #expect(!MacInboxItemDetail.writesSharedReading(standalone: true))
    }
}

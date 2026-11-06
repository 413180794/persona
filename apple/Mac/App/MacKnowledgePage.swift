import SwiftUI

/// The sidebar's Knowledge: the list (the kinds' chips and their entries) in a column of its own and the open entry beside
/// it, as Mail and the inbox have them (#2318). Across the whole window a row's time sat a thousand points from its
/// name, and opening an entry took the list away. The list's width is dragged and remembered (`MacListBeside`); what
/// is open is the page's own state, not written anywhere. On the phone, and pushed in Persona's page, Knowledge still
/// pushes its entries (no `kbOpenEntry` there).
struct MacKnowledgePage: View {
    @State private var opener = KBOpenEntry()

    var body: some View {
        MacListBeside(listShown: true, widthKey: "mac.knowledge.listWidth", ideal: Theme.Size.macKnowledgeListIdeal) {
            NavigationStack {
                KnowledgeView().environment(\.macBelowTitleRow, true) // below the title (#1243)
                    // The column in the page's color top to bottom, as the entry beside it: its rows paint it
                    // (`macListRow`), and the window's gray showed between and below them in dark mode.
                    .scrollContentBackground(.hidden)
                    .background(Theme.pageBackground.ignoresSafeArea())
            }
                .environment(\.macSectionStack, true) // anything pushed here: its ‹ in the title row (#2318)
                .environment(\.kbOpenEntry, opener)
        } page: {
            MacKnowledgeDetail(opener: opener)
        }
    }
}

/// The open entry, or a quiet line while none is. What it pushes (a day, a related entry, the project it was merged
/// into) is pushed in this column, the ‹ and the page's title in the column's part of the title row (`MacPushedPage`).
/// Its own view, so opening an entry redraws this column only.
private struct MacKnowledgeDetail: View {
    let opener: KBOpenEntry

    var body: some View {
        let open = opener.open
        NavigationStack {
            Group {
                if let open {
                    KnowledgeDetailView(entry: open.entry, query: open.query)
                } else {
                    SplitPlaceholder(symbol: Theme.Icon.knowledge, title: String(localized: "Select an Entry"), message: String(localized: "Select one from the list on the left to see its details here."))
                }
            }
            .modifier(MacBelowTitleRow()) // under the title row, as the list beside it
            // The page's color as Agents' and Inbox's detail: the entry's grouped list painted the window's gray over it.
            .scrollContentBackground(.hidden)
            .background(Theme.pageBackground.ignoresSafeArea())
        }
        // Another entry starts its own stack: what the last one pushed goes with it.
        .id(open)
        .environment(\.macSectionStack, true)
        // At the column's own edge, not past the traffic lights (the list is at the window's edge, not this).
        .environment(\.macTitleRowLeading, 0)
        // A page pushed here titles its part of the row; the section's name over the list stays.
        .transformPreference(MacTitleRowTaken.self) { $0 = false }
    }
}

import SwiftUI

/// A section's list kept below the title bar's row, where the shell draws the section's name over it (`MacPageTitle`),
/// #1243. Lists and scroll views reach up under the window's toolbar on their own, their rows inset below it, and with
/// the toolbar's backing hidden (`MacShell`: it cut the sections column off) the system's scroll edge effect that
/// would cover them there is gone too: rows scrolled up ran through the title. Clipped at its frame, laid out from
/// the row's foot, the list starts where its inset put it before and its rows go out of sight at the row's foot.
/// The row's fixed height, not the safe area's: the title is drawn at that height whatever the toolbar holds.
struct MacBelowTitleRow: ViewModifier {
    func body(content: Content) -> some View {
        content
            // Padding alone does not do it: the list's scroll view still reached up under the toolbar in a split's
            // column, so it is clipped to the frame the padding gives it.
            .clipped()
            .padding(.top, Theme.Size.macToolbarHeight)
            .ignoresSafeArea(.container, edges: .top)
    }
}

/// `MacBelowTitleRow` for a page that is a section's first page in the window but is opened elsewhere too (Knowledge is also
/// pushed in Persona's page, Machines in Settings' detail column): the shell asks for it where the page sits under the section's
/// title (`macBelowTitleRow`), and the page applies it to its list inside its own background, which then still reaches
/// up behind the title (clipped with the list, the title row lost the page's color in Clay and the other tinted styles).
struct MacBelowTitleRowIfAsked: ViewModifier {
    @Environment(\.macBelowTitleRow) private var asked

    func body(content: Content) -> some View {
        if asked { content.modifier(MacBelowTitleRow()) } else { content }
    }
}

extension EnvironmentValues {
    /// The page sits under the section's title in the title bar's row (`MacBelowTitleRowIfAsked`, #1243).
    @Entry var macBelowTitleRow = false
}

import SwiftUI

/// A page in a split's detail column, in the title bar's row at the column's own leading edge: the way back (‹) when
/// there is a page under this one, then the page's title when it gives one (`detailTitle`, Settings' pages), as Finder and
/// System Settings have them (#1224 B2).
/// The system's back button went to the leading end of the window's toolbar instead, by the traffic lights and over
/// the sections column, drawn as an iPhone's round glass button (#1170). The first page has nothing to go back to.
struct MacDetailBack: ViewModifier {
    /// Whether there is a page under this one.
    var shown: Bool
    /// Past the traffic lights and the toolbar's buttons when the page is at the window's edge (`macTitleRowLeading`).
    var leading: CGFloat = 0
    @Environment(\.dismiss) private var dismiss

    func body(content: Content) -> some View {
        content
            .navigationBarBackButtonHidden(true)
            .overlayPreferenceValue(MacDetailTitle.self, alignment: .topLeading) { title in
                if shown || title != nil {
                    HStack(spacing: Theme.Space.xs) {
                        if shown { back }
                        if let title {
                            // A label: the row still drags the window.
                            Text(title)
                                .font(Theme.Font.heading)
                                .foregroundStyle(Theme.macLabel)
                                .lineLimit(1)
                                .allowsHitTesting(false)
                                .accessibilityAddTraits(.isHeader)
                        }
                    }
                    .padding(.leading, leading + (shown ? Self.backInset : Theme.Space.l))
                    .frame(height: Theme.Size.macToolbarHeight)
                    .ignoresSafeArea(.container, edges: .top)
                }
            }
    }

    /// The ‹'s distance from the row's leading edge, and its own width with the gap after it: where a title after it
    /// starts (a page's own `pageTitle` title, `MacTitleRowTitle`, starts there too).
    static let backInset = Theme.Space.s
    static func titleAfterBack(_ typeSize: DynamicTypeSize) -> CGFloat {
        backInset + Theme.Size.icon * Theme.sizeScale(typeSize) + Theme.Space.xs
    }

    private var back: some View {
        Button { dismiss() } label: {
            Image(systemName: Theme.Icon.back)
                .font(Theme.Font.heading)
                .scaledFrame(width: Theme.Size.icon, height: Theme.Size.icon)
                .contentShape(.rect)
        }
        .buttonStyle(.borderless)
        .foregroundStyle(.secondary)
        .help("Back [Task Sheets 572cf4]")
        .accessibilityLabel("Back [Task Sheets 572cf4]")
        .accessibilityIdentifier("mac.detail.back")
    }
}

/// A page pushed in a section's own stack (Knowledge, Machines: no list beside it, #2318): the ‹ in the title bar's row, then
/// the page's title (`detailTitle`, or its own `pageTitle` moved past the ‹), where the section's name was. The
/// system's back button went by the traffic lights as a round glass button, and as the window's toolbar item it stayed
/// there when another section came to the front (the sections' pages are kept, #390): a click on it took the hidden
/// page back. Only in such a stack (`macSectionStack`); elsewhere the page is left as it was. `pageTitle` and
/// `detailTitle` apply it, so a page pushed there with its title set either way gets it; a page with only a
/// `navigationTitle` takes it with `sectionPushedPage()`.
struct MacPushedPage: ViewModifier {
    /// A page's own title for the row (`pageTitle`, `MacTitleRowTitle`): drawn here, over the part kept below the row,
    /// not inside it (clipped there with the list, a knowledge day's title after the ‹ did not show).
    var title: String? = nil
    var subtitle: String? = nil
    @Environment(\.macSectionStack) private var inSectionStack
    @Environment(\.macTitleRowLeading) private var leading
    /// Already drawn by an outer one (a page with both `pageTitle` and `detailTitle`, or `sectionPushedPage` as well).
    @Environment(\.macTitleRowBack) private var drawn
    /// Pushed, not the stack's first page (which keeps the section's name in the row).
    @Environment(\.isPresented) private var pushed

    func body(content: Content) -> some View {
        if inSectionStack, pushed, !drawn {
            content
                .modifier(MacBelowTitleRow()) // as the section's first page: no strip under the title row
                .modifier(OwnTitle(title: title, subtitle: subtitle))
                .environment(\.macTitleRowBack, true)
                .modifier(MacDetailBack(shown: true, leading: leading))
                .preference(key: MacTitleRowTaken.self, value: true)
        } else {
            content.modifier(OwnTitle(title: title, subtitle: subtitle))
        }
    }

    private struct OwnTitle: ViewModifier {
        var title: String?
        var subtitle: String?

        func body(content: Content) -> some View {
            if let title { content.modifier(MacTitleRowTitle(title: title, subtitle: subtitle)) } else { content }
        }
    }
}

extension EnvironmentValues {
    /// The page is in a section's own navigation stack (Knowledge, Machines), set by the shell on the stack: its pushed pages
    /// draw their ‹ and title in the title bar's row (`sectionPushedPage`).
    @Entry var macSectionStack = false
    /// A ‹ is drawn at the title row's leading edge: a page's own title there (`MacTitleRowTitle`) starts after it.
    @Entry var macTitleRowBack = false
}

/// The title a page in a split's detail column gives for the column's title row (`detailTitle`).
struct MacDetailTitle: PreferenceKey {
    static let defaultValue: String? = nil
    static func reduce(value: inout String?, nextValue: () -> String?) { value = value ?? nextValue() }
}

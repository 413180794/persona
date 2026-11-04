import SwiftUI

extension View {
    /// A pushed page's title, and the line under it where it has one (知识库's day: the chat). The phone shows the
    /// title in its navigation bar as before (`navigationTitle`; its own principal item says the rest). The Mac's
    /// window has no title in its bar (#830: it ran over the sections column), so a pushed page draws its title in the
    /// title bar's row over its own leading edge instead, the shell's section name giving way to it (#855).
    @ViewBuilder func pageTitle(_ title: String, subtitle: String? = nil) -> some View {
        #if os(macOS)
        navigationTitle(title)
            .navigationSubtitle(subtitle ?? "") // still the window's, for the Window menu
            .modifier(MacTitleRowTitle(title: title, subtitle: subtitle))
        #else
        navigationTitle(title)
        #endif
    }
}

extension View {
    /// A page's title in a split's detail column (设置's pages, #1224 B2). The phone shows it in its navigation bar as
    /// before (`navigationTitle`); the Mac's window has no title in its bar (#830), so the page hands it to the column's
    /// title row (`MacDetailBack` draws it after the ‹, where there is one), without taking the shell's section name
    /// away as `pageTitle` does: the list beside still shows its section's name.
    @ViewBuilder func detailTitle(_ title: String) -> some View {
        #if os(macOS)
        navigationTitle(title).preference(key: MacDetailTitle.self, value: title)
        #else
        navigationTitle(title)
        #endif
    }
}

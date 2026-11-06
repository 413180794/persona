import SwiftUI

/// A page's list beside its content in a Mac window, the list folding away on request (Persona's Chat History, #856).
///
/// Not a `NavigationSplitView`: on the Mac the list sat in a three-column split's content column (the sidebar column
/// is a floating glass card on macOS 26, `SplitColumns`), and that split does not fold its content column. Set to
/// `.detailOnly`, it wrote `.all` straight back: the empty sidebar column came out as a grey strip and the list stayed.
/// Laid out as the inbox lays out its list (`MacInboxPage`): the list at its width (dragged between the split's bounds,
/// remembered), a divider up into the title bar's row, the page in the rest. The page keeps its identity while the
/// list comes and goes.
struct MacListBeside<ListContent: View, Page: View>: View {
    var listShown: Bool
    let list: ListContent
    let page: Page
    /// The list's width, remembered across launches.
    @StoredValue private var width: Double
    /// The width while the divider is being dragged.
    @State private var dragging: CGFloat?
    /// The whole width, list and page.
    @State private var total: CGFloat = 0

    /// `ideal`: the list's width until it is dragged (Knowledge's is narrower than Persona's history, #2318).
    init(listShown: Bool, widthKey: String, ideal: CGFloat = Theme.Size.sidebar,
         @ViewBuilder list: () -> ListContent, @ViewBuilder page: () -> Page) {
        self.listShown = listShown
        self.list = list()
        self.page = page()
        _width = StoredValue(wrappedValue: Double(ideal), widthKey)
    }

    static func clamped(_ w: CGFloat) -> CGFloat { min(max(w, Theme.Size.sidebarMin), Theme.Size.sidebarMax) }

    /// The list's width in a page `total` wide: the remembered one, narrowed (down to its least) so the page keeps
    /// `macDetailMin` (#1200: in the narrowest window the page beside a 400-point list was cut off). 0: not measured yet.
    static func fitted(_ w: CGFloat, in total: CGFloat) -> CGFloat {
        let wanted = clamped(w)
        guard total > 0 else { return wanted }
        return max(Theme.Size.sidebarMin, min(wanted, total - Theme.Size.macResizeHitWidth - Theme.Size.macDetailMin))
    }

    var body: some View {
        HStack(spacing: 0) {
            if listShown {
                list.frame(width: Self.fitted(dragging ?? CGFloat(width), in: total))
                MacPanelResizeHandle { delta in
                    dragging = Self.clamped((dragging ?? CGFloat(width)) + delta)
                } ended: {
                    if let dragging { width = Double(dragging) }
                    dragging = nil
                }
                // Up into the title bar's row, as the shell's divider: stopping under it left a step there (#830).
                .ignoresSafeArea(.container, edges: .top)
            }
            page.frame(maxWidth: .infinity, maxHeight: .infinity)
        }
        .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { total = $0 }
    }
}

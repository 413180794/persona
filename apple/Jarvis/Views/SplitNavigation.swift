import SwiftUI

/// A list and the pages it opens (Agents, 收件箱, 设置), laid out for the width there is (docs/ios.md「iPhone Duo」).
///
/// Compact width — every iPhone, and iPhone Duo closed — is one stack with the list at its root, exactly as before.
/// Regular width — iPhone Duo open — keeps the list as a sidebar column and opens its pages in the detail column,
/// the way Mail shows a list and a message side by side; the split view also keeps its columns clear of the fold
/// when the device is partly folded. Both share `path`, so folding or unfolding keeps the page that was open.
///
/// In the split, the list's own links (`NavigationLink(value:)`) replace what the detail column shows; code in the list
/// should do the same (`path = NavigationPath([value])`, which in the single stack is the same as appending).
struct SplitNavigation<Sidebar: View, Empty: View, Destinations: ViewModifier>: View {
    @Environment(\.horizontalSizeClass) private var widthClass
    /// Whether the list column shows (its button in the toolbar closes it).
    @State private var columns = NavigationSplitViewVisibility.all
    @Binding var path: NavigationPath
    /// The pages the list opens (`navigationDestination`s), registered in both layouts.
    var destinations: Destinations
    @ViewBuilder var sidebar: Sidebar
    /// The detail column before anything is open.
    @ViewBuilder var empty: Empty

    var body: some View {
        if widthClass == .regular {
            NavigationSplitView(columnVisibility: $columns) {
                // No stack of its own: a value link in the sidebar column replaces the detail column's stack. A sidebar
                // stack that refused its pushes (a path binding that always read empty) first slid the list away, then
                // back: the sidebar folded and unfolded on every tap.
                sidebar.modifier(destinations)
                    .modifier(SidebarWidth())
            } detail: {
                NavigationStack(path: $path) {
                    empty.modifier(destinations)
                }
            }
            .navigationSplitViewStyle(.balanced)
            .modifier(SidebarTopFill(shown: columns != .detailOnly))
            // On the whole split, not the detail stack: its pages are built from the destinations the sidebar
            // registered, in the sidebar's environment.
            .environment(\.splitDetail, SplitDetail(isFirstPage: path.count <= 1))
        } else {
            NavigationStack(path: $path) {
                sidebar.modifier(destinations)
            }
        }
    }
}

/// The list column's width. iPhone Duo: none of our own (the system sizes the columns, and partly folded it moves the
/// divide onto the fold). iPad: the system's sidebar is narrow for the agents' cards (they wrapped every title), so a
/// wider one.
struct SidebarWidth: ViewModifier {
    func body(content: Content) -> some View {
        // A Mac window: the same wider list column as on iPad.
        if UIDevice.current.userInterfaceIdiom == .pad || UIDevice.current.userInterfaceIdiom == .mac {
            content.navigationSplitViewColumnWidth(min: Theme.Size.sidebarMin, ideal: Theme.Size.sidebar, max: Theme.Size.sidebarMax)
                .background(GeometryReader { g in Color.clear.preference(key: SidebarEdge.self, value: g.frame(in: .global).maxX) })
        } else {
            content
        }
    }
}

/// Where the list column ends, for `SidebarTopFill`.
private struct SidebarEdge: PreferenceKey {
    static let defaultValue: CGFloat = 0
    static func reduce(value: inout CGFloat, nextValue: () -> CGFloat) { value = max(value, nextValue()) }
}

/// iPad: the list column starts below the status bar (its background cannot reach above it), so the strip over it was
/// the window's white and the list looked cut off at the top (2026-10-31). The split fills that strip with the
/// column's color, as wide as the column is.
struct SidebarTopFill: ViewModifier {
    /// The column is open (closed, its last frame stays reported: the fill must go by this).
    var shown: Bool
    @State private var edge: CGFloat = 0

    func body(content: Content) -> some View {
        if UIDevice.current.userInterfaceIdiom == .pad {
            content
                .onPreferenceChange(SidebarEdge.self) { edge = $0 }
                .background(alignment: .topLeading) {
                    (AppStyle.current.usesGlass ? Theme.systemSidebar : Theme.pageBackground)
                        .frame(width: shown ? max(edge, 0) : 0)
                        .animation(Theme.Motion.layout, value: shown)
                        .ignoresSafeArea()
                }
        } else {
            content
        }
    }
}

/// Set on the detail column of a `SplitNavigation` in regular width; nil in a single stack.
struct SplitDetail: Equatable {
    /// The page is the first one the list opened: there is nothing to go back to (the list is beside it).
    var isFirstPage: Bool
}

extension EnvironmentValues {
    @Entry var splitDetail: SplitDetail?
}

extension View {
    /// A page the list opens: in the split's detail column its first page has no back button.
    func splitDetailPage() -> some View { modifier(SplitDetailPage()) }
}

private struct SplitDetailPage: ViewModifier {
    @Environment(\.splitDetail) private var split

    func body(content: Content) -> some View {
        content.navigationBarBackButtonHidden(split?.isFirstPage == true)
    }
}

/// The detail column before anything is open: a quiet hint.
struct SplitPlaceholder: View {
    var symbol: String
    var title: String
    var message: String

    var body: some View {
        ContentUnavailableView {
            Label(title, systemImage: symbol)
        } description: {
            Text(message)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Theme.pageBackground)
        .toolbarVisibility(.hidden, for: .navigationBar)
    }
}

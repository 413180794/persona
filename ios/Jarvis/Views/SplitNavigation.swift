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
    @Binding var path: NavigationPath
    /// The pages the list opens (`navigationDestination`s), registered in both layouts.
    var destinations: Destinations
    @ViewBuilder var sidebar: Sidebar
    /// The detail column before anything is open.
    @ViewBuilder var empty: Empty

    var body: some View {
        if widthClass == .regular {
            NavigationSplitView {
                // No stack of its own: a value link in the sidebar column replaces the detail column's stack. A sidebar
                // stack that refused its pushes (a path binding that always read empty) first slid the list away, then
                // back: the sidebar folded and unfolded on every tap.
                sidebar.modifier(destinations)
                // No width of our own: the system sizes the columns, and partly folded it moves the divide onto the fold.
            } detail: {
                NavigationStack(path: $path) {
                    empty.modifier(destinations)
                }
            }
            .navigationSplitViewStyle(.balanced)
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

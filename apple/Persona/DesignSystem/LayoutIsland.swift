import SwiftUI

#if os(iOS)
import UIKit

/// A piece of a page laid out on its own: in a hosting controller of its own, so what changes inside it (a text field
/// gaining a character) does not lay out the page around it. In the page's own view graph every keystroke in the chat's
/// composer laid the whole window out again from the root, the chat list re-measuring its visible rows on the way
/// (about 10 ms a character on the simulator, docs/testing.md「Persona 页（长主对话）」). The page only hears from it
/// when its height changes (a new line, a chip above the field): the content measures itself and reports the height,
/// which the page keeps as the island's fixed height.
///
/// What the content draws outside its own frame (a menu hanging above it) shows but takes no touches: the hosting
/// view only hit-tests inside its bounds, and taps there land on the page below. Such content stays out of an island.
///
/// `Content` is Equatable: the page hands it in again on every update, and only a different value reaches the island.
/// The page's environment goes in whole (`\.self`), the way `DrawerSidebarHost` does it.
struct LayoutIsland<Content: View & Equatable>: View {
    let content: Content
    /// The content's own height, as it last reported it (nil until it has).
    @State private var height: CGFloat?

    var body: some View {
        IslandHost(content: content) { h in if h != height { height = h } }
            .frame(height: height)
            .accessibilityIdentifier("layout-island") // UI tests: what is inside does not show in their tree (#995)
    }
}

private struct IslandHost<Content: View & Equatable>: UIViewControllerRepresentable {
    let content: Content
    let measured: (CGFloat) -> Void

    func makeUIViewController(context: Context) -> UIHostingController<AnyView> {
        let host = UIHostingController(rootView: root(context))
        // The page keeps the keyboard and the screen edges off it already.
        host.safeAreaRegions = []
        host.sizingOptions = []
        host.view.backgroundColor = .clear
        return host
    }

    func updateUIViewController(_ host: UIHostingController<AnyView>, context: Context) {
        // The environment (color scheme, type size, style) can change without the content changing; the content
        // itself is compared (`EquatableView`) and left alone when it is the same.
        host.rootView = root(context)
    }

    /// Before the content has reported its height (the first layout): ask the hosting controller.
    func sizeThatFits(_ proposal: ProposedViewSize, uiViewController host: UIHostingController<AnyView>, context: Context) -> CGSize? {
        guard let width = proposal.width, width.isFinite else { return nil }
        if let h = proposal.height, h.isFinite { return CGSize(width: width, height: h) }
        let fit = host.sizeThatFits(in: CGSize(width: width, height: .greatestFiniteMagnitude))
        return CGSize(width: width, height: ceil(fit.height))
    }

    private func root(_ context: Context) -> AnyView {
        AnyView(
            EquatableView(content: content)
                .fixedSize(horizontal: false, vertical: true)
                .onGeometryChange(for: CGFloat.self, of: { ceil($0.size.height) }) { measured($0) }
                .frame(maxHeight: .infinity, alignment: .bottom) // a frame grown taller than the last report: from the bottom
                .environment(\.self, context.environment)
        )
    }
}

extension View where Self: Equatable {
    /// This view laid out apart from the page (`LayoutIsland`).
    @ViewBuilder func layoutIsland(enabled: Bool = true) -> some View {
        if enabled { LayoutIsland(content: self) } else { self.equatable() }
    }
}
#else
extension View where Self: Equatable {
    func layoutIsland(enabled: Bool = true) -> some View { self }
}
#endif

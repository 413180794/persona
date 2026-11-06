import SwiftUI
import Testing
import UIKit
@testable import Persona

/// The drawer's sidebar takes its look from the window it is drawn in (#4496). It is rendered by a hosting controller of
/// its own (`DrawerSidebarHost`) with a copy of the page's environment, and the copy's color scheme was only as new as
/// the last update SwiftUI sent the representable. iOS draws the app-switcher snapshots in the other look as the app goes
/// to the background, sending the sidebar that look; when the update back did not come, the sidebar's `Color.primary`
/// text stayed white on the light backdrop (the glass buttons and the badges, drawn from the window's traits, were fine).
@MainActor struct DrawerSidebarLookTests {
    @Observable final class Seen {
        var scheme: ColorScheme?
        /// What the page's environment says (stale on purpose: not the window's look).
        var pageScheme: ColorScheme = .dark
    }

    struct Probe: View {
        let seen: Seen
        @Environment(\.colorScheme) private var scheme
        var body: some View {
            Color.clear.onChange(of: scheme, initial: true) { seen.scheme = scheme }
        }
    }

    struct Screen: View {
        let seen: Seen
        let drawer: AssistantDrawer
        var body: some View {
            AssistantDrawerLayout(drawer: drawer, enabled: true, sidebar: { Probe(seen: seen) }, page: Color.clear)
                .environment(\.colorScheme, seen.pageScheme)
        }
    }

    private func settle() async {
        for _ in 0..<10 {
            try? await Task.sleep(for: .milliseconds(30))
        }
    }

    @Test func theSidebarFollowsItsWindowNotACopiedLook() async throws {
        let scene = try #require(UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }.first)
        let window = UIWindow(windowScene: scene)
        defer { window.isHidden = true }
        window.frame = CGRect(x: 0, y: 0, width: 390, height: 844)
        window.overrideUserInterfaceStyle = .light
        let seen = Seen()
        let drawer = AssistantDrawer()
        drawer.progress = 1 // out: shown
        window.rootViewController = UIHostingController(rootView: Screen(seen: seen, drawer: drawer))
        window.isHidden = false
        window.layoutIfNeeded()
        await settle()
        #expect(seen.scheme == .light, "a light window, the page's environment still dark: the sidebar drew \(String(describing: seen.scheme))")

        // The window changes look (the system's, or Settings › Appearance › Light & Dark): the sidebar follows without any page update.
        window.overrideUserInterfaceStyle = .dark
        window.layoutIfNeeded()
        await settle()
        #expect(seen.scheme == .dark, "the window went dark: the sidebar drew \(String(describing: seen.scheme))")

        // An update from the page in the other look (an app-switcher snapshot's) does not take the sidebar with it.
        seen.pageScheme = .light
        window.layoutIfNeeded()
        await settle()
        #expect(seen.scheme == .dark, "the page updated in the other look: the sidebar drew \(String(describing: seen.scheme))")
    }
}

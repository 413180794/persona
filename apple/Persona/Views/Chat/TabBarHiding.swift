import SwiftUI
#if canImport(UIKit)
import UIKit
#endif

#if os(iOS) || os(visionOS)
extension View {
    /// Hides the tab bar while this pushed page shows, the UIKit way: animated, with the push.
    ///
    /// `.toolbar(.hidden, for: .tabBar)` on a pushed page took the tab bar away on the push's first frame, before
    /// anything moved, and the page underneath lost its navigation bar and search field in the same frame: opening a
    /// chat from the Agents list flashed (2026-10-31; filmed with `simctl io recordVideo`, `OpenChatTabBarTests`).
    func hidesTabBarWhenShown(_ hides: Bool = true) -> some View {
        background { if hides { TabBarHider() } }
    }
}

private struct TabBarHider: UIViewControllerRepresentable {
    func makeUIViewController(context: Context) -> Probe { Probe() }
    func updateUIViewController(_ vc: Probe, context: Context) {}

    final class Probe: UIViewController {
        override func viewWillAppear(_ animated: Bool) {
            super.viewWillAppear(animated)
            tabBarController?.setTabBarHidden(true, animated: animated)
        }

        override func viewWillDisappear(_ animated: Bool) {
            super.viewWillDisappear(animated)
            // Covered by a full-screen page of its own (the terminal): still this page, the bar stays away.
            if view.window?.rootViewController?.presentedViewController != nil { return }
            tabBarController?.setTabBarHidden(false, animated: animated)
        }
    }
}
#endif

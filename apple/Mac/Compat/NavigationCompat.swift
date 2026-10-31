import SwiftUI

extension View {
    /// iOS hides the tab bar under a pushed chat (`Views/Chat/TabBarHiding.swift`). The Mac's sidebar is not a tab
    /// bar and stays where it is.
    func hidesTabBarWhenShown(_ hides: Bool = true) -> some View { self }
}

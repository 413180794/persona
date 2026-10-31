import SwiftUI

// Design tokens only the Mac shell uses (windows, sheets, columns). Same rule as `Theme`: pages read these, never literals.
extension Theme.Size {
    /// The Jarvis drawer's widest (narrow windows only; wide ones show it as a column).
    static let macDrawer: CGFloat = 380
    /// The smallest a sheet standing in for an iOS full-screen cover opens.
    static let macSheetMinWidth: CGFloat = 640
    static let macSheetMinHeight: CGFloat = 520
    /// The main window: its smallest, and its size the first time it opens.
    static let macWindowMinWidth: CGFloat = 820
    static let macWindowMinHeight: CGFloat = 560
    static let macWindowWidth: CGFloat = 1280
    static let macWindowHeight: CGFloat = 820
    /// The shell's sidebar column (Jarvis / Agents / 收件箱 / 设置).
    static let macSidebarMin: CGFloat = 180
    static let macSidebarIdeal: CGFloat = 210
    static let macSidebarMax: CGFloat = 280
    /// The 设置 window.
    static let macSettingsWidth: CGFloat = 620
    static let macSettingsHeight: CGFloat = 640
    /// The link panel at the detail column's trailing edge: its first width, its narrowest, and the least it leaves the page.
    static let macLinkPanelIdeal: CGFloat = 520
    static let macLinkPanelMin: CGFloat = 360
    static let macLinkPanelPageMin: CGFloat = 360
    /// How wide a draggable divider takes the mouse (the line itself is a hairline).
    static let macResizeHitWidth: CGFloat = 8
    /// The inbox's list column (until the window's three columns take it over) and its filters column.
    static let macInboxListMin: CGFloat = 300
    static let macInboxListIdeal: CGFloat = 380
    static let macInboxListMax: CGFloat = 520
    static let macInboxFiltersWidth: CGFloat = 190
}

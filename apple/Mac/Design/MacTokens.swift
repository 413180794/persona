import SwiftUI

// Design tokens only the Mac shell uses (windows, sheets, columns). Same rule as `Theme`: pages read these, never literals.
extension Theme.Size {
    /// The Persona drawer's widest (narrow windows only; wide ones show it as a column).
    static let macDrawer: CGFloat = 380
    /// The smallest a sheet standing in for an iOS full-screen cover opens.
    static let macSheetMinWidth: CGFloat = 640
    static let macSheetMinHeight: CGFloat = 520
    /// A sheet's title bar and bottom toolbar together, on top of the height its navigation stack's content asks for.
    static let macSheetBars: CGFloat = 116
    /// The main window: its smallest, and its size the first time it opens.
    static let macWindowMinWidth: CGFloat = 820
    static let macWindowMinHeight: CGFloat = 560
    static let macWindowWidth: CGFloat = 1280
    static let macWindowHeight: CGFloat = 820
    /// A window's title bar with a unified toolbar: where the shell's sections start, whatever the page's toolbar holds.
    static let macToolbarHeight: CGFloat = 52
    /// Where a page's title starts in the title bar's row when the sections column is hidden: after the traffic lights
    /// and the toolbar's buttons beside them (#830).
    static let macTitleAfterControls: CGFloat = 200
    /// The shell's sidebar column (Persona / Agents / 收件箱 / 设置).
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
    static let macInboxListIdeal: CGFloat = 340 // 380 left no room for the item's actions column at the default size (#831)
    static let macInboxListMax: CGFloat = 520
    static let macInboxFiltersWidth: CGFloat = 190
    /// An opened inbox item's actions column at the detail's trailing edge (#831), and the least it leaves the item:
    /// narrower than both, the actions sit over the item as a strip.
    static let macInboxActionsWidth: CGFloat = 180
    static let macInboxActionsPageMin: CGFloat = 340
    /// ⌘K 快速打开: the panel's width, the most its list grows to, and how far below the window's top it sits.
    static let macQuickOpenWidth: CGFloat = 600
    static let macQuickOpenListMax: CGFloat = 420
    static let macQuickOpenTop: CGFloat = 96
    /// The chat's inspector (会话信息, ⌥⌘I).
    static let macInspectorMin: CGFloat = 260
    static let macInspector: CGFloat = 300
    static let macInspectorMax: CGFloat = 420
    /// A split's detail column narrower than this is logged as squeezed (no page fits in it).
    static let macSplitSquished: CGFloat = 200
}

extension Theme {
    /// A glyph on a tinted fill (the composer's send / 中断 circles), as the system's prominent buttons draw it.
    static let macOnTint = Color.white
    /// The words' color, fixed (not `.primary`, which a selected list row turns white).
    static let macLabel = Color(nsColor: .labelColor)
}

extension Theme.Icon {
    /// Shows or hides the window's sections column (the toolbar button beside the traffic lights, ⌃⌘S).
    static let sidebar = "sidebar.left"
    /// A click with the mouse or trackpad (the Mac's version of a tap, 终端设置 › 操作).
    static let click = "cursorarrow.click"
    /// The right-click menu.
    static let contextMenu = "contextualmenu.and.cursorarrow"
    /// A trackpad swipe sideways.
    static let swipe = "arrow.left.arrow.right"
    /// This window takes the terminal over (iOS: `handoff`, the phone does).
    static let takeOverWindow = "macwindow.and.cursorarrow"
    /// Moving an item one place up / down in a list without an edit mode (终端设置 › 快捷命令).
    static let moveUp = "arrow.up"
    static let moveDown = "arrow.down"
}

extension Theme.Fill {
    /// A row under the pointer in the shell's sidebar: half the selected row's well.
    static let macHover = Theme.Fill.well.opacity(0.5)
    /// The faint dim behind ⌘K (a click on it closes the panel).
    static let macScrim = Color.black.opacity(0.12)
    /// Floating panels over the page (⌘K): the window background, opaque like Xcode's Open Quickly.
    static let macPanel = Color(nsColor: .windowBackgroundColor)
    /// Find in the code viewer (⌘F): every match, and the current one, as AppKit's own find highlights them.
    static let macFindMatch = Color(nsColor: .findHighlightColor).opacity(0.35)
    static let macFindCurrent = Color(nsColor: .findHighlightColor)
}

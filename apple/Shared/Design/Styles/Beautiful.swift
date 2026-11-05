import SwiftUI

nonisolated extension ComponentSpec {
    /// BeautifulUI: 液态玻璃's surfaces with BeautifulUI's components (cards, pills, empty states, the thinking trace, tool
    /// chips in work cards, its prompt bar). Only this style uses them (CLAUDE.md: a new look is a new style). Its cards
    /// are white in light: BeautifulUI's card is a white surface with a ring, and on the grouped pages' gray
    /// (`systemGroupedBackground`) the glass style's light gray card would show only its ring.
    static let beautiful: ComponentSpec = {
        var s = ComponentSpec.glass
        s.title = "BeautifulUI"
        s.subtitle = String(localized: "BeautifulUI 的卡片、工具卡片和输入框")
        s.canvas = .neutral(page: Theme.systemPage, card: Theme.groupedCard)
        s.components = .beautiful
        // BeautifulUI's work card (its card color and ring), not 液态玻璃's timeline.
        s.workList = .card
        return s
    }()
}

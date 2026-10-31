import SwiftUI

nonisolated extension StyleSpec {
    /// BeautifulUI: 液态玻璃's colors and surfaces with BeautifulUI's components (the thinking trace, tool chips in
    /// work cards, its prompt bar). Only this style uses them (CLAUDE.md: a new look is a new style).
    static let beautiful: StyleSpec = {
        var s = StyleSpec.glass
        s.title = "BeautifulUI"
        s.subtitle = "新的思考轨迹、工具卡片和输入框"
        s.components = .beautiful
        return s
    }()
}

import SwiftUI
import Testing
@testable import Jarvis

@MainActor
struct ShellNavigationClearanceTests {
    /// 安全区随顶栏的字号增长；放大后的按钮、角标和间距都必须落在系统搜索框之前。
    @Test func clearanceIncludesScaledControlsAndInboxMark() {
        for size in DynamicTypeSize.allCases {
            let scale = Theme.sizeScale(TextSize.large.applied(to: size))
            let occupied = Theme.Size.tapTarget * scale + Theme.Size.badge * scale / 2 + Theme.Space.xxs * 2
            let reserved = Theme.Size.tapTarget + ShellNavigationClearance.height(at: size)
            #expect(reserved >= occupied + Theme.Space.s)
        }
    }

    @Test func clearanceNeverShrinksAsTextGrows() {
        let heights = DynamicTypeSize.allCases.map { ShellNavigationClearance.height(at: $0) }
        for (smaller, larger) in zip(heights, heights.dropFirst()) {
            #expect(larger >= smaller)
        }
    }
}

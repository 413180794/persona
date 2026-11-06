import SwiftUI
import Testing
@testable import Persona

/// A sidebar heading draws its ⋯ only when the menu has items (#778: Pin with only the main conversation pinned had
/// a ⋯ that opened nothing).
@MainActor
struct HeadingMenuTests {
    @Test func noItemsIsEmpty() {
        #expect(HeadingMenu.isEmpty(items {}))
    }

    /// Pin's menu: Unpin All only while something besides the main conversation is pinned.
    @Test func anItemBehindAFalseConditionIsEmpty() {
        let othersPinned = false
        #expect(HeadingMenu.isEmpty(items { if othersPinned { Button("全部取消置顶") {} } }))
    }

    @Test func itemsAreNotEmpty() {
        let othersPinned = true
        #expect(!HeadingMenu.isEmpty(items { if othersPinned { Button("全部取消置顶") {} } }))
        #expect(!HeadingMenu.isEmpty(items { Button("新对话") {}; Button("已归档") {} }))
    }

    private func items<V: View>(@ViewBuilder _ build: () -> V) -> V { build() }
}

/// A heading whose menu can empty keeps the ⋯'s height; one that never has a menu keeps its own.
struct HeadingMenuHeightTests {
    @Test func aMenuThatCanEmptyKeepsTheHeight() {
        #expect(HeadingMenu.minHeight(Optional<Button<Text>>.self) == Theme.Size.wellXS)
    }

    @Test func noMenuKeepsItsOwnHeight() {
        #expect(HeadingMenu.minHeight(EmptyView.self) == nil)
    }
}

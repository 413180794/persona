import Foundation
import Testing
@testable import Persona

/// The terminal's help on the Mac is about a click, the keyboard, the wheel and the trackpad: none of the iPhone's
/// touches (long press, swipes on the screen, the on-screen keyboard) and no Edit button the Mac does not show.
@MainActor struct MacTerminalWordingTests {
    private static let touchOnly = ["手机", "长按", "上滑", "左右滑", "弹出键盘", "双击终端", "在终端上双指捏合", "左上角「编辑」", "听写"]

    @Test func helpTalksAboutTheMac() {
        #expect(!TerminalHelpText.gestures.isEmpty)
        for text in TerminalHelpText.all {
            for word in Self.touchOnly {
                #expect(!text.contains(word), "「\(word)」 in 「\(text)」")
            }
        }
    }

    @Test func shortcutsMoveUpAndDown() {
        let list = ["a", "b", "c"]
        #expect(MacShortcutOrder.move(list, at: 0, by: 1) == ["b", "a", "c"])
        #expect(MacShortcutOrder.move(list, at: 2, by: -1) == ["a", "c", "b"])
        #expect(MacShortcutOrder.move(list, at: 0, by: -1) == list)
        #expect(MacShortcutOrder.move(list, at: 2, by: 1) == list)
        #expect(MacShortcutOrder.move(list, at: 5, by: 1) == list)
    }
}

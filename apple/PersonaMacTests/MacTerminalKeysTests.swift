import AppKit
import Testing
@testable import Persona

/// Terminal Settings › Use Option as Meta Key: a key press as herdr's key name (`MacTerminalKeys.name`). Off, Option types the
/// layout's characters (they go to the input method as text); on, Option+key is alt+key, which herdr sends as ESC+key.
struct MacTerminalKeysTests {
    // US layout key codes.
    private let a: UInt16 = 0, b: UInt16 = 11, f: UInt16 = 3, two: UInt16 = 19, period: UInt16 = 47, space: UInt16 = 49
    private let left: UInt16 = 123, backspace: UInt16 = 51

    private func name(_ code: UInt16, _ chars: String?, _ flags: NSEvent.ModifierFlags, meta: Bool) -> String? {
        MacTerminalKeys.name(keyCode: code, characters: chars, flags: flags, optionAsMeta: meta)
    }

    @Test func metaOffLeavesOptionCharactersAsText() {
        // Option+2 types ™, Option+e starts a dead key: both are the input method's.
        #expect(name(two, "2", .option, meta: false) == nil)
        #expect(name(b, "b", .option, meta: false) == nil)
        #expect(name(a, "A", [.option, .shift], meta: false) == nil)
        // Plain typing and input methods are untouched either way.
        #expect(name(a, "a", [], meta: false) == nil)
        #expect(name(a, "a", [], meta: true) == nil)
        #expect(name(a, "A", .shift, meta: true) == nil)
    }

    @Test func metaOnSendsAltKeys() {
        #expect(name(b, "b", .option, meta: true) == "alt+b")
        #expect(name(f, "f", .option, meta: true) == "alt+f")
        #expect(name(two, "2", .option, meta: true) == "alt+2")
        #expect(name(period, ".", .option, meta: true) == "alt+.")
        #expect(name(space, " ", .option, meta: true) == "alt+space")
        // Shift on a letter is a modifier (herdr's order: ctrl+alt+shift); on a symbol it is already in the character.
        #expect(name(a, "A", [.option, .shift], meta: true) == "alt+shift+a")
        #expect(name(period, ">", [.option, .shift], meta: true) == "alt+>")
        #expect(name(b, "b", [.control, .option], meta: true) == "ctrl+alt+b")
    }

    @Test func metaOnIgnoresWhatIsNotAKeyName() {
        // A non-Latin layout's character has no herdr key name: it stays text.
        #expect(name(b, "и", .option, meta: true) == nil)
    }

    @Test func specialKeysAndCtrlAsBefore() {
        for meta in [false, true] {
            #expect(name(left, nil, .option, meta: meta) == "alt+left")
            #expect(name(backspace, nil, .option, meta: meta) == "alt+backspace")
            #expect(name(left, nil, [.shift], meta: meta) == "shift+left")
            #expect(name(a, "a", .control, meta: meta) == "ctrl+a")
            #expect(name(space, " ", .control, meta: meta) == "ctrl+space")
        }
        // Without Meta, ctrl+option+letter stays ctrl+letter (as it was).
        #expect(name(b, "b", [.control, .option], meta: false) == "ctrl+b")
    }
}

import AppKit
import Testing
@testable import Persona

/// ⌘V in a composer (#3472): the shortcut is recognised even with Caps Lock on, when the event's
/// charactersIgnoringModifiers comes through as an uppercase "V". Shift and other extra modifiers still disqualify it.
@MainActor
struct MacComposerPasteTests {
    /// Builds a key-down event the way AppKit delivers one; keyCode 9 is the V key.
    private func keyEvent(_ modifiers: NSEvent.ModifierFlags, characters: String) -> NSEvent {
        NSEvent.keyEvent(with: .keyDown, location: .zero, modifierFlags: modifiers, timestamp: 0,
                         windowNumber: 0, context: nil, characters: characters,
                         charactersIgnoringModifiers: characters, isARepeat: false, keyCode: 9)!
    }

    @Test func commandVIsPaste() {
        #expect(MacComposerPaste.isPasteShortcut(keyEvent(.command, characters: "v")))
    }

    /// Caps Lock keeps the uppercase "V" in charactersIgnoringModifiers; the paste must still be taken here.
    @Test func commandUppercaseVWithCapsLockIsPaste() {
        #expect(MacComposerPaste.isPasteShortcut(keyEvent([.command, .capsLock], characters: "V")))
    }

    /// ⌘⇧V has another meaning and stays with the field.
    @Test func commandShiftVIsNotPaste() {
        #expect(!MacComposerPaste.isPasteShortcut(keyEvent([.command, .shift], characters: "V")))
    }

    /// ⌥⌘V is not the plain paste shortcut.
    @Test func optionCommandVIsNotPaste() {
        #expect(!MacComposerPaste.isPasteShortcut(keyEvent([.command, .option], characters: "v")))
    }

    /// V without ⌘ is just typing.
    @Test func plainVIsNotPaste() {
        #expect(!MacComposerPaste.isPasteShortcut(keyEvent([], characters: "v")))
    }
}
